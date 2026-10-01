package zones

import (
	"math"
	"slices"

	"github.com/rootxkit/uspace-core/core"
)

// GroundKind says what the caller knows about the ground under the
// aircraft. The zero value is GroundNotConfigured: an Env built without
// a DEM never claims to know the ground.
type GroundKind int

const (
	// GroundNotConfigured means no terrain is configured for this system.
	GroundNotConfigured GroundKind = iota
	// GroundUnknown means terrain is configured but the ground is unknown at
	// this position (cell never fetched, nodata, unreadable tile). Unknown
	// ground is never 0 (LESSONS D-04).
	GroundUnknown
	// GroundKnown means GroundM holds the DEM elevation (AMSL) here.
	GroundKnown
)

// Env is what the caller resolved for the aircraft's position, from
// terrain.Ground and geoid.Undulator; zones does no I/O. A GroundM that
// is not finite is taken as unknown ground, and an UndulationM that is
// nil or not finite as no geoid.
type Env struct {
	Ground  GroundKind
	GroundM float64
	// UndulationM is the geoid undulation N (HAE = AMSL + N) here; nil
	// when no geoid is configured.
	UndulationM *float64
}

// Aircraft is the vertical state of one aircraft. AltAMSLM is nil when it
// has no usable altitude; AltSource says which rule produced it (04 §3.1).
type Aircraft struct {
	AltAMSLM  *float64
	AltSource core.AltSource
}

// Policy holds the thresholds of the judgement; they are data, never
// literals (LESSONS INV-03). Start from DefaultPolicy.
type Policy struct {
	// PressureUncertaintyM widens every judged limit, each way, for an
	// aircraft on a pressure altitude (R-09). A negative or non-finite
	// value is taken as unbounded: every such aircraft inside a zone's
	// widened band warns rather than none of them.
	PressureUncertaintyM float64
	// ConditionalSeverity is what a CONDITIONAL zone raises (info or
	// warning, Z-10). A value that is not a core severity is taken as
	// warning.
	ConditionalSeverity core.Severity
	// MaxHeightAGLM is the height limit over the ground (the 120 m rule
	// of the open category, judged at the authority only, spec 09 §2);
	// nil when this system does not judge it.
	MaxHeightAGLM *float64
}

// DefaultPolicy is the predecessor's policy: a 250 m pressure margin,
// CONDITIONAL zones at warning, and no height limit (the authority sets
// its own).
func DefaultPolicy() Policy {
	return Policy{PressureUncertaintyM: 250, ConditionalSeverity: core.SeverityWarning}
}

// Raise kinds.
const (
	KindZone   = "zone"
	KindHeight = "height"
)

// Counter names (E-09). The caller adds a Result to its counters with
// Result.Count.
const (
	// CounterZoneNotEvaluated counts zone checks left silent because a
	// limit needs a height that is not known here (Z-09), or because the
	// altitude or the zone is not usable.
	CounterZoneNotEvaluated = "zone_checks_not_evaluated"
	// CounterZoneLimitNotJudged counts zone raises that warn because an
	// AGL limit could not be judged (Z-09).
	CounterZoneLimitNotJudged = "zone_limits_not_judged"
	// CounterHeightNotEvaluated counts height-limit checks not evaluated
	// (unknown ground, no terrain, no altitude). It is not one of the
	// vectors' two zone counters, which stay 0 for the height limit.
	CounterHeightNotEvaluated = "height_checks_not_evaluated"
)

// Reason says why a check was not evaluated, or why a limit was not
// judged; "" when everything needed was known.
type Reason string

