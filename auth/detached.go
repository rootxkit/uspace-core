package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/rootxkit/uspace-core/core"
)

// The RFC 7797 extension a detached signature declares critical.
const critB64 = "b64"

// DetachedHeader is the protected header of a detached JWS as
// ParseDetachedHeader reads it: Alg "RS256", KID, IssuedAt (from iat;
// zero when absent or not a NumericDate), B64 (false for the unencoded
// payload; true when the member is absent, its RFC 7797 default) and Crit.
// The helpers write exactly alg, kid, iat, b64 false and crit ["b64"].
type DetachedHeader struct {
	Alg      string
	KID      string
	IssuedAt time.Time
	B64      bool
	Crit     []string
}

// SignDetached signs payload with the ring's active key and returns the
// X-JWS-Signature value <BASE64URL(protected)>..<BASE64URL(signature)>
// (RFC 7515 Appendix F, RFC 7797: the payload is neither encoded nor
// carried; the signing input is ASCII(BASE64URL(protected)) || '.' ||
// payload). iat is now in whole seconds.
func (r *KeyRing) SignDetached(payload []byte, now time.Time) (string, error) {
	k := r.signer()
	return signDetached(k.kid, k.priv, payload, now)
}

// SignDetached is KeyRing.SignDetached with one key, checked as NewIssuer
// checks its key on every call (tests and tools).
func SignDetached(k SigningKey, payload []byte, now time.Time) (string, error) {
	priv, _, err := importSigningKey(k.KID, k.Key)
	if err != nil {
		return "", err
	}
	return signDetached(k.KID, priv, payload, now)
}

func signDetached(kid string, priv jwk.Key, payload []byte, now time.Time) (string, error) {
	hdr := jws.NewHeaders()
	for _, kv := range []struct {
		name  string
		value any
	}{{jws.KeyIDKey, kid}, {"iat", now.Unix()}, {jws.B64Key, false}} {
		if err := hdr.Set(kv.name, kv.value); err != nil {
			return "", core.Fieldf("header", "%v", err)
		}
	}
	if payload == nil {
		payload = []byte{}
	}
	signed, err := jws.Sign(nil, jws.WithKey(jwa.RS256(), priv, jws.WithProtectedHeaders(hdr)),
		jws.WithDetachedPayload(payload))
	if err != nil {
		return "", core.Fieldf("signature", "%v", err)
	}
	return string(signed), nil
}

// DetachedConfig configures a DetachedVerifier. Zero durations and sizes
// take their defaults.
type DetachedConfig struct {
	// Publishers is the allow-list: publisher id -> its JWKS URL or
	// static key set. The caller names the publisher (from the bearer
	// token it verified), never the signature header.
	Publishers map[string]IssuerConfig
	// MaxAge is how old iat may be (default DefaultDetachedMaxAge).
	MaxAge time.Duration
	// MaxSkew is how far ahead of now iat may be (default DefaultMaxSkew).
	MaxSkew time.Duration
	// MaxHeaderBytes bounds the header value (default DefaultMaxTokenBytes).
	MaxHeaderBytes int
	// MaxPayloadBytes bounds the payload (default
	// DefaultMaxDetachedPayloadBytes).
	MaxPayloadBytes int64
	// The JWKS cache settings, as in Config.
	JWKSCacheTTL       time.Duration
	MinRefreshInterval time.Duration
	MaxJWKSBytes       int64
	JWKSFetchTimeout   time.Duration
	JWKSRefreshAhead   time.Duration
	HTTPClient         *http.Client
	Now                func() time.Time
}

// Signature is an accepted detached signature: the publisher the caller
// named, the kid that verified and the iat of the header.
type Signature struct {
	Publisher string
	KID       string
	IssuedAt  time.Time
}

// DetachedVerifier verifies X-JWS-Signature values against the allow-
// listed publishers' JWKS. It is safe for concurrent use and never logs.
type DetachedVerifier struct {
	kv              *Verifier
	maxAge          time.Duration
	maxHeaderBytes  int
	maxPayloadBytes int64
}

