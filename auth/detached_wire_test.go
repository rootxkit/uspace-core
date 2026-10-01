package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
)

// E-03: the signature SignDetached writes is the RSA PKCS #1 v1.5
// signature over the signing input built by hand from RFC 7797 section
// 3, and the protected header holds exactly alg, kid, iat, b64 and crit.
func TestDetachedSigningInputByHand(t *testing.T) {
	key := testKey()
	payload := []byte("{\"a\":\"x.y\"}\n.")
	sig, err := SignDetached(SigningKey{KID: testKID, Key: key}, payload, testNow)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(sig, ".")
	if len(parts) != 3 || parts[1] != "" {
		t.Fatalf("not <protected>..<signature>: %q", sig)
	}
	protected, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var hdr map[string]json.RawMessage
	if err := json.Unmarshal(protected, &hdr); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"alg": `"RS256"`, "kid": `"` + testKID + `"`, "iat": itoa64(testNow.Unix()),
		"b64": `false`, "crit": `["b64"]`,
	}
	if len(hdr) != len(want) {
		t.Errorf("header members %v, want exactly alg, kid, iat, b64, crit", hdr)
	}
	for k, w := range want {
		if string(hdr[k]) != w {
			t.Errorf("%s = %s, want %s", k, hdr[k], w)
		}
	}
	// The same key, header bytes and payload signed with crypto/rsa.
	input := append([]byte(parts[0]+"."), payload...)
	digest := sha256.Sum256(input)
	byHand, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	if parts[2] != b64(byHand) {
		t.Fatal("SignDetached does not sign ASCII(BASE64URL(protected)) || '.' || payload")
	}
	// A value built entirely by hand (other member order) verifies.
	v := staticDetached(t, map[string]jwk.Set{pubA: publicSet(t, testKID, key)}, func() time.Time { return testNow })
	hand := detachedByHand([]byte(`{"crit":["b64"],"b64":false,"kid":"`+testKID+`","iat":`+itoa64(testNow.Unix())+`,"alg":"RS256"}`),
		payload, rs256(key))
	if _, err := v.Verify(context.Background(), pubA, hand, payload); err != nil {
		t.Fatalf("the hand-built value was refused: %v", err)
	}
	if _, err := v.Verify(context.Background(), pubA, sig, payload); err != nil {
		t.Fatal(err)
	}
	// The signature over the base64url payload (b64 true) is not a
	// signature over the raw bytes.
	enc := detachedByHand(protected, []byte(b64(payload)), rs256(key))
	_, err = v.Verify(context.Background(), pubA, enc, payload)
	wantTokenRefused(t, err, CounterRejectedSignature, "signature")
}

// RFC 7797 section 4.2: the header encoding and the signing input of the
// example (HS256 there, so only the construction is pinned, and the
// library verifies it with the RFC 7515 A.1 key).
func TestDetachedRFC7797Example(t *testing.T) {
	const (
		protectedB64 = "eyJhbGciOiJIUzI1NiIsImI2NCI6ZmFsc2UsImNyaXQiOlsiYjY0Il19"
		sigB64       = "A5dxf2s96_n5FLueVuW1Z_vh161FwXZC4YLPff6dmDY"
		keyB64       = "AyM1SysPpbyDfgZld3umj1qzKObwVMkoqQ-EstJQLr_T-1qS0gZH75aKtMN3Yj0iPS4hcgUuTwjAzZr1Z9CAow"
		payload      = "$.02"
	)
	if got := b64([]byte(`{"alg":"HS256","b64":false,"crit":["b64"]}`)); got != protectedB64 {
		t.Fatalf("header encodes as %s", got)
	}
	secret, err := base64.RawURLEncoding.DecodeString(keyB64)
	if err != nil {
		t.Fatal(err)
	}
	value := detachedByHand([]byte(`{"alg":"HS256","b64":false,"crit":["b64"]}`), []byte(payload), hs256(secret))
	if value != protectedB64+".."+sigB64 {
		t.Fatalf("the signing input construction does not give the RFC signature: %s", value)
	}
	if _, err := jws.Verify([]byte(value), jws.WithKey(jwa.HS256(), secret), jws.WithDetachedPayload([]byte(payload))); err != nil {
		t.Fatalf("the library refused the RFC example: %v", err)
	}
	h, err := ParseDetachedHeader(value)
	if err != nil {
		t.Fatal(err)
	}
	if h.Alg != "HS256" || h.B64 || !slices.Equal(h.Crit, []string{"b64"}) || h.KID != "" || !h.IssuedAt.IsZero() {
		t.Errorf("parsed %+v", h)
	}
}
