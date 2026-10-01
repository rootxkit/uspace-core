package auth

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
)

// E-02: after Rotate the new key signs, the previous one is retired and
// keeps verifying; a verifier that cached the old JWKS keeps verifying
// the old key and learns the new one from the new JWKS.
func TestKeyRingRotateKeepsTheRetiredKeyVerifying(t *testing.T) {
	r := mustRing(t, SigningKey{KID: "k-old", Key: testKey()})
	payload := []byte(`{"seq":1}`)
	oldSig, err := r.SignDetached(payload, testNow)
	if err != nil {
		t.Fatal(err)
	}
	cached := r.JWKS()
	if err := r.Rotate(SigningKey{KID: "k-new", Key: otherKey()}); err != nil {
		t.Fatal(err)
	}
	if r.ActiveKID() != "k-new" || !slices.Equal(r.KIDs(), []string{"k-new", "k-old"}) {
		t.Fatalf("after Rotate: active %s, kids %v", r.ActiveKID(), r.KIDs())
	}
	newSig, err := r.SignDetached(payload, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if h, err := ParseDetachedHeader(newSig); err != nil || h.KID != "k-new" {
		t.Fatalf("the ring did not sign with the new key: %+v %v", h, err)
	}
	now := func() time.Time { return testNow }
	fresh := staticDetached(t, map[string]jwk.Set{pubA: r.JWKS()}, now)
	for _, sig := range []string{oldSig, newSig} {
		if _, err := fresh.Verify(context.Background(), pubA, sig, payload); err != nil {
			t.Errorf("the rotated JWKS refused a signature: %v", err)
		}
	}
	stale := staticDetached(t, map[string]jwk.Set{pubA: cached}, now)
	if _, err := stale.Verify(context.Background(), pubA, oldSig, payload); err != nil {
		t.Errorf("the cached JWKS refused the old key: %v", err)
	}
	_, err = stale.Verify(context.Background(), pubA, newSig, payload)
	wantTokenRefused(t, err, CounterRejectedKID, "kid")

	// Rotate refuses a kid already in the ring and a bad key.
	wantFieldError(t, r.Rotate(SigningKey{KID: "k-old", Key: otherKey()}), "next.kid")
	wantFieldError(t, r.Rotate(SigningKey{KID: "k-new", Key: otherKey()}), "next.kid")
	wantFieldError(t, r.Rotate(SigningKey{KID: "k-3"}), "next.key")
}

// Retire drops a retired key from the JWKS; the active key and an unknown
// kid are refused.
func TestKeyRingRetire(t *testing.T) {
	r := mustRing(t, SigningKey{KID: "a", Key: testKey()}, SigningKey{KID: "b", Key: otherKey()})
	sigB, err := SignDetached(SigningKey{KID: "b", Key: otherKey()}, []byte("x"), testNow)
	if err != nil {
		t.Fatal(err)
	}
	now := func() time.Time { return testNow }
	before := staticDetached(t, map[string]jwk.Set{pubA: r.JWKS()}, now)
	if _, err := before.Verify(context.Background(), pubA, sigB, []byte("x")); err != nil {
		t.Fatalf("the retired key did not verify before Retire: %v", err)
	}
	wantFieldError(t, r.Retire("a"), "kid")
	wantFieldError(t, r.Retire("zz"), "kid")
	if err := r.Retire("b"); err != nil {
		t.Fatal(err)
	}
	if got := r.KIDs(); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("KIDs %v", got)
	}
	after := staticDetached(t, map[string]jwk.Set{pubA: r.JWKS()}, now)
	_, err = after.Verify(context.Background(), pubA, sigB, []byte("x"))
	wantTokenRefused(t, err, CounterRejectedKID, "kid")
	wantFieldError(t, r.Retire("b"), "kid")
}

// E-11: Rotate concurrent with signing and publishing.
func TestKeyRingConcurrent(t *testing.T) {
	r := mustRing(t, SigningKey{KID: "k0", Key: testKey()})
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 1; i < MaxRingKeys; i++ {
			if err := r.Rotate(SigningKey{KID: fmt.Sprintf("k%d", i), Key: testKey()}); err != nil {
				t.Error(err)
			}
		}
	})
	for range 4 {
		wg.Go(func() {
			for range 20 {
				sig, err := r.SignDetached([]byte("p"), testNow)
				if err != nil {
					t.Error(err)
					return
				}
				if !strings.Contains(sig, "..") {
					t.Error("not a detached value")
				}
				if r.JWKS().Len() == 0 || len(r.KIDs()) == 0 {
					t.Error("an empty ring")
				}
			}
		})
	}
	wg.Wait()
	if len(r.KIDs()) != MaxRingKeys {
		t.Errorf("%d keys", len(r.KIDs()))
	}
}
