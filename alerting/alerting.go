package alerting

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/cpa"
	"github.com/rootxkit/uspace-core/zones"
)

// ClearReason says why an alert was cleared. Every clear carries one
// (C-06, C-14); a clear without a reason never happens.
type ClearReason string

// The clear reasons. Only ClearResolved rests on evidence that the
// condition is false; the others say why the monitor stopped judging it.
const (
	// ClearResolved: messages showed the condition false for longer than
	// Config.ClearAfterS since it was last true (C-06).
	ClearResolved ClearReason = "resolved"
	// ClearStale: an aircraft involved has not been heard for
	// Config.StaleAfterS; nothing showed the condition false (T-10).
	ClearStale ClearReason = "stale"
	// ClearSourceDisabled: an aircraft involved came from a source that was
	// switched off (B-11).
	ClearSourceDisabled ClearReason = "source_disabled"
	// ClearLanded: an aircraft involved reported that it is not flying
	// (disarmed, or declared on the ground). C-14 and the owner's decision
	// on plan §11 gap 4; the recorded old behaviour was ClearStale.
	ClearLanded ClearReason = "landed"
	// ClearFlightEnded: the caller ended the flight with Drop (spec 04
	// §3.3).
	ClearFlightEnded ClearReason = "flight_ended"
)

// Alert kinds.
const (
	KindConflict               = "conflict"
	KindZone                   = zones.KindZone
	KindHeight                 = zones.KindHeight
	KindIdentification         = "identification"
	KindIdentificationMismatch = "identification_mismatch"
)

// Counter names (E-09). The first six are the ones alert_lifecycle.json
// compares.
const (
	// CounterRejectedBacklog counts samples the ingest flagged as backlog
	// (T-04): recorded by the caller, never judged here.
	CounterRejectedBacklog = "rejected_backlog"
	// CounterRejectedLate counts samples with wall - RxAtS > LiveMaxAgeS
	// (T-05).
	CounterRejectedLate = "rejected_late"
	// CounterRejectedOutOfOrder counts samples older on their source's own
	// clock than the one held from that source, and not received later
	// (T-03).
	CounterRejectedOutOfOrder = "rejected_out_of_order"
	// CounterRejectedPlacedAhead counts samples placed ahead of their
	// receipt (CapturedAtS - RxAtS) or of the wall time (CapturedAtS -
	// wallS), or received ahead of the wall time (RxAtS - wallS), by more
	// than AheadToleranceS: a clock ahead, never
	// admitted, so it can neither pin the track in the future nor resolve
	// an alert ahead of time (the clock-ahead rule of timeplace).
	CounterRejectedPlacedAhead = "rejected_placed_ahead"
	// CounterRejectedOlderPlacement counts samples placed (CapturedAtS)
	// before the latest sample admitted for the same aircraft, from any
	// source: a newer state is never overwritten by an older one (T-06).
	// It counts every such sample, including those also counted as
	// rejected_out_of_order because their source's clock caught them
	// first. The caller records them as history.
	CounterRejectedOlderPlacement = "rejected_older_than_held"
	// CounterRejectedSourceDisabled counts samples from a switched-off
	// source (B-11).
	CounterRejectedSourceDisabled = "rejected_source_disabled"
	// CounterZoneNotEvaluated and CounterZoneLimitNotJudged come from
	// zones.Result.Count.
	CounterZoneNotEvaluated   = zones.CounterZoneNotEvaluated
	CounterZoneLimitNotJudged = zones.CounterZoneLimitNotJudged
	// CounterHeightNotEvaluated comes from zones.Result.Count for the
	// height limit.
	CounterHeightNotEvaluated = zones.CounterHeightNotEvaluated

	// CounterRejectedInvalid counts samples with an empty id or a time
	// that is not finite, and calls whose wall time is not finite: nothing
	// of them is judged and nothing is cleared by them (C-09).
	CounterRejectedInvalid = "rejected_invalid"
	// CounterInvalidPosition counts flying samples whose position is not
	// a valid WGS84 position: admitted for identity, but neither paired
	// nor zone-judged, and they do not keep the track alive (C-09).
	CounterInvalidPosition = "invalid_position"
	// CounterFlyingUnknown counts admitted samples whose flying state is
	// unknown (Track.Flying nil): not flying (C-05), but no evidence of a
	// landing either, so active alerts are held until they go stale.
	CounterFlyingUnknown = "flying_unknown"
	// CounterAircraftEvicted counts aircraft without an active alert
	// evicted past MaxAircraft.
	CounterAircraftEvicted = "aircraft_evicted"
	// CounterRejectedCapacity counts samples of a new id refused because
	// every aircraft in MaxAircraft holds an active alert (see
	// Monitor.CapacityExceeded). Also counted per source under this name
	// plus "/<source>/<station>".
	CounterRejectedCapacity = "rejected_capacity"
	// CounterRejectedSourceShare counts samples of a new id refused
	// because its source holds MaxSourceShare of MaxAircraft. Also counted
	// per source under this name plus "/<source>/<station>".
	CounterRejectedSourceShare = "rejected_source_share"
	// CounterSourceOrderEvicted counts per-source ordering entries evicted
	// past MaxSourcesPerAircraft.
	CounterSourceOrderEvicted = "source_order_evicted"
	// CounterSourceStateIgnored counts SwitchSource states the follower did
	// not take (an equal or older version in the same epoch, B-09).
	CounterSourceStateIgnored = "source_state_ignored"
	// CounterDropWithoutReason counts Drop calls refused for an empty
	// reason: a clear always has one.
	CounterDropWithoutReason = "drop_without_reason"
	// CounterZoneKeyDuplicate counts zones given a fallback key because an
	// earlier zone has the same country and identifier.
	CounterZoneKeyDuplicate = "zone_key_duplicate"
	// CounterDropRefusedReason counts Drop calls refused because they
	// named resolved or stale, which only the monitor's own judgement
	// gives.
	CounterDropRefusedReason = "drop_refused_reason"
	// CounterPlacementBehindStale counts admitted samples placed more than
	// StaleAfterS before their RxAtS: live by the ingest's verdict, but
	// stale by their placement, so the sweep drops them at once. A
	// placement anomaly of the ingest (T-02 clamps a batch's spread at
	// 120 s), never silent.
	CounterPlacementBehindStale = "placement_behind_stale"
	// CounterConfigInvalid counts configuration values NewMonitor replaced
	// with their default (a NaN, infinite or negative time, an unknown
	// severity) or found unusable (a cpa.Policy Evaluate refuses).
	CounterConfigInvalid = "config_invalid"
)

