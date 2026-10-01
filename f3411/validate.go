package f3411

import (
	"fmt"
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// What UnmarshalRIDFlight and UnmarshalGetFlightsResponse check after
// decoding, and nothing more (a member not listed is passed through as
// decoded). Required members are those the OpenAPI file requires; ranges
// are the OpenAPI minimum and maximum or, where it gives none, the Table 1
// constants of constants.py:
//
//   - RIDFlight: id present and not empty; aircraft_type present and one
//     of UAType; current_state, or else operating_area with at least one
//     volume (the OpenAPI description requires one of them).
//   - RIDAircraftState: timestamp present, format RFC3339, a non-zero
//     value; timestamp_accuracy >= 0; speed_accuracy present and one of
//     SpeedAccuracy; operational_status, when given, one of
//     RIDOperationalStatus; speed, when given, 0 to MaxSpeed or
//     SpecialSpeed; track, when given, 0 to below MaxTrackDirection or
//     SpecialTrackDirection; vertical_speed, when given, within
//     MaxAbsVerticalSpeed of 0 or SpecialVerticalSpeed.
//   - RIDAircraftPosition: lat, when given, -90 to 90; lng, when given,
//     -180 to 180; accuracy_h and accuracy_v, when given, in their
//     enumerations; height.reference, when height is given, one of
//     RIDHeightReference. alt, pressure_altitude and height.distance are
//     finite by decoding; the OpenAPI file gives them no range.
//   - recent_positions: each with a valid time and position as above.
//   - operating_area volumes: what Volume4DToZonesEnvelope requires, and
//     each altitude W84, M, -8000 to 100000 m (OpenAPI Altitude).
//   - GetFlightsResponse: timestamp as above, and each flight.
//
// A refusal is a *core.FieldError naming the member by its JSON path.

// checker collects the first problem with its path.
type checker struct{ err error }

func (c *checker) fail(path, format string, args ...any) {
	if c.err == nil {
		c.err = core.Fieldf(path, format, args...)
	}
}

func (c *checker) time(path string, t *Time) {
	if t == nil {
		c.fail(path, "missing: required")
		return
	}
	if t.Format != RFC3339 {
		c.fail(path+".format", "%q is not RFC3339", string(t.Format))
	}
	if t.Value.IsZero() {
		c.fail(path+".value", "missing: required")
	}
}

func (c *checker) latLng(path string, lat, lng *float64) {
	if lat != nil && (*lat < -90 || *lat > 90) {
		c.fail(path+".lat", "%v is outside -90 to 90", *lat)
	}
	if lng != nil && (*lng < -180 || *lng > 180) {
		c.fail(path+".lng", "%v is outside -180 to 180", *lng)
	}
}

// altitudeRange is the OpenAPI Altitude.value range in metres.
const (
	altitudeMinM = -8000
	altitudeMaxM = 100000
)

func (c *checker) altitude(path string, a *Altitude) {
	if a == nil {
		return
	}
	if _, err := a.HAEM(); err != nil {
		c.fail(path, "%v", err)
		return
	}
	if a.Value < altitudeMinM || a.Value > altitudeMaxM {
		c.fail(path+".value", "%v is outside %d to %d m", a.Value, altitudeMinM, altitudeMaxM)
	}
}

func (c *checker) volume(path string, v *Volume4D) {
	if _, _, _, err := Volume4DToZonesEnvelope(*v); err != nil {
		c.fail(path, "%v", err)
	}
	c.altitude(path+".volume.altitude_lower", v.Volume.AltitudeLower)
	c.altitude(path+".volume.altitude_upper", v.Volume.AltitudeUpper)
}

func (c *checker) position(path string, p *RIDAircraftPosition) {
	c.latLng(path, p.Lat, p.Lng)
	if p.AccuracyH != nil && !p.AccuracyH.Valid() {
		c.fail(path+".accuracy_h", "%q is not a HorizontalAccuracy", string(*p.AccuracyH))
	}
	if p.AccuracyV != nil && !p.AccuracyV.Valid() {
		c.fail(path+".accuracy_v", "%q is not a VerticalAccuracy", string(*p.AccuracyV))
	}
	if p.Height != nil && !p.Height.Reference.Valid() {
		c.fail(path+".height.reference", "%q is not a RIDHeightReference", string(p.Height.Reference))
	}
}

func (c *checker) state(path string, s *RIDAircraftState) {
	ts := s.Timestamp
	c.time(path+".timestamp", &ts)
	if s.TimestampAccuracy < 0 {
		c.fail(path+".timestamp_accuracy", "%v is negative", s.TimestampAccuracy)
	}
	if !s.SpeedAccuracy.Valid() {
		c.fail(path+".speed_accuracy", "%q is not a SpeedAccuracy (required)", string(s.SpeedAccuracy))
	}
	if s.OperationalStatus != nil && !s.OperationalStatus.Valid() {
		c.fail(path+".operational_status", "%q is not a RIDOperationalStatus", string(*s.OperationalStatus))
	}
	if v := s.Speed; v != nil && *v != SpecialSpeed && (*v < 0 || *v > MaxSpeed) {
		c.fail(path+".speed", "%v is outside 0 to %v and not %d", *v, MaxSpeed, SpecialSpeed)
	}
	if v := s.Track; v != nil && *v != SpecialTrackDirection && (*v < MinTrackDirection || *v >= MaxTrackDirection) {
		c.fail(path+".track", "%v is outside %d to below %d and not %d", *v, MinTrackDirection, MaxTrackDirection, SpecialTrackDirection)
	}
	if v := s.VerticalSpeed; v != nil && *v != SpecialVerticalSpeed && math.Abs(float64(*v)) > MaxAbsVerticalSpeed {
		c.fail(path+".vertical_speed", "%v is beyond ±%d and not %d", *v, MaxAbsVerticalSpeed, SpecialVerticalSpeed)
	}
	c.position(path+".position", &s.Position)
}

func (c *checker) flight(path string, f *RIDFlight) {
	if f.Id == "" {
		c.fail(path+".id", "missing: required")
	}
	if !f.AircraftType.Valid() {
		c.fail(path+".aircraft_type", "%q is not a UAType (required)", string(f.AircraftType))
	}
	switch {
	case f.CurrentState != nil:
		c.state(path+".current_state", f.CurrentState)
	case f.OperatingArea == nil || f.OperatingArea.Volumes == nil || len(*f.OperatingArea.Volumes) == 0:
		c.fail(path, "has neither current_state nor an operating_area with a volume; one is required")
	}
	if f.OperatingArea != nil && f.OperatingArea.Volumes != nil {
		for i := range *f.OperatingArea.Volumes {
			c.volume(fmt.Sprintf("%s.operating_area.volumes[%d]", path, i), &(*f.OperatingArea.Volumes)[i])
		}
	}
	if f.RecentPositions != nil {
		for i := range *f.RecentPositions {
			rp := &(*f.RecentPositions)[i]
			here := fmt.Sprintf("%s.recent_positions[%d]", path, i)
			t := rp.Time
			c.time(here+".time", &t)
			c.position(here+".position", &rp.Position)
		}
	}
}
