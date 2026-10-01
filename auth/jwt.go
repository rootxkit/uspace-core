package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/rootxkit/uspace-core/core"
)

// Defaults of Config (spec 00 section 6.2).
const (
	DefaultMaxSkew            = 30 * time.Second
	DefaultJWKSCacheTTL       = 24 * time.Hour
	DefaultMinRefreshInterval = time.Minute
	DefaultMaxTokenBytes      = 8 << 10
	DefaultMaxJWKSBytes       = 1 << 20
	DefaultHTTPTimeout        = 10 * time.Second
	DefaultJWKSFetchTimeout   = 2 * time.Second
	// MinRSABits is the shortest RSA modulus a JWKS key may have.
	MinRSABits = 2048
)

// The one accepted signature algorithm.
const algRS256 = "RS256"

// JWT counter names (E-09). CounterAccepted is shared with the receiver
// verifier.
const (
	CounterRejectedIssuer      = "rejected_issuer"
	CounterRejectedAlgorithm   = "rejected_algorithm"
	CounterRejectedKID         = "rejected_kid"
	CounterRejectedSignature   = "rejected_signature"
	CounterRejectedExpired     = "rejected_expired"
	CounterRejectedNotYetValid = "rejected_not_yet_valid"
	CounterRejectedAudience    = "rejected_audience"
	CounterRejectedClaims      = "rejected_claims"
	CounterJWKSRefresh         = "jwks_refresh"
	CounterJWKSRefreshFailed   = "jwks_refresh_failed"
	CounterJWKSRefreshLimited  = "jwks_refresh_rate_limited"
)

// IssuerConfig is one allow-listed issuer: either the HTTPS URL of its
// JWKS, or a static key set (tests and static deployments). Exactly one
// is set.
type IssuerConfig struct {
	JWKSURL string
	Keys    jwk.Set
}

