package alerting

import (
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/vectors"
	"github.com/rootxkit/uspace-core/zones"
)

// skipConfig is DefaultConfig with the conflict check switched as given.
func skipConfig(skip bool) Config {
	cfg := DefaultConfig()
	cfg.SkipConflicts = skip
	return cfg
}

// converge feeds A and B head-on, 500 m apart at t=0 and closing at
// 20 m/s, for six seconds, and returns every event and how many flying
// samples it fed.
func converge(m *Monitor) (raised, cleared []string, samples uint64) {
	for s := 0.0; s <= 5; s++ {
		pair := []Track{at("A", 10*s, 10, s), at("B", 500-10*s, -10, s)}
		for i := range pair {
			r, c := kinds(m.Observe(pair[i], s))
			raised = append(raised, r...)
			cleared = append(cleared, c...)
			samples++
		}
	}
	return raised, cleared, samples
}

// TestSkipConflictsPair is the E-01 pair: the same converging tracks
// raise conflict with the check on and nothing with it off, where every
// flying sample is counted as skipped and no pair is counted as not
// judged.
func TestSkipConflictsPair(t *testing.T) {
	// Presence: the check on raises the conflict once and holds it.
	m := NewMonitor(skipConfig(false))
	raised, cleared, _ := converge(m)
	if !slices.Equal(raised, []string{"conflict:A:B"}) || cleared != nil {
		t.Fatalf("check on: raised %v cleared %v", raised, cleared)
	}
	if act := m.Active(); len(act) != 1 || act[0].Key != "conflict:A:B" {
		t.Fatalf("check on: active %+v", act)
	}
	if n := m.Counters().Get(CounterConflictChecksSkipped); n != 0 {
		t.Fatalf("check on: conflict_checks_skipped = %d", n)
	}

	// Absence: the check off raises nothing and counts every sample.
	m = NewMonitor(skipConfig(true))
	raised, cleared, samples := converge(m)
	if raised != nil || cleared != nil {
		t.Fatalf("check off: raised %v cleared %v", raised, cleared)
	}
	if act := m.Active(); len(act) != 0 {
		t.Fatalf("check off: active %+v", act)
	}
	if n := m.Counters().Get(CounterConflictChecksSkipped); n != samples || samples != 12 {
		t.Fatalf("check off: conflict_checks_skipped = %d, want %d (12)", n, samples)
	}
	if n := m.Counters().Get(CounterPairsNotJudged); n != 0 {
		t.Fatalf("check off: conflict_pairs_not_judged = %d", n)
	}
	// Neither aircraft was put in the neighbour grid.
	if ids := m.grid.Near(at("A", 0, 0, 0).Pos, 10_000); len(ids) != 0 {
		t.Fatalf("check off: grid holds %v", ids)
	}
	if m.Tracked() != 2 {
		t.Fatalf("check off: tracked %d, want 2", m.Tracked())
	}
}

// TestSkipConflictsCountsFlyingSamplesOnly: a sample that is not flying,
// of unknown flying state or with an invalid position never reaches the
// conflict check, so it is not counted as skipped.
func TestSkipConflictsCountsFlyingSamplesOnly(t *testing.T) {
	m := NewMonitor(skipConfig(true))
	unknown := at("A", 0, 0, 0)
	unknown.Flying = nil
	m.Observe(unknown, 0)
	ground := at("B", 0, 0, 0)
	ground.Flying = ptr(false)
	m.Observe(ground, 0)
	bad := at("C", 0, 0, 0)
	bad.Pos.LatDeg = 91
	m.Observe(bad, 0)
	if n := m.Counters().Get(CounterConflictChecksSkipped); n != 0 {
		t.Fatalf("conflict_checks_skipped = %d, want 0", n)
	}
	m.Observe(at("D", 0, 0, 0), 0)
	if n := m.Counters().Get(CounterConflictChecksSkipped); n != 1 {
		t.Fatalf("conflict_checks_skipped = %d, want 1", n)
	}
}

// everything returns a monitor with the conflict check switched as given,
// a PROHIBITED zone and a 120 m height limit, and one sample of A inside
// the zone, 150 m above known ground, with an operator mismatch: it
// raises zone, identification, height and identification_mismatch.
func everything(t *testing.T, skip bool) (*Monitor, []string) {
	t.Helper()
	cfg := skipConfig(skip)
	cfg.Zones = []*zones.Zone{polygonZone(t, "Z", "PROHIBITED", "")}
	cfg.ZonePolicy.MaxHeightAGLM = ptr(120.0)
	m := NewMonitor(cfg)
	a := at("A", 0, 0, 0)
	a.AltAMSLM = ptr(650.0)
	a.Env = zones.Env{Ground: zones.GroundKnown, GroundM: 500}
	a.Identification = ident(core.IdentUnknownOperator, core.ReasonOperatorMismatch, true)
	raised, cleared := kinds(m.Observe(a, 0))
	if cleared != nil {
		t.Fatalf("cleared %v", cleared)
	}
	slices.Sort(raised)
	return m, raised
}

