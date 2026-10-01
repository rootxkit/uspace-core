package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/rootxkit/uspace-core/core"
)

// Test keys are generated at test time and never written anywhere
// (06 section 4).
var (
	testKey  = sync.OnceValue(func() *rsa.PrivateKey { return mustRSA(2048) })
	otherKey = sync.OnceValue(func() *rsa.PrivateKey { return mustRSA(2048) })
)

func mustRSA(bits int) *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		panic(err)
	}
	return k
}

const (
	testIss = "https://authority.example/"
	testAud = "ussp-1"
	testKID = "k1"
)

var testNow = time.Unix(1_790_000_000, 0).UTC()

// clock is a settable clock safe for concurrent reads.
type clock struct{ ns atomic.Int64 }

func newClock(t time.Time) *clock { c := &clock{}; c.set(t); return c }
func (c *clock) set(t time.Time)  { c.ns.Store(t.UnixNano()) }
func (c *clock) add(d time.Duration) {
	c.ns.Add(int64(d))
}
func (c *clock) now() time.Time { return time.Unix(0, c.ns.Load()).UTC() }

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func b64JSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b64(raw)
}

// compact signs header.payload with the named algorithm by hand, so the
// tests do not depend on the library's encoder to build hostile tokens.
func compact(header, claims map[string]any, sign func(input []byte) []byte) string {
	input := b64JSON(header) + "." + b64JSON(claims)
	return input + "." + b64(sign([]byte(input)))
}

func rs256(key *rsa.PrivateKey) func([]byte) []byte {
	return func(in []byte) []byte {
		h := sha256.Sum256(in)
		s, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, h[:])
		if err != nil {
			panic(err)
		}
		return s
	}
}

func goodHeader() map[string]any { return map[string]any{"alg": "RS256", "typ": "JWT", "kid": testKID} }

func goodClaims() map[string]any {
	return map[string]any{
		"iss": testIss, "sub": "op-1", "aud": testAud, "jti": "j-1",
		"scope": "rid.read rid.write",
		"iat":   testNow.Unix() - 10, "exp": testNow.Unix() + 300,
	}
}

func with(m map[string]any, kv ...any) map[string]any {
	out := maps.Clone(m)
	for i := 0; i < len(kv); i += 2 {
		k := kv[i].(string)
		if kv[i+1] == nil {
			delete(out, k)
		} else {
			out[k] = kv[i+1]
		}
	}
	return out
}

