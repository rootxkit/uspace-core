package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/rootxkit/uspace-core/core"
)

func sessionVerifier(t testing.TB, set jwk.Set, aud string, strict bool, now func() time.Time) *Verifier {
	t.Helper()
	v, err := NewVerifier(context.Background(), Config{
		Issuers:             map[string]IssuerConfig{testIss: {Keys: set}},
		Audience:            aud,
		StrictSessionClaims: strict,
		Now:                 now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func goodSession() SessionClaims {
	return SessionClaims{
		Audience:  "authority.example",
		Subject:   "acct-7",
		Roles:     []string{"operator", "viewer"},
		Realm:     "console",
		IssuedAt:  testNow,
		ExpiresAt: testNow.Add(12 * time.Hour),
		JTI:       "sess-01",
	}
}

func mustIssuer(t testing.TB) *Issuer {
	t.Helper()
	is, err := NewIssuer(testIss, testKey(), testKID)
	if err != nil {
		t.Fatal(err)
	}
	return is
}

// wantSessionClaims checks that verified claims carry exactly what was
// issued.
func wantSessionClaims(t testing.TB, got Claims, want SessionClaims, kid string) {
	t.Helper()
	if got.Issuer != testIss || got.Audience != want.Audience || got.Subject != want.Subject ||
		got.JTI != want.JTI || got.KeyID != kid || got.Realm != want.Realm ||
		!slices.Equal(got.Roles, want.Roles) || !slices.Equal(got.Scopes, []string{SessionScope}) ||
		!got.IssuedAt.Equal(time.Unix(want.IssuedAt.Unix(), 0)) ||
		!got.ExpiresAt.Equal(time.Unix(want.ExpiresAt.Unix(), 0)) {
		t.Fatalf("verified claims %+v, issued %+v", got, want)
	}
}

// A session token verifies under core's verifier with and without
// StrictSessionClaims, at iat and up to exp plus the skew, and carries
// exactly the Appendix A claims; past exp plus the skew it is refused.
func TestIssueSessionRoundTrip(t *testing.T) {
	is := mustIssuer(t)
	sc := goodSession()
	sc.IssuedAt = testNow.Add(700 * time.Millisecond) // written as testNow
	sc.ExpiresAt = testNow.Add(time.Hour + 300*time.Millisecond)
	tok, err := is.IssueSession(sc)
	if err != nil {
		t.Fatal(err)
	}
	clk := newClock(testNow)
	for _, strict := range []bool{true, false} {
		v := sessionVerifier(t, is.JWKS(), sc.Audience, strict, clk.now)
		clk.set(testNow)
		c, err := v.Verify(context.Background(), tok)
		if err != nil {
			t.Fatalf("strict %v: %v", strict, err)
		}
		wantSessionClaims(t, c, sc, testKID)
		clk.set(testNow.Add(time.Hour + DefaultMaxSkew - time.Second))
		if _, err := v.Verify(context.Background(), tok); err != nil {
			t.Fatalf("strict %v, within the skew after exp: %v", strict, err)
		}
		clk.set(testNow.Add(time.Hour + DefaultMaxSkew))
		_, err = v.Verify(context.Background(), tok)
		wantTokenRefused(t, err, CounterRejectedExpired, "exp")
	}

	// The payload is Appendix A's session row, nothing more, in order;
	// the header is alg RS256, kid and typ JWT.
	parts := strings.Split(tok, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"iss":"https://authority.example/","aud":"authority.example","sub":"acct-7","scope":"session",` +
		`"roles":["operator","viewer"],"realm":"console","iat":1790000000,"exp":1790003600,"jti":"sess-01"}`
	if string(payload) != want {
		t.Errorf("payload\n%s\nwant\n%s", payload, want)
	}
	hraw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var hdr map[string]any
	if err := json.Unmarshal(hraw, &hdr); err != nil {
		t.Fatal(err)
	}
	if len(hdr) != 3 || hdr["alg"] != "RS256" || hdr["kid"] != testKID || hdr["typ"] != "JWT" {
		t.Errorf("header %v", hdr)
	}
}

// A KeyRing's issuer signs sessions with the active kid, and the token
// verifies against the ring's JWKS.
func TestIssueSessionFromKeyRing(t *testing.T) {
	ring := mustRing(t, SigningKey{KID: "k2", Key: otherKey()}, SigningKey{KID: testKID, Key: testKey()})
	is, err := ring.Issuer(testIss)
	if err != nil {
		t.Fatal(err)
	}
	sc := goodSession()
	tok, err := is.IssueSession(sc)
	if err != nil {
		t.Fatal(err)
	}
	c, err := sessionVerifier(t, ring.JWKS(), sc.Audience, true, func() time.Time { return testNow }).
		Verify(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	wantSessionClaims(t, c, sc, "k2")
}

// Each refusal beside the accepted session that differs from it in one
// field (E-01).
func TestIssueSessionRefusalsBesideAcceptance(t *testing.T) {
	is := mustIssuer(t)
	edit := func(f func(*SessionClaims)) SessionClaims { s := goodSession(); f(&s); return s }
	for _, tc := range []struct {
		name  string
		is    *Issuer
		sc    SessionClaims
		field string // empty: accepted
	}{
		{"good", is, goodSession(), ""},
		{"nil issuer", nil, goodSession(), "iss"},
		{"zero issuer", &Issuer{}, goodSession(), "iss"},
		{"issuer without a key", &Issuer{iss: testIss}, goodSession(), "key"},
		{"empty aud", is, edit(func(s *SessionClaims) { s.Audience = "" }), "aud"},
		{"empty sub", is, edit(func(s *SessionClaims) { s.Subject = "" }), "sub"},
		{"empty jti", is, edit(func(s *SessionClaims) { s.JTI = "" }), "jti"},
		{"empty realm", is, edit(func(s *SessionClaims) { s.Realm = "" }), "realm"},
		{"nil roles", is, edit(func(s *SessionClaims) { s.Roles = nil }), "roles"},
		{"no roles", is, edit(func(s *SessionClaims) { s.Roles = []string{} }), "roles"},
		{"an empty role", is, edit(func(s *SessionClaims) { s.Roles = []string{"operator", ""} }), "roles"},
		{"one role", is, edit(func(s *SessionClaims) { s.Roles = []string{"operator"} }), ""},
		{"aud not UTF-8", is, edit(func(s *SessionClaims) { s.Audience = "h\xff" }), "aud"},
		{"sub not UTF-8", is, edit(func(s *SessionClaims) { s.Subject = "a\xc3" }), "sub"},
		{"jti not UTF-8", is, edit(func(s *SessionClaims) { s.JTI = "\xfe" }), "jti"},
		{"realm not UTF-8", is, edit(func(s *SessionClaims) { s.Realm = "c\x80" }), "realm"},
		{"role not UTF-8", is, edit(func(s *SessionClaims) { s.Roles = []string{"op\xff"} }), "roles"},
		{"non-ASCII UTF-8", is, edit(func(s *SessionClaims) {
			s.Subject = "რე<&>"
			s.Roles = []string{"\u10DB\u10E4\u10E0\u10D8\u10DC\u10D0\u10D5\u10D8"}
		}), ""},
		{"a role with a leading em space", is, edit(func(s *SessionClaims) { s.Roles = []string{"\u2003x"} }), "roles"},
		{"a role with a leading space", is, edit(func(s *SessionClaims) { s.Roles = []string{" operator"} }), "roles"},
		{"a role with a trailing space", is, edit(func(s *SessionClaims) { s.Roles = []string{"operator "} }), "roles"},
		{"a role with a leading tab", is, edit(func(s *SessionClaims) { s.Roles = []string{"\toperator"} }), "roles"},
		{"a role of spaces only", is, edit(func(s *SessionClaims) { s.Roles = []string{"   "} }), "roles"},
		{"a role with a space inside", is, edit(func(s *SessionClaims) { s.Roles = []string{"flight operator"} }), ""},
		{"a role twice", is, edit(func(s *SessionClaims) { s.Roles = []string{"operator", "viewer", "operator"} }), "roles"},
		{"two different roles", is, edit(func(s *SessionClaims) { s.Roles = []string{"operator", "viewer"} }), ""},
		{"exp equal to iat", is, edit(func(s *SessionClaims) { s.ExpiresAt = s.IssuedAt }), "exp"},
		{"exp before iat", is, edit(func(s *SessionClaims) { s.ExpiresAt = s.IssuedAt.Add(-time.Second) }), "exp"},
		{"exp after iat in the same second", is, edit(func(s *SessionClaims) {
			s.IssuedAt = testNow.Add(100 * time.Millisecond)
			s.ExpiresAt = testNow.Add(900 * time.Millisecond)
		}), "exp"},
		{"exp one second after iat", is, edit(func(s *SessionClaims) { s.ExpiresAt = s.IssuedAt.Add(time.Second) }), ""},
		{"zero times", is, edit(func(s *SessionClaims) { s.IssuedAt, s.ExpiresAt = time.Time{}, time.Time{} }), "iat"},
		{"iat before 1970", is, edit(func(s *SessionClaims) { s.IssuedAt = time.Unix(-1, 0) }), "iat"},
		{"iat at 1970", is, edit(func(s *SessionClaims) { s.IssuedAt = time.Unix(0, 0) }), ""},
		{"exp past 9999", is, edit(func(s *SessionClaims) { s.ExpiresAt = time.Unix(maxNumericDate+1, 0) }), "exp"},
		{"exp at the end of 9999", is, edit(func(s *SessionClaims) { s.ExpiresAt = time.Unix(maxNumericDate, 0) }), ""},
		{"token too long", is, edit(func(s *SessionClaims) { s.Subject = strings.Repeat("a", DefaultMaxTokenBytes) }), "token"},
		{"long token within the bound", is, edit(func(s *SessionClaims) { s.Subject = strings.Repeat("a", 5000) }), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := tc.is.IssueSession(tc.sc)
			if tc.field != "" {
				wantFieldError(t, err, tc.field)
				if tok != "" {
					t.Error("a refusal returned a token")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			now := func() time.Time { return tc.sc.IssuedAt }
			c, err := sessionVerifier(t, is.JWKS(), tc.sc.Audience, true, now).Verify(context.Background(), tok)
			if err != nil {
				t.Fatal(err)
			}
			wantSessionClaims(t, c, tc.sc, testKID)
		})
	}
}

// Issue is unchanged by IssueSession: no roles, no realm, no typ.
func TestIssueStillWritesNoSessionClaims(t *testing.T) {
	is := mustIssuer(t)
	tok, err := is.Issue("op-1", testAud, []string{"rid.read"}, time.Minute, testNow)
	if err != nil {
		t.Fatal(err)
	}
	c, err := sessionVerifier(t, is.JWKS(), testAud, true, func() time.Time { return testNow }).Verify(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Roles != nil || c.Realm != "" {
		t.Errorf("Issue wrote roles %v realm %q", c.Roles, c.Realm)
	}
}

// Every session token IssueSession returns verifies under core's verifier
// with StrictSessionClaims and carries the given claims; every refusal is
// a *core.FieldError.
func FuzzIssueSession(f *testing.F) {
	f.Add("acct-7", "authority.example", "operator,viewer", "console", "sess-01", int64(1_790_000_000), int64(43200))
	f.Add("", "", "", "", "", int64(0), int64(0))
	f.Add("a", "b", ",", "c", "d", int64(-1), int64(1))
	f.Add("რ", "h\xff", "op", "police", "j", int64(maxNumericDate-1), int64(2))
	f.Add("s", "a", "r", "portal", "j", int64(0), int64(maxNumericDate))
	is := mustIssuer(f)
	set := is.JWKS()
	f.Fuzz(func(t *testing.T, sub, aud, roles, realm, jti string, iatS, ttlS int64) {
		sc := SessionClaims{
			Audience: aud, Subject: sub, Realm: realm, JTI: jti,
			IssuedAt: time.Unix(iatS, 0), ExpiresAt: time.Unix(iatS+ttlS, 0),
		}
		if roles != "" {
			sc.Roles = strings.Split(roles, ",")
		}
		tok, err := is.IssueSession(sc)
		if err != nil {
			var fe *core.FieldError
			if !errors.As(err, &fe) || tok != "" {
				t.Fatalf("refusal %v is not a FieldError or returned a token", err)
			}
			return
		}
		v := sessionVerifier(t, set, aud, true, func() time.Time { return sc.IssuedAt })
		c, err := v.Verify(context.Background(), tok)
		if err != nil {
			t.Fatalf("issued token refused: %v (%+v)", err, sc)
		}
		wantSessionClaims(t, c, sc, testKID)
	})
}

func BenchmarkIssueSession(b *testing.B) {
	is := mustIssuer(b)
	sc := goodSession()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := is.IssueSession(sc); err != nil {
			b.Fatal(err)
		}
	}
}
