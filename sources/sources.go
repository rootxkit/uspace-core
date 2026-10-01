package sources

import (
	"sync"

	"github.com/rootxkit/uspace-core/core"
)

// Control is one switch: a whole source type when InstanceID is nil, one
// instance of it otherwise.
type Control struct {
	SourceType string
	InstanceID *string
	Enabled    bool
}

// State is a published source-control state. DefaultDeny travels with the
// state so every follower agrees on an instance with no row. Version
// orders states within an Epoch; a new Epoch is a restored database.
type State struct {
	Controls    []Control
	DefaultDeny bool
	Version     uint64
	Epoch       string
}

// Why says why a source is disabled. It travels with the refusal and the
// cleared alerts (LESSONS B-11): disabled by the authority is not silent.
type Why string

// The reasons a source is disabled.
const (
	WhyType        Why = "type"         // the whole type is switched off
	WhyInstance    Why = "instance"     // this instance is switched off
	WhyDefaultDeny Why = "default_deny" // no row for the instance and the state denies by default
)

// Decision is the answer to "is this source enabled, and if not why".
// WhyDisabled is nil exactly when Enabled is true.
type Decision struct {
	Enabled     bool
	WhyDisabled *Why
}

// Counter names of a Follower.
const (
	CounterApplied             = "applied"
	CounterIgnoredOlderVersion = "ignored_older_version"
	CounterNewEpoch            = "new_epoch"
)

func enabled() Decision { return Decision{Enabled: true} }

// disabled points at its own copy of w, so a caller writing through the
// pointer changes nothing shared.
func disabled(w Why) Decision { return Decision{WhyDisabled: &w} }

// Query decides whether the source (sourceType, instanceID) is enabled.
// A nil instanceID asks about the whole type.
//
//   - A type row that is off disables every instance (WhyType), even one
//     whose own row is on.
//   - A whole-type query is decided by the type row only: enabled unless it
//     is off, whatever DefaultDeny says.
//   - An instance row decides its instance: off is WhyInstance even when
//     the type row is on; on overrides DefaultDeny.
//   - With no row for the instance: enabled, unless DefaultDeny
//     (WhyDefaultDeny).
//
// When two rows have the same key the last one counts. A State with no
// rows and no DefaultDeny enables everything.
func (s State) Query(sourceType string, instanceID *string) Decision {
	// Scan from the end, so the last of duplicate rows counts, and stop
	// once both rows that can decide are found.
	var typeRow, instRow *Control
	for i := len(s.Controls) - 1; i >= 0 && (typeRow == nil || (instanceID != nil && instRow == nil)); i-- {
		c := &s.Controls[i]
		if c.SourceType != sourceType {
			continue
		}
		switch {
		case c.InstanceID == nil:
			if typeRow == nil {
				typeRow = c
			}
		case instanceID != nil && instRow == nil && *c.InstanceID == *instanceID:
			instRow = c
		}
	}
	var inst *bool
	if instRow != nil {
		inst = &instRow.Enabled
	}
	var typ *bool
	if typeRow != nil {
		typ = &typeRow.Enabled
	}
	return decide(typ, inst, instanceID == nil, s.DefaultDeny)
}

// decide applies the rules of Query to the looked-up rows (nil = no row).
func decide(typeEnabled, instEnabled *bool, wholeType, defaultDeny bool) Decision {
	switch {
	case typeEnabled != nil && !*typeEnabled:
		return disabled(WhyType)
	case wholeType:
		return enabled()
	case instEnabled != nil && *instEnabled:
		return enabled()
	case instEnabled != nil:
		return disabled(WhyInstance)
	case defaultDeny:
		return disabled(WhyDefaultDeny)
	}
	return enabled()
}

// clone copies s deeply, so neither the caller nor the follower can change
// the other's state through a shared slice or pointer.
func (s State) clone() State {
	out := s
	if s.Controls != nil {
		out.Controls = make([]Control, len(s.Controls))
		for i, c := range s.Controls {
			if c.InstanceID != nil {
				id := *c.InstanceID
				c.InstanceID = &id
			}
			out.Controls[i] = c
		}
	}
	return out
}

type rowKey struct {
	sourceType string
	instanceID string
	whole      bool
}

// Follower holds the last State applied, as every consumer of the switches
// does (LESSONS B-09). It is safe for concurrent use.
//
// Callers that refuse a disabled source answer 503 with Retry-After (and
// close an open session with 1013), never 401 or 403, which clients treat
// as fatal (B-10); the refusal is counted by the caller.
type Follower struct {
	mu       sync.RWMutex
	state    State
	index    map[rowKey]bool
	has      bool
	counters core.Counters
}

// NewFollower returns a Follower with no state: everything is enabled.
func NewFollower() *Follower {
	return &Follower{}
}

// Apply takes in if it moves forward, and reports whether it did. The
// first state is always taken. Within the same Epoch only a strictly
// higher Version is taken (an equal or lower one is counted as
// ignored_older_version); a state from another Epoch (a restored
// database) is taken whatever its Version and counted as new_epoch. Every
// state taken is counted as applied. in is copied.
func (f *Follower) Apply(in State) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.has && in.Epoch == f.state.Epoch && in.Version <= f.state.Version {
		f.counters.Inc(CounterIgnoredOlderVersion)
		return false
	}
	if f.has && in.Epoch != f.state.Epoch {
		f.counters.Inc(CounterNewEpoch)
	}
	f.state = in.clone()
	f.index = make(map[rowKey]bool, len(in.Controls))
	for _, c := range in.Controls {
		k := rowKey{sourceType: c.SourceType, whole: c.InstanceID == nil}
		if c.InstanceID != nil {
			k.instanceID = *c.InstanceID
		}
		f.index[k] = c.Enabled
	}
	f.has = true
	f.counters.Inc(CounterApplied)
	return true
}

// State returns a copy of the state held, and false when none has been
// applied yet.
func (f *Follower) State() (State, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if !f.has {
		return State{}, false
	}
	return f.state.clone(), true
}

// Query is State.Query on the state held. With no state everything is
// enabled: a follower never fails closed (B-09).
func (f *Follower) Query(sourceType string, instanceID *string) Decision {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if !f.has {
		return enabled()
	}
	var typ, inst *bool
	if v, ok := f.index[rowKey{sourceType: sourceType, whole: true}]; ok {
		typ = &v
	}
	if instanceID != nil {
		if v, ok := f.index[rowKey{sourceType: sourceType, instanceID: *instanceID}]; ok {
			inst = &v
		}
	}
	return decide(typ, inst, instanceID == nil, f.state.DefaultDeny)
}

// Counters returns the follower's counters: applied,
// ignored_older_version and new_epoch.
func (f *Follower) Counters() *core.Counters {
	return &f.counters
}
