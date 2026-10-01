package auth

import (
	"context"
	"slices"
	"testing"
	"time"
)

func audienceVerifier(t *testing.T, aud string, auds ...string) *Verifier {
	t.Helper()
	v, err := NewVerifier(context.Background(), Config{
		Issuers:   map[string]IssuerConfig{testIss: {Keys: publicSet(t, testKID, testKey())}},
		Audience:  aud,
		Audiences: auds,
		Now:       func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Audiences: aud must contain Audience or one of Audiences; the matched
// value is reported. Each refusal beside its acceptance (E-01).
func TestVerifierAudiences(t *testing.T) {
	sign := func(aud any) string { return compact(goodHeader(), with(goodClaims(), "aud", aud), rs256(testKey())) }
	for _, tc := range []struct {
		name     string
		audience string
		list     []string
		aud      any
		want     string // empty: refused on aud
	}{
		{"list only, first host", "", []string{"h1.example", "h2.example"}, "h1.example", "h1.example"},
		{"list only, second host", "", []string{"h1.example", "h2.example"}, "h2.example", "h2.example"},
		{"list only, other host", "", []string{"h1.example", "h2.example"}, "h3.example", ""},
		{"list only, array with the second host", "", []string{"h1.example", "h2.example"}, []string{"x", "h2.example"}, "h2.example"},
		{"audience preferred over the list", testAud, []string{"h1.example"}, []string{"h1.example", testAud}, testAud},
		{"audience absent, list matches", testAud, []string{"h1.example"}, "h1.example", "h1.example"},
		{"neither", testAud, []string{"h1.example"}, "cisp-1", ""},
		{"audience alone, as v1.0.0", testAud, nil, testAud, testAud},
		{"audience alone refuses another", testAud, nil, "h1.example", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := audienceVerifier(t, tc.audience, tc.list...)
			c, err := v.Verify(context.Background(), sign(tc.aud))
			if tc.want == "" {
				wantTokenRefused(t, err, CounterRejectedAudience, "aud")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.Audience != tc.want {
				t.Errorf("matched %s, want %s", c.Audience, tc.want)
			}
		})
	}
}

func TestNewVerifierAudiencesConfig(t *testing.T) {
	keys := map[string]IssuerConfig{testIss: {Keys: publicSet(t, testKID, testKey())}}
	_, err := NewVerifier(context.Background(), Config{Issuers: keys})
	wantFieldError(t, err, "audience")
	_, err = NewVerifier(context.Background(), Config{Issuers: keys, Audiences: []string{"h1", ""}})
	wantFieldError(t, err, "audiences")
	if _, err := NewVerifier(context.Background(), Config{Issuers: keys, Audiences: []string{"h1"}}); err != nil {
		t.Fatalf("Audiences without Audience was refused: %v", err)
	}
	// The verifier keeps its own copy of the list.
	list := []string{"h1"}
	v, err := NewVerifier(context.Background(), Config{Issuers: keys, Audiences: list, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	list[0] = "changed"
	if _, err := v.Verify(context.Background(), compact(goodHeader(), with(goodClaims(), "aud", "h1"), rs256(testKey()))); err != nil {
		t.Fatalf("the caller's slice changed the verifier: %v", err)
	}
}

// roles and realm are read when present and never required; a malformed
// one is rejected_claims.
func TestVerifierRolesAndRealm(t *testing.T) {
	v := staticVerifier(t, publicSet(t, testKID, testKey()), func() time.Time { return testNow })
	sign := func(kv ...any) string { return compact(goodHeader(), with(goodClaims(), kv...), rs256(testKey())) }

	c, err := v.Verify(context.Background(), sign("roles", []string{"operator", "viewer"}, "realm", "ge", "scope", "session"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Roles, []string{"operator", "viewer"}) || c.Realm != "ge" || !c.HasScope("session") {
		t.Errorf("claims %+v", c)
	}
	c, err = v.Verify(context.Background(), sign())
	if err != nil {
		t.Fatal(err)
	}
	if c.Roles != nil || c.Realm != "" {
		t.Errorf("absent roles and realm read as %v %q", c.Roles, c.Realm)
	}
	c, err = v.Verify(context.Background(), sign("roles", []string{}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Roles == nil || len(c.Roles) != 0 {
		t.Errorf("an empty roles array read as %#v", c.Roles)
	}
	for _, tc := range []struct {
		name  string
		token string
		claim string
	}{
		{"roles a string", sign("roles", "operator"), "roles"},
		{"roles with a number", sign("roles", []any{"operator", 1}), "roles"},
		{"roles null", sign("roles", []any(nil)), "roles"},
		{"realm a number", sign("realm", 1), "realm"},
		{"realm an array", sign("realm", []string{"ge"}), "realm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), tc.token)
			wantTokenRefused(t, err, CounterRejectedClaims, tc.claim)
		})
	}
}
