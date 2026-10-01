package sources

import (
	"strconv"
	"sync"
	"testing"
)

func ptr(s string) *string { return &s }

func checkDecision(t *testing.T, what string, got Decision, wantEnabled bool, wantWhy Why) {
	t.Helper()
	if got.Enabled != wantEnabled {
		t.Errorf("%s: enabled %v, want %v", what, got.Enabled, wantEnabled)
	}
	switch {
	case wantEnabled && got.WhyDisabled != nil:
		t.Errorf("%s: enabled with a reason %q", what, *got.WhyDisabled)
	case !wantEnabled && got.WhyDisabled == nil:
		t.Errorf("%s: disabled without a reason", what)
	case !wantEnabled && *got.WhyDisabled != wantWhy:
		t.Errorf("%s: why %q, want %q", what, *got.WhyDisabled, wantWhy)
	}
}

// TestQueryPairs runs each disabling rule beside the change that enables
// the same source (E-01), on State.Query and on a Follower holding the
// same state.
func TestQueryPairs(t *testing.T) {
	off := func(typ string, id *string) Control { return Control{SourceType: typ, InstanceID: id} }
	on := func(typ string, id *string) Control { return Control{SourceType: typ, InstanceID: id, Enabled: true} }
	tests := []struct {
		name     string
		state    State
		typ      string
		id       *string
		enabled  bool
		why      Why
		twin     State // the state under which the same query is enabled
		twinNote string
	}{
		{"type off", State{Controls: []Control{off("rid", nil), on("rid", ptr("rx-1"))}}, "rid", ptr("rx-1"), false, WhyType,
			State{Controls: []Control{on("rid", nil), on("rid", ptr("rx-1"))}}, "type on"},
		{"type off whole-type query", State{Controls: []Control{off("rid", nil)}}, "rid", nil, false, WhyType,
			State{Controls: []Control{off("relay", nil)}}, "other type off"},
		{"instance off", State{Controls: []Control{on("relay", nil), off("relay", ptr("gs-1"))}}, "relay", ptr("gs-1"), false, WhyInstance,
			State{Controls: []Control{on("relay", nil), off("relay", ptr("gs-2"))}}, "other instance off"},
		{"default deny", State{DefaultDeny: true, Controls: []Control{on("relay", nil)}}, "relay", ptr("gs-9"), false, WhyDefaultDeny,
			State{DefaultDeny: true, Controls: []Control{on("relay", ptr("gs-9"))}}, "instance row on"},
		{"last duplicate row counts", State{Controls: []Control{on("relay", ptr("gs-1")), off("relay", ptr("gs-1"))}}, "relay", ptr("gs-1"), false, WhyInstance,
			State{Controls: []Control{off("relay", ptr("gs-1")), on("relay", ptr("gs-1"))}}, "reversed"},
		{"last duplicate type row counts", State{Controls: []Control{on("relay", nil), off("relay", nil)}}, "relay", ptr("gs-1"), false, WhyType,
			State{Controls: []Control{off("relay", nil), on("relay", nil)}}, "reversed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkDecision(t, "state", tt.state.Query(tt.typ, tt.id), tt.enabled, tt.why)
			checkDecision(t, "twin "+tt.twinNote, tt.twin.Query(tt.typ, tt.id), true, "")
			f := NewFollower()
			f.Apply(tt.state)
			checkDecision(t, "follower", f.Query(tt.typ, tt.id), tt.enabled, tt.why)
			tt.twin.Version = 1
			f.Apply(tt.twin)
			checkDecision(t, "follower twin", f.Query(tt.typ, tt.id), true, "")
		})
	}
	// Whole-type query under default deny: only the type row decides.
	s := State{DefaultDeny: true}
	checkDecision(t, "whole type default deny", s.Query("relay", nil), true, "")
}

func TestDecisionPointerIsOwned(t *testing.T) {
	s := State{Controls: []Control{{SourceType: "rid"}}}
	d := s.Query("rid", nil)
	*d.WhyDisabled = "changed"
	checkDecision(t, "after a write through a previous answer", s.Query("rid", nil), false, WhyType)
}

func TestFollowerNoState(t *testing.T) {
	f := NewFollower()
	if _, ok := f.State(); ok {
		t.Fatal("State reports a state before any Apply")
	}
	// B-09: never fail closed.
	checkDecision(t, "no state", f.Query("relay", ptr("gs-1")), true, "")
	checkDecision(t, "no state whole type", f.Query("relay", nil), true, "")
	if n := len(f.Counters().Snapshot()); n != 0 {
		t.Errorf("counters before any Apply: %v", f.Counters().Snapshot())
	}
	// E-01 twin: the same query after a deny-all state is applied.
	f.Apply(State{DefaultDeny: true, Epoch: "e1"})
	checkDecision(t, "after apply", f.Query("relay", ptr("gs-1")), false, WhyDefaultDeny)
}

