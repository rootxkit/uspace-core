package core

import (
	"sort"
	"sync"
)

// Counters is a small set of named monotonic counters, safe for
// concurrent use. Every refusal, fallback, drop or anomaly in a package
// increments a named counter here, so that silence is distinguishable
// from health (LESSONS E-09). Names are snake_case and stable: the vectors
// compare them (rejected_backlog, zone_checks_not_evaluated, ...).
type Counters struct {
	mu sync.Mutex
	m  map[string]uint64
}

// Inc adds one to name.
func (c *Counters) Inc(name string) { c.Add(name, 1) }

// Add adds n to name.
func (c *Counters) Add(name string, n uint64) {
	c.mu.Lock()
	if c.m == nil {
		c.m = make(map[string]uint64)
	}
	c.m[name] += n
	c.mu.Unlock()
}

// Get returns the value of name (0 when never incremented).
func (c *Counters) Get(name string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[name]
}

// Snapshot returns a copy of every counter.
func (c *Counters) Snapshot() map[string]uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]uint64, len(c.m))
	for k, v := range c.m {
		out[k] = v
	}
	return out
}

// Names returns the counter names in sorted order, for status lines.
func (c *Counters) Names() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, 0, len(c.m))
	for k := range c.m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