// Config configures a Verifier. Zero durations and sizes take the
// Default* values.
type Config struct {
	// Issuers is the allow-list, keyed by the exact iss value.
	Issuers map[string]IssuerConfig
	// Audience is this system's id; aud must contain it or one of
	// Audiences.
	Audience string
	// Audiences are further ids (hosts, M18) this system accepts: aud
	// must contain Audience or any one of them. Empty means Audience
	// alone, as in v1.0.0. Added in v1.1.0.
	Audiences []string
	// MaxSkew is the clock skew allowed on exp, nbf and iat.
	MaxSkew time.Duration
	// JWKSCacheTTL is how long a fetched JWKS is fresh. From
	// JWKSCacheTTL - JWKSRefreshAhead on, a known kid is still served from
	// the cache while a fetch runs in the background; a failed fetch keeps
	// the cached set, past the TTL too (06 section 2 T5: tokens stay
	// verifiable during an issuer outage, and no request waits on it).
	JWKSCacheTTL time.Duration
	// MinRefreshInterval rate-limits JWKS fetches per issuer, for an
	// unknown kid and for a cache due for refresh alike.
	MinRefreshInterval time.Duration
	// MaxTokenBytes bounds the compact token length.
	MaxTokenBytes int
	// MaxJWKSBytes bounds a JWKS response body.
	MaxJWKSBytes int64
	// JWKSFetchTimeout bounds one JWKS fetch. A fetch runs detached from
	// the context of the request that triggered it, so a caller that
	// cancels cannot make it fail.
	JWKSFetchTimeout time.Duration
	// JWKSRefreshAhead starts a background fetch this long before the
	// cached set reaches JWKSCacheTTL (default a tenth of the TTL; less
	// than the TTL). Requests keep being served from the cache meanwhile.
	JWKSRefreshAhead time.Duration
	// HTTPClient fetches JWKS; nil uses a client with DefaultHTTPTimeout
	// that refuses a redirect to a non-HTTPS URL. A client supplied here
	// is used as is: its own CheckRedirect (Go's default follows any
	// redirect, plain HTTP included) replaces that check, so a caller
	// that supplies one must refuse non-HTTPS redirects itself.
	HTTPClient *http.Client
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// Claims are the verified claims of a token.
type Claims struct {
	Issuer  string
	Subject string
	// Audience is the configured audience the token was accepted for:
	// Audience when aud contains it, otherwise the first of Audiences
	// that aud contains.
	Audience string
	JTI      string
	KeyID    string
	// Scopes is the space-separated scope claim split into words; nil
	// when the claim is absent.
	Scopes    []string
	ExpiresAt time.Time
	// IssuedAt is zero when the token has no iat.
	IssuedAt time.Time
	// Roles is the roles claim, a JSON array of strings (M20 session
	// tokens); nil when absent. Never required. Added in v1.1.0.
	Roles []string
	// Realm is the realm claim, a string (M20); empty when absent. Never
	// required. Added in v1.1.0.
	Realm string
}

// HasScope reports whether the token grants scope.
func (c Claims) HasScope(scope string) bool { return slices.Contains(c.Scopes, scope) }

// RequireScope returns a *core.FieldError on "scope" unless c grants
// scope (06 section 3: endpoint scope checks).
func RequireScope(c Claims, scope string) error {
	if c.HasScope(scope) {
		return nil
	}
	return core.Fieldf("scope", "the token does not grant %s", quoteShort(scope))
}

// TokenError is a refused token. Claim names the header field or claim
// at fault (alg, kid, iss, signature, exp, nbf, iat, aud, sub, jti,
// scope, token); Counter is the counter the refusal incremented. The
// message never contains the token.
type TokenError struct {
	Counter string
	Claim   string
	Reason  string
}

func (e *TokenError) Error() string { return e.Claim + ": " + e.Reason }

func refuseToken(counter, claim, format string, args ...any) *TokenError {
	return &TokenError{Counter: counter, Claim: claim, Reason: fmt.Sprintf(format, args...)}
}

// Verifier verifies ecosystem JWTs (spec 00 section 6.2): RS256 only,
// iss on the allow-list, kid in that issuer's JWKS, aud containing the
// own audience, exp, nbf and iat within MaxSkew, sub and jti present. It
// is safe for concurrent use and never logs a token.
type Verifier struct {
	cfg      Config
	issuers  map[string]*issuerKeys
	counters core.Counters
	// background counts the JWKS fetches running in the background.
	background sync.WaitGroup
}

// NewVerifier validates c, applies the defaults and fetches the JWKS of
// every URL-configured issuer. It refuses an empty allow-list or
// audience, an issuer with neither or both of JWKSURL and Keys, a JWKS
// URL that is not HTTPS (plain HTTP is allowed for localhost only), a
// negative duration or size, and a JWKS that cannot be fetched at start.
func NewVerifier(ctx context.Context, c Config) (*Verifier, error) {
	if len(c.Issuers) == 0 {
		return nil, core.Fieldf("issuers", "no issuer is allowed")
	}
	if c.Audience == "" && len(c.Audiences) == 0 {
		return nil, core.Fieldf("audience", "empty")
	}
	if slices.Contains(c.Audiences, "") {
		return nil, core.Fieldf("audiences", "an audience is empty")
	}
	c.Audiences = slices.Clone(c.Audiences)
	return newVerifier(ctx, c, "issuers", "issuer")
}

// newVerifier is NewVerifier without the audience: it applies the
// defaults and loads the allow-listed key sets, naming the allow-list
// field and its entries (noun) in its errors. The detached and compact
// verifiers each hold one for its JWKS cache and its counters.
func newVerifier(ctx context.Context, c Config, field, noun string) (*Verifier, error) {
	if len(c.Issuers) == 0 {
		return nil, core.Fieldf(field, "no %s is allowed", noun)
	}
	if c.MaxSkew < 0 || c.JWKSCacheTTL < 0 || c.MinRefreshInterval < 0 || c.MaxTokenBytes < 0 || c.MaxJWKSBytes < 0 ||
		c.JWKSFetchTimeout < 0 || c.JWKSRefreshAhead < 0 {
		return nil, core.Fieldf("config", "a duration or size is negative")
	}
	if c.MaxSkew == 0 {
		c.MaxSkew = DefaultMaxSkew
	}
	if c.JWKSCacheTTL == 0 {
		c.JWKSCacheTTL = DefaultJWKSCacheTTL
	}
	if c.MinRefreshInterval == 0 {
		c.MinRefreshInterval = DefaultMinRefreshInterval
	}
	if c.MaxTokenBytes == 0 {
		c.MaxTokenBytes = DefaultMaxTokenBytes
	}
	if c.MaxJWKSBytes == 0 {
		c.MaxJWKSBytes = DefaultMaxJWKSBytes
	}
	if c.JWKSRefreshAhead == 0 {
		c.JWKSRefreshAhead = c.JWKSCacheTTL / 10
	}
	if c.JWKSRefreshAhead >= c.JWKSCacheTTL {
		return nil, core.Fieldf("jwks_refresh_ahead", "%s is not less than the cache TTL %s", c.JWKSRefreshAhead, c.JWKSCacheTTL)
	}
	if c.JWKSFetchTimeout == 0 {
		c.JWKSFetchTimeout = DefaultJWKSFetchTimeout
	}
	if c.HTTPClient == nil {
		c.HTTPClient = defaultHTTPClient()
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	v := &Verifier{cfg: c, issuers: make(map[string]*issuerKeys, len(c.Issuers))}
	for iss, ic := range c.Issuers {
		if iss == "" {
			return nil, core.Fieldf(field, "an %s is empty", noun)
		}
		field := field + "." + iss
		switch {
		case ic.JWKSURL != "" && ic.Keys != nil:
			return nil, core.Fieldf(field, "both a JWKS URL and static keys")
		case ic.JWKSURL == "" && ic.Keys == nil:
			return nil, core.Fieldf(field, "neither a JWKS URL nor static keys")
		case ic.Keys != nil:
			v.issuers[iss] = &issuerKeys{keys: indexKeys(ic.Keys)}
		default:
			if err := checkJWKSURL(ic.JWKSURL); err != nil {
				return nil, core.Fieldf(field, "%v", err)
			}
			now := c.Now()
			ik := &issuerKeys{url: ic.JWKSURL, lastAttempt: now}
			if err := v.refresh(ctx, ik, now); err != nil {
				return nil, core.Fieldf(field, "%v", err)
			}
			v.issuers[iss] = ik
		}
	}
	return v, nil
}

// Counters returns the verifier's counters: accepted, rejected_issuer,
// rejected_algorithm, rejected_kid, rejected_signature, rejected_expired,
// rejected_not_yet_valid, rejected_audience, rejected_claims,
// rejected_malformed, jwks_refresh, jwks_refresh_failed,
// jwks_refresh_rate_limited.
func (v *Verifier) Counters() *core.Counters { return &v.counters }

// Verify verifies a compact JWS token and returns its claims. Every
// refusal is a *TokenError naming the claim and counted by reason. An
// unknown issuer is refused before any network call; an unknown kid
// fetches the issuer's JWKS again at most once per MinRefreshInterval.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	c, err := v.verify(ctx, token)
	if err != nil {
		var te *TokenError
		if errors.As(err, &te) {
			v.counters.Inc(te.Counter)
		}
		return Claims{}, err
	}
	v.counters.Inc(CounterAccepted)
	return c, nil
}

func (v *Verifier) verify(ctx context.Context, token string) (Claims, error) {
	if len(token) > v.cfg.MaxTokenBytes {
		return Claims{}, refuseToken(CounterRejectedMalformed, "token", "longer than %d bytes", v.cfg.MaxTokenBytes)
	}
	h64, rest, ok1 := strings.Cut(token, ".")
	p64, s64, ok2 := strings.Cut(rest, ".")
	if !ok1 || !ok2 || strings.Contains(s64, ".") {
		return Claims{}, refuseToken(CounterRejectedMalformed, "token", "not a compact JWS of three parts")
	}

	// The header is read as a map so that member names match exactly
	// (encoding/json would match "ALG" to an alg field).
	var hdr map[string]json.RawMessage
	hraw, err := base64.RawURLEncoding.Strict().DecodeString(h64)
	if err != nil || json.Unmarshal(hraw, &hdr) != nil || hdr == nil {
		return Claims{}, refuseToken(CounterRejectedMalformed, "token", "the header is not a base64url JSON object")
	}
	alg, ok := jsonString(hdr["alg"])
	if !ok {
		return Claims{}, refuseToken(CounterRejectedAlgorithm, "alg", "missing")
	}
	if alg != algRS256 {
		return Claims{}, refuseToken(CounterRejectedAlgorithm, "alg", "%s is not RS256", quoteShort(alg))
	}
	if _, has := hdr["crit"]; has {
		return Claims{}, refuseToken(CounterRejectedMalformed, "crit", "no critical header extension is understood")
	}
	kid, ok := jsonString(hdr["kid"])
	if !ok || kid == "" {
		return Claims{}, refuseToken(CounterRejectedKID, "kid", "missing")
	}

	payload, err := base64.RawURLEncoding.Strict().DecodeString(p64)
	if err != nil {
		return Claims{}, refuseToken(CounterRejectedMalformed, "token", "the payload is not base64url")
	}
	claims, err := parseClaims(payload)
	if err != nil {
		return Claims{}, err
	}
	iss, ok := claims.str("iss")
	if !ok || iss == "" {
		return Claims{}, refuseToken(CounterRejectedIssuer, "iss", "missing")
	}
	ik, ok := v.issuers[iss]
	if !ok {
		return Claims{}, refuseToken(CounterRejectedIssuer, "iss", "%s is not an allowed issuer", quoteShort(iss))
	}

	key, err := v.key(ctx, ik, kid)
	if err != nil {
		return Claims{}, err
	}
	verified, err := jws.Verify([]byte(token), jws.WithKey(jwa.RS256(), key.pub))
	if err != nil || !bytes.Equal(verified, payload) {
		return Claims{}, refuseToken(CounterRejectedSignature, "signature", "does not verify with kid %s", quoteShort(kid))
	}
	return v.judgeClaims(claims, iss, kid)
}

func (v *Verifier) judgeClaims(cl claimSet, iss, kid string) (Claims, error) {
	now := v.cfg.Now()
	skew := v.cfg.MaxSkew
	exp, present, err := cl.numericDate("exp")
	if err != nil {
		return Claims{}, err
	}
	if !present {
		return Claims{}, refuseToken(CounterRejectedClaims, "exp", "missing")
	}
	if !now.Before(exp.Add(skew)) {
		return Claims{}, refuseToken(CounterRejectedExpired, "exp", "expired at %s, more than %s ago",
			exp.UTC().Format(time.RFC3339), skew)
	}
	nbf, present, err := cl.numericDate("nbf")
	if err != nil {
		return Claims{}, err
	}
	if present && now.Add(skew).Before(nbf) {
		return Claims{}, refuseToken(CounterRejectedNotYetValid, "nbf", "not valid before %s, more than %s from now",
			nbf.UTC().Format(time.RFC3339), skew)
	}
	iat, present, err := cl.numericDate("iat")
	if err != nil {
		return Claims{}, err
	}
	if present && now.Add(skew).Before(iat) {
		return Claims{}, refuseToken(CounterRejectedNotYetValid, "iat", "issued at %s, more than %s from now",
			iat.UTC().Format(time.RFC3339), skew)
	}
	aud, err := cl.audience()
	if err != nil {
		return Claims{}, err
	}
	matched, ok := v.matchAudience(aud)
	if !ok {
		return Claims{}, refuseToken(CounterRejectedAudience, "aud", "%s", v.audienceRefusal())
	}
	sub, ok := cl.str("sub")
	if !ok || sub == "" {
		return Claims{}, refuseToken(CounterRejectedClaims, "sub", "missing")
	}
	jti, ok := cl.str("jti")
	if !ok || jti == "" {
		return Claims{}, refuseToken(CounterRejectedClaims, "jti", "missing")
	}
	var scopes []string
	if raw, has := cl["scope"]; has {
		s, ok := jsonString(raw)
		if !ok {
			return Claims{}, refuseToken(CounterRejectedClaims, "scope", "not a space-separated string")
		}
		scopes = strings.Fields(s)
	}
	var roles []string
	if raw, has := cl["roles"]; has {
		if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &roles) != nil || roles == nil {
			return Claims{}, refuseToken(CounterRejectedClaims, "roles", "not an array of strings")
		}
	}
	var realm string
	if raw, has := cl["realm"]; has {
		if realm, ok = jsonString(raw); !ok {
			return Claims{}, refuseToken(CounterRejectedClaims, "realm", "not a string")
		}
	}
	return Claims{
		Issuer: iss, Subject: sub, Audience: matched, JTI: jti, KeyID: kid,
		Scopes: scopes, ExpiresAt: exp, IssuedAt: iat, Roles: roles, Realm: realm,
	}, nil
}