// Config holds the monitor's thresholds; they are data (INV-03). Start
// from DefaultConfig.
type Config struct {
	// Policy is the separation policy: minima, window, neighbour radius
	// and the largest age gap at which a pair is judged.
	Policy cpa.Policy
	// ClearAfterS is the hysteresis: an alert clears as resolved once
	// messages have shown it false for more than this since it was last
	// true (C-06).
	ClearAfterS float64
	// StaleAfterS is how long an aircraft may go unheard before it is
	// dropped and its alerts clear as stale (T-10).
	StaleAfterS float64
	// LiveMaxAgeS bounds wall - RxAtS, the ingest-to-monitor leg (T-05).
	LiveMaxAgeS float64
	// AheadToleranceS bounds how far a sample may be placed ahead of its
	// receipt or of the wall time, and received ahead of the wall time:
	// beyond it the clock is ahead and the sample is refused
	// (rejected_placed_ahead), as timeplace's clock-ahead rule. Within it,
	// the alert times still use min(placement, wall), so a placement
	// ahead buys no hysteresis. ClearAfterS must exceed twice it.
	AheadToleranceS float64
	// Zones are the zones judged; ZonePolicy the zone thresholds (the
	// pressure margin, the CONDITIONAL severity, the height limit; a nil
	// MaxHeightAGLM means the height limit is not judged here).
	Zones      []*zones.Zone
	ZonePolicy zones.Policy
	// MismatchSeverity and IdentificationSeverity are what the two
	// identification alerts raise (G-02, G-03).
	MismatchSeverity       core.Severity
	IdentificationSeverity core.Severity
	// GridCellM is the neighbour grid's cell side; raised to
	// Policy.NeighbourRadiusM when smaller (C-15).
	GridCellM float64
	// MaxAircraft bounds the aircraft held (E-10). Past it, an aircraft
	// without an active alert is evicted (not flying first, then
	// unidentified, then the least recently heard); with none, the new id
	// is refused. An alert is never cleared to make room.
	MaxAircraft int
	// MaxSourceShare is the largest share of MaxAircraft one source key
	// (type and station or receiver) may hold, in (0, 1]: a new id from a
	// source already holding it is refused (rejected_source_share), so one
	// flooding receiver leaves room for the others. Default 0.5.
	MaxSourceShare float64
	// RefusalEventIntervalS rate-limits Events.Refused: at most one
	// Refusal per source key per interval of wall time, carrying how many
	// were suppressed since the last. Default 1 s.
	RefusalEventIntervalS float64
	// MaxSourcesPerAircraft bounds the per-source ordering entries of one
	// aircraft (T-03); the least recently updated is evicted and counted.
	MaxSourcesPerAircraft int
}