// NewDetachedVerifier validates c, applies the defaults and fetches the
// JWKS of every URL-configured publisher, as NewVerifier does for its
// issuers.
func NewDetachedVerifier(ctx context.Context, c DetachedConfig) (*DetachedVerifier, error) {
	if c.MaxAge < 0 || c.MaxHeaderBytes < 0 || c.MaxPayloadBytes < 0 {
		return nil, core.Fieldf("config", "a duration or size is negative")
	}
	kv, err := newVerifier(ctx, Config{
		Issuers: c.Publishers, MaxSkew: c.MaxSkew,
		JWKSCacheTTL: c.JWKSCacheTTL, MinRefreshInterval: c.MinRefreshInterval, MaxJWKSBytes: c.MaxJWKSBytes,
		JWKSFetchTimeout: c.JWKSFetchTimeout, JWKSRefreshAhead: c.JWKSRefreshAhead,
		HTTPClient: c.HTTPClient, Now: c.Now,
	}, "publishers", "publisher")
	if err != nil {
		return nil, err
	}
	v := &DetachedVerifier{kv: kv, maxAge: c.MaxAge, maxHeaderBytes: c.MaxHeaderBytes, maxPayloadBytes: c.MaxPayloadBytes}
	if v.maxAge == 0 {
		v.maxAge = DefaultDetachedMaxAge
	}
	if v.maxHeaderBytes == 0 {
		v.maxHeaderBytes = DefaultMaxTokenBytes
	}
	if v.maxPayloadBytes == 0 {
		v.maxPayloadBytes = DefaultMaxDetachedPayloadBytes
	}
	return v, nil
}

// Counters returns the verifier's counters: accepted, rejected_malformed,
// rejected_algorithm, rejected_b64, rejected_crit, rejected_kid,
// rejected_publisher, rejected_signature, rejected_iat,
// rejected_too_large, jwks_refresh, jwks_refresh_failed,
// jwks_refresh_rate_limited.
func (v *DetachedVerifier) Counters() *core.Counters { return &v.kv.counters }

// Verify checks that header is a detached RS256 JWS over payload by a key
// of publisher. Every refusal is a *TokenError naming the part at fault
// and counted by reason; an unknown publisher is refused before any
// network call.
func (v *DetachedVerifier) Verify(ctx context.Context, publisher, header string, payload []byte) (Signature, error) {
	s, err := v.verify(ctx, publisher, header, payload)
	if err != nil {
		var te *TokenError
		if errors.As(err, &te) {
			v.kv.counters.Inc(te.Counter)
		}
		return Signature{}, err
	}
	v.kv.counters.Inc(CounterAccepted)
	return s, nil
}

func (v *DetachedVerifier) verify(ctx context.Context, publisher, header string, payload []byte) (Signature, error) {
	if len(header) > v.maxHeaderBytes {
		return Signature{}, refuseToken(CounterRejectedTooLarge, "header", "longer than %d bytes", v.maxHeaderBytes)
	}
	if int64(len(payload)) > v.maxPayloadBytes {
		return Signature{}, refuseToken(CounterRejectedTooLarge, "payload", "longer than %d bytes", v.maxPayloadBytes)
	}
	ik, ok := v.kv.issuers[publisher]
	if !ok {
		return Signature{}, refuseToken(CounterRejectedPublisher, "publisher", "%s is not an allowed publisher", quoteShort(publisher))
	}
	_, hdr, err := parseDetached(header)
	if err != nil {
		return Signature{}, err
	}
	kid, err := judgeDetachedHeader(hdr)
	if err != nil {
		return Signature{}, err
	}
	key, err := v.kv.key(ctx, ik, kid)
	if err != nil {
		return Signature{}, err
	}
	if payload == nil {
		payload = []byte{}
	}
	if _, err := jws.Verify([]byte(header), jws.WithKey(jwa.RS256(), key.pub),
		jws.WithDetachedPayload(payload), jws.WithCritExtension(critB64)); err != nil {
		return Signature{}, refuseToken(CounterRejectedSignature, "signature", "does not verify with kid %s", quoteShort(kid))
	}
	iat, err := judgeIAT(hdr, v.kv.cfg.Now(), v.maxAge, v.kv.cfg.MaxSkew)
	if err != nil {
		return Signature{}, err
	}
	return Signature{Publisher: publisher, KID: kid, IssuedAt: iat}, nil
}

