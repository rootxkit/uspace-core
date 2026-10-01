package f3411

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

func f32(v float32) *float32 { return &v }

func mustFlight(t *testing.T, name string) *RIDFlight {
	t.Helper()
	f, err := UnmarshalRIDFlight(readExample(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// Normal values decode to their value, as the sender wrote it (04 section
// 3.1): the presence side of the special-value pairs below.
func TestValuesDecodeAsWritten(t *testing.T) {
	s := mustFlight(t, "rid_flight.json").CurrentState
	checks := []struct {
		name string
		got  *float64
		want float64
	}{
		{"speed", s.SpeedMS(), 1.9},
		{"track", s.TrackDeg(), 120},
		{"vertical speed", s.VerticalSpeedMS(), 0.2},
		{"height", s.Position.Height.DistanceM(), 50},
		{"alt", s.Position.AltHAEM(), 1321.2},
		{"pressure alt", s.Position.PressureAltM(), 1300.5},
	}
	for _, c := range checks {
		if c.got == nil || *c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	if s.SpeedIsMax() {
		t.Error("1.9 m/s read as the open-ended maximum")
	}
	if p := s.Position.LatLon(); p != (core.LatLon{LatDeg: 34.123, LonDeg: -118.456}) {
		t.Errorf("LatLon %+v", p)
	}
}

// The special values of Table 1 decode to nil, never to a number.
func TestSpecialValuesDecodeToNil(t *testing.T) {
	s := mustFlight(t, "rid_flight_special_values.json").CurrentState
	for name, got := range map[string]*float64{
		"speed 255":                s.SpeedMS(),
		"track 361":                s.TrackDeg(),
		"vertical speed 63":        s.VerticalSpeedMS(),
		"height -1000":             s.Position.Height.DistanceM(),
		"alt -1000":                s.Position.AltHAEM(),
		"pressure altitude -1000":  s.Position.PressureAltM(),
		"absent speed":             RIDAircraftState{}.SpeedMS(),
		"absent track":             RIDAircraftState{}.TrackDeg(),
		"absent vertical speed":    RIDAircraftState{}.VerticalSpeedMS(),
		"absent height":            RIDHeight{}.DistanceM(),
		"absent alt":               RIDAircraftPosition{}.AltHAEM(),
		"absent pressure altitude": RIDAircraftPosition{}.PressureAltM(),
	} {
		if got != nil {
			t.Errorf("%s decoded to %v, want nil", name, *got)
		}
	}
}

// MaxSpeed is a value, read as "at least 254.25"; the other ends of the
// ranges are values too.
func TestMaxSpeedIsOpenEnded(t *testing.T) {
	s := mustFlight(t, "rid_flight_max_speed.json").CurrentState
	if v := s.SpeedMS(); v == nil || *v != MaxSpeed {
		t.Fatalf("speed %v, want 254.25", v)
	}
	if !s.SpeedIsMax() {
		t.Error("254.25 not reported as the open-ended maximum")
	}
	if v := s.TrackDeg(); v == nil || *v != 0 {
		t.Errorf("track 0 read as %v", v)
	}
	if v := s.VerticalSpeedMS(); v == nil || *v != -MaxAbsVerticalSpeed {
		t.Errorf("vertical speed -62 read as %v", v)
	}
	if (RIDAircraftState{Speed: f32(SpecialSpeed)}).SpeedIsMax() {
		t.Error("255 reported as the maximum")
	}
}

// Only Ground is not airborne (R-11); an absent status is Undeclared.
func TestAirborneRule(t *testing.T) {
	for _, st := range []RIDOperationalStatus{Undeclared, Airborne, Emergency, RemoteIDSystemFailure, "SomethingNew"} {
		if !st.Airborne() {
			t.Errorf("%s not airborne", st)
		}
		if !(RIDAircraftState{OperationalStatus: &st}).Airborne() {
			t.Errorf("state with %s not airborne", st)
		}
	}
	g := Ground
	if g.Airborne() || (RIDAircraftState{OperationalStatus: &g}).Airborne() {
		t.Error("Ground counted as airborne")
	}
	if !(RIDAircraftState{}).Airborne() {
		t.Error("absent status (Undeclared) not airborne")
	}
}

// A position without lat or lng is not a valid LatLon, never 0, 0.
func TestLatLonMissingIsInvalid(t *testing.T) {
	lat := 41.7
	for name, p := range map[string]RIDAircraftPosition{
		"no lat or lng": {},
		"no lng":        {Lat: &lat},
		"no lat":        {Lng: &lat},
	} {
		if ll := p.LatLon(); ll.Valid() {
			t.Errorf("%s: %+v is valid", name, ll)
		}
	}
	lng := 44.8
	if !(RIDAircraftPosition{Lat: &lat, Lng: &lng}).LatLon().Valid() {
		t.Error("a full position is not valid")
	}
}

func TestAltitudeHAEM(t *testing.T) {
	v, err := Altitude{Reference: W84, Units: AltitudeUnitsM, Value: 19.5}.HAEM()
	if err != nil || v == nil || *v != 19.5 {
		t.Fatalf("W84 M 19.5: %v, %v", v, err)
	}
	v, err = Altitude{Reference: W84, Units: AltitudeUnitsM, Value: SpecialHeight}.HAEM()
	if err != nil || v != nil {
		t.Errorf("-1000 (unknown): %v, %v", v, err)
	}
	for name, a := range map[string]Altitude{
		"reference": {Reference: "AMSL", Units: AltitudeUnitsM, Value: 1},
		"units":     {Reference: W84, Units: "FT", Value: 1},
		"value":     {Reference: W84, Units: AltitudeUnitsM, Value: math.Inf(1)},
	} {
		_, err := a.HAEM()
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != name {
			t.Errorf("%s: error %v, want a FieldError on %q", name, err, name)
		}
	}
}

func ts(t *testing.T, s string) *Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return &Time{Format: RFC3339, Value: v}
}

func TestVolume4DEnvelopeCircle(t *testing.T) {
	f := mustFlight(t, "rid_flight_operating_area.json")
	v := (*f.OperatingArea.Volumes)[0]
	box, start, end, err := Volume4DToZonesEnvelope(v)
	if err != nil {
		t.Fatal(err)
	}
	if !box.Contains(core.LatLon{LatDeg: 34.123, LonDeg: -118.456}) {
		t.Error("the centre is outside the box")
	}
	// 300.183 m north of the centre is on the circle: inside the box.
	if !box.Contains(core.LatLon{LatDeg: 34.123 + 300.183/111000, LonDeg: -118.456}) {
		t.Error("the circle's edge is outside the box")
	}
	if box.Contains(core.LatLon{LatDeg: 34.2, LonDeg: -118.456}) {
		t.Error("a point 8 km away is inside the box")
	}
	if !start.Equal(time.Date(1985, 4, 12, 23, 20, 50, 520000000, time.UTC)) || !end.Equal(start.Add(30*time.Minute)) {
		t.Errorf("window %v .. %v", start, end)
	}
}

func TestVolume4DEnvelopePolygon(t *testing.T) {
	var p CreateIdentificationServiceAreaParameters
	if err := strict(readExample(t, "put_isa_parameters.json"), &p); err != nil {
		t.Fatal(err)
	}
	box, _, _, err := Volume4DToZonesEnvelope(p.Extents)
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []core.LatLon{{LatDeg: 34.123, LonDeg: -118.456}, {LatDeg: 34.133, LonDeg: -118.446}} {
		if !box.Contains(in) {
			t.Errorf("vertex %+v outside the box", in)
		}
	}
	if box.Contains(core.LatLon{LatDeg: 34.2, LonDeg: -118.45}) {
		t.Error("a far point is inside the box")
	}
	// No time bounds: both zero.
	v := p.Extents
	v.TimeStart, v.TimeEnd = nil, nil
	if _, s, e, err := Volume4DToZonesEnvelope(v); err != nil || !s.IsZero() || !e.IsZero() {
		t.Errorf("unbounded window: %v %v %v", s, e, err)
	}
}

// A long edge at high latitude bows poleward of its vertices; the box is
// padded so that the geodesic's highest point is inside it.
func TestVolume4DEnvelopeHoldsTheGeodesicBulge(t *testing.T) {
	poly := Polygon{Vertices: []LatLngPoint{{Lat: 60, Lng: 0}, {Lat: 60, Lng: 10}, {Lat: 59, Lng: 5}}}
	box, _, _, err := Volume4DToZonesEnvelope(Volume4D{Volume: Volume3D{OutlinePolygon: &poly}})
	if err != nil {
		t.Fatal(err)
	}
	// On the sphere the great circle from (60, 0) to (60, 10) peaks at
	// 5 E, latitude atan(tan 60 / cos 5) = 60.0945 N, above both vertices.
	peakDeg := math.Atan(math.Tan(60*math.Pi/180)/math.Cos(5*math.Pi/180)) * 180 / math.Pi
	if !box.Contains(core.LatLon{LatDeg: peakDeg, LonDeg: 5}) {
		t.Errorf("box %+v misses the edge's highest point", box)
	}
}

func TestVolume4DEnvelopeRefusals(t *testing.T) {
	circle := Circle{Center: &LatLngPoint{Lat: 41.7, Lng: 44.8}, Radius: &Radius{Units: RadiusUnitsM, Value: 100}}
	tri := Polygon{Vertices: []LatLngPoint{{Lat: 41.7, Lng: 44.8}, {Lat: 41.8, Lng: 44.8}, {Lat: 41.8, Lng: 44.9}}}
	tooMany := Polygon{Vertices: make([]LatLngPoint, MaxVolumeVertices+1)}
	for i := range tooMany.Vertices {
		tooMany.Vertices[i] = LatLngPoint{Lat: 41 + float64(i)*1e-5, Lng: 44}
	}
	cases := map[string]struct {
		v     Volume4D
		field string
	}{
		"no outline":       {Volume4D{}, "volume"},
		"both outlines":    {Volume4D{Volume: Volume3D{OutlineCircle: &circle, OutlinePolygon: &tri}}, "volume"},
		"no centre":        {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Radius: circle.Radius}}}, "volume.outline_circle.center"},
		"bad centre":       {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Center: &LatLngPoint{Lat: 91}, Radius: circle.Radius}}}, "volume.outline_circle.center"},
		"no radius":        {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Center: circle.Center}}}, "volume.outline_circle.radius"},
		"radius in feet":   {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Center: circle.Center, Radius: &Radius{Units: "FT", Value: 1}}}}, "volume.outline_circle.radius.units"},
		"zero radius":      {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Center: circle.Center, Radius: &Radius{Units: RadiusUnitsM}}}}, "volume.outline_circle.radius.value"},
		"two vertices":     {Volume4D{Volume: Volume3D{OutlinePolygon: &Polygon{Vertices: tri.Vertices[:2]}}}, "volume.outline_polygon.vertices"},
		"too many":         {Volume4D{Volume: Volume3D{OutlinePolygon: &tooMany}}, "volume.outline_polygon.vertices"},
		"bad vertex":       {Volume4D{Volume: Volume3D{OutlinePolygon: &Polygon{Vertices: []LatLngPoint{{Lat: 1}, {Lat: 2}, {Lat: 3, Lng: 200}}}}}, "volume.outline_polygon.vertices[2]"},
		"start format":     {Volume4D{Volume: Volume3D{OutlinePolygon: &tri}, TimeStart: &Time{Format: "ISO"}}, "time_start.format"},
		"end format":       {Volume4D{Volume: Volume3D{OutlinePolygon: &tri}, TimeEnd: &Time{Format: "ISO"}}, "time_end.format"},
		"end before start": {Volume4D{Volume: Volume3D{OutlinePolygon: &tri}, TimeStart: ts(t, "2026-01-02T00:00:00Z"), TimeEnd: ts(t, "2026-01-01T00:00:00Z")}, "time_end"},
	}
	for name, c := range cases {
		_, _, _, err := Volume4DToZonesEnvelope(c.v)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: error %v, want a FieldError on %q", name, err, c.field)
		}
	}
	// E-01: the same volumes with the defect removed are accepted.
	for name, v := range map[string]Volume4D{
		"circle":           {Volume: Volume3D{OutlineCircle: &circle}},
		"triangle":         {Volume: Volume3D{OutlinePolygon: &tri}},
		"bound vertices":   {Volume: Volume3D{OutlinePolygon: &Polygon{Vertices: tooMany.Vertices[:MaxVolumeVertices]}}},
		"start equals end": {Volume: Volume3D{OutlinePolygon: &tri}, TimeStart: ts(t, "2026-01-01T00:00:00Z"), TimeEnd: ts(t, "2026-01-01T00:00:00Z")},
	} {
		if _, _, _, err := Volume4DToZonesEnvelope(v); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
}