func publicSet(t testing.TB, kid string, keys ...*rsa.PrivateKey) jwk.Set {
	t.Helper()
	set := jwk.NewSet()
	for i, k := range keys {
		pk, err := jwk.Import(&k.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		id := kid
		if i > 0 {
			id = fmt.Sprintf("%s-%d", kid, i)
		}
		if err := pk.Set(jwk.KeyIDKey, id); err != nil {
			t.Fatal(err)
		}
		if err := set.AddKey(pk); err != nil {
			t.Fatal(err)
		}
	}
	return set
}

func staticVerifier(t testing.TB, set jwk.Set, now func() time.Time) *Verifier {
	t.Helper()
	v, err := NewVerifier(context.Background(), Config{
		Issuers:  map[string]IssuerConfig{testIss: {Keys: set}},
		Audience: testAud,
		Now:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func wantTokenRefused(t *testing.T, err error, counter, claim string) {
	t.Helper()
	var te *TokenError
	if !errors.As(err, &te) {
		t.Fatalf("got %v, want a *TokenError on %s", err, claim)
	}
	if te.Counter != counter || te.Claim != claim {
		t.Errorf("got %s on %s (%v), want %s on %s", te.Counter, te.Claim, err, counter, claim)
	}
	if !strings.HasPrefix(te.Error(), claim+": ") {
		t.Errorf("error %q does not name the claim %s", te.Error(), claim)
	}
}

// Each refusal beside the accepted token that differs from it in one
// header field or claim (E-01).
func TestJWTRefusalsBesideAcceptance(t *testing.T) {
	key := testKey()
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: mustPKIX(&key.PublicKey)})
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed := func(h, c map[string]any) string { return compact(h, c, rs256(key)) }
	good := signed(goodHeader(), goodClaims())
	cases := []struct {
		name    string
		token   string
		counter string
		claim   string
	}{
		{"accepted", good, CounterAccepted, ""},
		{"alg none", compact(with(goodHeader(), "alg", "none"), goodClaims(), func([]byte) []byte { return nil }), CounterRejectedAlgorithm, "alg"},
		{"alg none with the RS256 signature", strings.Join(append([]string{b64JSON(with(goodHeader(), "alg", "none"))}, strings.Split(good, ".")[1:]...), "."), CounterRejectedAlgorithm, "alg"},
		{"alg missing", signed(with(goodHeader(), "alg", nil), goodClaims()), CounterRejectedAlgorithm, "alg"},
		{"alg lower case", signed(with(goodHeader(), "alg", "rs256"), goodClaims()), CounterRejectedAlgorithm, "alg"},
		{"alg upper-case member name", signed(with(goodHeader(), "alg", nil, "ALG", "RS256"), goodClaims()), CounterRejectedAlgorithm, "alg"},
		{"HS256 with the public key PEM as secret", compact(with(goodHeader(), "alg", "HS256"), goodClaims(), func(in []byte) []byte {
			m := hmac.New(sha256.New, pubPEM)
			m.Write(in)
			return m.Sum(nil)
		}), CounterRejectedAlgorithm, "alg"},
		{"RS512 by the right key", compact(with(goodHeader(), "alg", "RS512"), goodClaims(), func(in []byte) []byte {
			h := sha512.Sum512(in)
			s, _ := rsa.SignPKCS1v15(nil, key, crypto.SHA512, h[:])
			return s
		}), CounterRejectedAlgorithm, "alg"},
		{"PS256 by the right key", compact(with(goodHeader(), "alg", "PS256"), goodClaims(), func(in []byte) []byte {
			h := sha256.Sum256(in)
			s, _ := rsa.SignPSS(rand.Reader, key, crypto.SHA256, h[:], nil)
			return s
		}), CounterRejectedAlgorithm, "alg"},
		{"ES256 by an EC key", compact(with(goodHeader(), "alg", "ES256"), goodClaims(), func(in []byte) []byte {
			h := sha256.Sum256(in)
			s, _ := ecdsa.SignASN1(rand.Reader, ec, h[:])
			return s
		}), CounterRejectedAlgorithm, "alg"},
		{"crit header", signed(with(goodHeader(), "crit", []string{"b64"}, "b64", false), goodClaims()), CounterRejectedMalformed, "crit"},
		{"kid missing", signed(with(goodHeader(), "kid", nil), goodClaims()), CounterRejectedKID, "kid"},
		{"kid empty", signed(with(goodHeader(), "kid", ""), goodClaims()), CounterRejectedKID, "kid"},
		{"kid unknown", signed(with(goodHeader(), "kid", "k-unknown"), goodClaims()), CounterRejectedKID, "kid"},
		{"iss missing", signed(goodHeader(), with(goodClaims(), "iss", nil)), CounterRejectedIssuer, "iss"},
		{"iss not allowed", signed(goodHeader(), with(goodClaims(), "iss", "https://other.example/")), CounterRejectedIssuer, "iss"},
		{"iss with a trailing difference", signed(goodHeader(), with(goodClaims(), "iss", strings.TrimSuffix(testIss, "/"))), CounterRejectedIssuer, "iss"},
		{"signed by another key under the same kid", compact(goodHeader(), goodClaims(), rs256(otherKey())), CounterRejectedSignature, "signature"},
		{"payload changed after signing", tamper(good, with(goodClaims(), "sub", "op-2")), CounterRejectedSignature, "signature"},
		{"signature not base64url", strings.Join(strings.Split(good, ".")[:2], ".") + ".!!!", CounterRejectedSignature, "signature"},
		{"exp missing", signed(goodHeader(), with(goodClaims(), "exp", nil)), CounterRejectedClaims, "exp"},
		{"exp a string", signed(goodHeader(), with(goodClaims(), "exp", "soon")), CounterRejectedClaims, "exp"},
		{"exp negative", signed(goodHeader(), with(goodClaims(), "exp", -1)), CounterRejectedClaims, "exp"},
		{"exp past year 9999", signed(goodHeader(), with(goodClaims(), "exp", 1e300)), CounterRejectedClaims, "exp"},
		{"exp 31 s ago", signed(goodHeader(), with(goodClaims(), "exp", testNow.Unix()-31)), CounterRejectedExpired, "exp"},
		{"exp 30 s ago", signed(goodHeader(), with(goodClaims(), "exp", testNow.Unix()-30)), CounterRejectedExpired, "exp"},
		{"exp 29 s ago", signed(goodHeader(), with(goodClaims(), "exp", testNow.Unix()-29)), CounterAccepted, ""},
		{"exp fractional", signed(goodHeader(), with(goodClaims(), "exp", float64(testNow.Unix())+0.5)), CounterAccepted, ""},
		{"nbf 31 s ahead", signed(goodHeader(), with(goodClaims(), "nbf", testNow.Unix()+31)), CounterRejectedNotYetValid, "nbf"},
		{"nbf 29 s ahead", signed(goodHeader(), with(goodClaims(), "nbf", testNow.Unix()+29)), CounterAccepted, ""},
		{"nbf not a number", signed(goodHeader(), with(goodClaims(), "nbf", true)), CounterRejectedClaims, "nbf"},
		{"iat 31 s ahead", signed(goodHeader(), with(goodClaims(), "iat", testNow.Unix()+31)), CounterRejectedNotYetValid, "iat"},
		{"iat absent", signed(goodHeader(), with(goodClaims(), "iat", nil)), CounterAccepted, ""},
		{"iat not a number", signed(goodHeader(), with(goodClaims(), "iat", "x")), CounterRejectedClaims, "iat"},
		{"aud another system", signed(goodHeader(), with(goodClaims(), "aud", "cisp-1")), CounterRejectedAudience, "aud"},
		{"aud array with ours", signed(goodHeader(), with(goodClaims(), "aud", []string{"cisp-1", testAud})), CounterAccepted, ""},
		{"aud array without ours", signed(goodHeader(), with(goodClaims(), "aud", []string{"cisp-1"})), CounterRejectedAudience, "aud"},
		{"aud missing", signed(goodHeader(), with(goodClaims(), "aud", nil)), CounterRejectedAudience, "aud"},
		{"aud a number", signed(goodHeader(), with(goodClaims(), "aud", 7)), CounterRejectedAudience, "aud"},
		{"sub missing", signed(goodHeader(), with(goodClaims(), "sub", nil)), CounterRejectedClaims, "sub"},
		{"jti missing", signed(goodHeader(), with(goodClaims(), "jti", nil)), CounterRejectedClaims, "jti"},
		{"jti empty", signed(goodHeader(), with(goodClaims(), "jti", "")), CounterRejectedClaims, "jti"},
		{"scope an array", signed(goodHeader(), with(goodClaims(), "scope", []string{"rid.read"})), CounterRejectedClaims, "scope"},
		{"scope absent", signed(goodHeader(), with(goodClaims(), "scope", nil)), CounterAccepted, ""},
		{"two parts", strings.Join(strings.Split(good, ".")[:2], "."), CounterRejectedMalformed, "token"},
		{"four parts", good + ".x", CounterRejectedMalformed, "token"},
		{"header not base64url", "*." + strings.SplitN(good, ".", 2)[1], CounterRejectedMalformed, "token"},
		{"header padded base64", b64JSON(goodHeader()) + "=." + strings.SplitN(good, ".", 2)[1], CounterRejectedMalformed, "token"},
		{"header an array", b64([]byte(`[1]`)) + "." + strings.SplitN(good, ".", 2)[1], CounterRejectedMalformed, "token"},
		{"payload not base64url", b64JSON(goodHeader()) + ".*.sig", CounterRejectedMalformed, "token"},
		{"payload not an object", compact(goodHeader(), nil, rs256(key)), CounterRejectedMalformed, "token"},
		{"too long", good + strings.Repeat("A", DefaultMaxTokenBytes), CounterRejectedMalformed, "token"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := staticVerifier(t, publicSet(t, testKID, key), func() time.Time { return testNow })
			c, err := v.Verify(context.Background(), tc.token)
			if tc.counter == CounterAccepted {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if c.Issuer != testIss || c.Subject != "op-1" || c.Audience != testAud || c.JTI != "j-1" || c.KeyID != testKID {
					t.Errorf("claims %+v", c)
				}
			} else {
				wantTokenRefused(t, err, tc.counter, tc.claim)
				for _, part := range strings.Split(tc.token, ".") {
					if len(part) >= 16 && strings.Contains(err.Error(), part) {
						t.Errorf("the error carries the token: %v", err)
					}
				}
			}
			if got := v.Counters().Get(tc.counter); got != 1 {
				t.Errorf("counter %s = %d, want 1: %v", tc.counter, got, v.Counters().Snapshot())
			}
		})
	}
}

func mustPKIX(pub *rsa.PublicKey) []byte {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		panic(err)
	}
	return der
}

