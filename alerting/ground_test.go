package alerting

import (
	"math"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/zones"
)

// declaredGround is at() with the status "not flying": what a Remote ID
// transmitter that leaves its status at Ground, or a disarmed MAVLink
// vehicle, delivers.
func declaredGround(id string, northM, vnMS, tS float64) Track {
	a := at(id, northM, vnMS, tS)
	a.Flying = ptr(false)
	return a
}

// onGround sets a known ground heightAGLM below the track's altitude.
func onGround(a Track, heightAGLM float64) Track {
	a.Env = zones.Env{Ground: zones.GroundKnown, GroundM: *a.AltAMSLM - heightAGLM}
	return a
}

// groundConfig is DefaultConfig with the ground bounds g and one
// PROHIBITED zone without limits around the origin.
func groundConfig(t *testing.T, g GroundTrust) Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Zones = []*zones.Zone{polygonZone(t, "Z", "PROHIBITED", "")}
	cfg.GroundTrust = g
	return cfg
}

func groundCounters(m *Monitor) [3]uint64 {
	c := m.Counters()
	return [3]uint64{c.Get(CounterGroundContradicted), c.Get(CounterGroundContradictedSpeed), c.Get(CounterGroundContradictedHeight)}
}

// A track that says it is on the ground while it crosses a PROHIBITED
// zone at 15 m/s is judged as flying: the zone alert is raised and kept
// (review 2026-11-12 F3: it raised nothing, for 30 s, with nothing
// active).
func TestDeclaredGroundContradictedBySpeed(t *testing.T) {
	m := NewMonitor(groundConfig(t, DefaultGroundTrust()))
	var raised []string
	for s := 0.0; s < 30; s++ {
		ev := m.Observe(declaredGround("A", -200+15*s, 15, s), s)
		r, cleared := kinds(ev)
		if len(cleared) != 0 {
			t.Fatalf("t=%v: cleared %v", s, cleared)
		}
		raised = append(raised, r...)
	}
	if !slices.Equal(raised, []string{"zone:GEO:Z:A"}) {
		t.Fatalf("raised %v, want the zone alert once", raised)
	}
	if a := m.Active(); len(a) != 1 || a[0].Severity != core.SeverityCritical || a[0].LastTrueS != 29 {
		t.Fatalf("active %+v, want the critical alert last true at 29", a)
	}
	// Counted, by speed only: at() has no known ground.
	if got := groundCounters(m); got != [3]uint64{30, 30, 0} {
		t.Fatalf("ground_status_contradicted, _speed, _height = %v, want 30 30 0", got)
	}
}

// The same by height alone: hovering 150 m above known ground.
func TestDeclaredGroundContradictedByHeight(t *testing.T) {
	m := NewMonitor(groundConfig(t, DefaultGroundTrust()))
	wantEvents(t, "hover at 150 m", m.Observe(onGround(declaredGround("A", 0, 0, 0), 150), 0), []string{"zone:GEO:Z:A"}, nil)
	if got := groundCounters(m); got != [3]uint64{1, 0, 1} {
		t.Fatalf("counters %v, want 1 0 1", got)
	}
	// Both at once count the sample once and each evidence once.
	m.Observe(onGround(declaredGround("A", 0, 15, 1), 150), 1)
	if got := groundCounters(m); got != [3]uint64{2, 1, 2} {
		t.Fatalf("counters %v, want 2 1 2", got)
	}
}