// DefaultConfig is the policy alert_lifecycle.json pins: cpa.DefaultPolicy
// (60 s, 60 m, 20 m, 800 m, 10 s), 3 s hysteresis, 15 s stale, 10 s live
// age, 1 s ahead tolerance, zones.DefaultPolicy (250 m pressure margin), mismatch warning,
// identification critical, 1000 m grid cells, 50 000 aircraft with at
// most half of them from one source, one refusal event per source per
// second, 16 sources per aircraft.
func DefaultConfig() Config {
	return Config{
		Policy:                 cpa.DefaultPolicy,
		ClearAfterS:            3,
		StaleAfterS:            15,
		LiveMaxAgeS:            10,
		AheadToleranceS:        1,
		ZonePolicy:             zones.DefaultPolicy(),
		MismatchSeverity:       core.SeverityWarning,
		IdentificationSeverity: core.SeverityCritical,
		GridCellM:              1000,
		MaxAircraft:            50_000,
		MaxSourcesPerAircraft:  16,
		MaxSourceShare:         0.5,
		RefusalEventIntervalS:  1,
	}
}

// Track is one sample of one aircraft as the caller's ingest delivered
// it, with the identification resolved and the ground and geoid looked up
// for its position (zones does no I/O).
type Track struct {
	// ID is the aircraft id; one id is one aircraft for every check.
	ID string
	// Pos is the WGS84 position.
	Pos core.LatLon
	// AltAMSLM is the AMSL altitude, nil when unknown. With AltSource it
	// says whether the vertical position is known: only a finite geodetic
	// or network altitude is; a pressure altitude, AltNone or an unknown
	// source is judged on the horizontal alone (R-09).
	AltAMSLM  *float64
	AltSource core.AltSource
	// VNMS, VEMS, VDMS are the velocity north, east and down in m/s.
	VNMS, VEMS, VDMS float64
	// Flying is true for armed (MAVLink) or declared airborne (Remote ID).
	// Nil is unknown and counts as not flying (C-05); false is a landing.
	Flying *bool
	// CapturedAtS is where the ingest placed the sample on its own clock
	// (T-01); RxAtS when the ingest received it. Both in seconds on the
	// clock wallS is on.
	CapturedAtS, RxAtS float64
	// SourceTS is the sample's time on its source's own clock, used only
	// to order samples within one source (T-03); nil when it has none.
	SourceTS *float64
	// Backlog is the ingest's verdict that the sample is history (T-04).
	Backlog bool
	// Source is the source type (relay, remote_id, ...) and Station the
	// instance (station or receiver id; "" for none), as sources.State
	// names them.
	Source, Station string
	// Transmitter is the Remote ID transmitter address, nil otherwise;
	// Identified is false for a track with no fresh identity (I-02), nil
	// when the source says nothing.
	Transmitter *string
	Identified  *bool
	// Identification is the registry's verdict on who the aircraft is;
	// nil when the message carries none (then neither identification alert
	// is refreshed or shown false by it).
	Identification *core.Identification
	// Env is the ground and geoid at Pos (zones.Env).
	Env zones.Env
}

// Alert is one active condition. Times are on the placed clock
// (CapturedAtS of the samples that judged it).
type Alert struct {
	// Key identifies the condition: conflict:<a>:<b> (ids sorted),
	// zone:<country>:<identifier>:<aircraft>,
	// identification:<country>:<identifier>:<aircraft>,
	// height:<aircraft>, identification_mismatch:<aircraft>. A ':' or '%'
	// in an id is escaped as %3A or %25, so keys never collide.
	Key  string
	Kind string
	// Severity is the current severity; a change is raised again (C-07).
	Severity core.Severity
	// Aircraft are the ids involved, sorted.
	Aircraft []string
	// Detail is the judgement's numbers at the last time the condition was
	// shown true, at full precision (a display rounds them). A clear
	// carries the detail the alert held; a resolved conflict also carries
	// Cleared.ClearingDetail (C-14).
	Detail map[string]any
	// RaisedAtS is when the condition was first raised; LastTrueS when it
	// was last shown true; LastFalseS when it was last shown false, valid
	// only when ShownFalse.
	RaisedAtS, LastTrueS, LastFalseS float64
	ShownFalse                       bool
}

