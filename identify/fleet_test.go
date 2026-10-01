package identify_test

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
	"github.com/rootxkit/uspace-core/identify"
)

var (
	home = core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}
	// near is 50 m north of home, far 500 m north (fleet_match.json).
	near = core.LatLon{LatDeg: 41.71554966050632, LonDeg: 44.8271}
	far  = core.LatLon{LatDeg: 41.719596605063174, LonDeg: 44.8271}
)

func pos(p core.LatLon) *core.LatLon { return &p }

// fleet is a judgement at now 1 s with the spec defaults (5 s, 300 m).
func fleet(broadcast core.LatLon, rows ...identify.AuthRow) identify.FleetInput {
	return identify.FleetInput{SerialIsOurs: true, Rows: rows, Broadcast: broadcast, NowS: 1, LiveForS: 5, SpoofDistanceM: 300}
}

type wantFleet struct {
	verdict identify.Verdict
	apartM  float64 // NaN: nil
	ignored int
}

func checkFleet(t *testing.T, got identify.FleetResult, w wantFleet) {
	t.Helper()
	if got.Verdict != w.verdict {
		t.Errorf("verdict %q, want %q", got.Verdict, w.verdict)
	}
	switch {
	case math.IsNaN(w.apartM) && got.ApartM != nil:
		t.Errorf("apart_m %v, want nil", *got.ApartM)
	case !math.IsNaN(w.apartM) && (got.ApartM == nil || math.Abs(*got.ApartM-w.apartM) > 0.01):
		t.Errorf("apart_m %v, want %v", got.ApartM, w.apartM)
	}
	if got.IgnoredHistoryRows != w.ignored {
		t.Errorf("ignored_history_rows %d, want %d", got.IgnoredHistoryRows, w.ignored)
	}
}

func TestJudgeFleet(t *testing.T) {
	none := math.NaN()
	nearM, farM := geodesy.HaversineM(home, near), geodesy.HaversineM(home, far)
	stranger := fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home), Backlog: true})
	stranger.SerialIsOurs = false
	atBound := fleet(near, identify.AuthRow{HeardAtS: 0, Pos: pos(home)})
	atBound.NowS = 5
	pastBound := atBound
	pastBound.NowS = 5.000001
	exactlyAtSpoof := fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home)})
	exactlyAtSpoof.SpoofDistanceM = farM

	cases := []struct {
		name string
		in   identify.FleetInput
		want wantFleet
	}{
		{"stranger-counts-nothing", stranger, wantFleet{identify.VerdictStranger, none, 0}},
		{"ours-quiet-no-rows", fleet(far), wantFleet{identify.VerdictAsOurs, none, 0}},
		{"live-near-withhold", fleet(near, identify.AuthRow{HeardAtS: 0, Pos: pos(home)}), wantFleet{identify.VerdictWithhold, nearM, 0}},
		{"live-far-conflict", fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home)}), wantFleet{identify.VerdictConflict, farM, 0}},
		{"exactly-at-spoof-distance-withhold", exactlyAtSpoof, wantFleet{identify.VerdictWithhold, farM, 0}},
		{"heard-exactly-live-for-ago-is-live", atBound, wantFleet{identify.VerdictWithhold, nearM, 0}},
		{"heard-past-live-for-is-quiet", pastBound, wantFleet{identify.VerdictAsOurs, none, 0}},
		{"live-without-position-withhold", fleet(far, identify.AuthRow{HeardAtS: 0}), wantFleet{identify.VerdictWithhold, none, 0}},
		{"backlog-is-history", fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home), Backlog: true}), wantFleet{identify.VerdictAsOurs, none, 1}},
		{"captured-long-before-is-history", fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home), BehindS: 5.01}), wantFleet{identify.VerdictAsOurs, none, 1}},
		{"captured-exactly-live-for-before-is-live", fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home), BehindS: 5}), wantFleet{identify.VerdictConflict, farM, 0}},
		{"broadcast-rows-never-vouch", fleet(far,
			identify.AuthRow{HeardAtS: 0, Pos: pos(home), Source: "remote_id"},
			identify.AuthRow{HeardAtS: 0.5, Pos: pos(home), Source: "network_remote_id", Backlog: true}),
			wantFleet{identify.VerdictAsOurs, none, 0}},
		{"history-does-not-move-the-position", fleet(near,
			identify.AuthRow{HeardAtS: 0, Pos: pos(home)},
			identify.AuthRow{HeardAtS: 0.9, Pos: pos(far), Backlog: true}),
			wantFleet{identify.VerdictWithhold, nearM, 1}},
		{"newest-live-position-wins", fleet(far,
			identify.AuthRow{HeardAtS: 0.5, Pos: pos(far)},
			identify.AuthRow{HeardAtS: 0, Pos: pos(home)}),
			wantFleet{identify.VerdictWithhold, 0, 0}},
		{"equal-times-the-later-row-wins", fleet(far,
			identify.AuthRow{HeardAtS: 0, Pos: pos(far)},
			identify.AuthRow{HeardAtS: 0, Pos: pos(home)}),
			wantFleet{identify.VerdictConflict, farM, 0}},
		{"newer-row-without-position-keeps-the-live-position", fleet(far,
			identify.AuthRow{HeardAtS: 0, Pos: pos(home)},
			identify.AuthRow{HeardAtS: 0.9}),
			wantFleet{identify.VerdictConflict, farM, 0}},
		{"a-stale-position-is-not-live", fleet(far,
			identify.AuthRow{HeardAtS: -10, Pos: pos(home)},
			identify.AuthRow{HeardAtS: 0.9}),
			wantFleet{identify.VerdictWithhold, none, 0}},
		{"invalid-row-position-withhold", fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(core.LatLon{LatDeg: math.NaN(), LonDeg: 0})}),
			wantFleet{identify.VerdictWithhold, none, 0}},
		{"invalid-broadcast-withhold", fleet(core.LatLon{LatDeg: 91, LonDeg: 0}, identify.AuthRow{HeardAtS: 0, Pos: pos(home)}),
			wantFleet{identify.VerdictWithhold, none, 0}},
		{"nan-heard-is-not-live", fleet(far, identify.AuthRow{HeardAtS: math.NaN(), Pos: pos(home)}),
			wantFleet{identify.VerdictAsOurs, none, 0}},
		{"nan-behind-is-history", fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home), BehindS: math.NaN()}),
			wantFleet{identify.VerdictAsOurs, none, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := identify.JudgeFleet(c.in)
			checkFleet(t, got, c.want)
			if got.Verdict == identify.VerdictConflict && got.ApartM == nil {
				t.Error("a conflict without a distance")
			}
		})
	}
}

