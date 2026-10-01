package auth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
)

const (
	pubA = "ansp-1"
	pubB = "ansp-2"
)

var testPayload = []byte(`{"type":"cis/change/v1","seq":7,"note":"a.b.c"}`)

func staticDetached(t testing.TB, sets map[string]jwk.Set, now func() time.Time) *DetachedVerifier {
	t.Helper()
	pubs := make(map[string]IssuerConfig, len(sets))
	for p, s := range sets {
		pubs[p] = IssuerConfig{Keys: s}
	}
	v, err := NewDetachedVerifier(context.Background(), DetachedConfig{Publishers: pubs, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func goodDetachedHeader(iat int64) map[string]any {
	return map[string]any{"alg": "RS256", "kid": testKID, "iat": iat, "b64": false, "crit": []string{"b64"}}
}

// detachedByHand builds <protected>..<signature> from the RFC 7797 text,
// without the library: the signing input is ASCII(BASE64URL(protected))
// || '.' || payload.
func detachedByHand(protected []byte, payload []byte, sign func([]byte) []byte) string {
	h := b64(protected)
	input := append([]byte(h+"."), payload...)
	return h + ".." + b64(sign(input))
}

func detachedJSON(hdr map[string]any, payload []byte, sign func([]byte) []byte) string {
	raw, err := json.Marshal(hdr)
	if err != nil {
		panic(err)
	}
	return detachedByHand(raw, payload, sign)
}

// Each refusal beside the accepted value that differs from it in one
// thing (E-01).
func TestDetachedRefusalsBesideAcceptance(t *testing.T) {
	key := testKey()
	now := testNow.Unix()
	v := staticDetached(t, map[string]jwk.Set{
		pubA: publicSet(t, testKID, key),
		pubB: publicSet(t, "kid-b", otherKey()),
	}, func() time.Time { return testNow })
	good := detachedJSON(goodDetachedHeader(now), testPayload, rs256(key))
	hdr := goodDetachedHeader(now)
	sign := func(h map[string]any) string { return detachedJSON(h, testPayload, rs256(key)) }
	es, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h64 := strings.Split(good, ".")[0]
	s64 := strings.Split(good, ".")[2]
	maxAge := int64(DefaultDetachedMaxAge / time.Second)
	skew := int64(DefaultMaxSkew / time.Second)

	type tc struct {
		name, publisher, header string
		payload                 []byte
		counter, claim          string // empty: accepted
	}
	cases := []tc{
		{"accept", pubA, good, testPayload, "", ""},
		{"accept-unknown-header-member", pubA, sign(with(hdr, "x-trace", "abc")), testPayload, "", ""},
		{"accept-empty-payload", pubA, detachedJSON(hdr, nil, rs256(key)), nil, "", ""},
		{"refuse-payload-one-byte-changed", pubA, good, append(slices.Clone(testPayload[:len(testPayload)-1]), ']'), CounterRejectedSignature, "signature"},
		{"refuse-signature-changed", pubA, h64 + ".." + b64(rs256(otherKey())([]byte(h64+"."+string(testPayload)))), testPayload, CounterRejectedSignature, "signature"},
		{"refuse-attached-payload", pubA, h64 + "." + b64(testPayload) + "." + s64, testPayload, CounterRejectedMalformed, "header"},
		{"refuse-one-dot", pubA, h64 + "." + s64, testPayload, CounterRejectedMalformed, "header"},
		{"refuse-three-dots", pubA, h64 + "..." + s64, testPayload, CounterRejectedMalformed, "header"},
		{"refuse-empty", pubA, "", testPayload, CounterRejectedMalformed, "header"},
		{"refuse-no-protected", pubA, ".." + s64, testPayload, CounterRejectedMalformed, "header"},
		{"refuse-empty-signature", pubA, h64 + "..", testPayload, CounterRejectedSignature, "signature"},
		{"refuse-protected-not-base64url", pubA, "e30=.." + s64, testPayload, CounterRejectedMalformed, "header"},
		{"refuse-signature-not-base64url", pubA, h64 + "..a+b", testPayload, CounterRejectedMalformed, "header"},
		{"refuse-protected-not-an-object", pubA, detachedByHand([]byte(`[1]`), testPayload, rs256(key)), testPayload, CounterRejectedMalformed, "header"},
		{"refuse-protected-trailing-data", pubA, detachedByHand([]byte(`{"alg":"RS256"}{}`), testPayload, rs256(key)), testPayload, CounterRejectedMalformed, "header"},
		{"refuse-repeated-member", pubA, detachedByHand([]byte(`{"alg":"RS256","kid":"k1","iat":`+itoa64(now)+`,"b64":false,"b64":true,"crit":["b64"]}`), testPayload, rs256(key)), testPayload, CounterRejectedMalformed, "header"},
		{"refuse-alg-missing", pubA, sign(with(hdr, "alg", nil)), testPayload, CounterRejectedAlgorithm, "alg"},
		{"refuse-alg-none", pubA, detachedJSON(with(hdr, "alg", "none"), testPayload, func([]byte) []byte { return []byte{0} }), testPayload, CounterRejectedAlgorithm, "alg"},
		{"refuse-alg-hs256-public-key", pubA, detachedJSON(with(hdr, "alg", "HS256"), testPayload, hs256(mustPKIX(&key.PublicKey))), testPayload, CounterRejectedAlgorithm, "alg"},
		{"refuse-alg-ps256", pubA, detachedJSON(with(hdr, "alg", "PS256"), testPayload, ps256(key)), testPayload, CounterRejectedAlgorithm, "alg"},
		{"refuse-alg-es256", pubA, detachedJSON(with(hdr, "alg", "ES256"), testPayload, es256(es)), testPayload, CounterRejectedAlgorithm, "alg"},
		{"refuse-b64-absent", pubA, sign(with(hdr, "b64", nil, "crit", nil)), testPayload, CounterRejectedB64, "b64"},
		{"refuse-b64-true", pubA, sign(with(hdr, "b64", true)), testPayload, CounterRejectedB64, "b64"},
		{"refuse-b64-string", pubA, sign(with(hdr, "b64", "false")), testPayload, CounterRejectedB64, "b64"},
		{"refuse-crit-without-b64", pubA, sign(with(hdr, "crit", nil)), testPayload, CounterRejectedB64, "crit"},
		{"refuse-crit-unknown-member", pubA, sign(with(hdr, "crit", []string{"b64", "exp"}, "exp", 1)), testPayload, CounterRejectedCrit, "crit"},
		{"refuse-crit-repeated", pubA, sign(with(hdr, "crit", []string{"b64", "b64"})), testPayload, CounterRejectedCrit, "crit"},
		{"refuse-crit-empty", pubA, sign(with(hdr, "crit", []string{})), testPayload, CounterRejectedCrit, "crit"},
		{"refuse-crit-not-an-array", pubA, sign(with(hdr, "crit", "b64")), testPayload, CounterRejectedCrit, "crit"},
		{"refuse-kid-missing", pubA, sign(with(hdr, "kid", nil)), testPayload, CounterRejectedKID, "kid"},
		{"refuse-kid-unknown", pubA, sign(with(hdr, "kid", "k-x")), testPayload, CounterRejectedKID, "kid"},
		{"refuse-publisher-unknown", "ansp-9", good, testPayload, CounterRejectedPublisher, "publisher"},
		{"refuse-publisher-a-key-under-b", pubB, good, testPayload, CounterRejectedKID, "kid"},
		{"refuse-publisher-b-kid-signed-by-a", pubB, sign(with(hdr, "kid", "kid-b")), testPayload, CounterRejectedSignature, "signature"},
		{"refuse-iat-missing", pubA, sign(with(hdr, "iat", nil)), testPayload, CounterRejectedIAT, "iat"},
		{"refuse-iat-not-a-number", pubA, sign(with(hdr, "iat", "1790000000")), testPayload, CounterRejectedIAT, "iat"},
		{"accept-iat-at-max-age", pubA, sign(with(hdr, "iat", now-maxAge)), testPayload, "", ""},
		{"refuse-iat-one-second-past-max-age", pubA, sign(with(hdr, "iat", now-maxAge-1)), testPayload, CounterRejectedIAT, "iat"},
		{"accept-iat-ahead-by-max-skew", pubA, sign(with(hdr, "iat", now+skew)), testPayload, "", ""},
		{"refuse-iat-one-second-past-max-skew", pubA, sign(with(hdr, "iat", now+skew+1)), testPayload, CounterRejectedIAT, "iat"},
		{"refuse-header-too-large", pubA, sign(with(hdr, "pad", strings.Repeat("x", DefaultMaxTokenBytes))), testPayload, CounterRejectedTooLarge, "header"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := v.Counters().Get(c.counter)
			if c.counter == "" {
				before = v.Counters().Get(CounterAccepted)
			}
			s, err := v.Verify(context.Background(), c.publisher, c.header, c.payload)
			if c.counter == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if s.Publisher != c.publisher || s.KID != testKID || s.IssuedAt.IsZero() {
					t.Errorf("signature %+v", s)
				}
				if v.Counters().Get(CounterAccepted) != before+1 {
					t.Error("accepted not counted")
				}
				return
			}
			wantTokenRefused(t, err, c.counter, c.claim)
			if s != (Signature{}) {
				t.Errorf("a refusal returned %+v", s)
			}
			if v.Counters().Get(c.counter) != before+1 {
				t.Errorf("%s not counted", c.counter)
			}
		})
	}
}

func itoa64(n int64) string { b, _ := json.Marshal(n); return string(b) }

func hs256(secret []byte) func([]byte) []byte {
	return func(in []byte) []byte {
		m := hmac.New(sha256.New, secret)
		m.Write(in)
		return m.Sum(nil)
	}
}

func ps256(key *rsa.PrivateKey) func([]byte) []byte {
	return func(in []byte) []byte {
		h := sha256.Sum256(in)
		s, err := rsa.SignPSS(rand.Reader, key, crypto.SHA256, h[:], nil)
		if err != nil {
			panic(err)
		}
		return s
	}
}

func es256(key *ecdsa.PrivateKey) func([]byte) []byte {
	return func(in []byte) []byte {
		h := sha256.Sum256(in)
		s, err := ecdsa.SignASN1(rand.Reader, key, h[:])
		if err != nil {
			panic(err)
		}
		return s
	}
}

// E-02: the accepted Signature read field by field.
func TestDetachedAcceptedSignatureFields(t *testing.T) {
	r := mustRing(t, SigningKey{KID: testKID, Key: testKey()})
	signedAt := testNow.Add(-90*time.Second + 400*time.Millisecond)
	sig, err := r.SignDetached(testPayload, signedAt)
	if err != nil {
		t.Fatal(err)
	}
	v := staticDetached(t, map[string]jwk.Set{pubA: r.JWKS()}, func() time.Time { return testNow })
	s, err := v.Verify(context.Background(), pubA, sig, testPayload)
	if err != nil {
		t.Fatal(err)
	}
	want := Signature{Publisher: pubA, KID: testKID, IssuedAt: time.Unix(signedAt.Unix(), 0).UTC()}
	if s != want {
		t.Errorf("got %+v, want %+v", s, want)
	}
}

// Interop both ways with the library called directly.
func TestDetachedInterop(t *testing.T) {
	key := testKey()
	sig, err := SignDetached(SigningKey{KID: testKID, Key: key}, testPayload, testNow)
	if err != nil {
		t.Fatal(err)
	}
	got, err := jws.Verify([]byte(sig), jws.WithKey(jwa.RS256(), &key.PublicKey), jws.WithDetachedPayload(testPayload))
	if err != nil || !bytes.Equal(got, testPayload) {
		t.Fatalf("jws.Verify refused SignDetached: %v", err)
	}
	if _, err := jws.Verify([]byte(sig), jws.WithKey(jwa.RS256(), &key.PublicKey), jws.WithDetachedPayload([]byte("other"))); err == nil {
		t.Fatal("jws.Verify accepted another payload")
	}

	hdr := jws.NewHeaders()
	for k, val := range map[string]any{jws.KeyIDKey: testKID, "iat": testNow.Unix(), jws.B64Key: false} {
		if err := hdr.Set(k, val); err != nil {
			t.Fatal(err)
		}
	}
	direct, err := jws.Sign(nil, jws.WithKey(jwa.RS256(), key, jws.WithProtectedHeaders(hdr)), jws.WithDetachedPayload(testPayload))
	if err != nil {
		t.Fatal(err)
	}
	v := staticDetached(t, map[string]jwk.Set{pubA: publicSet(t, testKID, key)}, func() time.Time { return testNow })
	if _, err := v.Verify(context.Background(), pubA, string(direct), testPayload); err != nil {
		t.Fatalf("DetachedVerifier refused jws.Sign: %v", err)
	}
}

func TestSignDetachedRefusesABadKey(t *testing.T) {
	_, err := SignDetached(SigningKey{KID: testKID}, testPayload, testNow)
	wantFieldError(t, err, "key")
	_, err = SignDetached(SigningKey{Key: testKey()}, testPayload, testNow)
	wantFieldError(t, err, "kid")
	if _, err := SignDetached(SigningKey{KID: testKID, Key: testKey()}, testPayload, testNow); err != nil {
		t.Fatal(err)
	}
}

// E-10: the payload bound, with the payload at the bound accepted.
func TestDetachedPayloadBound(t *testing.T) {
	key := testKey()
	v, err := NewDetachedVerifier(context.Background(), DetachedConfig{
		Publishers:      map[string]IssuerConfig{pubA: {Keys: publicSet(t, testKID, key)}},
		MaxPayloadBytes: int64(len(testPayload)),
		MaxHeaderBytes:  500,
		Now:             func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	good := detachedJSON(goodDetachedHeader(testNow.Unix()), testPayload, rs256(key))
	if len(good) > 500 {
		t.Fatalf("the test header is %d bytes", len(good))
	}
	if _, err := v.Verify(context.Background(), pubA, good, testPayload); err != nil {
		t.Fatalf("a payload at the bound was refused: %v", err)
	}
	longer := append(slices.Clone(testPayload), ' ')
	_, err = v.Verify(context.Background(), pubA, detachedJSON(goodDetachedHeader(testNow.Unix()), longer, rs256(key)), longer)
	wantTokenRefused(t, err, CounterRejectedTooLarge, "payload")
	padded := detachedJSON(with(goodDetachedHeader(testNow.Unix()), "pad", strings.Repeat("x", 100)), testPayload, rs256(key))
	_, err = v.Verify(context.Background(), pubA, padded, testPayload)
	wantTokenRefused(t, err, CounterRejectedTooLarge, "header")
}

func TestParseDetachedHeader(t *testing.T) {
	sig, err := SignDetached(SigningKey{KID: testKID, Key: testKey()}, testPayload, testNow)
	if err != nil {
		t.Fatal(err)
	}
	h, err := ParseDetachedHeader(sig)
	if err != nil {
		t.Fatal(err)
	}
	want := DetachedHeader{Alg: "RS256", KID: testKID, IssuedAt: testNow, B64: false, Crit: []string{"b64"}}
	if h.Alg != want.Alg || h.KID != want.KID || !h.IssuedAt.Equal(want.IssuedAt) || h.B64 || !slices.Equal(h.Crit, want.Crit) {
		t.Errorf("got %+v, want %+v", h, want)
	}
	// b64 absent reads as its RFC 7797 default, true; a bad iat or crit
	// reads as zero and nil without refusing.
	h, err = ParseDetachedHeader(detachedByHand([]byte(`{"alg":"RS256","iat":"x","crit":"b64"}`), nil, rs256(testKey())))
	if err != nil {
		t.Fatal(err)
	}
	if !h.B64 || !h.IssuedAt.IsZero() || h.Crit != nil {
		t.Errorf("got %+v", h)
	}
	for _, bad := range []string{"", "a.b.c", "..e30", "e30=..", strings.Repeat("e", DefaultMaxTokenBytes+1)} {
		if _, err := ParseDetachedHeader(bad); err == nil {
			t.Errorf("%.20q parsed", bad)
		}
	}
	_, err = ParseDetachedHeader(strings.Repeat("e", DefaultMaxTokenBytes+1))
	wantTokenRefused(t, err, CounterRejectedTooLarge, "header")
	_, err = ParseDetachedHeader("e30.x.e30")
	wantTokenRefused(t, err, CounterRejectedMalformed, "header")
}

func TestNewDetachedVerifierRefusesBadConfig(t *testing.T) {
	keys := map[string]IssuerConfig{pubA: {Keys: publicSet(t, testKID, testKey())}}
	for _, tc := range []struct {
		name  string
		c     DetachedConfig
		field string
	}{
		{"no publishers", DetachedConfig{}, "publishers"},
		{"empty publisher", DetachedConfig{Publishers: map[string]IssuerConfig{"": {Keys: jwk.NewSet()}}}, "publishers"},
		{"neither URL nor keys", DetachedConfig{Publishers: map[string]IssuerConfig{pubA: {}}}, "publishers." + pubA},
		{"plain http", DetachedConfig{Publishers: map[string]IssuerConfig{pubA: {JWKSURL: "http://example.com/jwks"}}}, "publishers." + pubA},
		{"negative max age", DetachedConfig{Publishers: keys, MaxAge: -1}, "config"},
		{"negative payload bound", DetachedConfig{Publishers: keys, MaxPayloadBytes: -1}, "config"},
		{"negative header bound", DetachedConfig{Publishers: keys, MaxHeaderBytes: -1}, "config"},
		{"negative skew", DetachedConfig{Publishers: keys, MaxSkew: -1}, "config"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewDetachedVerifier(context.Background(), tc.c)
			wantFieldError(t, err, tc.field)
		})
	}
	if _, err := NewDetachedVerifier(context.Background(), DetachedConfig{Publishers: keys}); err != nil {
		t.Fatal(err)
	}
}

// E-02: a URL-configured publisher; an unknown publisher makes no fetch;
// a refresh failure keeps the cached set and is counted.
func TestDetachedJWKSRefreshFailureKeepsCachedSet(t *testing.T) {
	s := newJWKSServer(t, publicSet(t, testKID, testKey()))
	clk := newClock(testNow)
	v, err := NewDetachedVerifier(context.Background(), DetachedConfig{
		Publishers: map[string]IssuerConfig{pubA: {JWKSURL: s.URL + "/jwks"}},
		Now:        clk.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	signNow := func() string {
		sig, err := SignDetached(SigningKey{KID: testKID, Key: testKey()}, testPayload, clk.now())
		if err != nil {
			t.Fatal(err)
		}
		return sig
	}
	hits := s.hits.Load()
	_, err = v.Verify(ctx, "ansp-9", signNow(), testPayload)
	wantTokenRefused(t, err, CounterRejectedPublisher, "publisher")
	if s.hits.Load() != hits {
		t.Fatal("an unknown publisher made a fetch")
	}
	s.setBody(http.StatusInternalServerError, []byte("down"))
	clk.add(DefaultJWKSCacheTTL)
	if _, err := v.Verify(ctx, pubA, signNow(), testPayload); err != nil {
		t.Fatalf("the cached key was dropped: %v", err)
	}
	v.kv.background.Wait()
	if v.Counters().Get(CounterJWKSRefreshFailed) != 1 {
		t.Fatalf("jwks_refresh_failed %d", v.Counters().Get(CounterJWKSRefreshFailed))
	}
	if _, err := v.Verify(ctx, pubA, signNow(), testPayload); err != nil {
		t.Fatalf("the cached key was dropped after the failure: %v", err)
	}
	// A rotated key is learnt from the next good fetch.
	s.setKeys(t, publicSet(t, "k2", otherKey()))
	clk.add(DefaultMinRefreshInterval)
	rotated, err := SignDetached(SigningKey{KID: "k2", Key: otherKey()}, testPayload, clk.now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Verify(ctx, pubA, rotated, testPayload); err != nil {
		t.Fatalf("the rotated key was not fetched: %v", err)
	}
}

func FuzzVerifyDetached(f *testing.F) {
	key := testKey()
	now := testNow.Unix()
	hdr := goodDetachedHeader(now)
	f.Add(detachedJSON(hdr, testPayload, rs256(key)), testPayload)
	f.Add(detachedJSON(hdr, nil, rs256(key)), []byte{})
	for _, h := range []map[string]any{
		with(hdr, "alg", "HS256"), with(hdr, "alg", nil), with(hdr, "b64", true), with(hdr, "b64", nil),
		with(hdr, "crit", nil), with(hdr, "crit", []string{"b64", "x"}), with(hdr, "kid", "k-x"), with(hdr, "kid", nil),
		with(hdr, "iat", nil), with(hdr, "iat", now-3600), with(hdr, "iat", now+3600),
	} {
		f.Add(detachedJSON(h, testPayload, rs256(key)), testPayload)
	}
	f.Add("", []byte(nil))
	f.Add("..", []byte("x"))
	f.Add("e30.e30.e30", []byte("x"))
	f.Add(strings.Repeat("e", 600), []byte("x"))
	v, err := NewDetachedVerifier(context.Background(), DetachedConfig{
		Publishers:      map[string]IssuerConfig{pubA: {Keys: publicSet(f, testKID, key)}},
		MaxHeaderBytes:  512,
		MaxPayloadBytes: 1024,
		Now:             func() time.Time { return testNow },
	})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, header string, payload []byte) {
		s, err := v.Verify(context.Background(), pubA, header, payload)
		if err != nil {
			var te *TokenError
			if !errors.As(err, &te) || te.Counter == "" || te.Claim == "" {
				t.Fatalf("refusal %v is not a counted TokenError", err)
			}
			if (len(header) > 512 || len(payload) > 1024) && te.Counter != CounterRejectedTooLarge {
				t.Fatalf("past a bound but refused as %s", te.Counter)
			}
			return
		}
		if s.Publisher != pubA || s.KID != testKID || s.IssuedAt.IsZero() {
			t.Fatalf("accepted %+v", s)
		}
		if _, err := ParseDetachedHeader(header); err != nil {
			t.Fatalf("accepted a value ParseDetachedHeader refuses: %v", err)
		}
	})
}

func BenchmarkSignDetached(b *testing.B) {
	r := mustRing(b, SigningKey{KID: testKID, Key: testKey()})
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.SignDetached(testPayload, testNow); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVerifyDetached(b *testing.B) {
	v := staticDetached(b, map[string]jwk.Set{pubA: publicSet(b, testKID, testKey())}, func() time.Time { return testNow })
	sig := detachedJSON(goodDetachedHeader(testNow.Unix()), testPayload, rs256(testKey()))
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := v.Verify(ctx, pubA, sig, testPayload); err != nil {
			b.Fatal(err)
		}
	}
}

// decodeObject accepts exactly one JSON object with distinct member
// names (E-01: each refusal beside an accepted spelling).
func TestDecodeObject(t *testing.T) {
	for _, ok := range []string{`{}`, ` {"a":1} `, `{"a":{"a":1},"b":[1,"a"]}`, "{\"a\":1}\n"} {
		if _, got := decodeObject([]byte(ok)); !got {
			t.Errorf("%s refused", ok)
		}
	}
	for _, bad := range []string{``, `[]`, `"x"`, `{"a":}`, `{"a":1`, `{"a" 1}`, `{"a":1}x`, `{} {}`, `{"a":1,"a":1}`, `{1:2}`, `{"a":1,}`} {
		if _, got := decodeObject([]byte(bad)); got {
			t.Errorf("%s accepted", bad)
		}
	}
}
