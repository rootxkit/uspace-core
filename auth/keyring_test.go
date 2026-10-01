package auth

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/rootxkit/uspace-core/core"
)

func wantFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var fe *core.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("got %v, want a *core.FieldError on %s", err, field)
	}
	if fe.Field != field {
		t.Errorf("got the field %s (%v), want %s", fe.Field, err, field)
	}
}

func mustRing(t testing.TB, active SigningKey, retired ...SigningKey) *KeyRing {
	t.Helper()
	r, err := NewKeyRing(active, retired...)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// Each refusal of NewKeyRing beside the ring it would otherwise build
// (E-01).
func TestNewKeyRingRefusalsBesideAcceptance(t *testing.T) {
	a := SigningKey{KID: "a", Key: testKey()}
	b := SigningKey{KID: "b", Key: otherKey()}
	r := mustRing(t, a, b)
	if got := r.KIDs(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("KIDs %v", got)
	}
	if r.ActiveKID() != "a" {
		t.Fatalf("active %s", r.ActiveKID())
	}

	short := mustRSA(1024)
	broken := *testKey()
	broken.D = new(big.Int).Add(broken.D, big.NewInt(2))
	broken.Precomputed = rsa.PrecomputedValues{}
	for _, tc := range []struct {
		name    string
		active  SigningKey
		retired []SigningKey
		field   string
	}{
		{"empty active kid", SigningKey{Key: testKey()}, nil, "active.kid"},
		{"nil active key", SigningKey{KID: "a"}, nil, "active.key"},
		{"key without modulus", SigningKey{KID: "a", Key: &rsa.PrivateKey{}}, nil, "active.key"},
		{"short active key", SigningKey{KID: "a", Key: short}, nil, "active.key"},
		{"invalid active key", SigningKey{KID: "a", Key: &broken}, nil, "active.key"},
		{"empty retired kid", a, []SigningKey{{Key: otherKey()}}, "retired[0].kid"},
		{"short retired key", a, []SigningKey{b, {KID: "c", Key: short}}, "retired[1].key"},
		{"retired kid equals the active one", a, []SigningKey{{KID: "a", Key: otherKey()}}, "retired[0].kid"},
		{"retired kid twice", a, []SigningKey{b, {KID: "b", Key: testKey()}}, "retired[1].kid"},
		{"more than MaxRingKeys", a, ringKeys(MaxRingKeys, "r"), "keys"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewKeyRing(tc.active, tc.retired...)
			wantFieldError(t, err, tc.field)
		})
	}
	// MaxRingKeys keys exactly is accepted (E-10 twin).
	full := mustRing(t, a, ringKeys(MaxRingKeys-1, "r")...)
	if n := len(full.KIDs()); n != MaxRingKeys {
		t.Fatalf("%d keys", n)
	}
}

// ringKeys returns n keys named prefix0..; one RSA key serves them all.
func ringKeys(n int, prefix string) []SigningKey {
	out := make([]SigningKey, n)
	for i := range out {
		out[i] = SigningKey{KID: fmt.Sprintf("%s%d", prefix, i), Key: otherKey()}
	}
	return out
}

// The JWKS holds every key, public only, in KIDs order, with alg and use.
func TestKeyRingJWKS(t *testing.T) {
	r := mustRing(t, SigningKey{KID: "a", Key: testKey()}, SigningKey{KID: "b", Key: otherKey()})
	set := r.JWKS()
	raw, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Keys) != 2 {
		t.Fatalf("%d keys", len(doc.Keys))
	}
	for i, want := range []struct {
		kid string
		key *rsa.PrivateKey
	}{{"a", testKey()}, {"b", otherKey()}} {
		k := doc.Keys[i]
		if k["kid"] != want.kid || k["alg"] != "RS256" || k["use"] != "sig" || k["kty"] != "RSA" {
			t.Errorf("key %d: %v", i, k)
		}
		if k["n"] != b64(want.key.N.Bytes()) {
			t.Errorf("key %d is not the public key of %s", i, want.kid)
		}
		for _, private := range []string{"d", "p", "q", "dp", "dq", "qi"} {
			if _, ok := k[private]; ok {
				t.Errorf("key %d carries the private member %s", i, private)
			}
		}
	}
	// A caller that edits the returned set does not edit the ring.
	k0, _ := set.Key(0)
	if err := k0.Set(jwk.KeyIDKey, "edited"); err != nil {
		t.Fatal(err)
	}
	if got := mustKIDs(t, r.JWKS()); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("the ring's JWKS changed with the copy: %v", got)
	}
}

func mustKIDs(t *testing.T, set jwk.Set) []string {
	t.Helper()
	var out []string
	for i := range set.Len() {
		k, _ := set.Key(i)
		kid, _ := k.KeyID()
		out = append(out, kid)
	}
	return out
}

// E-10: Rotate past MaxRingKeys is refused and counted; after a Retire
// it succeeds.
func TestKeyRingBound(t *testing.T) {
	r := mustRing(t, SigningKey{KID: "a", Key: testKey()}, ringKeys(MaxRingKeys-2, "r")...)
	if err := r.Rotate(SigningKey{KID: "n1", Key: testKey()}); err != nil {
		t.Fatalf("the ring refused its %dth key: %v", MaxRingKeys, err)
	}
	if r.Counters().Get(CounterKeyRingFull) != 0 {
		t.Fatal("counted before the bound")
	}
	wantFieldError(t, r.Rotate(SigningKey{KID: "n2", Key: testKey()}), "keys")
	if r.Counters().Get(CounterKeyRingFull) != 1 {
		t.Fatalf("key_ring_full %d", r.Counters().Get(CounterKeyRingFull))
	}
	if r.ActiveKID() != "n1" || len(r.KIDs()) != MaxRingKeys {
		t.Fatalf("a refused Rotate changed the ring: %s %d", r.ActiveKID(), len(r.KIDs()))
	}
	if err := r.Retire("r0"); err != nil {
		t.Fatal(err)
	}
	if err := r.Rotate(SigningKey{KID: "n2", Key: testKey()}); err != nil {
		t.Fatal(err)
	}
}

// The ring's Issuer signs tokens with the active key that a Verifier on
// the ring's JWKS accepts; the JWKS is the ring's.
func TestKeyRingIssuer(t *testing.T) {
	r := mustRing(t, SigningKey{KID: "a", Key: testKey()}, SigningKey{KID: "b", Key: otherKey()})
	_, err := r.Issuer("")
	wantFieldError(t, err, "iss")
	is, err := r.Issuer(testIss)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustKIDs(t, is.JWKS()); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("the issuer's JWKS is not the ring's: %v", got)
	}
	tok, err := is.Issue("op-1", testAud, nil, time.Minute, testNow)
	if err != nil {
		t.Fatal(err)
	}
	v := staticVerifier(t, r.JWKS(), func() time.Time { return testNow })
	c, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.KeyID != "a" || c.Issuer != testIss {
		t.Errorf("claims %+v", c)
	}
}