// tamper replaces the payload of token, keeping its signature.
func tamper(token string, claims map[string]any) string {
	p := strings.Split(token, ".")
	return p[0] + "." + b64JSON(claims) + "." + p[2]
}

func TestClaimsFieldsAndScopes(t *testing.T) {
	v := staticVerifier(t, publicSet(t, testKID, testKey()), func() time.Time { return testNow })
	c, err := v.Verify(context.Background(), compact(goodHeader(), goodClaims(), rs256(testKey())))
	if err != nil {
		t.Fatal(err)
	}
	if !c.ExpiresAt.Equal(testNow.Add(300*time.Second)) || !c.IssuedAt.Equal(testNow.Add(-10*time.Second)) {
		t.Errorf("times %s %s", c.ExpiresAt, c.IssuedAt)
	}
	if len(c.Scopes) != 2 || !c.HasScope("rid.read") || !c.HasScope("rid.write") || c.HasScope("rid") {
		t.Errorf("scopes %q", c.Scopes)
	}
	if err := RequireScope(c, "rid.read"); err != nil {
		t.Errorf("granted scope refused: %v", err)
	}
	err = RequireScope(c, "uss.admin")
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "scope" {
		t.Errorf("got %v, want a FieldError on scope", err)
	}
	c2, err := v.Verify(context.Background(), compact(goodHeader(), with(goodClaims(), "scope", nil), rs256(testKey())))
	if err != nil {
		t.Fatal(err)
	}
	if c2.Scopes != nil || RequireScope(c2, "rid.read") == nil {
		t.Errorf("a token without scope grants %q", c2.Scopes)
	}
}