// The reasons.
const (
	// ReasonNoAltitude: no altitude, a non-finite one, or AltSource none.
	ReasonNoAltitude Reason = "no_altitude"
	// ReasonNoTerrain: an AGL limit (or the height limit) and no terrain
	// configured.
	ReasonNoTerrain Reason = "no_terrain"
	// ReasonGroundUnknown: terrain configured, ground unknown here.
	ReasonGroundUnknown Reason = "ground_unknown"
	// ReasonNoGeoid: a WGS84 limit and no usable geoid undulation.
	ReasonNoGeoid Reason = "no_geoid"
	// ReasonInvalidZone: a nil zone, or a limit that is not finite or has
	// an unknown reference (FromED269 refuses these; a Zone built in code
	// can still hold one).
	ReasonInvalidZone Reason = "invalid_zone"
	// ReasonInvalidPolicy: a height limit that is not finite or is
	// negative.
	ReasonInvalidPolicy Reason = "invalid_policy"
)

// Detail is what a raise says about the judgement. Every optional member
// is a pointer (or a nil slice) so that "not said" differs from false or
// 0; the vectors compare absent keys as nil. Numbers are at full
// precision; a display rounds them.
type Detail struct {
	Identifier  string `json:"identifier,omitempty"`
	Restriction string `json:"restriction,omitempty"`
	// VerticalKnown is false when the vertical position was not known
	// exactly: a pressure altitude (R-09) or an unjudged limit (Z-09).
	VerticalKnown *bool `json:"vertical_known,omitempty"`
	// WithinBand, for a pressure altitude, is true when every judged limit
	// includes the aircraft as indicated, false when only the band widened
	// by the pressure margin does.
	WithinBand *bool `json:"within_band,omitempty"`
	// LimitNotJudged and NotJudged: an AGL limit could not be judged and
	// the zone warns instead (Z-09); NotJudged lists the references.
	LimitNotJudged *bool    `json:"limit_not_judged,omitempty"`
	NotJudged      []string `json:"not_judged,omitempty"`
	// HeightAGLM is the height over the ground when an AGL limit (or the
	// height limit) was judged; AltHAEM the height above the ellipsoid
	// when a WGS84 limit was.
	HeightAGLM    *float64 `json:"height_agl_m,omitempty"`
	AltHAEM       *float64 `json:"alt_hae_m,omitempty"`
	MaxHeightAGLM *float64 `json:"max_height_agl_m,omitempty"`
}

// Raise is one alert-worthy finding for one aircraft.
type Raise struct {
	Kind     string
	Severity core.Severity
	Detail   Detail
}

// Result is one judgement. Exactly one of these holds: Raise is set
// (LimitNotJudged may be true with it); NotEvaluated is true (nothing
// raised, and an active alert must be neither refreshed nor cleared,
// Z-09, C-09); or neither, which is "judged and clear". A Result is
// never clear because something was unknown.
type Result struct {
	Raise          *Raise
	NotEvaluated   bool
	LimitNotJudged bool
	// Reason says what was missing when NotEvaluated or LimitNotJudged.
	Reason Reason

	height bool
}

// Count adds r to c under the counter names above: a zone check not
// evaluated, a zone limit not judged, a height check not evaluated.
func (r Result) Count(c *core.Counters) {
	switch {
	case r.height && r.NotEvaluated:
		c.Inc(CounterHeightNotEvaluated)
	case r.NotEvaluated:
		c.Inc(CounterZoneNotEvaluated)
	case r.LimitNotJudged:
		c.Inc(CounterZoneLimitNotJudged)
	}
}

// Severity is what being inside a zone of type t raises (Z-10):
// PROHIBITED critical, REQ_AUTHORIZATION warning, CONDITIONAL
// pol.ConditionalSeverity, USPACE info, and false for NO_RESTRICTION,
// which raises nothing. USPACE is at the lowest severity so that being
// in U-space airspace is visible (owner decision, PR #12); whether the
// flight there is authorised is judged by the authorisation check, not
// here. A type that is not a core.ZoneType is critical: an unknown
// restriction fails towards enforcing it.
//
// Lifting REQ_AUTHORIZATION for an aircraft authorised there at that time
// is the caller's concern (U-05); nothing lifts PROHIBITED.
func Severity(t core.ZoneType, pol Policy) (core.Severity, bool) {
	switch t {
	case core.ZoneProhibited:
		return core.SeverityCritical, true
	case core.ZoneReqAuthorization:
		return core.SeverityWarning, true
	case core.ZoneConditional:
		switch pol.ConditionalSeverity {
		case core.SeverityInfo, core.SeverityWarning, core.SeverityCritical:
			return pol.ConditionalSeverity, true
		}
		return core.SeverityWarning, true
	case core.ZoneUSpace:
		return core.SeverityInfo, true
	case core.ZoneNoRestriction:
		return "", false
	}
	return core.SeverityCritical, true
}