// matchAudience returns Audience when aud contains it, otherwise the
// first of Audiences that aud contains.
func (v *Verifier) matchAudience(aud []string) (string, bool) {
	if v.cfg.Audience != "" && slices.Contains(aud, v.cfg.Audience) {
		return v.cfg.Audience, true
	}
	return firstContained(v.cfg.Audiences, aud)
}

func (v *Verifier) audienceRefusal() string {
	if len(v.cfg.Audiences) == 0 {
		return "does not contain " + quoteShort(v.cfg.Audience)
	}
	if v.cfg.Audience == "" {
		return fmt.Sprintf("does not contain any of the %d configured audiences", len(v.cfg.Audiences))
	}
	return fmt.Sprintf("does not contain %s or any of the %d further audiences", quoteShort(v.cfg.Audience), len(v.cfg.Audiences))
}

// claimSet is the payload's top-level members, undecoded.
type claimSet map[string]json.RawMessage

func parseClaims(payload []byte) (claimSet, error) {
	var cl claimSet
	if err := json.Unmarshal(payload, &cl); err != nil || cl == nil {
		return nil, refuseToken(CounterRejectedMalformed, "token", "the payload is not a JSON object")
	}
	return cl, nil
}

func (cl claimSet) str(name string) (string, bool) { return jsonString(cl[name]) }

