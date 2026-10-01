package auth

import (
	"crypto/rsa"
	"errors"
	"slices"
	"strconv"
	"sync"

	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/rootxkit/uspace-core/core"
)

// MaxRingKeys bounds the keys of a KeyRing, the active one included
// (E-10).
const MaxRingKeys = 16

// CounterKeyRingFull counts a Rotate refused because the ring already
// holds MaxRingKeys keys.
const CounterKeyRingFull = "key_ring_full"

// SigningKey is one RSA private key under a kid.
type SigningKey struct {
	KID string
	Key *rsa.PrivateKey
}

// ringKey is a checked SigningKey with its JWKs.
type ringKey struct {
	kid  string
	priv jwk.Key
	pub  jwk.Key
	rsa  *rsa.PrivateKey
}

// KeyRing holds a publisher's signing keys: one active key that signs,
// and retired keys that are still published in the JWKS so that a
// verifier which cached the old set keeps verifying through a rotation
// (two-key overlap). Retired keys verify but never sign. KeyRing holds
// keys the caller already loaded; it never reads a file or an
// environment variable. It is safe for concurrent use.
type KeyRing struct {
	mu       sync.RWMutex
	active   ringKey
	retired  []ringKey
	counters core.Counters
}

// NewKeyRing builds a ring signing with active and publishing retired
// besides it. Every key is checked as NewIssuer checks its key (non-empty
// kid, non-nil, at least MinRSABits, rsa.PrivateKey.Validate); a kid that
// appears twice and more than MaxRingKeys keys are refused.
func NewKeyRing(active SigningKey, retired ...SigningKey) (*KeyRing, error) {
	if 1+len(retired) > MaxRingKeys {
		return nil, core.Fieldf("keys", "%d keys, more than %d", 1+len(retired), MaxRingKeys)
	}
	a, err := checkRingKey("active", active)
	if err != nil {
		return nil, err
	}
	r := &KeyRing{active: a}
	for i, sk := range retired {
		rk, err := checkRingKey("retired["+strconv.Itoa(i)+"]", sk)
		if err != nil {
			return nil, err
		}
		if r.has(rk.kid) {
			return nil, core.Fieldf("retired["+strconv.Itoa(i)+"].kid", "%s appears twice", quoteShort(rk.kid))
		}
		r.retired = append(r.retired, rk)
	}
	return r, nil
}

func checkRingKey(field string, sk SigningKey) (ringKey, error) {
	priv, pub, err := importSigningKey(sk.KID, sk.Key)
	if err != nil {
		var fe *core.FieldError
		if errors.As(err, &fe) {
			return ringKey{}, core.Fieldf(field+"."+fe.Field, "%s", fe.Reason)
		}
		return ringKey{}, err
	}
	return ringKey{kid: sk.KID, priv: priv, pub: pub, rsa: sk.Key}, nil
}

// has reports whether kid is in the ring; the caller holds mu.
func (r *KeyRing) has(kid string) bool {
	if r.active.kid == kid {
		return true
	}
	return slices.ContainsFunc(r.retired, func(k ringKey) bool { return k.kid == kid })
}

// Rotate makes next the active key; the previous active key joins the
// retired keys (last in KIDs) and keeps being published. A kid already in
// the ring is refused, and so is a rotation past MaxRingKeys (counted as
// key_ring_full): Retire a key first.
func (r *KeyRing) Rotate(next SigningKey) error {
	rk, err := checkRingKey("next", next)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.has(rk.kid) {
		return core.Fieldf("next.kid", "%s is already in the ring", quoteShort(rk.kid))
	}
	if 2+len(r.retired) > MaxRingKeys {
		r.counters.Inc(CounterKeyRingFull)
		return core.Fieldf("keys", "the ring holds %d keys already, the most allowed", MaxRingKeys)
	}
	r.retired = append(r.retired, r.active)
	r.active = rk
	return nil
}

// Retire drops the retired key kid from the ring and from its JWKS. The
// active key cannot be retired (Rotate first), and an unknown kid is
// refused.
func (r *KeyRing) Retire(kid string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kid == r.active.kid {
		return core.Fieldf("kid", "%s is the active key", quoteShort(kid))
	}
	i := slices.IndexFunc(r.retired, func(k ringKey) bool { return k.kid == kid })
	if i < 0 {
		return core.Fieldf("kid", "%s is not a retired key of the ring", quoteShort(kid))
	}
	r.retired = slices.Delete(r.retired, i, i+1)
	return nil
}

// ActiveKID returns the kid of the key that signs.
func (r *KeyRing) ActiveKID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active.kid
}

// KIDs returns every kid of the ring: the active one first, then the
// retired ones in the order they were given or retired.
func (r *KeyRing) KIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, 1+len(r.retired))
	out = append(out, r.active.kid)
	for _, k := range r.retired {
		out = append(out, k.kid)
	}
	return out
}

// JWKS returns a new public key set holding every key of the ring, in
// the order of KIDs, each with kid, alg RS256 and use sig. It is the set
// to publish at the publisher's JWKS URL.
func (r *KeyRing) JWKS() jwk.Set {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.jwksLocked()
}

func (r *KeyRing) jwksLocked() jwk.Set {
	set := jwk.NewSet()
	for _, k := range append([]ringKey{r.active}, r.retired...) {
		// Each key was imported and checked when it entered the ring, so
		// cloning and adding it cannot fail; a failure leaves it out.
		pub, err := k.pub.Clone()
		if err != nil {
			continue
		}
		_ = set.AddKey(pub)
	}
	return set
}

// Issuer returns an Issuer for iss that signs with the key active now
// and whose JWKS is the ring's JWKS at this moment, so a token service
// and a publication signer can share one JWKS. The Issuer keeps that key
// after a Rotate: build a new one then.
func (r *KeyRing) Issuer(iss string) (*Issuer, error) {
	if iss == "" {
		return nil, core.Fieldf("iss", "empty")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return &Issuer{iss: iss, priv: r.active.priv, jwks: r.jwksLocked()}, nil
}

// Counters returns the ring's counters: key_ring_full.
func (r *KeyRing) Counters() *core.Counters { return &r.counters }

// signer returns the active key; retired keys never sign.
func (r *KeyRing) signer() ringKey {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.active
}
