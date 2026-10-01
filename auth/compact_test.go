package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

const (
	cispIss  = "https://cisp.example/"
	hostA    = "ussp-1.example"
	hostB    = "ussp-1.internal.example"
	testBody = "{ \"type\": \"cis/change/v1\",\n  \"seq\": 7 }"
)

func goodCompactClaims() CompactClaims {
	return CompactClaims{Issuer: cispIss, Audience: hostA, Subject: "cisp", JTI: "delivery-1"}
}

func staticCompact(t testing.TB, set jwk.Set, now func() time.Time) *CompactVerifier {
	t.Helper()
	v, err := NewCompactVerifier(context.Background(), CompactConfig{
		Issuers:   map[string]IssuerConfig{cispIss: {Keys: set}},
		Audiences: []string{hostA, hostB},
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func compactPayload(iat int64) map[string]any {
	return map[string]any{
		"iss": cispIss, "aud": hostA, "sub": "cisp", "iat": iat, "jti": "delivery-1",
		"body": json.RawMessage(`{"type":"cis/change/v1"}`),
	}
}

// Each refusal beside the accepted token that differs from it in one
// header member or claim (E-01).
func TestCompactRefusalsBesideAcceptance(t *testing.T) {
	key := testKey()
	now := testNow.Unix()
	v := staticCompact(t, publicSet(t, testKID, key), func() time.Time { return testNow })
	hdr := goodHeader()
	pl := compactPayload(now)
	good := compact(hdr, pl, rs256(key))
	maxAge := int64(DefaultDetachedMaxAge / time.Second)
	skew := int64(DefaultMaxSkew / time.Second)
	parts := strings.Split(good, ".")
	byHand := func(h, p string) string {
		return b64([]byte(h)) + "." + b64([]byte(p)) + "." + b64(rs256(key)([]byte(b64([]byte(h))+"."+b64([]byte(p)))))
	}
	hdrJSON := `{"alg":"RS256","kid":"` + testKID + `"}`

	type tc struct {
		name, token    string
		counter, claim string
	}
	cases := []tc{
		{"accept", good, "", ""},
		{"accept-without-typ", compact(with(hdr, "typ", nil), pl, rs256(key)), "", ""},
		{"accept-aud-array-with-second-host", compact(hdr, with(pl, "aud", []string{"x.example", hostB}), rs256(key)), "", ""},
		{"accept-iat-at-max-age", compact(hdr, with(pl, "iat", now-maxAge), rs256(key)), "", ""},
		{"accept-iat-ahead-by-max-skew", compact(hdr, with(pl, "iat", now+skew), rs256(key)), "", ""},
		{"refuse-too-large", compact(hdr, with(pl, "pad", strings.Repeat("x", DefaultMaxTokenBytes)), rs256(key)), CounterRejectedTooLarge, "token"},
		{"refuse-two-parts", parts[0] + "." + parts[1], CounterRejectedMalformed, "token"},
		{"refuse-four-parts", good + ".x", CounterRejectedMalformed, "token"},
		{"refuse-header-not-base64url", "e30=." + parts[1] + "." + parts[2], CounterRejectedMalformed, "token"},
		{"refuse-header-not-an-object", byHand(`[]`, `{}`), CounterRejectedMalformed, "token"},
		{"refuse-header-repeated-member", byHand(`{"alg":"RS256","kid":"k1","kid":"k2"}`, `{}`), CounterRejectedMalformed, "token"},
		{"refuse-payload-not-base64url", parts[0] + ".e30=." + parts[2], CounterRejectedMalformed, "token"},
		{"refuse-payload-not-an-object", byHand(hdrJSON, `"x"`), CounterRejectedMalformed, "token"},
		{"refuse-payload-repeated-member", byHand(hdrJSON, `{"iss":"`+cispIss+`","iss":"x"}`), CounterRejectedMalformed, "token"},
		{"refuse-alg-missing", compact(with(hdr, "alg", nil), pl, rs256(key)), CounterRejectedAlgorithm, "alg"},
		{"refuse-alg-none", compact(with(hdr, "alg", "none"), pl, func([]byte) []byte { return nil }), CounterRejectedAlgorithm, "alg"},
		{"refuse-alg-hs256-public-key", compact(with(hdr, "alg", "HS256"), pl, hs256(mustPKIX(&key.PublicKey))), CounterRejectedAlgorithm, "alg"},
		{"refuse-alg-ps256", compact(with(hdr, "alg", "PS256"), pl, ps256(key)), CounterRejectedAlgorithm, "alg"},
		{"refuse-crit", compact(with(hdr, "crit", []string{"x"}, "x", 1), pl, rs256(key)), CounterRejectedCrit, "crit"},
		{"refuse-b64", compact(with(hdr, "b64", true), pl, rs256(key)), CounterRejectedB64, "b64"},
		{"refuse-kid-missing", compact(with(hdr, "kid", nil), pl, rs256(key)), CounterRejectedKID, "kid"},
		{"refuse-kid-unknown", compact(with(hdr, "kid", "k-x"), pl, rs256(key)), CounterRejectedKID, "kid"},
		{"refuse-iss-missing", compact(hdr, with(pl, "iss", nil), rs256(key)), CounterRejectedIssuer, "iss"},
		{"refuse-iss-unknown", compact(hdr, with(pl, "iss", "https://ansp.example/"), rs256(key)), CounterRejectedIssuer, "iss"},
		{"refuse-signature", compact(hdr, pl, rs256(otherKey())), CounterRejectedSignature, "signature"},
		{"refuse-tampered-body", parts[0] + "." + b64JSON(with(pl, "body", json.RawMessage(`{"type":"other"}`))) + "." + parts[2], CounterRejectedSignature, "signature"},
		{"refuse-aud-other-host", compact(hdr, with(pl, "aud", "ussp-2.example"), rs256(key)), CounterRejectedAudience, "aud"},
		{"refuse-aud-missing", compact(hdr, with(pl, "aud", nil), rs256(key)), CounterRejectedAudience, "aud"},
		{"refuse-aud-number", compact(hdr, with(pl, "aud", 1), rs256(key)), CounterRejectedAudience, "aud"},
		{"refuse-iat-missing", compact(hdr, with(pl, "iat", nil), rs256(key)), CounterRejectedIAT, "iat"},
		{"refuse-iat-one-second-past-max-age", compact(hdr, with(pl, "iat", now-maxAge-1), rs256(key)), CounterRejectedIAT, "iat"},
		{"refuse-iat-one-second-past-max-skew", compact(hdr, with(pl, "iat", now+skew+1), rs256(key)), CounterRejectedIAT, "iat"},
		{"refuse-sub-empty", compact(hdr, with(pl, "sub", ""), rs256(key)), CounterRejectedClaims, "sub"},
		{"refuse-jti-missing", compact(hdr, with(pl, "jti", nil), rs256(key)), CounterRejectedClaims, "jti"},
		{"refuse-body-missing", compact(hdr, with(pl, "body", nil), rs256(key)), CounterRejectedClaims, "body"},
		{"refuse-body-not-an-object", compact(hdr, with(pl, "body", "x"), rs256(key)), CounterRejectedClaims, "body"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name := c.counter
			if name == "" {
				name = CounterAccepted
			}
			before := v.Counters().Get(name)
			cl, body, err := v.Verify(context.Background(), c.token)
			if v.Counters().Get(name) != before+1 {
				t.Errorf("%s not counted", name)
			}
			if c.counter == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if cl.Issuer != cispIss || cl.JTI == "" || body == nil {
					t.Errorf("claims %+v body %s", cl, body)
				}
				return
			}
			wantTokenRefused(t, err, c.counter, c.claim)
			if cl != (CompactClaims{}) || body != nil {
				t.Errorf("a refusal returned %+v and a body", cl)
			}
		})
	}
}

// E-02: SignCompact's output read back field by field; the body keeps its
// bytes; the second audience is reported when it is the one matched.
func TestCompactRoundTrip(t *testing.T) {
	r := mustRing(t, SigningKey{KID: testKID, Key: testKey()})
	v := staticCompact(t, r.JWKS(), func() time.Time { return testNow })
	signedAt := testNow.Add(-time.Minute + 300*time.Millisecond)
	for _, aud := range []string{hostA, hostB} {
		cl := goodCompactClaims()
		cl.Audience = aud
		cl.IssuedAt = time.Unix(1, 0) // not read: iat is now
		tok, err := r.SignCompact(cl, json.RawMessage(testBody), signedAt)
		if err != nil {
			t.Fatal(err)
		}
		got, body, err := v.Verify(context.Background(), tok)
		if err != nil {
			t.Fatal(err)
		}
		want := CompactClaims{Issuer: cispIss, Audience: aud, Subject: "cisp", JTI: "delivery-1", IssuedAt: time.Unix(signedAt.Unix(), 0).UTC()}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if string(body) != testBody {
			t.Errorf("body %q, want the producer's bytes %q", body, testBody)
		}
		// The header is alg, kid and typ JWT; the payload is the five
		// claims and body.
		parts := strings.Split(tok, ".")
		var hdr, pl map[string]json.RawMessage
		mustDecodeB64JSON(t, parts[0], &hdr)
		mustDecodeB64JSON(t, parts[1], &pl)
		if string(hdr["alg"]) != `"RS256"` || string(hdr["kid"]) != `"`+testKID+`"` || string(hdr["typ"]) != `"JWT"` || len(hdr) != 3 {
			t.Errorf("header %v", hdr)
		}
		for _, k := range []string{"iss", "aud", "sub", "iat", "jti", "body"} {
			if _, ok := pl[k]; !ok {
				t.Errorf("payload lacks %s", k)
			}
		}
		if len(pl) != 6 {
			t.Errorf("payload members %d", len(pl))
		}
	}
}

func mustDecodeB64JSON(t *testing.T, s string, v any) {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
}

// Interop: the delivery is a JWT any JWT library reads, and a token the
// library signs with the same payload verifies here.
func TestCompactInterop(t *testing.T) {
	key := testKey()
	tok, err := SignCompact(SigningKey{KID: testKID, Key: key}, goodCompactClaims(), json.RawMessage(testBody), testNow)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse([]byte(tok), jwt.WithKey(jwa.RS256(), &key.PublicKey), jwt.WithValidate(false))
	if err != nil {
		t.Fatal(err)
	}
	if iss, _ := parsed.Issuer(); iss != cispIss {
		t.Errorf("iss %s", iss)
	}
	hdr := jws.NewHeaders()
	if err := hdr.Set(jws.KeyIDKey, testKID); err != nil {
		t.Fatal(err)
	}
	pl, err := json.Marshal(compactPayload(testNow.Unix()))
	if err != nil {
		t.Fatal(err)
	}
	direct, err := jws.Sign(pl, jws.WithKey(jwa.RS256(), key, jws.WithProtectedHeaders(hdr)))
	if err != nil {
		t.Fatal(err)
	}
	v := staticCompact(t, publicSet(t, testKID, key), func() time.Time { return testNow })
	if _, _, err := v.Verify(context.Background(), string(direct)); err != nil {
		t.Fatalf("CompactVerifier refused jws.Sign: %v", err)
	}
}

func TestSignCompactRefusals(t *testing.T) {
	k := SigningKey{KID: testKID, Key: testKey()}
	body := json.RawMessage(`{}`)
	for _, tc := range []struct {
		name  string
		k     SigningKey
		cl    CompactClaims
		body  json.RawMessage
		field string
	}{
		{"no iss", k, CompactClaims{Audience: hostA, Subject: "s", JTI: "j"}, body, "iss"},
		{"no aud", k, CompactClaims{Issuer: cispIss, Subject: "s", JTI: "j"}, body, "aud"},
		{"no sub", k, CompactClaims{Issuer: cispIss, Audience: hostA, JTI: "j"}, body, "sub"},
		{"no jti", k, CompactClaims{Issuer: cispIss, Audience: hostA, Subject: "s"}, body, "jti"},
		{"body nil", k, goodCompactClaims(), nil, "body"},
		{"body an array", k, goodCompactClaims(), json.RawMessage(`[]`), "body"},
		{"body not JSON", k, goodCompactClaims(), json.RawMessage(`{"a":`), "body"},
		{"no key", SigningKey{KID: testKID}, goodCompactClaims(), body, "key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SignCompact(tc.k, tc.cl, tc.body, testNow)
			wantFieldError(t, err, tc.field)
		})
	}
	if _, err := SignCompact(k, goodCompactClaims(), body, testNow); err != nil {
		t.Fatal(err)
	}
}