// maxNumericDate is 9999-12-31T23:59:59Z: a later date is not a date.
const maxNumericDate = 253402300799

// numericDate reads an RFC 7519 NumericDate (seconds since the epoch,
// fractions allowed). present is false when the claim is absent.
func (cl claimSet) numericDate(name string) (t time.Time, present bool, err error) {
	raw, ok := cl[name]
	if !ok {
		return time.Time{}, false, nil
	}
	var f float64
	if len(raw) == 0 || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) || json.Unmarshal(raw, &f) != nil ||
		math.IsNaN(f) || f < 0 || f > maxNumericDate {
		return time.Time{}, true, refuseToken(CounterRejectedClaims, name, "not a NumericDate")
	}
	sec, frac := math.Modf(f)
	return time.Unix(int64(sec), int64(frac*1e9)).UTC(), true, nil
}

// audience reads aud as a string or an array of strings.
func (cl claimSet) audience() ([]string, error) {
	raw, ok := cl["aud"]
	if !ok {
		return nil, refuseToken(CounterRejectedAudience, "aud", "missing")
	}
	if s, ok := jsonString(raw); ok {
		return []string{s}, nil
	}
	var list []string
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &list) != nil {
		return nil, refuseToken(CounterRejectedAudience, "aud", "not a string or an array of strings")
	}
	return list, nil
}

// checkJWKSURL allows https, and http to a loopback host only.
func checkJWKSURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("the JWKS URL does not parse")
	}
	if u.Host == "" {
		return fmt.Errorf("the JWKS URL has no host")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
	}
	return fmt.Errorf("the JWKS URL is not https (plain http is allowed for localhost only)")
}

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: DefaultHTTPTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return checkJWKSURL(req.URL.String())
		},
	}
}
