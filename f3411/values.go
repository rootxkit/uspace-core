package f3411

import (
	"math"
	"strconv"

	"github.com/rootxkit/uspace-core/core"
)

// The special values of Table 1 decode to nil (spec 04 section 3.1): a nil
// result means "Invalid, No Value or Unknown", never zero. A field that is
// absent on the wire is nil as well.

// wire converts a value the generated types hold as float32 (the OpenAPI
// `format: float`) to the float64 the sender wrote: the shortest decimal
// that reads back as the float32, so 1.9 stays 1.9 and does not become
// 1.899999976158142.
func wire(f float32) float64 {
	// FormatFloat's output always parses (Inf and NaN included).
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	return v
}

// SpeedMS is the ground speed in metres per second, nil when absent or
// SpecialSpeed (255). A value of MaxSpeed (254.25) means "254.25 m/s or
// more" and is returned as 254.25.
func (s RIDAircraftState) SpeedMS() *float64 {
	if s.Speed == nil || *s.Speed == SpecialSpeed {
		return nil
	}
	v := wire(*s.Speed)
	return &v
}

// SpeedIsMax reports whether the ground speed is the open-ended top
// value MaxSpeed, which means "at least 254.25 m/s".
func (s RIDAircraftState) SpeedIsMax() bool {
	return s.Speed != nil && *s.Speed == MaxSpeed
}

// TrackDeg is the track direction in degrees true, nil when absent or
// SpecialTrackDirection (361).
func (s RIDAircraftState) TrackDeg() *float64 {
	if s.Track == nil || *s.Track == SpecialTrackDirection {
		return nil
	}
	v := wire(*s.Track)
	return &v
}

// VerticalSpeedMS is the vertical speed in metres per second, nil when
// absent or SpecialVerticalSpeed (63).
func (s RIDAircraftState) VerticalSpeedMS() *float64 {
	if s.VerticalSpeed == nil || *s.VerticalSpeed == SpecialVerticalSpeed {
		return nil
	}
	v := wire(*s.VerticalSpeed)
	return &v
}

// Airborne applies the airborne rule of odid.Status (LESSONS R-11) to the
// state's operational status: only Ground is not airborne. An absent
// status is the OpenAPI default, Undeclared, and counts as airborne.
func (s RIDAircraftState) Airborne() bool {
	if s.OperationalStatus == nil {
		return true
	}
	return s.OperationalStatus.Airborne()
}

// Airborne reports whether the status counts as airborne: Undeclared,
// Airborne, Emergency and RemoteIDSystemFailure do; Ground does not. A
// value outside the enumeration counts as airborne, as an undeclared one
// does: a monitor that drops an aircraft it cannot classify misses it.
func (s RIDOperationalStatus) Airborne() bool {
	return s != Ground
}

// DistanceM is the height above the reference in metres (Reference says
// GroundLevel or TakeoffLocation), nil when absent or SpecialHeight
// (-1000).
func (h RIDHeight) DistanceM() *float64 {
	if h.Distance == nil || *h.Distance == SpecialHeight {
		return nil
	}
	v := wire(*h.Distance)
	return &v
}

// AltHAEM is the geodetic altitude, height above the WGS84 ellipsoid in
// metres, nil when absent or SpecialHeight (-1000). F3411 calls it `alt`;
// the name carries its datum (E-13): AMSL is derived from it through the
// geoid by the consumer.
func (p RIDAircraftPosition) AltHAEM() *float64 {
	if p.Alt == nil || *p.Alt == SpecialHeight {
		return nil
	}
	v := wire(*p.Alt)
	return &v
}

// PressureAltM is the uncorrected pressure altitude (ISA 1013.25 hPa, not
// AMSL) in metres, nil when absent or -1000 (Invalid, No Value or Unknown).
func (p RIDAircraftPosition) PressureAltM() *float64 {
	if p.PressureAltitude == nil || *p.PressureAltitude == SpecialHeight {
		return nil
	}
	v := wire(*p.PressureAltitude)
	return &v
}

// LatLon is the position as a core.LatLon. A missing latitude or longitude
// is NaN, so that the result is not Valid and is never read as 0, 0.
func (p RIDAircraftPosition) LatLon() core.LatLon {
	out := core.LatLon{LatDeg: math.NaN(), LonDeg: math.NaN()}
	if p.Lat != nil {
		out.LatDeg = *p.Lat
	}
	if p.Lng != nil {
		out.LonDeg = *p.Lng
	}
	return out
}

// LatLon is the point as a core.LatLon.
func (p LatLngPoint) LatLon() core.LatLon {
	return core.LatLon{LatDeg: p.Lat, LonDeg: p.Lng}
}
