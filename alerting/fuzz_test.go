package alerting

import (
	"math"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/sources"
	"github.com/rootxkit/uspace-core/zones"
)

// fuzzMaxAircraft keeps the fuzzed monitor small, so eviction runs.
const fuzzMaxAircraft = 4

// fuzzReader hands out the fuzz input byte by byte, zero when exhausted.
type fuzzReader struct {
	b []byte
}

func (r *fuzzReader) byte() byte {
	if len(r.b) == 0 {
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

// num maps a byte onto a value in [lo, hi], or a non-finite value for a
// few byte values, so the bad numbers of C-09 are fuzzed too.
func (r *fuzzReader) num(lo, hi float64) float64 {
	v := r.byte()
	switch v {
	case 255:
		return math.NaN()
	case 254:
		return math.Inf(1)
	case 253:
		return math.Inf(-1)
	}
	return lo + (hi-lo)*float64(v)/252
}

var fuzzIdents = []*core.Identification{
	nil,
	{Status: core.IdentRegistered, Reason: core.ReasonMatched},
	{Status: core.IdentUnidentified, Reason: core.ReasonNoSerial},
	{Status: core.IdentUnknownOperator, Reason: core.ReasonOperatorMismatch, Mismatch: true},
}

// fuzzTrack builds a track from the reader near the vectors' origin.
func fuzzTrack(r *fuzzReader, tS float64) Track {
	flags := r.byte()
	id := string(rune('A' + int(r.byte()%6)))
	tr := Track{
		ID:      id,
		Pos:     core.LatLon{LatDeg: originLatDeg + r.num(-0.01, 0.01), LonDeg: originLonDeg + r.num(-0.01, 0.01)},
		VNMS:    r.num(-30, 30),
		VEMS:    r.num(-30, 30),
		RxAtS:   tS - r.num(-3, 12),
		Source:  []string{"relay", "remote_id"}[flags&1],
		Station: []string{"", "s1", "s2"}[int(flags>>1&3)%3],
		Backlog: flags&8 != 0 && flags&16 != 0,
	}
	// Placed behind its receipt, or up to 5 s ahead of it: the clock-ahead
	// rule must refuse what is beyond AheadToleranceS.
	tr.CapturedAtS = tr.RxAtS - r.num(-5, 20)
	ts := tr.CapturedAtS + r.num(-5, 5)
	tr.SourceTS = &ts
	alt := r.num(300, 900)
	tr.AltAMSLM = &alt
	tr.AltSource = []core.AltSource{core.AltGeodetic, core.AltPressure, core.AltNetwork, ""}[flags>>5&3]
	switch r.byte() % 4 {
	case 0:
		tr.Flying = nil
	case 1:
		tr.Flying = ptr(false)
	default:
		tr.Flying = ptr(true)
	}
	tr.Identification = fuzzIdents[flags>>3&3]
	if flags&128 != 0 {
		tr.Transmitter = ptr("TX")
		tr.Identified = ptr(flags&64 != 0)
	}
	tr.Env = zones.Env{Ground: zones.GroundKind(flags % 3), GroundM: 400}
	return tr
}

func fuzzMonitor() *Monitor {
	cfg := DefaultConfig()
	cfg.MaxAircraft = fuzzMaxAircraft
	cfg.MaxSourcesPerAircraft = 2
	cfg.ZonePolicy.MaxHeightAGLM = ptr(120.0)
	p := boxZone("P", core.ZoneProhibited, 0, 0, 1000)
	p.Lower = &zones.Limit{ValueM: 0, Ref: core.RefAGL}
	p.Upper = &zones.Limit{ValueM: 150, Ref: core.RefAGL}
	cfg.Zones = []*zones.Zone{p, boxZone("C", core.ZoneConditional, 300, 300, 800)}
	return NewMonitor(cfg)
}

// activeKeys is the set of active alert keys.
func activeKeys(m *Monitor) map[string]Alert {
	out := make(map[string]Alert, len(m.active))
	for k, s := range m.active {
		out[k] = s.snapshot()
	}
	return out
}

// FuzzMonitor drives one monitor with a fuzzed sequence of observations,
// ticks, source switches and drops, and checks the state machine's
// invariants after every call:
//
//   - it never panics, and the aircraft and alerts held stay bounded;
//   - the active set after a call is the set before, plus what it raised,
//     less what it cleared, and a clear is always of an alert that was
//     active or raised in the same call;
//   - an alert's LastTrueS and LastFalseS never go back;
//   - every clear has a reason the call may give: resolved only with an
//     alert shown false for more than the hysteresis; stale only when an
//     aircraft of the alert is no longer tracked for it; landed only from
//     an observation; source_disabled only from a switch; the Drop
//     reason only from Drop; nothing is ever cleared to make room;
//   - an observation or tick at a non-finite wall time changes nothing
//     (a switch or a drop is the caller's explicit act and still
//     applies), and an observation whose position is not valid resolves
//     nothing judged from the position.
func FuzzMonitor(f *testing.F) {
	f.Add([]byte{0, 0, 0, 126, 126, 160, 126, 0, 0, 2, 0, 1, 1, 126, 200, 100, 126, 0, 0, 2})
	f.Add([]byte{0, 0, 0, 126, 126, 126, 126, 0, 0, 1, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 4, 0, 1})
	f.Add([]byte{0, 0x88, 0, 126, 126, 160, 126, 0, 0, 2, 0, 0x88, 1, 126, 127, 160, 126, 0, 0, 2})
	f.Add([]byte{0, 0x18, 0, 126, 126, 255, 126, 0, 0, 2, 1, 200})
	f.Fuzz(func(t *testing.T, data []byte) {
		m := fuzzMonitor()
		r := &fuzzReader{b: data}
		wallS, maxWallS := 0.0, 0.0
		var sw sources.State
		sw.Epoch = "fuzz"
		for len(r.b) > 0 {
			op := r.byte() % 5
			wallS += r.num(0, 6)
			if r.byte() == 251 {
				wallS = math.NaN()
			}
			if core.IsFinite(wallS) {
				maxWallS = math.Max(maxWallS, wallS)
			}
			before := activeKeys(m)
			var ev Events
			var tr Track
			allowed := map[ClearReason]bool{ClearResolved: true, ClearStale: true}
			switch op {
			case 0, 1:
				tr = fuzzTrack(r, wallS)
				ev = m.Observe(tr, wallS)
				allowed[ClearLanded] = true
			case 2:
				ev = m.Tick(wallS)
			case 3:
				sw.Version++
				typ := []string{"relay", "remote_id"}[r.byte()&1]
				var inst *string
				if b := r.byte(); b&1 != 0 {
					inst = ptr([]string{"", "s1", "s2"}[int(b>>1)%3])
				}
				sw.Controls = append(sw.Controls[:0], sources.Control{SourceType: typ, InstanceID: inst, Enabled: r.byte()&1 != 0})
				ev = m.SwitchSource(sw, wallS)
				allowed[ClearSourceDisabled] = true
			case 4:
				reason := []ClearReason{ClearFlightEnded, ClearLanded, ""}[r.byte()%3]
				ev = m.Drop(string(rune('A'+int(r.byte()%6))), reason, wallS)
				if reason != "" {
					allowed[reason] = true
				}
			}
			checkInvariants(t, m, op, tr, wallS, maxWallS, before, ev, allowed)
			if math.IsNaN(wallS) {
				wallS = 0
			}
		}
	})
}

func checkInvariants(t *testing.T, m *Monitor, op byte, tr Track, wallS, maxWallS float64, before map[string]Alert, ev Events, allowed map[ClearReason]bool) {
	t.Helper()
	if m.Tracked() > fuzzMaxAircraft {
		t.Fatalf("%d aircraft held", m.Tracked())
	}
	for _, ac := range m.aircraft {
		if ac.placedS > maxWallS+m.cfg.AheadToleranceS {
			t.Fatalf("%s placed at %v, beyond wall %v + tolerance", ac.id, ac.placedS, maxWallS)
		}
	}
	for k, a := range activeKeys(m) {
		if a.LastTrueS > maxWallS || (a.ShownFalse && a.LastFalseS > maxWallS) {
			t.Fatalf("%s timed beyond the wall %v: %+v", k, maxWallS, a)
		}
	}
	evictable := false
	perSource := map[sourceKey]int{}
	for _, ac := range m.aircraft {
		evictable = evictable || len(ac.alerts) == 0
		if len(ac.alerts) > 0 {
			perSource[ac.src]++
		}
	}
	if m.CapacityExceeded() != (m.Tracked() >= fuzzMaxAircraft && !evictable) {
		t.Fatalf("CapacityExceeded %v with %d held, evictable %v", m.CapacityExceeded(), m.Tracked(), evictable)
	}
	if len(perSource) != len(m.bySource) {
		t.Fatalf("source counts %v, held %v", m.bySource, perSource)
	}
	for k, n := range perSource {
		if m.bySource[k] != n {
			t.Fatalf("source counts %v, held %v", m.bySource, perSource)
		}
	}
	for i := range ev.Refused {
		r := ev.Refused[i]
		if r.ID == "" || (r.Reason != RefusedSourceShare && r.Reason != RefusedCapacity) {
			t.Fatalf("refusal %+v", r)
		}
		// Checked at the refusal: the sweep after it may clear alerts.
		if r.Reason == RefusedSourceShare && r.SourceAlertHolders < m.sourceShareLimit() {
			t.Fatalf("source_share refusal with %d alert holders, limit %d", r.SourceAlertHolders, m.sourceShareLimit())
		}
	}
	pooled := 0
	for _, p := range m.pools {
		pooled += p.Len()
	}
	held := 0
	for _, ac := range m.aircraft {
		if (len(ac.alerts) == 0) != (ac.elem != nil) || ac.pool != poolOf(ac) {
			t.Fatalf("%s in pool %d with %d alerts", ac.id, ac.pool, len(ac.alerts))
		}
		if ac.elem != nil {
			held++
		}
	}
	if pooled != held {
		t.Fatalf("%d pooled, %d aircraft without alerts", pooled, held)
	}
	// Conflicts among the held aircraft, plus per aircraft two zone
	// alerts, two identification alerts, height and mismatch.
	if maxActive := fuzzMaxAircraft*(fuzzMaxAircraft-1)/2 + fuzzMaxAircraft*6; len(m.active) > maxActive {
		t.Fatalf("%d active alerts, bound %d", len(m.active), maxActive)
	}
	if !core.IsFinite(wallS) && op <= 2 && (len(ev.Raised) > 0 || len(ev.Cleared) > 0) {
		t.Fatalf("a call at wall %v changed something: %+v", wallS, ev)
	}
	want := make(map[string]bool, len(before))
	for k := range before {
		want[k] = true
	}
	for i := range ev.Raised {
		want[ev.Raised[i].Key] = true
	}
	for i := range ev.Cleared {
		c := &ev.Cleared[i]
		if !want[c.Key] {
			t.Fatalf("cleared %s, which was not active", c.Key)
		}
		delete(want, c.Key)
		if c.Reason == "" || !allowed[c.Reason] {
			t.Fatalf("op %d cleared %s with reason %q", op, c.Key, c.Reason)
		}
		switch c.Reason {
		case ClearResolved:
			if !c.ShownFalse || !(c.LastFalseS-c.LastTrueS > m.cfg.ClearAfterS) {
				t.Fatalf("resolved without evidence: %+v", c.Alert)
			}
			// Alert times are placements capped at the wall time: a sample
			// from a clock ahead, even within the tolerance, cannot buy the
			// hysteresis.
			if c.LastFalseS > wallS {
				t.Fatalf("resolved by a placement ahead of the wall (%v): %+v", wallS, c.Alert)
			}
			if op <= 1 && !tr.Pos.Valid() && c.Kind != KindIdentificationMismatch && slices.Contains(c.Aircraft, tr.ID) {
				t.Fatalf("an invalid position resolved %s", c.Key)
			}
		case ClearStale:
			gone := false
			for _, id := range c.Aircraft {
				ac, ok := m.aircraft[id]
				switch {
				case !ok:
					gone = true
				case c.Kind == KindIdentificationMismatch:
					gone = gone || !ac.hasIdentity
				default:
					gone = gone || !ac.hasTrack
				}
			}
			if !gone {
				t.Fatalf("stale clear of %s with every aircraft still tracked", c.Key)
			}
		case ClearSourceDisabled, ClearLanded, ClearFlightEnded:
			// Allowed by the call (checked above); no state to check.
		}
	}
	got := activeKeys(m)
	if len(got) != len(want) {
		t.Fatalf("active %d, want %d", len(got), len(want))
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("%s missing from the active set", k)
		}
		if old, ok := before[k]; ok {
			cur := got[k]
			if cur.LastTrueS < old.LastTrueS || (old.ShownFalse && cur.LastFalseS < old.LastFalseS) {
				t.Fatalf("%s went back in time: %+v then %+v", k, old, cur)
			}
		}
		for _, id := range got[k].Aircraft {
			ac, ok := m.aircraft[id]
			if !ok {
				t.Fatalf("%s names %s, which is not held", k, id)
			}
			if _, ok := ac.alerts[k]; !ok {
				t.Fatalf("%s not indexed under %s", k, id)
			}
		}
	}
}