// judgeDetachedHeader checks alg, crit, b64 and kid and returns the kid.
func judgeDetachedHeader(hdr claimSet) (string, error) {
	alg, ok := hdr.str("alg")
	if !ok {
		return "", refuseToken(CounterRejectedAlgorithm, "alg", "missing")
	}
	if alg != algRS256 {
		return "", refuseToken(CounterRejectedAlgorithm, "alg", "%s is not RS256", quoteShort(alg))
	}
	crit, err := critList(hdr)
	if err != nil {
		return "", err
	}
	declared := false
	for _, name := range crit {
		// RFC 7515 section 4.1.11: an extension the recipient does not
		// understand is refused; b64 is the only one understood.
		if name != critB64 || declared {
			return "", refuseToken(CounterRejectedCrit, "crit", "%s is not understood or is repeated", quoteShort(name))
		}
		declared = true
	}
	raw, has := hdr[critB64]
	switch {
	case !has:
		return "", refuseToken(CounterRejectedB64, "b64", "missing: the payload must be declared unencoded (b64 false)")
	case !bytes.Equal(bytes.TrimSpace(raw), []byte("false")):
		return "", refuseToken(CounterRejectedB64, "b64", "is not false: only the unencoded payload is accepted")
	case !declared:
		return "", refuseToken(CounterRejectedB64, "crit", "does not list b64 (RFC 7797 section 3)")
	}
	kid, ok := hdr.str("kid")
	if !ok || kid == "" {
		return "", refuseToken(CounterRejectedKID, "kid", "missing")
	}
	return kid, nil
}

// critList reads crit as an array of strings; absent is an empty list.
func critList(hdr claimSet) ([]string, error) {
	raw, has := hdr["crit"]
	if !has {
		return nil, nil
	}
	var crit []string
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &crit) != nil || len(crit) == 0 {
		return nil, refuseToken(CounterRejectedCrit, "crit", "not a non-empty array of strings")
	}
	return crit, nil
}

// ParseDetachedHeader reads the protected header of an X-JWS-Signature
// value without verifying anything but its form: for logging, and for a
// receiver that must choose the publisher by kid. A value longer than
// DefaultMaxTokenBytes, one that is not <protected>..<signature>, or one
// whose protected header is not a base64url JSON object without repeated
// members is refused with a *TokenError on "header" (not counted).
func ParseDetachedHeader(header string) (DetachedHeader, error) {
	if len(header) > DefaultMaxTokenBytes {
		return DetachedHeader{}, refuseToken(CounterRejectedTooLarge, "header", "longer than %d bytes", DefaultMaxTokenBytes)
	}
	dh, _, err := parseDetached(header)
	return dh, err
}

func parseDetached(header string) (DetachedHeader, claimSet, error) {
	if strings.Count(header, ".") != 2 {
		return DetachedHeader{}, nil, refuseToken(CounterRejectedMalformed, "header", "not <protected>..<signature>")
	}
	p64, rest, _ := strings.Cut(header, ".")
	mid, s64, _ := strings.Cut(rest, ".")
	if mid != "" {
		return DetachedHeader{}, nil, refuseToken(CounterRejectedMalformed, "header",
			"carries an attached payload; the payload of X-JWS-Signature is detached")
	}
	if p64 == "" {
		return DetachedHeader{}, nil, refuseToken(CounterRejectedMalformed, "header", "no protected header")
	}
	hraw, err := base64.RawURLEncoding.Strict().DecodeString(p64)
	if err != nil {
		return DetachedHeader{}, nil, refuseToken(CounterRejectedMalformed, "header", "the protected header is not base64url")
	}
	if _, err := base64.RawURLEncoding.Strict().DecodeString(s64); err != nil {
		return DetachedHeader{}, nil, refuseToken(CounterRejectedMalformed, "header", "the signature is not base64url")
	}
	m, ok := decodeObject(hraw)
	if !ok {
		return DetachedHeader{}, nil, refuseToken(CounterRejectedMalformed, "header",
			"the protected header is not a JSON object with distinct members")
	}
	hdr := claimSet(m)
	dh := DetachedHeader{B64: true}
	dh.Alg, _ = hdr.str("alg")
	dh.KID, _ = hdr.str("kid")
	if iat, present, err := hdr.numericDate("iat"); present && err == nil {
		dh.IssuedAt = iat
	}
	if raw, has := hdr[critB64]; has && bytes.Equal(bytes.TrimSpace(raw), []byte("false")) {
		dh.B64 = false
	}
	if crit, err := critList(hdr); err == nil {
		dh.Crit = crit
	}
	return dh, hdr, nil
}
