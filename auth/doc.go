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
// Vectors: vectors/testdata/rid_receiver_auth.json (14 cases) and
// vectors/testdata/jwt_verify.json (written here at G-M3, proposed to
// the lab). Owned by WP-11.
package auth
