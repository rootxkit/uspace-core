package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// audJSON is the raw aud member of a compact token's payload.
func audJSON(t *testing.T, tok string) string {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("%d parts", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var cl map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cl); err != nil {
		t.Fatal(err)
	}
	return string(cl["aud"])
}

// Issue writes aud as a one-element array; an Issuer from
// WithStringAudience writes it as a string (uspace-lab finding C3).
// Both forms verify, with the matched audience reported, and the issuer
// WithStringAudience was called on keeps the array (E-01: each form
// beside the other).
func TestIssuerAudienceForm(t *testing.T) {
	arr, err := NewIssuer(testIss, testKey(), testKID)
	if err != nil {
		t.Fatal(err)
	}
	str := arr.WithStringAudience()
	v := staticVerifier(t, arr.JWKS(), newClock(testNow).now)
	for _, tc := range []struct {
		name string
		is   *Issuer
		want string
	}{
		{"default array", arr, `["` + testAud + `"]`},
		{"string", str, `"` + testAud + `"`},
		{"default array after WithStringAudience", arr, `["` + testAud + `"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tok, err := tc.is.Issue("op-1", testAud, []string{"rid.read"}, time.Minute, testNow)
			if err != nil {
				t.Fatal(err)
			}
			if got := audJSON(t, tok); got != tc.want {
				t.Errorf("aud %s, want %s", got, tc.want)
			}
			c, err := v.Verify(context.Background(), tok)
			if err != nil {
				t.Fatal(err)
			}
			if c.Audience != testAud || c.Subject != "op-1" || c.Issuer != testIss || !c.HasScope("rid.read") {
				t.Errorf("claims %+v", c)
			}
		})
	}
	// The same refusals as Issue's, and the same key and JWKS.
	if _, err := str.Issue("op-1", "", nil, time.Minute, testNow); err == nil {
		t.Error("an empty aud was issued")
	}
	if str.JWKS() != arr.JWKS() {
		t.Error("WithStringAudience changed the JWKS")
	}
}

// A ring's Issuer takes the string form too, and a verifier with that
// audience refuses the token for another (the string is still judged).
func TestKeyRingIssuerStringAudience(t *testing.T) {
	r, err := NewKeyRing(SigningKey{KID: testKID, Key: testKey()})
	if err != nil {
		t.Fatal(err)
	}
	is, err := r.Issuer(testIss)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := is.WithStringAudience().Issue("op-1", "other.example", nil, time.Minute, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if got := audJSON(t, tok); got != `"other.example"` {
		t.Errorf("aud %s", got)
	}
	_, err = staticVerifier(t, r.JWKS(), newClock(testNow).now).Verify(context.Background(), tok)
	wantTokenRefused(t, err, CounterRejectedAudience, "aud")
}