func TestNewCompactVerifierRefusesBadConfig(t *testing.T) {
	keys := map[string]IssuerConfig{cispIss: {Keys: publicSet(t, testKID, testKey())}}
	for _, tc := range []struct {
		name  string
		c     CompactConfig
		field string
	}{
		{"no issuers", CompactConfig{Audiences: []string{hostA}}, "issuers"},
		{"no audiences", CompactConfig{Issuers: keys}, "audiences"},
		{"an empty audience", CompactConfig{Issuers: keys, Audiences: []string{hostA, ""}}, "audiences"},
		{"negative max age", CompactConfig{Issuers: keys, Audiences: []string{hostA}, MaxAge: -1}, "config"},
		{"negative token bound", CompactConfig{Issuers: keys, Audiences: []string{hostA}, MaxTokenBytes: -1}, "config"},
		{"plain http", CompactConfig{Issuers: map[string]IssuerConfig{cispIss: {JWKSURL: "http://example.com/"}}, Audiences: []string{hostA}}, "issuers." + cispIss},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCompactVerifier(context.Background(), tc.c)
			wantFieldError(t, err, tc.field)
		})
	}
}

// E-02: an unknown issuer makes no fetch; a refresh failure keeps the
// cached set.
func TestCompactJWKSRefreshFailureKeepsCachedSet(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v, err := NewCompactVerifier(context.Background(), CompactConfig{
		Issuers:   map[string]IssuerConfig{cispIss: {JWKSURL: s.URL + "/jwks"}},
		Audiences: []string{hostA},
		Now:       clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tok := func(cl CompactClaims) string {
		out, err := SignCompact(SigningKey{KID: testKID, Key: testKey()}, cl, json.RawMessage(`{}`), clk.now())
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	hits := s.hits.Load()
	other := goodCompactClaims()
	other.Issuer = "https://ansp.example/"
	_, _, err = v.Verify(ctx, tok(other))
	wantTokenRefused(t, err, CounterRejectedIssuer, "iss")
	if s.hits.Load() != hits {
		t.Fatal("an unknown issuer made a fetch")
	}
	s.setBody(http.StatusBadGateway, nil)
	clk.add(DefaultJWKSCacheTTL)
	if _, _, err := v.Verify(ctx, tok(goodCompactClaims())); err != nil {
		t.Fatalf("the cached key was dropped: %v", err)
	}
	v.kv.background.Wait()
	if v.Counters().Get(CounterJWKSRefreshFailed) != 1 {
		t.Fatalf("jwks_refresh_failed %d", v.Counters().Get(CounterJWKSRefreshFailed))
	}
	if _, _, err := v.Verify(ctx, tok(goodCompactClaims())); err != nil {
		t.Fatalf("the cached key was dropped after the failure: %v", err)
	}
}

func FuzzVerifyCompact(f *testing.F) {
	key := testKey()
	now := testNow.Unix()
	hdr := goodHeader()
	pl := compactPayload(now)
	f.Add(compact(hdr, pl, rs256(key)))
	for _, h := range []map[string]any{with(hdr, "alg", "none"), with(hdr, "kid", nil), with(hdr, "crit", []string{"b64"}), with(hdr, "b64", false)} {
		f.Add(compact(h, pl, rs256(key)))
	}
	for _, p := range []map[string]any{
		with(pl, "iss", nil), with(pl, "aud", "x"), with(pl, "iat", now-3600), with(pl, "iat", nil),
		with(pl, "sub", nil), with(pl, "jti", nil), with(pl, "body", nil), with(pl, "body", 1),
	} {
		f.Add(compact(hdr, p, rs256(key)))
	}
	f.Add("")
	f.Add("..")
	f.Add("e30.e30.")
	f.Add(strings.Repeat("e", 2000))
	v, err := NewCompactVerifier(context.Background(), CompactConfig{
		Issuers:       map[string]IssuerConfig{cispIss: {Keys: publicSet(f, testKID, key)}},
		Audiences:     []string{hostA},
		MaxTokenBytes: 1024,
		Now:           func() time.Time { return testNow },
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, token string) {
		cl, body, err := v.Verify(context.Background(), token)
		if err != nil {
			var te *TokenError
			if !errors.As(err, &te) || te.Counter == "" || te.Claim == "" {
				t.Fatalf("refusal %v is not a counted TokenError", err)
			}
			if len(token) > 1024 && te.Counter != CounterRejectedTooLarge {
				t.Fatalf("past the bound but refused as %s", te.Counter)
			}
			if body != nil || cl != (CompactClaims{}) {
				t.Fatal("a refusal returned claims or a body")
			}
			return
		}
		if cl.Issuer != cispIss || cl.Audience != hostA || cl.Subject == "" || cl.JTI == "" || !isJSONObject(body) {
			t.Fatalf("accepted %+v %s", cl, body)
		}
	})
}

func BenchmarkVerifyCompact(b *testing.B) {
	v := staticCompact(b, publicSet(b, testKID, testKey()), func() time.Time { return testNow })
	tok := compact(goodHeader(), compactPayload(testNow.Unix()), rs256(testKey()))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := v.Verify(ctx, tok); err != nil {
			b.Fatal(err)
		}
	}
}
