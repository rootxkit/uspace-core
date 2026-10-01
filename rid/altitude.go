package rid

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// verticalAccuracyUnknown is MAV_ODID_VER_ACC 0: unknown, which is not a
// flag (R-08).
const verticalAccuracyUnknown = 0

// AltInput is what one Location says about altitude, plus the geoid.
type AltInput struct {
	// AltHAEM is the geodetic altitude above the WGS84 ellipsoid; nil when
	// the broadcast said unknown.
	AltHAEM *float64
	// AltPressureM is the pressure altitude (ISA 1013.25 hPa, not AMSL);
	// nil when unknown.
	AltPressureM *float64
	// VertAccuracyCode is MAV_ODID_VER_ACC: 0 unknown, then 1 to 6 for
	// under 150, 45, 25, 10, 3 and 1 m.
	VertAccuracyCode uint8
	// UndulationM is the geoid height N above the ellipsoid at the
	// position; nil when no geoid is configured or the position is
	// unknown.
	UndulationM *float64
}

// AltPolicy holds the altitude selection thresholds (R-08).
type AltPolicy struct {
	// MinVerticalAccuracy is the lowest known vertical accuracy code at
	// which the geodetic altitude is used. Default 2 (under 45 m).
	MinVerticalAccuracy uint8
	// HoldPressure: in SelectAltitude, the pressure hold is in force for
	// this message (pressure first, while there is one). In an
	// AltitudeSelector, the hold is enabled and the selector decides when
	// it is in force. Default true.
	HoldPressure bool
	// PressureHoldS is how long an AltitudeSelector stays on pressure
	// after the last poor geodetic altitude. Default 10 s.
	PressureHoldS float64
}

// DefaultAltPolicy returns utm's thresholds: code 2, hold on, 10 s.
func DefaultAltPolicy() AltPolicy {
	return AltPolicy{MinVerticalAccuracy: 2, HoldPressure: true, PressureHoldS: 10}
}

// AltResult is the AMSL altitude of an observation and where it came
// from.
type AltResult struct {
	// AltAMSLM is the altitude: the geodetic one through the geoid, or
	// the pressure altitude as broadcast (which is not AMSL; Source says
	// so). Nil when there is no usable altitude.
	AltAMSLM *float64
	// Source is core.AltGeodetic, core.AltPressure, or core.AltNone when
	// AltAMSLM is nil.
	Source core.AltSource
}

// finite returns p when it points at a finite number, else nil.
func finite(p *float64) *float64 {
	if p == nil || math.IsNaN(*p) || math.IsInf(*p, 0) {
		return nil
	}
	return p
}

// geodeticUsable reports whether there is a geodetic altitude whose
// accuracy is not flagged poor: unknown (0) or at least the minimum.
func geodeticUsable(in AltInput, minAccuracy uint8) bool {
	if finite(in.AltHAEM) == nil {
		return false
	}
	c := in.VertAccuracyCode
	return c == verticalAccuracyUnknown || c >= minAccuracy
}

// SelectAltitude chooses the AMSL altitude of one Location without state
// (R-07, R-08):
//
//   - while the hold is in force (pol.HoldPressure) and there is a
//     pressure altitude: the pressure altitude;
//   - a usable geodetic altitude with a geoid: HAE - N (geodetic);
//   - a usable geodetic altitude without a geoid: none, even with a
//     pressure altitude: pressure replaces a poor geodetic altitude, never
//     a missing geoid;
//   - a missing or poor geodetic altitude with a pressure altitude: the
//     pressure altitude as broadcast (pressure, not AMSL);
//   - otherwise none.
func SelectAltitude(in AltInput, pol AltPolicy) AltResult {
	return selectAltitude(in, pol.MinVerticalAccuracy, pol.HoldPressure)
}

func selectAltitude(in AltInput, minAccuracy uint8, holding bool) AltResult {
	pressure := finite(in.AltPressureM)
	if holding && pressure != nil {
		v := *pressure
		return AltResult{AltAMSLM: &v, Source: core.AltPressure}
	}
	if geodeticUsable(in, minAccuracy) {
		n := finite(in.UndulationM)
		if n == nil {
			return AltResult{Source: core.AltNone}
		}
		v := *in.AltHAEM - *n
		return AltResult{AltAMSLM: &v, Source: core.AltGeodetic}
	}
	if pressure != nil {
		v := *pressure
		return AltResult{AltAMSLM: &v, Source: core.AltPressure}
	}
	return AltResult{Source: core.AltNone}
}

// AltitudeSelector is SelectAltitude with the pressure hold of one track
// (R-08): once a poor or missing geodetic altitude is seen, the track
// stays on pressure until PressureHoldS after the last such fix, so an
// accuracy hovering at the threshold does not flip the source, and the
// alerts with it, every message. The hold needs a pressure altitude in
// the message to hold. Keep one per track. Not safe for concurrent use.
type AltitudeSelector struct {
	pol            AltPolicy
	pressureUntilS float64
	poorSeen       bool
}

// NewAltitudeSelector returns a selector for one track.
func NewAltitudeSelector(pol AltPolicy) *AltitudeSelector {
	return &AltitudeSelector{pol: pol}
}

// Select chooses the altitude of one Location of the track at nowS (the
// tracker's monotonic clock, seconds). The hold is in force while nowS is
// strictly before the end of the hold.
func (s *AltitudeSelector) Select(in AltInput, nowS float64) AltResult {
	if !geodeticUsable(in, s.pol.MinVerticalAccuracy) {
		s.pressureUntilS = nowS + s.pol.PressureHoldS
		s.poorSeen = true
	}
	holding := s.pol.HoldPressure && s.poorSeen && nowS < s.pressureUntilS
	return selectAltitude(in, s.pol.MinVerticalAccuracy, holding)
}
