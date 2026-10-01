package auth

import (
	"context"
	"crypto/rsa"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
)

// keyEntry is one kid of an issuer's JWKS: the RSA public key, or why
// the key cannot verify an RS256 signature.
type keyEntry struct {
	pub     *rsa.PublicKey
	problem string
}

// issuerKeys is the key set of one allow-listed issuer, indexed by kid.
// A URL-configured issuer's set is replaced by a successful fetch only: a
// failed fetch keeps the cached set (06 section 2 T5).
type issuerKeys struct {
	url string

	mu        sync.RWMutex
	keys      map[string]keyEntry
	fetchedAt time.Time

	// refreshMu serialises fetches and guards lastAttempt.
	refreshMu   sync.Mutex
	lastAttempt time.Time
}

func (ik *issuerKeys) lookup(kid string, now time.Time, ttl time.Duration) (e keyEntry, ok, stale bool) {
	ik.mu.RLock()
	defer ik.mu.RUnlock()
	e, ok = ik.keys[kid]
	stale = ik.url != "" && now.Sub(ik.fetchedAt) >= ttl
	return e, ok, stale
}

// indexKeys indexes set by kid. A key without kid is unreachable and
// skipped; a kid that appears twice is ambiguous and never used.
func indexKeys(set jwk.Set) map[string]keyEntry {
	m := make(map[string]keyEntry, set.Len())
	for i := range set.Len() {
		k, ok := set.Key(i)
		if !ok {
			continue
		}
		kid, ok := k.KeyID()
		if !ok || kid == "" {
			continue
		}
		if _, dup := m[kid]; dup {
			m[kid] = keyEntry{problem: "appears more than once in the JWKS"}
			continue
		}
		m[kid] = entryFor(k)
	}
	return m
}

func entryFor(k jwk.Key) keyEntry {
	if alg, ok := k.Algorithm(); ok && alg.String() != algRS256 {
		return keyEntry{problem: fmt.Sprintf("is for alg %s, not RS256", quoteShort(alg.String()))}
	}
	if use, ok := k.KeyUsage(); ok && use != "sig" {
		return keyEntry{problem: fmt.Sprintf("has use %s, not sig", quoteShort(use))}
	}
	pk, err := k.PublicKey()
	if err != nil {
		return keyEntry{problem: "has no usable public key"}
	}
	var raw any
	if err := jwk.Export(pk, &raw); err != nil {
		return keyEntry{problem: "has no usable public key"}
	}
	var pub *rsa.PublicKey
	switch r := raw.(type) {
	case *rsa.PublicKey:
		pub = r
	case rsa.PublicKey:
		pub = &r
	}
	if pub == nil || pub.N == nil {
		return keyEntry{problem: "is not an RSA key"}
	}
	if bits := pub.N.BitLen(); bits < MinRSABits {
		return keyEntry{problem: fmt.Sprintf("is %d bits, shorter than %d", bits, MinRSABits)}
	}
	return keyEntry{pub: pub}
}

// key returns the usable key kid of issuer ik. For a URL-configured
// issuer an unknown kid or an expired cache triggers a fetch, rate-limited
// to one per MinRefreshInterval per issuer.
func (v *Verifier) key(ctx context.Context, ik *issuerKeys, kid string) (keyEntry, error) {
	now := v.cfg.Now()
	e, ok, stale := ik.lookup(kid, now, v.cfg.JWKSCacheTTL)
	if ik.url != "" && (!ok || stale) {
		v.maybeRefresh(ctx, ik, now)
		e, ok, _ = ik.lookup(kid, now, v.cfg.JWKSCacheTTL)
	}
	if !ok {
		return keyEntry{}, refuseToken(CounterRejectedKID, "kid", "%s is not in the issuer's JWKS", quoteShort(kid))
	}
	if e.problem != "" {
		return keyEntry{}, refuseToken(CounterRejectedAlgorithm, "kid", "the key %s %s", quoteShort(kid), e.problem)
	}
	return e, nil
}

// maybeRefresh fetches ik's JWKS unless a fetch was attempted less than
// MinRefreshInterval ago. A failure is counted and the cached set kept;
// it is not an error of the token being verified.
func (v *Verifier) maybeRefresh(ctx context.Context, ik *issuerKeys, now time.Time) {
	ik.refreshMu.Lock()
	defer ik.refreshMu.Unlock()
	if now.Sub(ik.lastAttempt) < v.cfg.MinRefreshInterval {
		v.counters.Inc(CounterJWKSRefreshLimited)
		return
	}
	// A failure is counted in refresh; the cached set stays in use.
	_ = v.refresh(ctx, ik, now)
}

// refresh fetches and installs ik's JWKS. The caller holds refreshMu or
// owns ik exclusively.
func (v *Verifier) refresh(ctx context.Context, ik *issuerKeys, now time.Time) error {
	ik.lastAttempt = now
	keys, err := v.fetch(ctx, ik.url)
	if err != nil {
		v.counters.Inc(CounterJWKSRefreshFailed)
		return err
	}
	v.counters.Inc(CounterJWKSRefresh)
	ik.mu.Lock()
	ik.keys = keys
	ik.fetchedAt = now
	ik.mu.Unlock()
	return nil
}

func (v *Verifier) fetch(ctx context.Context, url string) (map[string]keyEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("JWKS request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := v.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("JWKS fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS fetch: HTTP status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, v.cfg.MaxJWKSBytes+1))
	if err != nil {
		return nil, fmt.Errorf("JWKS read: %w", err)
	}
	if int64(len(body)) > v.cfg.MaxJWKSBytes {
		return nil, fmt.Errorf("JWKS larger than %d bytes", v.cfg.MaxJWKSBytes)
	}
	set, err := jwk.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("JWKS does not parse: %w", err)
	}
	if set.Len() == 0 {
		return nil, fmt.Errorf("JWKS has no keys")
	}
	return indexKeys(set), nil
}