// The absence pairs: a declared ground status that the sample does not
// contradict is a landing, exactly as without bounds. The bounds are
// inclusive: at the bound is believed.
func TestDeclaredGroundBelievedWhenConsistent(t *testing.T) {
	g := DefaultGroundTrust()
	for name, down := range map[string]func(tS float64) Track{
		"still, ground unknown": func(tS float64) Track { return declaredGround("A", 0, 0, tS) },
		"still, on the ground":  func(tS float64) Track { return onGround(declaredGround("A", 0, 0, tS), 2) },
		"at both bounds": func(tS float64) Track {
			return onGround(declaredGround("A", 0, g.MaxSpeedMS, tS), g.MaxHeightAGLM)
		},
		"carried at a walk": func(tS float64) Track { return onGround(declaredGround("A", 0, 1.5, tS), 1) },
		// R-09: a pressure altitude is not a height to contradict with.
		"pressure altitude far above the ground": func(tS float64) Track {
			a := onGround(declaredGround("A", 0, 0, tS), 400)
			a.AltSource = core.AltPressure
			return a
		},
		"no altitude": func(tS float64) Track {
			a := onGround(declaredGround("A", 0, 0, tS), 400)
			a.AltAMSLM, a.AltSource = nil, core.AltNone
			return a
		},
		"high over ground that is not known": func(tS float64) Track {
			a := declaredGround("A", 0, 0, tS)
			a.Env = zones.Env{Ground: zones.GroundUnknown, GroundM: 0}
			return a
		},
		"unknown velocity (NaN)": func(tS float64) Track {
			a := declaredGround("A", 0, 0, tS)
			a.VNMS, a.VEMS, a.VDMS = math.NaN(), math.NaN(), math.NaN()
			return a
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := NewMonitor(groundConfig(t, g))
			wantEvents(t, "on the ground from the start", m.Observe(down(0), 0), nil, nil)
			wantEvents(t, "takes off", m.Observe(at("A", 0, 0, 1), 1), []string{"zone:GEO:Z:A"}, nil)
			wantEvents(t, "lands", m.Observe(down(2), 2), nil, []string{"zone:GEO:Z:A=landed"})
			if got := groundCounters(m); got != [3]uint64{} {
				t.Fatalf("counters %v, want none", got)
			}
		})
	}
}

// A declared landing that the sample contradicts clears nothing: the
// alerts of a flying aircraft are kept, conflicts included, until a
// consistent landing.
func TestContradictedGroundDoesNotClear(t *testing.T) {
	cfg := DefaultConfig()
	cfg.GroundTrust = DefaultGroundTrust()
	m := headOn(t, cfg)
	wantEvents(t, "says ground at 10 m/s", m.Observe(declaredGround("B", 490, -10, 1), 1), nil, nil)
	if a := m.Active(); len(a) != 1 || a[0].Key != "conflict:A:B" || a[0].LastTrueS != 1 {
		t.Fatalf("active %+v, want the conflict refreshed at 1", a)
	}
	// Twin: the same sample standing still is a landing.
	wantEvents(t, "says ground, still", m.Observe(declaredGround("B", 490, 0, 2), 2), nil, []string{"conflict:A:B=landed"})

	// The vertical speed counts: a declared-ground aircraft climbing.
	m = NewMonitor(groundConfig(t, DefaultGroundTrust()))
	up := declaredGround("A", 0, 0, 0)
	up.VDMS = -5
	wantEvents(t, "climbing at 5 m/s", m.Observe(up, 0), []string{"zone:GEO:Z:A"}, nil)

	// A contradicted sample with no valid position neither judges nor
	// clears: it is not a landing.
	m = NewMonitor(groundConfig(t, DefaultGroundTrust()))
	m.Observe(at("A", 0, 0, 0), 0)
	bad := declaredGround("A", 0, 15, 1)
	bad.Pos.LatDeg = 91
	wantEvents(t, "no position", m.Observe(bad, 1), nil, nil)
	if c := m.Counters(); c.Get(CounterInvalidPosition) != 1 || c.Get(CounterGroundContradicted) != 1 || len(m.Active()) != 1 {
		t.Fatalf("counters %v, active %+v", c.Snapshot(), m.Active())
	}
}