// TestConflictIsSerialConflict: the verdict and the identification it
// leads to agree.
func TestConflictIsSerialConflict(t *testing.T) {
	r := identify.JudgeFleet(fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home)}))
	if r.Verdict != identify.VerdictConflict {
		t.Fatalf("verdict %q", r.Verdict)
	}
	id := identify.SerialConflict("SN-F", nil)
	if !id.Mismatch || id.Reason != core.ReasonSerialConflict || !id.Status.IncidentStatus() {
		t.Errorf("%+v", id)
	}
}

func TestIsOurs(t *testing.T) {
	owner := "o1"
	s := identify.NewSnapshot(nil, []identify.UASFacts{
		{DroneID: "d-fleet", Serial: "SN-FLEET", RegistrationStatus: identify.StatusActive, InRegistry: true},
		{DroneID: "d-owned", Serial: "SN-OWNED", RegistrationStatus: identify.StatusActive, OperatorID: &owner, InRegistry: true},
		{DroneID: "d-orphan", Serial: "SN-ORPHAN", InRegistry: false},
		{DroneID: "d-x1", Serial: "ab-1", InRegistry: true},
		{DroneID: "d-x2", Serial: "AB-1", InRegistry: true},
	})
	cases := map[string]bool{
		"SN-FLEET":     true,  // exact
		"sn-fleet":     true,  // folded: still ours
		"\tSn-Fleet\n": true,  // normalised and folded
		"ſn-fleet":     false, // a non-ASCII look-alike is not
		"SN-OWNED":     false, // another operator's aircraft
		"SN-ORPHAN":    false, // not in the registry
		"Ab-1":         false, // ambiguous
		"SN-NOBODY":    false,
	}
	for sn, want := range cases {
		if got := identify.IsOurs(s.UASBySerial(sn)); got != want {
			t.Errorf("IsOurs(%q) = %v, want %v", sn, got, want)
		}
	}
}

// TestFoldedBroadcastOfOurSerialIsJudged: "sn-fleet" heard 500 m from where
// our live "SN-FLEET" is must be a conflict, not a stranger that bypasses
// the guard; with the telemetry quiet it speaks for our aircraft.
func TestFoldedBroadcastOfOurSerialIsJudged(t *testing.T) {
	s := identify.NewSnapshot(nil, []identify.UASFacts{
		{DroneID: "d-fleet", Serial: "SN-FLEET", RegistrationStatus: identify.StatusActive, InRegistry: true},
	})
	in := fleet(far, identify.AuthRow{HeardAtS: 0, Pos: pos(home)})
	in.SerialIsOurs = identify.IsOurs(s.UASBySerial("sn-fleet"))
	if r := identify.JudgeFleet(in); r.Verdict != identify.VerdictConflict {
		t.Fatalf("folded broadcast of our serial: %q, want conflict", r.Verdict)
	}
	in.Rows = nil
	if r := identify.JudgeFleet(in); r.Verdict != identify.VerdictAsOurs {
		t.Fatalf("quiet telemetry: %q, want as_ours", r.Verdict)
	}
	in.SerialIsOurs = identify.IsOurs(s.UASBySerial("SN-STRANGER"))
	if r := identify.JudgeFleet(in); r.Verdict != identify.VerdictStranger {
		t.Fatalf("a stranger: %q", r.Verdict)
	}
}
