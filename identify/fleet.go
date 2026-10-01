package identify

import (
	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// AuthRow is one telemetry row the bus delivered for the aircraft whose
// serial a broadcast claims, in delivery order.
type AuthRow struct {
	// HeardAtS is when the row was received, on the caller's clock (the
	// same clock as FleetInput.NowS).
	HeardAtS float64
	// Pos is where the row places the aircraft; nil when it carries none.
	Pos *core.LatLon
	// Backlog is true for a row a relay delivered from its queue after an
	// outage (I-09): history, never live.
	Backlog bool
	// BehindS is how long before receipt the row was captured (receipt
	// time minus capture time); 0 when unknown.
	BehindS float64
	// Source is "" for authenticated telemetry. Any other value (for
	// example "remote_id" or "network_remote_id") is a broadcast, which
	// never vouches for where the aircraft is (I-09).
	Source string
}

// IsOurs reports whether a serial lookup names one of our own fleet
// aircraft: a unique match, exact or case-folded (G-05), of a registry
// aircraft with no UAS operator. It is the only correct way to set
// FleetInput.SerialIsOurs: a test on the exact spelling alone would let
// "sn-fleet" through as a stranger while our "SN-FLEET" is flying.
func IsOurs(uas UASFacts, m Match) bool {
	return (m == MatchExact || m == MatchFolded) && uas.OperatorID == nil && uas.InRegistry
}

// FleetInput is a broadcast of a serial judged against our fleet.
type FleetInput struct {
	// SerialIsOurs is true when the broadcast serial names one of our
	// aircraft. Callers must set it with IsOurs on the result of
	// Lookup.UASBySerial for the broadcast serial, so that a case-folded
	// spelling of our serial is judged as ours and cannot bypass the
	// guard.
	SerialIsOurs bool
	// Rows is that aircraft's telemetry as delivered, in order.
	Rows []AuthRow
	// Broadcast is where the broadcast places the aircraft.
	Broadcast core.LatLon
	// NowS is the judgement time on the clock of AuthRow.HeardAtS.
	NowS float64
	// LiveForS is how recently a row must have been heard, and at most
	// how long before receipt it may have been captured, to be live
	// (spec 04 section 3.2: 5 s). Exactly at the bound is live.
	LiveForS float64
	// SpoofDistanceM is the distance beyond which a broadcast is not our
	// aircraft (spec 04 section 3.2: 300 m), on the haversine (D-11).
	SpoofDistanceM float64
}

// Verdict is what a broadcast claiming a serial is, against our fleet.
type Verdict string

const (
	// VerdictStranger is not one of our serials: nothing to judge.
	VerdictStranger Verdict = "stranger"
	// VerdictWithhold is ours, with live authenticated telemetry that
	// agrees or has no position yet: the authenticated track is better.
	VerdictWithhold Verdict = "withhold"
	// VerdictAsOurs is ours with no live authenticated telemetry: the
	// broadcast speaks for the aircraft, still marked as a broadcast.
	VerdictAsOurs Verdict = "as_ours"
	// VerdictConflict is ours by serial but more than SpoofDistanceM from
	// where live authenticated telemetry places it: a separate unverified
	// track (SerialConflict).
	VerdictConflict Verdict = "conflict"
)

// FleetResult is the spoofing guard's verdict.
type FleetResult struct {
	Verdict Verdict
	// ApartM is the haversine distance between the newest live position
	// and the broadcast, when one was judged; nil otherwise.
	ApartM *float64
	// IgnoredHistoryRows counts the authenticated rows that were history:
	// backlog rows and rows captured more than LiveForS before receipt.
	// Broadcast rows are not counted; they are not telemetry.
	IgnoredHistoryRows int
}

// JudgeFleet is the spoofing guard (I-08, I-09, S-10). A row is live when
// it is authenticated telemetry (Source ""), not a backlog row, captured
// no more than LiveForS before receipt, and heard no more than LiveForS
// before NowS. With no live row the broadcast speaks for our aircraft
// (VerdictAsOurs). With live rows, the newest live row that carries a
// position is compared with the broadcast: within SpoofDistanceM is
// VerdictWithhold, beyond it VerdictConflict. Live rows without any
// position, or a position or broadcast that is not a valid coordinate,
// withhold: a conflict is never judged without a distance. A NaN
// HeardAtS makes a row not live; a NaN BehindS makes it history.
func JudgeFleet(in FleetInput) FleetResult {
	if !in.SerialIsOurs {
		return FleetResult{Verdict: VerdictStranger}
	}
	var res FleetResult
	live := false
	var pos *core.LatLon
	var posHeardS float64
	for i := range in.Rows {
		r := &in.Rows[i]
		if r.Source != "" {
			continue
		}
		if r.Backlog || !(r.BehindS <= in.LiveForS) {
			res.IgnoredHistoryRows++
			continue
		}
		if !(in.NowS-r.HeardAtS <= in.LiveForS) {
			continue
		}
		live = true
		if r.Pos != nil && (pos == nil || r.HeardAtS >= posHeardS) {
			pos, posHeardS = r.Pos, r.HeardAtS
		}
	}
	switch {
	case !live:
		res.Verdict = VerdictAsOurs
		return res
	case pos == nil || !pos.Valid() || !in.Broadcast.Valid():
		res.Verdict = VerdictWithhold
		return res
	}
	apartM := geodesy.HaversineM(*pos, in.Broadcast)
	res.ApartM = &apartM
	if apartM > in.SpoofDistanceM {
		res.Verdict = VerdictConflict
	} else {
		res.Verdict = VerdictWithhold
	}
	return res
}