func TestUnmarshalRefusals(t *testing.T) {
	_, err := UnmarshalRIDFlight([]byte(`{"id": "x", "current_state": {"speed": "fast"}}`))
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "flight.current_state.speed" {
		t.Errorf("a string speed: %v", err)
	}
	if _, err := UnmarshalRIDFlight([]byte(`{"id": `)); err == nil || !strings.Contains(err.Error(), "flight") {
		t.Errorf("truncated JSON: %v", err)
	}
	big := make([]byte, MaxMessageBytes+1)
	if _, err := UnmarshalRIDFlight(big); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("oversized input: %v", err)
	}
	if _, err := UnmarshalGetFlightsResponse([]byte(`{"flights": {}}`)); err == nil {
		t.Error("flights as an object accepted")
	}
	r, err := UnmarshalGetFlightsResponse(readExample(t, "get_flights_response.json"))
	if err != nil || r.Flights == nil || len(*r.Flights) != 1 {
		t.Errorf("a valid response: %v, %v", r, err)
	}
}

func TestConstantsFromUASStandards(t *testing.T) {
	if ScopeServiceProvider != "rid.service_provider" || ScopeDisplayProvider != "rid.display_provider" {
		t.Error("scope strings changed")
	}
	if NetMaxDisplayAreaDiagonalKm != 7 || NetDetailsMaxDisplayAreaDiagonalKm != 2 ||
		NetMaxNearRealTimeDataPeriodSeconds != 60 || NetDpMaxDataRetentionPeriodSeconds != 86400 ||
		NetMinUasLocRefreshFrequencyHz != 1 {
		t.Error("a Net* constant changed")
	}
}
