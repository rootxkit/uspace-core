// Package auth is the one verifier of the ecosystem (spec 00 section 6.2,
// 06 section 3): RS256 JWTs with iss, aud, sub, scope, exp, jti and kid,
// keys from the JWKS of an allow-listed issuer cached 24 h and selected by
// kid, exp with at most 30 s skew, aud equal to the own system id, endpoint
// scope checks, and issuance helpers for the token services. It also
// authenticates Remote ID receivers: HMAC-SHA256 over the exact report
// bytes with a per-receiver key, a +-30 s window on sent_at_ms and a nonce
// remembered for twice the window (LESSONS R-06).
//
// Vectors: vectors/testdata/rid_receiver_auth.json (14) and, at G-M3, the
// jwt_verify file this package adds to the knowledge set. Owned by WP-11.
package auth