func TestIssuerRoundTrip(t *testing.T) {
	is, err := NewIssuer(testIss, testKey(), testKID)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := is.Issue("op-1", testAud, []string{"rid.read", "uss.write"}, 5*time.Minute, testNow.Add(400*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	clk := newClock(testNow)
	v := staticVerifier(t, is.JWKS(), clk.now)
	c, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Issuer != testIss || c.Subject != "op-1" || c.KeyID != testKID || len(c.JTI) != 32 ||
		!c.HasScope("uss.write") || !c.ExpiresAt.Equal(testNow.Add(5*time.Minute)) || !c.IssuedAt.Equal(testNow) {
		t.Errorf("claims %+v", c)
	}
	tok2, err := is.Issue("op-1", testAud, nil, 5*time.Minute, testNow)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := v.Verify(context.Background(), tok2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.JTI == c.JTI {
		t.Error("jti repeated")
	}
	if c2.Scopes != nil {
		t.Errorf("scopes %q from no scopes", c2.Scopes)
	}
	clk.add(5*time.Minute + DefaultMaxSkew)
	_, err = v.Verify(context.Background(), tok)
	wantTokenRefused(t, err, CounterRejectedExpired, "exp")
	// The published JWKS is public only.
	k, _ := is.JWKS().Key(0)
	var priv *rsa.PrivateKey
	if jwk.Export(k, &priv) == nil {
		t.Error("the JWKS carries the private key")
	}
	if alg, _ := k.Algorithm(); alg.String() != "RS256" {
		t.Errorf("alg %v", alg)
	}
}

func TestNewIssuerAndIssueRefusals(t *testing.T) {
	short := mustRSA(1024)
	bad := *testKey()
	bad.D = nil
	for _, tc := range []struct {
		name  string
		iss   string
		key   *rsa.PrivateKey
		kid   string
		field string
	}{
		{"empty iss", "", testKey(), testKID, "iss"},
		{"empty kid", testIss, testKey(), "", "kid"},
		{"nil key", testIss, nil, testKID, "key"},
		{"1024-bit key", testIss, short, testKID, "key"},
		{"invalid key", testIss, &bad, testKID, "key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewIssuer(tc.iss, tc.key, tc.kid)
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("got %v, want a FieldError on %s", err, tc.field)
			}
		})
	}
	is, err := NewIssuer(testIss, testKey(), testKID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, sub, aud string
		scopes         []string
		ttl            time.Duration
		field          string
	}{
		{"empty sub", "", testAud, nil, time.Minute, "sub"},
		{"empty aud", "op", "", nil, time.Minute, "aud"},
		{"no ttl", "op", testAud, nil, 0, "ttl"},
		{"empty scope", "op", testAud, []string{""}, time.Minute, "scope"},
		{"scope with a space", "op", testAud, []string{"a b"}, time.Minute, "scope"},
		{"scope with a tab", "op", testAud, []string{"a\tb"}, time.Minute, "scope"},
		{"scope with a no-break space", "op", testAud, []string{"rid.read\u00a0uss.admin"}, time.Minute, "scope"},
		{"scope with an em space", "op", testAud, []string{"rid.read\u2003uss.admin"}, time.Minute, "scope"},
		{"scope with a next-line", "op", testAud, []string{"rid.read\u0085uss.admin"}, time.Minute, "scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := is.Issue(tc.sub, tc.aud, tc.scopes, tc.ttl, testNow)
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("got %v, want a FieldError on %s", err, tc.field)
			}
		})
	}
}

// The accepted twin of the white-space refusals: a scope with non-ASCII
// letters but no white space is issued and arrives as one scope.
func TestIssueScopeWithoutWhiteSpaceRoundTrips(t *testing.T) {
	is, err := NewIssuer(testIss, testKey(), testKID)
	if err != nil {
		t.Fatal(err)
	}
	scopes := []string{"rid.read", "zones.\u10e0\u10d4\u10d3"} // Georgian letters
	tok, err := is.Issue("op", testAud, scopes, time.Minute, testNow)
	if err != nil {
		t.Fatal(err)
	}
	c, err := staticVerifier(t, is.JWKS(), func() time.Time { return testNow }).Verify(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Scopes) != 2 || c.Scopes[0] != scopes[0] || c.Scopes[1] != scopes[1] {
		t.Errorf("scopes %q, want %q", c.Scopes, scopes)
	}
	// The same scope joined by a no-break space, which the verifier would
	// split on, never reaches a token.
	if _, err := is.Issue("op", testAud, []string{"rid.read\u00a0uss.admin"}, time.Minute, testNow); err == nil {
		t.Error("a scope with a no-break space was issued")
	}
}

