package auth

import (
	"context"
	"testing"
	"time"
)

// sessionPolicyVerifier is a strict verifier with Appendix A's session
// limits: 12 h and the three realms.
func sessionPolicyVerifier(t *testing.T, mutate func(*Config)) (*Verifier, error) {
	t.Helper()
	c := Config{
		Issuers:             map[string]IssuerConfig{testIss: {Keys: publicSet(t, testKID, testKey())}},
		Audience:            testAud,
		StrictSessionClaims: true,
		MaxSessionTTL:       12 * time.Hour,
		Realms:              []string{"console", "police", "portal"},
		Now:                 func() time.Time { return testNow },
	}
	if mutate != nil {
		mutate(&c)
	}
	return NewVerifier(context.Background(), c)
}

// A session token: scope session, realm console, iat an hour ago and
// exp ttl after it.
func sessionToken(ttl time.Duration, kv ...any) string {
	iat := testNow.Add(-time.Hour)
	base := with(goodClaims(), "scope", "session", "roles", []string{"operator"}, "realm", "console",
		"iat", iat.Unix(), "exp", iat.Add(ttl).Unix())
	return compact(goodHeader(), with(base, kv...), rs256(testKey()))
}

// MaxSessionTTL and Realms under StrictSessionClaims: each refusal
// beside the token accepted that differs from it in one claim (E-01).
func TestVerifierSessionTTLAndRealms(t *testing.T) {
	v, err := sessionPolicyVerifier(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		token   string
		counter string // empty: accepted
		claim   string
	}{
		{"12 h exactly", sessionToken(12 * time.Hour), "", ""},
		{"12 h and one second", sessionToken(12*time.Hour + time.Second), CounterRejectedClaims, "exp"},
		{"ten years", sessionToken(10 * 365 * 24 * time.Hour), CounterRejectedClaims, "exp"},
		{"session without iat", sessionToken(12*time.Hour, "iat", nil), CounterRejectedClaims, "iat"},
		{"a long service token is not a session", sessionToken(48*time.Hour, "scope", "rid.read"), "", ""},
		{"session among other scopes", sessionToken(48*time.Hour, "scope", "rid.read session"), CounterRejectedClaims, "exp"},
		{"realm police", sessionToken(time.Hour, "realm", "police"), "", ""},
		{"realm portal", sessionToken(time.Hour, "realm", "portal"), "", ""},
		{"realm misspelt", sessionToken(time.Hour, "realm", "consol"), CounterRejectedClaims, "realm"},
		{"realm in capitals", sessionToken(time.Hour, "realm", "Console"), CounterRejectedClaims, "realm"},
		{"realm empty", sessionToken(time.Hour, "realm", ""), CounterRejectedClaims, "realm"},
		{"realm absent", sessionToken(time.Hour, "realm", nil), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := v.Verify(context.Background(), tc.token)
			if tc.counter != "" {
				wantTokenRefused(t, err, tc.counter, tc.claim)
				return
			}
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			if c.Subject != "op-1" {
				t.Errorf("claims %+v", c)
			}
		})
	}
}

// Without the limits a strict verifier keeps v1.3.0's judgement: the
// ten-year session and the misspelt realm are accepted.
func TestVerifierSessionLimitsOff(t *testing.T) {
	v, err := sessionPolicyVerifier(t, func(c *Config) { c.MaxSessionTTL, c.Realms = 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{
		sessionToken(10 * 365 * 24 * time.Hour),
		sessionToken(time.Hour, "realm", "consol"),
	} {
		if _, err := v.Verify(context.Background(), tok); err != nil {
			t.Errorf("refused without limits: %v", err)
		}
	}
}

// A limit that would silently do nothing, or that is malformed, is a
// configuration error, beside the accepted configuration.
func TestNewVerifierSessionLimitConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		field  string // empty: accepted
	}{
		{"strict with both", nil, ""},
		{"strict with TTL only", func(c *Config) { c.Realms = nil }, ""},
		{"strict with realms only", func(c *Config) { c.MaxSessionTTL = 0 }, ""},
		{"TTL without strict", func(c *Config) { c.StrictSessionClaims, c.Realms = false, nil }, "strict_session_claims"},
		{"realms without strict", func(c *Config) { c.StrictSessionClaims, c.MaxSessionTTL = false, 0 }, "strict_session_claims"},
		{"neither without strict", func(c *Config) { c.StrictSessionClaims, c.MaxSessionTTL, c.Realms = false, 0, nil }, ""},
		{"negative TTL", func(c *Config) { c.MaxSessionTTL = -time.Second }, "max_session_ttl"},
		{"an empty realm", func(c *Config) { c.Realms = []string{"console", ""} }, "realms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, err := sessionPolicyVerifier(t, tc.mutate)
			if tc.field == "" {
				if err != nil || v == nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			wantFieldError(t, err, tc.field)
		})
	}
}

// The caller's Realms slice is copied: changing it later changes nothing.
func TestNewVerifierCopiesRealms(t *testing.T) {
	realms := []string{"console"}
	v, err := sessionPolicyVerifier(t, func(c *Config) { c.Realms = realms })
	if err != nil {
		t.Fatal(err)
	}
	realms[0] = "consol"
	if _, err := v.Verify(context.Background(), sessionToken(time.Hour)); err != nil {
		t.Fatalf("the caller's slice changed the verifier: %v", err)
	}
}
