package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/rootxkit/uspace-core/core"
)

// CompactClaims are the claims of a compact delivery JWS (M19): iss, aud
// (one host when signing; the matched one of CompactConfig.Audiences
// when verified), sub, jti (the delivery id) and iat.
type CompactClaims struct {
	Issuer   string
	Audience string
	Subject  string
	JTI      string
	IssuedAt time.Time
}

// SignCompact signs a delivery with the ring's active key: a compact JWS
// (a JWT, content type application/jose) with header alg RS256, kid and
// typ JWT, whose payload is {"iss","aud","sub","iat","jti","body"} with
// body the given JSON object byte for byte. iat is now in whole seconds;
// cl.IssuedAt is not read. Issuer, Audience, Subject and JTI are
// required, and body must be a JSON object.
func (r *KeyRing) SignCompact(cl CompactClaims, body json.RawMessage, now time.Time) (string, error) {
	k := r.signer()
	return signCompact(k.kid, k.priv, cl, body, now)
}

// SignCompact is KeyRing.SignCompact with one key, checked as NewIssuer
// checks its key on every call (tests and tools).
func SignCompact(k SigningKey, cl CompactClaims, body json.RawMessage, now time.Time) (string, error) {
	priv, _, err := importSigningKey(k.KID, k.Key)
	if err != nil {
		return "", err
	}
	return signCompact(k.KID, priv, cl, body, now)
}

func signCompact(kid string, priv jwk.Key, cl CompactClaims, body json.RawMessage, now time.Time) (string, error) {
	for _, f := range []struct{ name, value string }{
		{"iss", cl.Issuer}, {"aud", cl.Audience}, {"sub", cl.Subject}, {"jti", cl.JTI},
	} {
		if f.value == "" {
			return "", core.Fieldf(f.name, "empty")
		}
	}
	if !isJSONObject(body) {
		return "", core.Fieldf("body", "not a JSON object")
	}
	// The payload is written by hand so that body keeps its bytes:
	// encoding/json would compact a json.RawMessage.
	var p bytes.Buffer
	for i, m := range []struct{ name, value string }{
		{"iss", cl.Issuer}, {"aud", cl.Audience}, {"sub", cl.Subject},
	} {
		if i == 0 {
			p.WriteByte('{')
		} else {
			p.WriteByte(',')
		}
		writeJSONMember(&p, m.name, m.value)
	}
	p.WriteString(`,"iat":`)
	p.WriteString(strconv.FormatInt(now.Unix(), 10))
	p.WriteByte(',')
	writeJSONMember(&p, "jti", cl.JTI)
	p.WriteString(`,"body":`)
	p.Write(body)
	p.WriteByte('}')

	hdr := jws.NewHeaders()
	if err := hdr.Set(jws.KeyIDKey, kid); err != nil {
		return "", core.Fieldf("header", "%v", err)
	}
	if err := hdr.Set(jws.TypeKey, "JWT"); err != nil {
		return "", core.Fieldf("header", "%v", err)
	}
	signed, err := jws.Sign(p.Bytes(), jws.WithKey(jwa.RS256(), priv, jws.WithProtectedHeaders(hdr)))
	if err != nil {
		return "", core.Fieldf("signature", "%v", err)
	}
	return string(signed), nil
}

func writeJSONMember(b *bytes.Buffer, name, value string) {
	// Marshalling a string cannot fail.
	n, _ := json.Marshal(name)
	v, _ := json.Marshal(value)
	b.Write(n)
	b.WriteByte(':')
	b.Write(v)
}

// isJSONObject reports whether raw is one valid JSON object.
func isJSONObject(raw []byte) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

// CompactConfig configures a CompactVerifier. Zero durations and sizes
// take their defaults.
type CompactConfig struct {
	// Issuers is the allow-list, keyed by the exact iss value (the CISP
	// and the ANSP, M5).
	Issuers map[string]IssuerConfig
	// Audiences are this receiver's hosts; aud must contain one (M18).
	Audiences []string
	// MaxAge is how old iat may be (default DefaultDetachedMaxAge).
	MaxAge time.Duration
	// MaxSkew is how far ahead of now iat may be (default DefaultMaxSkew).
	MaxSkew time.Duration
	// MaxTokenBytes bounds the token (default DefaultMaxTokenBytes).
	MaxTokenBytes int
	// The JWKS cache settings, as in Config.
	JWKSCacheTTL       time.Duration
	MinRefreshInterval time.Duration
	MaxJWKSBytes       int64
	JWKSFetchTimeout   time.Duration
	JWKSRefreshAhead   time.Duration
	HTTPClient         *http.Client
	Now                func() time.Time
}