func TestNewVerifierRefusesBadConfig(t *testing.T) {
	set := publicSet(t, testKID, testKey())
	ok := map[string]IssuerConfig{testIss: {Keys: set}}
	for _, tc := range []struct {
		name  string
		cfg   Config
		field string
	}{
		{"no issuers", Config{Audience: testAud}, "issuers"},
		{"no audience", Config{Issuers: ok}, "audience"},
		{"negative skew", Config{Issuers: ok, Audience: testAud, MaxSkew: -1}, "config"},
		{"negative size", Config{Issuers: ok, Audience: testAud, MaxJWKSBytes: -1}, "config"},
		{"empty issuer", Config{Issuers: map[string]IssuerConfig{"": {Keys: set}}, Audience: testAud}, "issuers"},
		{"both", Config{Issuers: map[string]IssuerConfig{testIss: {Keys: set, JWKSURL: "https://a.example/jwks"}}, Audience: testAud}, "issuers." + testIss},
		{"neither", Config{Issuers: map[string]IssuerConfig{testIss: {}}, Audience: testAud}, "issuers." + testIss},
		{"plain http", Config{Issuers: map[string]IssuerConfig{testIss: {JWKSURL: "http://authority.example/jwks"}}, Audience: testAud}, "issuers." + testIss},
		{"no host", Config{Issuers: map[string]IssuerConfig{testIss: {JWKSURL: "https:///jwks"}}, Audience: testAud}, "issuers." + testIss},
		{"unparsable", Config{Issuers: map[string]IssuerConfig{testIss: {JWKSURL: "https://a b/%zz"}}, Audience: testAud}, "issuers." + testIss},
		{"unreachable at start", Config{Issuers: map[string]IssuerConfig{testIss: {JWKSURL: "https://127.0.0.1:1/jwks"}}, Audience: testAud}, "issuers." + testIss},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewVerifier(context.Background(), tc.cfg)
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("got %v, want a FieldError on %s", err, tc.field)
			}
		})
	}
}

func TestCheckJWKSURL(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://authority.example/jwks": true,
		"http://localhost:8080/jwks":     true,
		"http://127.0.0.1/jwks":          true,
		"http://[::1]:9/jwks":            true,
		"http://authority.example/jwks":  false,
		"http://localhost.evil/jwks":     false,
		"ftp://authority.example/jwks":   false,
		"file:///etc/jwks":               false,
	} {
		if got := checkJWKSURL(raw) == nil; got != want {
			t.Errorf("%s: allowed %v, want %v", raw, got, want)
		}
	}
	c := defaultHTTPClient()
	req := func(u string) *http.Request { r, _ := http.NewRequest(http.MethodGet, u, http.NoBody); return r }
	if err := c.CheckRedirect(req("http://authority.example/jwks"), []*http.Request{req("https://authority.example/")}); err == nil {
		t.Error("a redirect to plain http was followed")
	}
	if err := c.CheckRedirect(req("https://cdn.example/jwks"), []*http.Request{req("https://authority.example/")}); err != nil {
		t.Errorf("a redirect to https was refused: %v", err)
	}
	if err := c.CheckRedirect(req("https://cdn.example/jwks"), make([]*http.Request, 5)); err == nil {
		t.Error("a sixth redirect was followed")
	}
}

// jwksServer serves a key set that the test can swap or break.
type jwksServer struct {
	*httptest.Server
	mu     sync.Mutex
	body   []byte
	status int
	hits   atomic.Int64
	// gate, when set, holds every response until it is closed.
	gate chan struct{}
}