var everythingKeys = []string{"height:A", "identification:GEO:Z:A", "identification_mismatch:A", "zone:GEO:Z:A"}

// withReason sorts keys and appends =reason to each.
func withReason(keys []string, reason ClearReason) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k + "=" + string(reason)
	}
	slices.Sort(out)
	return out
}

func sortedCleared(ev Events) []string {
	_, c := kinds(ev)
	slices.Sort(c)
	return c
}

// TestSkipConflictsJudgesTheRest: with the conflict check off, zone,
// height and both identification alerts are raised exactly as with it
// on, and clear as stale, landed and by Drop, with no grid entry to
// remove (Remove of an id never inserted is a no-op).
func TestSkipConflictsJudgesTheRest(t *testing.T) {
	for _, skip := range []bool{false, true} {
		_, raised := everything(t, skip)
		if !slices.Equal(raised, everythingKeys) {
			t.Fatalf("skip=%v: raised %v, want %v", skip, raised, everythingKeys)
		}
	}

	// Stale: Tick past StaleAfterS clears every one as stale.
	m, _ := everything(t, true)
	if got, want := sortedCleared(m.Tick(15.5)), withReason(everythingKeys, ClearStale); !slices.Equal(got, want) {
		t.Fatalf("stale: cleared %v, want %v", got, want)
	}
	if m.Tracked() != 0 || len(m.Active()) != 0 {
		t.Fatalf("stale: tracked %d, active %+v", m.Tracked(), m.Active())
	}

	// Drop: every alert clears with the caller's reason.
	m, _ = everything(t, true)
	if got, want := sortedCleared(m.Drop("A", ClearFlightEnded, 1)), withReason(everythingKeys, ClearFlightEnded); !slices.Equal(got, want) {
		t.Fatalf("drop: cleared %v, want %v", got, want)
	}
	if m.Tracked() != 0 || len(m.Active()) != 0 {
		t.Fatalf("drop: tracked %d, active %+v", m.Tracked(), m.Active())
	}

	// Landed: the track stops (a grid Remove of an id never inserted) and
	// every alert but the mismatch clears as landed.
	m, _ = everything(t, true)
	down := at("A", 0, 0, 1)
	down.Flying = ptr(false)
	want := withReason([]string{"height:A", "identification:GEO:Z:A", "zone:GEO:Z:A"}, ClearLanded)
	if got := sortedCleared(m.Observe(down, 1)); !slices.Equal(got, want) {
		t.Fatalf("landed: cleared %v, want %v", got, want)
	}
	if act := m.Active(); len(act) != 1 || act[0].Key != "identification_mismatch:A" {
		t.Fatalf("landed: active %+v", act)
	}
}

// TestSkipConflictsIsReported: NewMonitor keeps the field as given and
// Config reports it; the default is off.
func TestSkipConflictsIsReported(t *testing.T) {
	if DefaultConfig().SkipConflicts {
		t.Fatal("DefaultConfig skips conflicts")
	}
	for _, skip := range []bool{false, true} {
		m := NewMonitor(skipConfig(skip))
		if got := m.Config().SkipConflicts; got != skip {
			t.Fatalf("Config().SkipConflicts = %v, want %v", got, skip)
		}
		if n := m.Counters().Get(CounterConfigInvalid); n != 0 {
			t.Fatalf("skip=%v: config_invalid = %d", skip, n)
		}
	}
}

// TestLifecycleMonitorJudgesConflicts: the monitor alert_lifecycle.json
// builds (DefaultConfig plus the file's policy header and each case's
// overrides) has the conflict check on, so a later change of the default
// shows in the diff.
func TestLifecycleMonitorJudgesConflicts(t *testing.T) {
	f := vectors.Load(t, "alert_lifecycle.json")
	var pol vPolicy
	f.Header(t, "policy", &pol)
	for _, c := range f.Cases {
		var in vInput
		c.Decode(t, &in, nil)
		if NewMonitor(configOf(t, pol, in.Config)).Config().SkipConflicts {
			t.Fatalf("%s: the vector monitor skips conflicts", c.Name)
		}
	}
}

// BenchmarkMonitorObserveSkipConflicts is BenchmarkMonitorObserve's
// layout (1000 aircraft over 14 km x 14 km, five zones) on a monitor with
// the conflict check off: the cost of zones, height and identification
// alone. Reported against the same target, which it is a cheaper path
// of; not a target of its own.
func BenchmarkMonitorObserveSkipConflicts(b *testing.B) {
	const n = 1000
	tracks, zs := fleet(n, 14_000)
	cfg := skipConfig(true)
	cfg.Zones = zs
	m := NewMonitor(cfg)
	stepS := 1.0 / float64(n)
	tS := 0.0
	observe := func(i int) {
		tr := &tracks[i%n]
		tr.CapturedAtS, tr.RxAtS = tS, tS
		*tr.SourceTS = tS
		m.Observe(*tr, tS)
		tS += stepS
	}
	for i := range n {
		observe(i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		observe(i)
	}
	b.ReportMetric(float64(len(m.active)), "active-alerts")
}
