// Package auth holds the ecosystem's one token verifier and the Remote ID
// receiver authenticator. It never logs or prints: every refusal is
// returned as a typed error and counted (E-09).
//
// # JWT (spec 00 section 6.2, 06 section 3)
//
// Verifier accepts a compact JWS whose protected header has alg exactly
// RS256 and a kid; none, HS*, ES*, PS*, RS384/RS512 and any other value
// are refused before a key is looked up, so the HS256-with-the-public-key
// confusion cannot arise. A crit header is refused (no extension is
// understood). The iss claim must be on the configured allow-list; an
// unknown issuer is refused before any network call. The key is the kid
// of that issuer's JWKS, which must be an RSA key of at least 2048 bits
// whose alg (when present) is RS256 and use (when present) is sig; the
// signature is checked by lestrrat-go/jwx/v3 with the algorithm pinned to
// RS256. Then exp is required and checked with MaxSkew (default 30 s),
// nbf and iat when present are checked with the same skew, aud (a string
// or an array) must contain the configured audience, and sub and jti are
// required. The scope claim is a space-separated string (06 section 3);
// an array-valued scope or scp is not read. A token without scope is
// valid and grants nothing: endpoints call RequireScope.
//
// JWKS of URL-configured issuers are fetched at NewVerifier (a failure
// stops start-up) and cached for JWKSCacheTTL (default 24 h). A request
// whose kid is cached is always served from the cache: from
// JWKSCacheTTL - JWKSRefreshAhead (default the last tenth of the TTL) on,
// and past the TTL during an issuer outage, it starts a fetch in the
// background and does not wait for it. A request with an unknown kid
// starts a fetch (or joins the running one) and waits for it or for its
// own context. Fetches are single-flight and rate-limited to one per
// MinRefreshInterval (default one minute) per issuer, and each is bounded
// by JWKSFetchTimeout (default 2 s). A fetch runs under
// context.WithoutCancel: a caller that cancels is released but cannot
// fail the fetch, and a caller whose context is already done starts none,
// so no unauthenticated request can spend the rate limit and hold a
// rotated key out. A failed fetch is counted and keeps the cached set, so
// tokens stay verifiable during an outage (06 section 2 T5). A JWKS URL
// must be HTTPS (plain HTTP to localhost only); responses are bounded by
// MaxJWKSBytes and tokens by MaxTokenBytes.
//
// Every refusal is a *TokenError whose text names the claim (alg, kid,
// iss, signature, exp, nbf, iat, aud, sub, jti, scope, crit, token) and
// whose Counter is one of rejected_issuer, rejected_algorithm,
// rejected_kid, rejected_signature, rejected_expired,
// rejected_not_yet_valid, rejected_audience, rejected_claims and
// rejected_malformed. Error texts never contain the token.
//
// Issuer signs tokens for the token services with the same claims.
//
// From v1.1.0, Config.Audiences lists further ids (hosts, M18) beside
// Audience: aud must contain one of them, and Claims.Audience is the one
// matched. Claims.Roles (a JSON array of strings) and Claims.Realm (a
// string) are read when present (M20 session tokens) and never required.
// With Config.StrictSessionClaims a roles or realm of another type is
// rejected_claims; without it (the default, v1.0.0's judgement) it is
// ignored. Every uspace system sets StrictSessionClaims; it is opt-in
// only so that v1.1.0 is additive.
//
// # Three signed forms, and when a system uses which
//
//   - Bearer JWT (Verifier, Issuer): who is calling. Every API request
//     carries one in Authorization; it has exp and is verified per
//     request.
//   - Detached JWS (SignDetached, DetachedVerifier): who published this
//     body. A publication (an ANSP or authority update posted to the
//     CISP, M26) carries X-JWS-Signature: <BASE64URL(protected)>..<BASE64URL(sig)>
//     over the exact request body bytes (RFC 7515 Appendix F, RFC 7797):
//     the protected header is alg RS256, kid, iat, b64 false and crit
//     ["b64"], and the signing input is ASCII(BASE64URL(protected)) || '.'
//     || payload, the payload unencoded. The verifier refuses a header
//     that does not declare b64 false in crit, so a signature over the
//     base64url payload never verifies against the raw bytes, and any
//     crit entry other than b64. The caller names the publisher from the
//     bearer token it verified (sub = client id), never from the header,
//     so a key of publisher B cannot sign a publication attributed to A
//     and kid collisions across publishers are harmless. iat may be at
//     most MaxAge old (default five minutes) and MaxSkew ahead.
//     ParseDetachedHeader reads the header without verifying it, for
//     logging.
//   - Compact JWS (SignCompact, CompactVerifier): a delivery to a
//     receiver that has no bearer to check (the CISP's webhook and the
//     ANSP's direct delivery, M19), sent as application/jose. It is a JWT
//     whose payload is {"iss","aud","sub","iat","jti","body"}, body being
//     the message as the producer wrote it, returned untouched for the
//     receiver to validate against its schema. aud must contain one of
//     the receiver's hosts. There is no exp: a delivery is single use and
//     jti is its id, so CompactVerifier keeps no nonce memory; the
//     receiver's idempotent delivery store is the replay guard.
//
// The refusals of the two JWS verifiers add the counters rejected_b64,
// rejected_crit, rejected_publisher, rejected_iat and rejected_too_large
// to those of Verifier. KeyRing holds a publisher's keys: one active key
// that signs and retired keys that stay in its JWKS so that verifiers
// holding a cached set keep verifying through a rotation (cisp Q16); at
// most MaxRingKeys, a Rotate past it counted as key_ring_full. It holds
// keys the caller loaded and never reads a file. Its Issuer shares the
// ring's JWKS, so a token service and its publication signer publish one
// set.
//
// # Receiver authentication (LESSONS R-06)
//
// A receiver sends <report bytes>\nsig=<hex HMAC-SHA256 of the report
// bytes under its key>. ReceiverVerifier refuses an unsigned datagram, a
// report that is not a JSON object, an unknown receiver, a bad signature
// (compared in constant time), a sent_at_ms that is not an integer or is
// more than the window (30 s at the ingest) from now, an empty nonce, and
// a nonce already seen from that receiver within twice the window. The
// nonce memory is bounded per receiver (oldest evicted, counted as
// nonces_evicted). An ingest without receiver keys cannot authenticate
// anyone and must bind to loopback only.
//
// The report is parsed before the HMAC is checked, because its
// receiver_id selects the key; a datagram longer than
// WithMaxDatagramBytes (default 4096) is therefore refused before it is
// parsed, and the UDP reader should not hand over more than that either.
// An unknown receiver is refused before any HMAC is computed, so the
// response time tells a sender whether a receiver id is configured. That
// timing oracle is accepted: receiver ids are not secrets, and knowing
// one does not help forge its HMAC.
//
// Vectors: vectors/testdata/rid_receiver_auth.json (14 cases) and
// vectors/testdata/jwt_verify.json (written here at G-M3, proposed to
// the lab). Owned by WP-11. The JWS helpers (WP-14) have no vector file
// in this module: their cases are generated by auth/internal/genvectors
// and proposed to the lab, and the unit tests pin them until a major
// syncs them (docs/RELEASING.md section 3.2).
package auth