// TestFollowerApply drives B-09: higher, equal and lower versions within
// an epoch, then a new epoch with a lower version, and reads the counters
// back (E-02).
func TestFollowerApply(t *testing.T) {
	f := NewFollower()
	off := []Control{{SourceType: "relay", InstanceID: ptr("gs-1")}}
	steps := []struct {
		name    string
		in      State
		applied bool
		version uint64
		epoch   string
	}{
		{"first state, any version", State{Version: 5, Epoch: "a"}, true, 5, "a"},
		{"higher version", State{Version: 6, Epoch: "a", Controls: off}, true, 6, "a"},
		{"equal version", State{Version: 6, Epoch: "a"}, false, 6, "a"},
		{"lower version", State{Version: 2, Epoch: "a"}, false, 6, "a"},
		{"new epoch, lower version", State{Version: 1, Epoch: "b"}, true, 1, "b"},
		{"same new epoch, higher", State{Version: 2, Epoch: "b", Controls: off}, true, 2, "b"},
		{"back to the old epoch", State{Version: 0, Epoch: "a"}, true, 0, "a"},
	}
	for _, st := range steps {
		if got := f.Apply(st.in); got != st.applied {
			t.Errorf("%s: applied %v, want %v", st.name, got, st.applied)
		}
		s, ok := f.State()
		if !ok || s.Version != st.version || s.Epoch != st.epoch {
			t.Errorf("%s: state v%d %q (%v), want v%d %q", st.name, s.Version, s.Epoch, ok, st.version, st.epoch)
		}
	}
	c := f.Counters()
	want := map[string]uint64{CounterApplied: 5, CounterIgnoredOlderVersion: 2, CounterNewEpoch: 2}
	for name, w := range want {
		if got := c.Get(name); got != w {
			t.Errorf("counter %s = %d, want %d", name, got, w)
		}
	}
	if len(c.Snapshot()) != len(want) {
		t.Errorf("unexpected counters: %v", c.Snapshot())
	}
}

// TestFollowerCopies checks the follower neither shares the caller's
// slice nor hands out its own.
func TestFollowerCopies(t *testing.T) {
	id := "gs-1"
	in := State{Version: 1, Controls: []Control{{SourceType: "relay", InstanceID: &id}}}
	f := NewFollower()
	f.Apply(in)
	in.Controls[0].Enabled = true
	id = "gs-2"
	checkDecision(t, "after the caller changed its input", f.Query("relay", ptr("gs-1")), false, WhyInstance)
	s, _ := f.State()
	s.Controls[0].Enabled = true
	*s.Controls[0].InstanceID = "gs-3"
	s2, _ := f.State()
	if s2.Controls[0].Enabled || *s2.Controls[0].InstanceID != "gs-1" {
		t.Errorf("State handed out the follower's own slice: %+v", s2.Controls[0])
	}
	// A state with no controls round-trips as such.
	f.Apply(State{Version: 2})
	if s3, _ := f.State(); s3.Controls != nil {
		t.Errorf("nil controls became %v", s3.Controls)
	}
}

// TestFollowerConcurrent drives Apply, Query, State and Counters from
// several goroutines; run with -race (PLAN 8.3).
func TestFollowerConcurrent(t *testing.T) {
	f := NewFollower()
	var wg sync.WaitGroup
	const writers, readers, rounds = 4, 8, 300
	for w := range writers {
		wg.Go(func() {
			for i := range rounds {
				f.Apply(State{
					Version:  uint64(i),
					Epoch:    "e" + strconv.Itoa(w%2),
					Controls: []Control{{SourceType: "relay", InstanceID: ptr("gs-" + strconv.Itoa(i%3)), Enabled: i%2 == 0}},
				})
			}
		})
	}
	for range readers {
		wg.Go(func() {
			for i := range rounds {
				d := f.Query("relay", ptr("gs-"+strconv.Itoa(i%3)))
				if d.Enabled == (d.WhyDisabled != nil) {
					t.Errorf("inconsistent decision %+v", d)
					return
				}
				_, _ = f.State()
				_ = f.Counters().Get(CounterApplied)
			}
		})
	}
	wg.Wait()
	c := f.Counters()
	if c.Get(CounterApplied)+c.Get(CounterIgnoredOlderVersion) != writers*rounds {
		t.Errorf("every Apply is counted once: %v", c.Snapshot())
	}
}

func fiftyControls() State {
	s := State{Version: 1, Epoch: "e"}
	for i := range 50 {
		var id *string
		if i%10 != 0 {
			id = ptr("gs-" + strconv.Itoa(i))
		}
		s.Controls = append(s.Controls, Control{SourceType: "type-" + strconv.Itoa(i/10), InstanceID: id, Enabled: i%3 != 0})
	}
	return s
}

func BenchmarkQuery(b *testing.B) {
	s := fiftyControls()
	id := ptr("gs-47")
	b.Run("state", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = s.Query("type-4", id)
		}
	})
	// Worst case: a type with no rows, every row is read.
	b.Run("state-full-scan", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = s.Query("type-9", id)
		}
	})
	f := NewFollower()
	f.Apply(s)
	b.Run("follower", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			_ = f.Query("type-4", id)
		}
	})
}