// CompactVerifier verifies compact delivery JWS. It keeps no nonce
// memory: a delivery is single use, jti is its id, and the receiver's
// idempotent delivery store is the replay guard. It is safe for
// concurrent use and never logs.
type CompactVerifier struct {
	kv        *Verifier
	audiences []string
	maxAge    time.Duration
}

// NewCompactVerifier validates c, applies the defaults and fetches the
// JWKS of every URL-configured issuer. It refuses an empty allow-list,
// no audience or an empty one, and a negative duration or size.
func NewCompactVerifier(ctx context.Context, c CompactConfig) (*CompactVerifier, error) {
	if len(c.Issuers) == 0 {
		return nil, core.Fieldf("issuers", "no issuer is allowed")
	}
	if len(c.Audiences) == 0 || slices.Contains(c.Audiences, "") {
		return nil, core.Fieldf("audiences", "none, or an empty one")
	}
	if c.MaxAge < 0 {
		return nil, core.Fieldf("config", "a duration or size is negative")
	}
	kv, err := newVerifier(ctx, Config{
		Issuers: c.Issuers, MaxSkew: c.MaxSkew, MaxTokenBytes: c.MaxTokenBytes,
		JWKSCacheTTL: c.JWKSCacheTTL, MinRefreshInterval: c.MinRefreshInterval, MaxJWKSBytes: c.MaxJWKSBytes,
		JWKSFetchTimeout: c.JWKSFetchTimeout, JWKSRefreshAhead: c.JWKSRefreshAhead,
		HTTPClient: c.HTTPClient, Now: c.Now,
	}, "issuers", "issuer")
	if err != nil {
		return nil, err
	}
	v := &CompactVerifier{kv: kv, audiences: slices.Clone(c.Audiences), maxAge: c.MaxAge}
	if v.maxAge == 0 {
		v.maxAge = DefaultDetachedMaxAge
	}
	return v, nil
}

// Counters returns the verifier's counters: accepted, rejected_too_large,
// rejected_malformed, rejected_algorithm, rejected_b64, rejected_issuer,
// rejected_kid, rejected_signature, rejected_audience, rejected_iat,
// rejected_claims, jwks_refresh, jwks_refresh_failed,
// jwks_refresh_rate_limited.
func (v *CompactVerifier) Counters() *core.Counters { return &v.kv.counters }

// Verify verifies a compact delivery JWS and returns its claims and its
// body, the bytes the producer wrote. Every refusal is a *TokenError
// naming the claim and counted by reason, and returns no claims and no
// body; an unknown issuer is refused before any network call.
func (v *CompactVerifier) Verify(ctx context.Context, token string) (CompactClaims, json.RawMessage, error) {
	c, body, err := v.verify(ctx, token)
	if err != nil {
		var te *TokenError
		if errors.As(err, &te) {
			v.kv.counters.Inc(te.Counter)
		}
		return CompactClaims{}, nil, err
	}
	v.kv.counters.Inc(CounterAccepted)
	return c, body, nil
}