// Cleared is an alert that cleared, with its reason. Alert.Detail keeps
// the numbers of the last judgement that showed the condition true (the
// values alert_lifecycle.json pins); ClearingDetail holds the numbers of
// the judgement that cleared it (C-14).
type Cleared struct {
	Alert
	Reason ClearReason
	// ClearingDetail is set on a resolved conflict: the separation of the
	// judgement that cleared it, with clearing_d_horizontal_now_m,
	// clearing_d_alt_now_m (nil when the vertical is unknown),
	// clearing_t_cpa_s, clearing_d_cpa_horizontal_m,
	// clearing_d_alt_at_cpa_m (nil when unknown),
	// clearing_vertical_separation_known and clearing_at_s (the placed
	// time of that judgement). Nil for every other clear: a zone, height
	// or identification alert is cleared by a judgement with no numbers
	// of its own, and stale, landed, source_disabled and the Drop reason
	// rest on no judgement at all.
	ClearingDetail map[string]any
}

// Events is what one call raised and cleared, in a deterministic order,
// and the new aircraft it refused for capacity.
type Events struct {
	Raised  []Alert
	Cleared []Cleared
	// Refused is set when a new id was refused for capacity: the caller
	// alerts on it, since that aircraft goes unjudged. Rate-limited per
	// source (Config.RefusalEventIntervalS); every refusal is counted.
	Refused []Refusal
}

// RefusalReason says why a new aircraft was refused.
type RefusalReason string

// The refusal reasons.
const (
	// RefusedSourceShare: its source already holds MaxSourceShare of
	// MaxAircraft.
	RefusedSourceShare RefusalReason = "source_share"
	// RefusedCapacity: the monitor is at MaxAircraft and every aircraft
	// holds an active alert (Monitor.CapacityExceeded).
	RefusedCapacity RefusalReason = "capacity"
)

// Refusal is a new aircraft refused for capacity, and how many more
// refusals from the same source the rate limit suppressed before it.
type Refusal struct {
	ID, Source, Station string
	Reason              RefusalReason
	AtS                 float64
	Suppressed          uint64
}

// validTime reports whether a configured time is usable: finite and not
// negative.
func validTime(v float64) bool {
	return core.IsFinite(v) && v >= 0
}

func validSeverity(s core.Severity) bool {
	switch s {
	case core.SeverityInfo, core.SeverityWarning, core.SeverityCritical:
		return true
	}
	return false
}

// sanitise replaces unusable configuration values with their defaults and
// counts each replacement. A threshold is never relaxed silently: a NaN
// or negative time would make comparisons read as "clear" or "never
// stale", so the documented default is used and the counter shows it.
func sanitise(c Config, counters *core.Counters) Config {
	d := DefaultConfig()
	fix := func(v *float64, def float64) {
		if !validTime(*v) {
			*v = def
			counters.Inc(CounterConfigInvalid)
		}
	}
	fix(&c.ClearAfterS, d.ClearAfterS)
	fix(&c.StaleAfterS, d.StaleAfterS)
	fix(&c.LiveMaxAgeS, d.LiveMaxAgeS)
	fix(&c.AheadToleranceS, d.AheadToleranceS)
	fix(&c.RefusalEventIntervalS, d.RefusalEventIntervalS)
	// The hysteresis must outlast what a clock ahead within the tolerance
	// could shift (receipt and placement ahead, each by the tolerance).
	if !(c.ClearAfterS > 2*c.AheadToleranceS) {
		counters.Inc(CounterConfigInvalid)
		c.ClearAfterS = d.ClearAfterS
		if !(c.ClearAfterS > 2*c.AheadToleranceS) {
			c.AheadToleranceS = d.AheadToleranceS
		}
	}
	if !(c.MaxSourceShare > 0 && c.MaxSourceShare <= 1) {
		c.MaxSourceShare = d.MaxSourceShare
		counters.Inc(CounterConfigInvalid)
	}
	if !validSeverity(c.MismatchSeverity) {
		c.MismatchSeverity = d.MismatchSeverity
		counters.Inc(CounterConfigInvalid)
	}
	if !validSeverity(c.IdentificationSeverity) {
		c.IdentificationSeverity = d.IdentificationSeverity
		counters.Inc(CounterConfigInvalid)
	}
	if c.MaxAircraft <= 0 {
		c.MaxAircraft = d.MaxAircraft
	}
	if c.MaxSourcesPerAircraft <= 0 {
		c.MaxSourcesPerAircraft = d.MaxSourcesPerAircraft
	}
	if !(c.GridCellM >= c.Policy.NeighbourRadiusM) {
		c.GridCellM = math.Max(c.Policy.NeighbourRadiusM, d.GridCellM)
	}
	// An invalid separation policy is the caller's data and is not
	// replaced (INV-03); every pair it refuses is counted as not judged.
	probe := cpa.State{Pos: core.LatLon{}, VerticalKnown: true}
	if r := cpa.Evaluate(probe, probe, c.Policy); r.NotJudged == cpa.ReasonInvalidPolicy {
		counters.Inc(CounterConfigInvalid)
	}
	return c
}