func newJWKSServer(t *testing.T, set jwk.Set) *jwksServer {
	t.Helper()
	s := &jwksServer{status: http.StatusOK}
	s.setKeys(t, set)
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		s.mu.Lock()
		gate := s.gate
		s.mu.Unlock()
		if gate != nil {
			<-gate
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		w.WriteHeader(s.status)
		_, _ = w.Write(s.body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *jwksServer) setKeys(t *testing.T, set jwk.Set) {
	t.Helper()
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	s.setBody(http.StatusOK, raw)
}

func (s *jwksServer) setBody(status int, body []byte) {
	s.mu.Lock()
	s.status, s.body = status, body
	s.mu.Unlock()
}

func urlVerifier(t *testing.T, s *jwksServer, clk *clock) *Verifier {
	t.Helper()
	v, err := NewVerifier(context.Background(), Config{
		Issuers:  map[string]IssuerConfig{testIss: {JWKSURL: s.URL + "/jwks"}},
		Audience: testAud,
		Now:      clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func tokenAt(key *rsa.PrivateKey, kid string, now time.Time) string {
	return compact(with(goodHeader(), "kid", kid),
		with(goodClaims(), "iat", now.Unix(), "exp", now.Unix()+3600), rs256(key))
}

// E-02: an unknown kid fetches the JWKS once, then fetches are
// rate-limited; after the interval a rotated key is picked up.
func TestJWKSUnknownKIDRefreshIsRateLimited(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v := urlVerifier(t, s, clk)
	ctx := context.Background()
	if _, err := v.Verify(ctx, tokenAt(testKey(), testKID, clk.now())); err != nil {
		t.Fatal(err)
	}
	if v.Counters().Get(CounterJWKSRefresh) != 1 || s.hits.Load() != 1 {
		t.Fatalf("fetches %d, jwks_refresh %d", s.hits.Load(), v.Counters().Get(CounterJWKSRefresh))
	}
	// The issuer rotates in k2; a token under k2 arrives at once: the
	// start-up fetch was less than a minute ago, so no fetch.
	s.setKeys(t, publicSet(t, testKID, testKey(), otherKey()))
	rotated := tokenAt(otherKey(), testKID+"-1", clk.now())
	_, err := v.Verify(ctx, rotated)
	wantTokenRefused(t, err, CounterRejectedKID, "kid")
	if s.hits.Load() != 1 || v.Counters().Get(CounterJWKSRefreshLimited) != 1 {
		t.Fatalf("fetches %d, rate-limited %d", s.hits.Load(), v.Counters().Get(CounterJWKSRefreshLimited))
	}
	clk.add(DefaultMinRefreshInterval)
	if _, err := v.Verify(ctx, rotated); err != nil {
		t.Fatalf("the rotated key was not fetched: %v", err)
	}
	if s.hits.Load() != 2 || v.Counters().Get(CounterJWKSRefresh) != 2 {
		t.Fatalf("fetches %d", s.hits.Load())
	}
	// A truly unknown kid: one fetch, then rate-limited for a minute.
	clk.add(DefaultMinRefreshInterval)
	for range 5 {
		_, err := v.Verify(ctx, tokenAt(testKey(), "k-nope", clk.now()))
		wantTokenRefused(t, err, CounterRejectedKID, "kid")
	}
	if s.hits.Load() != 3 || v.Counters().Get(CounterJWKSRefreshLimited) != 5 {
		t.Fatalf("fetches %d, rate-limited %d", s.hits.Load(), v.Counters().Get(CounterJWKSRefreshLimited))
	}
}

// The rate-limit attack: an unauthenticated request with an unknown kid
// and a cancelled context must not spend the issuer's refresh. Before
// the fix it stamped a failed fetch and kept a rotated key out for a
// minute; repeated, for ever. The legitimate token under the new kid is
// accepted right after the attack (presence), while the plain rate limit
// of TestJWKSUnknownKIDRefreshIsRateLimited still holds.
func TestJWKSCancelledCallerCannotHoldDownRotation(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v := urlVerifier(t, s, clk)
	s.setKeys(t, publicSet(t, testKID, testKey(), otherKey()))
	clk.add(DefaultMinRefreshInterval)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for range 3 {
		_, err := v.Verify(cancelled, tokenAt(testKey(), "k-attacker", clk.now()))
		wantTokenRefused(t, err, CounterRejectedKID, "kid")
	}
	if s.hits.Load() != 1 || v.Counters().Get(CounterJWKSRefreshFailed) != 0 || v.Counters().Get(CounterJWKSRefreshLimited) != 0 {
		t.Fatalf("a cancelled caller spent the refresh: fetches %d, %v", s.hits.Load(), v.Counters().Snapshot())
	}
	if _, err := v.Verify(context.Background(), tokenAt(otherKey(), testKID+"-1", clk.now())); err != nil {
		t.Fatalf("the rotated key is held down by the attack: %v", err)
	}
}

// A caller that cancels while the fetch is in flight is released at once
// but cannot fail the fetch: it completes and installs the rotated key
// for the next request.
func TestJWKSFetchSurvivesCallerCancellation(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v := urlVerifier(t, s, clk)
	s.setKeys(t, publicSet(t, testKID, testKey(), otherKey()))
	clk.add(DefaultMinRefreshInterval)
	gate := make(chan struct{})
	s.mu.Lock()
	s.gate = gate
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := v.Verify(ctx, tokenAt(testKey(), "k-attacker", clk.now()))
		done <- err
	}()
	for s.hits.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	// The caller is released at once, while the issuer still holds the
	// response; the fetch goes on without it.
	if err := <-done; err == nil {
		t.Fatal("unknown kid accepted")
	}
	close(gate)
	v.background.Wait()
	if v.Counters().Get(CounterJWKSRefresh) != 2 || v.Counters().Get(CounterJWKSRefreshFailed) != 0 {
		t.Fatalf("the fetch did not survive the caller: %v", v.Counters().Snapshot())
	}
	if _, err := v.Verify(context.Background(), tokenAt(otherKey(), testKID+"-1", clk.now())); err != nil {
		t.Fatalf("the rotated key is not installed: %v", err)
	}
}

// E-02 and T5: a failed fetch keeps the cached set, past its TTL too,
// and is counted; the next successful fetch replaces it. The request that
// finds the cache stale is served from it and the fetch runs behind it.
func TestJWKSRefreshFailureKeepsCachedSet(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v := urlVerifier(t, s, clk)
	ctx := context.Background()
	for _, broken := range []struct {
		status int
		body   []byte
	}{
		{http.StatusInternalServerError, []byte("down")},
		{http.StatusOK, []byte("not json")},
		{http.StatusOK, []byte(`{"keys":[]}`)},
		// The library refuses a whole set holding an RSA key under 2048 bits.
		{http.StatusOK, shortKeyJWKS()},
		{http.StatusOK, []byte(`{"keys":[` + strings.Repeat(" ", DefaultMaxJWKSBytes) + `]}`)},
	} {
		s.setBody(broken.status, broken.body)
		clk.add(DefaultJWKSCacheTTL) // the cache is stale: a fetch is due
		before := v.Counters().Get(CounterJWKSRefreshFailed)
		if _, err := v.Verify(ctx, tokenAt(testKey(), testKID, clk.now())); err != nil {
			t.Fatalf("status %d: the cached key was dropped: %v", broken.status, err)
		}
		v.background.Wait()
		if v.Counters().Get(CounterJWKSRefreshFailed) != before+1 {
			t.Fatalf("status %d: failure not counted", broken.status)
		}
		if _, err := v.Verify(ctx, tokenAt(testKey(), testKID, clk.now())); err != nil {
			t.Fatalf("status %d: the cached key was dropped after the failure: %v", broken.status, err)
		}
	}
	// The issuer comes back with only the other key: it replaces the set.
	s.setKeys(t, publicSet(t, testKID, otherKey()))
	clk.add(DefaultJWKSCacheTTL)
	if _, err := v.Verify(ctx, tokenAt(testKey(), testKID, clk.now())); err != nil {
		t.Fatalf("the stale set did not serve while the fetch ran: %v", err)
	}
	v.background.Wait()
	if _, err := v.Verify(ctx, tokenAt(otherKey(), testKID, clk.now())); err != nil {
		t.Fatal(err)
	}
	_, err := v.Verify(ctx, tokenAt(testKey(), testKID, clk.now()))
	wantTokenRefused(t, err, CounterRejectedSignature, "signature")
	if v.Counters().Get(CounterJWKSRefresh) != 2 {
		t.Errorf("jwks_refresh %d", v.Counters().Get(CounterJWKSRefresh))
	}
}

// Before the refresh-ahead point the cache is used without a fetch; from
// it, a fetch runs in the background while requests are served.
func TestJWKSRefreshAhead(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v := urlVerifier(t, s, clk)
	ahead := DefaultJWKSCacheTTL / 10
	clk.add(DefaultJWKSCacheTTL - ahead - time.Second)
	if _, err := v.Verify(context.Background(), tokenAt(testKey(), testKID, clk.now())); err != nil {
		t.Fatal(err)
	}
	v.background.Wait()
	if s.hits.Load() != 1 {
		t.Fatalf("fetched %d times before the refresh-ahead point", s.hits.Load())
	}
	clk.add(time.Second)
	if _, err := v.Verify(context.Background(), tokenAt(testKey(), testKID, clk.now())); err != nil {
		t.Fatal(err)
	}
	v.background.Wait()
	if s.hits.Load() != 2 || v.Counters().Get(CounterJWKSRefresh) != 2 {
		t.Fatalf("not refreshed ahead of the TTL (%d)", s.hits.Load())
	}
	// The refreshed set is fresh again: no fetch for another while.
	clk.add(time.Minute)
	if _, err := v.Verify(context.Background(), tokenAt(testKey(), testKID, clk.now())); err != nil {
		t.Fatal(err)
	}
	v.background.Wait()
	if s.hits.Load() != 2 {
		t.Fatalf("fetched again right after a refresh (%d)", s.hits.Load())
	}
}

// An issuer that hangs never stalls a request whose key is cached; the
// fetch ends at JWKSFetchTimeout and is counted as failed. Many requests
// share one fetch (single-flight).
func TestJWKSOutageDoesNotStallRequests(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v, err := NewVerifier(context.Background(), Config{
		Issuers:          map[string]IssuerConfig{testIss: {JWKSURL: s.URL + "/jwks"}},
		Audience:         testAud,
		Now:              clk.now,
		JWKSFetchTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	defer close(gate)
	s.mu.Lock()
	s.gate = gate
	s.mu.Unlock()
	clk.add(DefaultJWKSCacheTTL + time.Hour)
	tok := tokenAt(testKey(), testKID, clk.now()) // signed outside the timed loop
	start := time.Now()
	for range 20 {
		if _, err := v.Verify(context.Background(), tok); err != nil {
			t.Fatal(err)
		}
	}
	// A request that waited on the hung fetch would take the whole fetch
	// timeout (1 s); 20 RSA verifications take milliseconds, even under
	// -race on a slow runner.
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("20 requests took %s during the outage", d)
	}
	v.background.Wait()
	if got := v.Counters().Get(CounterJWKSRefreshFailed); got != 1 {
		t.Errorf("jwks_refresh_failed %d, want one shared fetch", got)
	}
	if got := s.hits.Load(); got != 2 {
		t.Errorf("%d fetches, want 2 (start-up and one shared refresh)", got)
	}
}

func TestNewVerifierRefreshAheadBound(t *testing.T) {
	set := publicSet(t, testKID, testKey())
	cfg := Config{Issuers: map[string]IssuerConfig{testIss: {Keys: set}}, Audience: testAud,
		JWKSCacheTTL: time.Hour, JWKSRefreshAhead: time.Hour}
	_, err := NewVerifier(context.Background(), cfg)
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "jwks_refresh_ahead" {
		t.Fatalf("got %v, want a FieldError on jwks_refresh_ahead", err)
	}
	cfg.JWKSRefreshAhead = time.Hour - time.Second
	if _, err := NewVerifier(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
}

// An unknown issuer is refused before any network call.
func TestUnknownIssuerMakesNoFetch(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v := urlVerifier(t, s, clk)
	clk.add(DefaultJWKSCacheTTL)
	_, err := v.Verify(context.Background(), compact(goodHeader(), with(goodClaims(), "iss", "https://evil.example/"), rs256(testKey())))
	wantTokenRefused(t, err, CounterRejectedIssuer, "iss")
	if s.hits.Load() != 1 {
		t.Fatalf("fetched for an unknown issuer (%d)", s.hits.Load())
	}
}

func TestJWKSKeysThatCannotVerifyRS256(t *testing.T) {
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	set := jwk.NewSet()
	add := func(raw any, kv ...any) {
		k, err := jwk.Import(raw)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(kv); i += 2 {
			if err := k.Set(kv[i].(string), kv[i+1]); err != nil {
				t.Fatal(err)
			}
		}
		if err := set.AddKey(k); err != nil {
			t.Fatal(err)
		}
	}
	add(&ec.PublicKey, jwk.KeyIDKey, "ec")
	add(&testKey().PublicKey, jwk.KeyIDKey, "rs512", jwk.AlgorithmKey, jwa.RS512())
	add(&testKey().PublicKey, jwk.KeyIDKey, "enc", jwk.KeyUsageKey, "enc")
	add(&testKey().PublicKey, jwk.KeyIDKey, "dup")
	add(&otherKey().PublicKey, jwk.KeyIDKey, "dup")
	add(&testKey().PublicKey) // no kid: unreachable
	add(&testKey().PublicKey, jwk.KeyIDKey, "sig", jwk.KeyUsageKey, "sig", jwk.AlgorithmKey, jwa.RS256())
	add([]byte("0123456789abcdef0123456789abcdef"), jwk.KeyIDKey, "oct")
	v := staticVerifier(t, set, func() time.Time { return testNow })
	for kid, counter := range map[string]string{
		"ec": CounterRejectedAlgorithm, "rs512": CounterRejectedAlgorithm,
		"enc": CounterRejectedAlgorithm, "dup": CounterRejectedAlgorithm, "oct": CounterRejectedAlgorithm,
		"sig": CounterAccepted, "": CounterRejectedKID,
	} {
		_, err := v.Verify(context.Background(), compact(with(goodHeader(), "kid", kid), goodClaims(), rs256(testKey())))
		if counter == CounterAccepted {
			if err != nil {
				t.Errorf("kid %q: %v", kid, err)
			}
			continue
		}
		var te *TokenError
		if !errors.As(err, &te) || te.Counter != counter || te.Claim != "kid" {
			t.Errorf("kid %q: got %v, want %s on kid", kid, err, counter)
		}
	}
}

func shortKeyJWKS() []byte {
	k := mustRSA(1024)
	return []byte(fmt.Sprintf(`{"keys":[{"kty":"RSA","kid":%q,"n":%q,"e":"AQAB"}]}`, testKID, b64(k.N.Bytes())))
}

func TestVerifyConcurrent(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v := urlVerifier(t, s, clk)
	// Valid across the TTL jump below.
	good := compact(goodHeader(), with(goodClaims(), "exp", testNow.Add(2*DefaultJWKSCacheTTL).Unix()), rs256(testKey()))
	unknown := tokenAt(testKey(), "k-x", testNow)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 20 {
				if g == 0 && i == 10 {
					clk.add(DefaultJWKSCacheTTL)
				}
				if _, err := v.Verify(context.Background(), good); err != nil {
					t.Error(err)
				}
				if _, err := v.Verify(context.Background(), unknown); err == nil {
					t.Error("unknown kid accepted")
				}
			}
		})
	}
	wg.Wait()
	if got := v.Counters().Get(CounterAccepted); got != 160 {
		t.Errorf("accepted %d", got)
	}
	if got := s.hits.Load(); got > 3 {
		t.Errorf("%d fetches: the refresh is not rate-limited across goroutines", got)
	}
}

func FuzzVerifyJWT(f *testing.F) {
	key := testKey()
	// The vector tokens are signed by another (discarded) key: they reach
	// the signature check here and every refusal before it.
	for _, s := range jwtVectorTokens(f) {
		f.Add(s)
	}
	f.Add(compact(goodHeader(), goodClaims(), rs256(key)))
	f.Add("")
	f.Add("..")
	f.Add("e30.e30.")
	v := staticVerifier(f, publicSet(f, testKID, key), func() time.Time { return testNow })
	f.Fuzz(func(t *testing.T, token string) {
		c, err := v.Verify(context.Background(), token)
		if err != nil {
			var te *TokenError
			if !errors.As(err, &te) || te.Counter == "" || te.Claim == "" {
				t.Fatalf("refusal %v is not a counted TokenError", err)
			}
			return
		}
		if c.Issuer != testIss || c.Audience != testAud || c.KeyID != testKID || c.JTI == "" {
			t.Fatalf("accepted inconsistent claims %+v", c)
		}
	})
}

func BenchmarkVerifyJWT(b *testing.B) {
	v := staticVerifier(b, publicSet(b, testKID, testKey()), func() time.Time { return testNow })
	tok := compact(goodHeader(), goodClaims(), rs256(testKey()))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := v.Verify(ctx, tok); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIssue(b *testing.B) {
	is, err := NewIssuer(testIss, testKey(), testKID)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := is.Issue("op-1", testAud, []string{"rid.read"}, time.Minute, testNow); err != nil {
			b.Fatal(err)
		}
	}
}