func (v *CompactVerifier) verify(ctx context.Context, token string) (CompactClaims, json.RawMessage, error) {
	if len(token) > v.kv.cfg.MaxTokenBytes {
		return CompactClaims{}, nil, refuseToken(CounterRejectedTooLarge, "token", "longer than %d bytes", v.kv.cfg.MaxTokenBytes)
	}
	if strings.Count(token, ".") != 2 {
		return CompactClaims{}, nil, refuseToken(CounterRejectedMalformed, "token", "not a compact JWS of three parts")
	}
	h64, rest, _ := strings.Cut(token, ".")
	p64, _, _ := strings.Cut(rest, ".")
	hraw, err := base64.RawURLEncoding.Strict().DecodeString(h64)
	if err != nil {
		return CompactClaims{}, nil, refuseToken(CounterRejectedMalformed, "token", "the header is not base64url")
	}
	hm, ok := decodeObject(hraw)
	if !ok {
		return CompactClaims{}, nil, refuseToken(CounterRejectedMalformed, "token", "the header is not a JSON object with distinct members")
	}
	hdr := claimSet(hm)
	alg, ok := hdr.str("alg")
	if !ok {
		return CompactClaims{}, nil, refuseToken(CounterRejectedAlgorithm, "alg", "missing")
	}
	if alg != algRS256 {
		return CompactClaims{}, nil, refuseToken(CounterRejectedAlgorithm, "alg", "%s is not RS256", quoteShort(alg))
	}
	if _, has := hdr["crit"]; has {
		return CompactClaims{}, nil, refuseToken(CounterRejectedCrit, "crit", "no critical header extension is understood")
	}
	if _, has := hdr[critB64]; has {
		// The payload of a delivery is base64url-encoded; b64 (false or
		// not) is the detached form's member only.
		return CompactClaims{}, nil, refuseToken(CounterRejectedB64, "b64", "not allowed in a compact delivery")
	}
	kid, ok := hdr.str("kid")
	if !ok || kid == "" {
		return CompactClaims{}, nil, refuseToken(CounterRejectedKID, "kid", "missing")
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(p64)
	if err != nil {
		return CompactClaims{}, nil, refuseToken(CounterRejectedMalformed, "token", "the payload is not base64url")
	}
	pm, ok := decodeObject(payload)
	if !ok {
		return CompactClaims{}, nil, refuseToken(CounterRejectedMalformed, "token", "the payload is not a JSON object with distinct members")
	}
	cl := claimSet(pm)
	iss, ok := cl.str("iss")
	if !ok || iss == "" {
		return CompactClaims{}, nil, refuseToken(CounterRejectedIssuer, "iss", "missing")
	}
	ik, ok := v.kv.issuers[iss]
	if !ok {
		return CompactClaims{}, nil, refuseToken(CounterRejectedIssuer, "iss", "%s is not an allowed issuer", quoteShort(iss))
	}
	key, err := v.kv.key(ctx, ik, kid)
	if err != nil {
		return CompactClaims{}, nil, err
	}
	verified, err := jws.Verify([]byte(token), jws.WithKey(jwa.RS256(), key.pub))
	if err != nil || !bytes.Equal(verified, payload) {
		return CompactClaims{}, nil, refuseToken(CounterRejectedSignature, "signature", "does not verify with kid %s", quoteShort(kid))
	}
	return v.judgeClaims(cl, iss)
}

func (v *CompactVerifier) judgeClaims(cl claimSet, iss string) (CompactClaims, json.RawMessage, error) {
	aud, err := cl.audience()
	if err != nil {
		return CompactClaims{}, nil, err
	}
	matched, ok := firstContained(v.audiences, aud)
	if !ok {
		return CompactClaims{}, nil, refuseToken(CounterRejectedAudience, "aud", "does not contain any of this receiver's %d audiences", len(v.audiences))
	}
	iat, err := judgeIAT(cl, v.kv.cfg.Now(), v.maxAge, v.kv.cfg.MaxSkew)
	if err != nil {
		return CompactClaims{}, nil, err
	}
	sub, ok := cl.str("sub")
	if !ok || sub == "" {
		return CompactClaims{}, nil, refuseToken(CounterRejectedClaims, "sub", "missing")
	}
	jti, ok := cl.str("jti")
	if !ok || jti == "" {
		return CompactClaims{}, nil, refuseToken(CounterRejectedClaims, "jti", "missing")
	}
	body, has := cl["body"]
	if !has || !isJSONObject(body) {
		return CompactClaims{}, nil, refuseToken(CounterRejectedClaims, "body", "missing or not a JSON object")
	}
	return CompactClaims{Issuer: iss, Audience: matched, Subject: sub, JTI: jti, IssuedAt: iat},
		slices.Clone(body), nil
}

// firstContained returns the first of wanted that got contains.
func firstContained(wanted, got []string) (string, bool) {
	for _, w := range wanted {
		if slices.Contains(got, w) {
			return w, true
		}
	}
	return "", false
}