// In 1.x DefaultConfig sets no bounds, so every declared ground status
// is believed, as alert_lifecycle.json#on-the-ground-raises-nothing
// pins (two disarmed aircraft 20 m apart with closing velocities raise
// nothing). A system opts in with DefaultGroundTrust.
func TestGroundTrustIsOffByDefault(t *testing.T) {
	if got := DefaultConfig().GroundTrust; got != (GroundTrust{}) {
		t.Fatalf("DefaultConfig().GroundTrust = %+v, want none in 1.x", got)
	}
	if got := DefaultGroundTrust(); got != (GroundTrust{MaxSpeedMS: 3, MaxHeightAGLM: 50}) {
		t.Fatalf("DefaultGroundTrust() = %+v", got)
	}
	fast := onGround(declaredGround("A", 0, 15, 0), 150)
	off := NewMonitor(groundConfig(t, GroundTrust{}))
	wantEvents(t, "no bounds", off.Observe(fast, 0), nil, nil)
	if got := groundCounters(off); got != [3]uint64{} {
		t.Fatalf("no bounds: counters %v", got)
	}
	// The presence twin, same sample.
	on := NewMonitor(groundConfig(t, DefaultGroundTrust()))
	wantEvents(t, "bounds set", on.Observe(fast, 0), []string{"zone:GEO:Z:A"}, nil)
}

// The bounds are configuration: tighter ones contradict sooner, each
// evidence is used only when its bound is set, and a NaN or negative
// bound is replaced and counted, never read as "nothing contradicts".
func TestGroundBoundsAreConfig(t *testing.T) {
	d := DefaultGroundTrust()
	walking := onGround(declaredGround("A", 0, 1.5, 0), 20)
	observe := func(g GroundTrust, tr Track) Events {
		m := NewMonitor(groundConfig(t, g))
		if n := m.Counters().Get(CounterConfigInvalid); n != 0 {
			t.Fatalf("%+v counted as invalid: %d", g, n)
		}
		return m.Observe(tr, 0)
	}
	hit := []string{"zone:GEO:Z:A"}
	wantEvents(t, "recommended bounds", observe(d, walking), nil, nil)
	wantEvents(t, "1 m/s bound", observe(GroundTrust{MaxSpeedMS: 1, MaxHeightAGLM: d.MaxHeightAGLM}, walking), hit, nil)
	wantEvents(t, "10 m bound", observe(GroundTrust{MaxSpeedMS: d.MaxSpeedMS, MaxHeightAGLM: 10}, walking), hit, nil)

	fast := onGround(declaredGround("A", 0, 15, 0), 150)
	wantEvents(t, "both +Inf", observe(GroundTrust{MaxSpeedMS: math.Inf(1), MaxHeightAGLM: math.Inf(1)}, fast), nil, nil)
	wantEvents(t, "height only", observe(GroundTrust{MaxHeightAGLM: 50}, fast), hit, nil)
	wantEvents(t, "speed only", observe(GroundTrust{MaxSpeedMS: 3}, fast), hit, nil)
	wantEvents(t, "speed off, height +Inf", observe(GroundTrust{MaxHeightAGLM: math.Inf(1)}, fast), nil, nil)
	// One evidence set does not use the other: slow and high with a
	// speed bound only is believed.
	slowHigh := onGround(declaredGround("A", 0, 0, 0), 150)
	wantEvents(t, "speed only, slow and high", observe(GroundTrust{MaxSpeedMS: 3}, slowHigh), nil, nil)

	for _, bad := range []float64{math.NaN(), -1, math.Inf(-1)} {
		m := NewMonitor(groundConfig(t, GroundTrust{MaxSpeedMS: bad, MaxHeightAGLM: bad}))
		if got := m.Config().GroundTrust; got != d {
			t.Fatalf("%v: bounds %+v, want the recommended ones", bad, got)
		}
		if n := m.Counters().Get(CounterConfigInvalid); n != 2 {
			t.Fatalf("%v: config_invalid = %d, want 2", bad, n)
		}
		wantEvents(t, "replaced bounds still judge", m.Observe(fast, 0), hit, nil)
	}
}