// altitude returns the usable AMSL altitude and whether it is widened by
// the pressure margin. A pressure altitude is widened (R-09); so is a
// source this package does not know, whose accuracy is unknown too.
func altitude(ac Aircraft) (altAMSLM float64, widened, ok bool) {
	if ac.AltAMSLM == nil || !core.IsFinite(*ac.AltAMSLM) {
		return 0, false, false
	}
	switch ac.AltSource {
	case core.AltGeodetic, core.AltNetwork:
		return *ac.AltAMSLM, false, true
	case core.AltPressure:
		return *ac.AltAMSLM, true, true
	case core.AltNone:
		return 0, false, false
	}
	return *ac.AltAMSLM, true, true
}

// marginM is the pressure margin, unbounded when the policy holds a
// value that could narrow the band (negative) or poison it (NaN).
func marginM(pol Policy) float64 {
	m := pol.PressureUncertaintyM
	if math.IsNaN(m) || m < 0 {
		return math.Inf(1)
	}
	return m
}

// heightIn is the aircraft's height in ref, from its AMSL altitude, or
// the reason it is not known here. Each reference is computed on its own
// and never through another (D-01, Z-08).
func heightIn(ref core.VerticalRef, altAMSLM float64, env Env) (float64, Reason) {
	switch ref {
	case core.RefAMSL:
		return altAMSLM, ""
	case core.RefAGL:
		switch env.Ground {
		case GroundKnown:
			if core.IsFinite(env.GroundM) {
				return altAMSLM - env.GroundM, ""
			}
			return 0, ReasonGroundUnknown
		case GroundNotConfigured:
			return 0, ReasonNoTerrain
		case GroundUnknown:
			return 0, ReasonGroundUnknown
		}
		return 0, ReasonGroundUnknown
	case core.RefWGS84:
		if env.UndulationM == nil || !core.IsFinite(*env.UndulationM) {
			return 0, ReasonNoGeoid
		}
		return altAMSLM + *env.UndulationM, ""
	}
	return 0, ReasonInvalidZone
}

// atMostWarning caps a severity at warning: being possibly inside a zone
// (the widened band, an unjudged limit) never raises more than being
// definitely inside it, and never more than a warning.
func atMostWarning(sev core.Severity) core.Severity {
	if sev == core.SeverityInfo {
		return sev
	}
	return core.SeverityWarning
}

// warnsUnjudged reports whether a zone type warns, rather than stays
// silent, when only its AGL limit cannot be judged (Z-09).
func warnsUnjudged(t core.ZoneType) bool {
	return t == core.ZoneProhibited || t == core.ZoneReqAuthorization
}

func ptr[T any](v T) *T { return &v }

