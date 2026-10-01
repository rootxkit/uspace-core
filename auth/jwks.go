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

	// refreshMu guards lastAttempt and inflight: at most one fetch runs
	// per issuer, and every request that needs it waits on inflight.
	refreshMu   sync.Mutex
	lastAttempt time.Time
	inflight    chan struct{}
}

// lookup returns the entry for kid and the age of the cached set.
func (ik *issuerKeys) lookup(kid string, now time.Time) (e keyEntry, ok bool, age time.Duration) {
	ik.mu.RLock()
	defer ik.mu.RUnlock()
	e, ok = ik.keys[kid]
	return e, ok, now.Sub(ik.fetchedAt)
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
// issuer:
//
//   - an unknown kid starts a fetch (or joins the one running) and waits
//     for it, or for the caller's context, whichever ends first;
//   - a known kid is served from the cache at once; when the cached set
//     is within JWKSRefreshAhead of its TTL, or past it, a fetch starts
//     in the background. During an issuer outage the request never waits
//     on the issuer, and the cached keys keep serving (T5).
//
// Fetches are single-flight per issuer and rate-limited to one per
// MinRefreshInterval.
func (v *Verifier) key(ctx context.Context, ik *issuerKeys, kid string) (keyEntry, error) {
	now := v.cfg.Now()
	e, ok, age := ik.lookup(kid, now)
	if ik.url != "" {
		switch {
		case !ok:
			if done := v.startRefresh(ctx, ik, now, true); done != nil {
				select {
				case <-done:
				case <-ctx.Done():
				}
				e, ok, _ = ik.lookup(kid, now)
			}
		case age >= v.cfg.JWKSCacheTTL-v.cfg.JWKSRefreshAhead:
			v.startRefresh(ctx, ik, now, false)
		}
	}
	if !ok {
		return keyEntry{}, refuseToken(CounterRejectedKID, "kid", "%s is not in the issuer's JWKS", quoteShort(kid))
	}
	if e.problem != "" {
		return keyEntry{}, refuseToken(CounterRejectedAlgorithm, "kid", "the key %s %s", quoteShort(kid), e.problem)
	}
	return e, nil
}

// startRefresh starts a background fetch of ik's JWKS and returns a
// channel closed when it ends. It joins a fetch already running, and
// returns nil when the rate limit forbids a new one (counted when
// countLimited, that is when the request needed the fetch).
//
// The caller's context cannot fail the fetch: a request whose context is
// already done starts nothing and leaves the rate limit untouched, and a
// started fetch runs under context.WithoutCancel with JWKSFetchTimeout.
// Otherwise an unauthenticated request with an unknown kid and a
// cancelled context would stamp the rate limit with a failed fetch and
// keep a rotated key out for MinRefreshInterval, again and again.
func (v *Verifier) startRefresh(ctx context.Context, ik *issuerKeys, now time.Time, countLimited bool) <-chan struct{} {
	if ctx.Err() != nil {
		return nil
	}
	ik.refreshMu.Lock()
	defer ik.refreshMu.Unlock()
	if ik.inflight != nil {
		return ik.inflight
	}
	if now.Sub(ik.lastAttempt) < v.cfg.MinRefreshInterval {
		if countLimited {
			v.counters.Inc(CounterJWKSRefreshLimited)
		}
		return nil
	}
	ik.lastAttempt = now
	done := make(chan struct{})
	ik.inflight = done
	fctx := context.WithoutCancel(ctx)
	v.background.Add(1)
	go func() {
		defer v.background.Done()
		// A failure is counted in refresh; the cached set stays in use.
		_ = v.refresh(fctx, ik, now)
		ik.refreshMu.Lock()
		ik.inflight = nil
		ik.refreshMu.Unlock()
		close(done)
	}()
	return done
}

// refresh fetches and installs ik's JWKS within JWKSFetchTimeout.
func (v *Verifier) refresh(ctx context.Context, ik *issuerKeys, now time.Time) error {
	fctx, cancel := context.WithTimeout(ctx, v.cfg.JWKSFetchTimeout)
	defer cancel()
	keys, err := v.fetch(fctx, ik.url)
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