// JudgeVertical judges an aircraft already inside the zone horizontally,
// at a time the zone applies (the caller checks ContainsHorizontally and
// AppliesAt first), against the zone's vertical limits.
//
// Each limit is compared in its own reference (Z-08): AMSL with the AMSL
// altitude, AGL with the altitude less env.GroundM (known ground only),
// WGS84 with the altitude plus env.UndulationM. A missing limit is
// unbounded; a lower AGL limit at or below 0 is met by any airborne
// aircraft and needs no DEM. Bounds are inclusive: lower <= height <=
// upper is inside. A judged limit that excludes the aircraft decides,
// whatever an unjudged one would have said.
//
// When a needed height is unknown (Z-09): a PROHIBITED or
// REQ_AUTHORIZATION zone whose only unjudged references are AGL raises a
// warning with vertical_known false, limit_not_judged true and
// not_judged ["AGL"] (LimitNotJudged; a false warning beats a missed
// critical); any other zone (CONDITIONAL, or a WGS84 limit without the
// geoid) is NotEvaluated.
//
// On a pressure altitude (R-09) every judged limit is widened by
// pol.PressureUncertaintyM each way: inside the band as indicated keeps
// the zone's severity with within_band true; inside only the widened
// band is a warning (or the zone's own severity when that is lower, an
// info CONDITIONAL zone) with within_band false; outside it nothing. Both
// say vertical_known false. A zone without a limit that needs a height
// is judged as for any aircraft: no margin and no flag, and no altitude
// is needed for it. With no usable altitude any other zone is
// NotEvaluated.
//
// Fail-safe: a non-finite altitude, ground or undulation is unknown,
// never a number that compares as outside; a zone limit that is not
// finite makes the zone NotEvaluated. The result is never clear because
// of something unknown.
func JudgeVertical(z *Zone, ac Aircraft, env Env, pol Policy) Result {
	if z == nil {
		return Result{NotEvaluated: true, Reason: ReasonInvalidZone}
	}
	sev, raises := Severity(z.Type, pol)
	if !raises {
		return Result{}
	}
	type bound struct {
		l     *Limit
		lower bool
	}
	var bounds [2]bound
	n := 0
	for _, b := range [2]bound{{z.Lower, true}, {z.Upper, false}} {
		if b.l == nil {
			continue
		}
		if !core.IsFinite(b.l.ValueM) || !b.l.Ref.Valid() {
			return Result{NotEvaluated: true, Reason: ReasonInvalidZone}
		}
		if needsHeight(b.l, b.lower) {
			bounds[n] = b
			n++
		}
	}
	detail := Detail{Identifier: z.Identifier, Restriction: string(z.Restriction)}
	if n == 0 {
		return Result{Raise: &Raise{Kind: KindZone, Severity: sev, Detail: detail}}
	}
	alt, widened, ok := altitude(ac)
	if !ok {
		return Result{NotEvaluated: true, Reason: ReasonNoAltitude}
	}
	margin := 0.0
	if widened {
		margin = marginM(pol)
	}
	withinBand := true
	judged := false
	var notJudged []string
	var reason Reason
	for _, b := range bounds[:n] {
		h, why := heightIn(b.l.Ref, alt, env)
		if why != "" {
			if !slices.Contains(notJudged, string(b.l.Ref)) {
				notJudged = append(notJudged, string(b.l.Ref))
			}
			if reason == "" {
				reason = why
			}
			continue
		}
		judged = true
		switch b.l.Ref {
		case core.RefAGL:
			detail.HeightAGLM = ptr(h)
		case core.RefWGS84:
			detail.AltHAEM = ptr(h)
		case core.RefAMSL:
		}
		beyondM := h - b.l.ValueM
		if b.lower {
			beyondM = b.l.ValueM - h
		}
		if math.IsNaN(beyondM) {
			return Result{NotEvaluated: true, Reason: ReasonInvalidZone}
		}
		if beyondM > margin {
			return Result{} // judged, and outside even the widened band
		}
		if beyondM > 0 {
			withinBand = false
		}
	}
	res := Result{Raise: &Raise{Kind: KindZone, Severity: sev, Detail: detail}}
	if widened && judged {
		res.Raise.Detail.VerticalKnown = ptr(false)
		res.Raise.Detail.WithinBand = ptr(withinBand)
		if !withinBand {
			res.Raise.Severity = atMostWarning(sev)
		}
	}
	if len(notJudged) == 0 {
		return res
	}
	if !warnsUnjudged(z.Type) || len(notJudged) != 1 || notJudged[0] != string(core.RefAGL) {
		return Result{NotEvaluated: true, Reason: reason}
	}
	res.LimitNotJudged = true
	res.Reason = reason
	res.Raise.Severity = atMostWarning(sev)
	res.Raise.Detail.VerticalKnown = ptr(false)
	res.Raise.Detail.LimitNotJudged = ptr(true)
	res.Raise.Detail.NotJudged = notJudged
	return res
}
