package f3548

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

func mustIntent(t *testing.T, name string) *OperationalIntent {
	t.Helper()
	oi, err := UnmarshalOperationalIntent(readExample(t, name))
	if err != nil {
		t.Fatal(err)
	}
	return oi
}

// The four DSS states and nothing else (09 section 1.5).
func TestDSSStates(t *testing.T) {
	want := []OperationalIntentState{"Accepted", "Activated", "Nonconforming", "Contingent"}
	if len(DSSStates) != len(want) {
		t.Fatalf("%d states", len(DSSStates))
	}
	for i, s := range want {
		if DSSStates[i] != s || !s.Valid() {
			t.Errorf("state %d: %q", i, DSSStates[i])
		}
	}
	if OperationalIntentState("Ended").Valid() {
		t.Error("a local state is a DSS state")
	}
}

func TestAltitudeHAEM(t *testing.T) {
	oi := mustIntent(t, "operational_intent.json")
	v3 := (*oi.Details.Volumes)[0].Volume
	lo, err := v3.AltitudeLower.HAEM()
	if err != nil || lo != 19.5 {
		t.Errorf("lower: %v, %v", lo, err)
	}
	hi, err := v3.AltitudeUpper.HAEM()
	if err != nil || hi != 139.5 {
		t.Errorf("upper: %v, %v", hi, err)
	}
	for name, a := range map[string]Altitude{
		"reference": {Reference: "AMSL", Units: AltitudeUnitsM, Value: 1},
		"units":     {Reference: W84, Units: "FT", Value: 1},
		"value":     {Reference: W84, Units: AltitudeUnitsM, Value: math.NaN()},
	} {
		_, err := a.HAEM()
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != name {
			t.Errorf("%s: error %v, want a FieldError on %q", name, err, name)
		}
	}
}

func TestVolume4DEnvelope(t *testing.T) {
	poly := (*mustIntent(t, "operational_intent.json").Details.Volumes)[0]
	box, start, end, err := Volume4DToZonesEnvelope(poly)
	if err != nil {
		t.Fatal(err)
	}
	if !box.Contains(core.LatLon{LatDeg: 34.13, LonDeg: -118.45}) || box.Contains(core.LatLon{LatDeg: 34.2, LonDeg: -118.45}) {
		t.Errorf("polygon box %+v", box)
	}
	if !start.Equal(time.Date(1985, 4, 12, 23, 20, 50, 520000000, time.UTC)) || end.Sub(start) != 30*time.Minute {
		t.Errorf("window %v .. %v", start, end)
	}
	circle := (*mustIntent(t, "operational_intent_activated_circle.json").Details.Volumes)[0]
	box, _, _, err = Volume4DToZonesEnvelope(circle)
	if err != nil {
		t.Fatal(err)
	}
	if !box.Contains(core.LatLon{LatDeg: 34.123 + 300.183/111000, LonDeg: -118.456}) || box.Contains(core.LatLon{LatDeg: 34.2, LonDeg: -118.456}) {
		t.Errorf("circle box %+v", box)
	}
	if p := (LatLngPoint{Lat: 1, Lng: 2}).LatLon(); p != (core.LatLon{LatDeg: 1, LonDeg: 2}) {
		t.Errorf("LatLon %+v", p)
	}
}

func TestVolume4DEnvelopeRefusals(t *testing.T) {
	tri := Polygon{Vertices: []LatLngPoint{{Lat: 41.7, Lng: 44.8}, {Lat: 41.8, Lng: 44.8}, {Lat: 41.8, Lng: 44.9}}}
	tooMany := Polygon{Vertices: make([]LatLngPoint, OiMaxVertices+1)}
	for i := range tooMany.Vertices {
		tooMany.Vertices[i] = LatLngPoint{Lat: 41 + float64(i)*1e-5, Lng: 44}
	}
	for name, c := range map[string]struct {
		v     Volume4D
		field string
	}{
		"no outline":     {Volume4D{}, "volume"},
		"too many":       {Volume4D{Volume: Volume3D{OutlinePolygon: &tooMany}}, "volume.outline_polygon.vertices"},
		"radius in feet": {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Center: &LatLngPoint{Lat: 1}, Radius: &Radius{Units: "FT", Value: 1}}}}, "volume.outline_circle.radius.units"},
		"both outlines":  {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{}, OutlinePolygon: &tri}}, "volume"},
		"no centre":      {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Radius: &Radius{Units: RadiusUnitsM, Value: 1}}}}, "volume.outline_circle.center"},
		"no radius":      {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Center: &LatLngPoint{Lat: 1}}}}, "volume.outline_circle.radius"},
		"zero radius":    {Volume4D{Volume: Volume3D{OutlineCircle: &Circle{Center: &LatLngPoint{Lat: 1}, Radius: &Radius{Units: RadiusUnitsM}}}}, "volume.outline_circle.radius.value"},
		"two vertices":   {Volume4D{Volume: Volume3D{OutlinePolygon: &Polygon{Vertices: tri.Vertices[:2]}}}, "volume.outline_polygon.vertices"},
		"bad vertex":     {Volume4D{Volume: Volume3D{OutlinePolygon: &Polygon{Vertices: []LatLngPoint{{Lat: 1}, {Lat: 2}, {Lat: 95}}}}}, "volume.outline_polygon.vertices[2]"},
		"start format":   {Volume4D{Volume: Volume3D{OutlinePolygon: &tri}, TimeStart: &Time{Format: "ISO"}}, "time_start.format"},
		"end format":     {Volume4D{Volume: Volume3D{OutlinePolygon: &tri}, TimeEnd: &Time{Format: "ISO"}}, "time_end.format"},
		"end before start": {Volume4D{Volume: Volume3D{OutlinePolygon: &tri},
			TimeStart: &Time{Format: RFC3339, Value: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
			TimeEnd:   &Time{Format: RFC3339, Value: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}, "time_end"},
	} {
		_, _, _, err := Volume4DToZonesEnvelope(c.v)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: error %v, want a FieldError on %q", name, err, c.field)
		}
	}
	// E-01: at the bound it is accepted.
	atBound := Polygon{Vertices: tooMany.Vertices[:OiMaxVertices]}
	if _, _, _, err := Volume4DToZonesEnvelope(Volume4D{Volume: Volume3D{OutlinePolygon: &atBound}}); err != nil {
		t.Errorf("OiMaxVertices vertices refused: %v", err)
	}
	if _, _, _, err := Volume4DToZonesEnvelope(Volume4D{Volume: Volume3D{OutlinePolygon: &tri}}); err != nil {
		t.Errorf("a triangle refused: %v", err)
	}
}

func TestUnmarshalRefusals(t *testing.T) {
	_, err := UnmarshalOperationalIntent([]byte(`{"reference": {"version": "one"}}`))
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "operational_intent.reference.version" {
		t.Errorf("a string version: %v", err)
	}
	if _, err := UnmarshalOperationalIntent([]byte(`[`)); err == nil {
		t.Error("truncated JSON accepted")
	}
	if _, err := UnmarshalOperationalIntent(make([]byte, MaxMessageBytes+1)); err == nil || !strings.Contains(err.Error(), "at most") {
		t.Errorf("oversized input: %v", err)
	}
}

func TestConstantsFromUASStandards(t *testing.T) {
	if ScopeStrategicCoordination != "utm.strategic_coordination" || ScopeConstraintManagement != "utm.constraint_management" ||
		ScopeConstraintProcessing != "utm.constraint_processing" || ScopeConformanceMonitoringForSituationalAwareness != "utm.conformance_monitoring_sa" ||
		ScopeAvailabilityArbitration != "utm.availability_arbitration" {
		t.Error("a scope string changed")
	}
	if MaxRecoverableTimeInNonconformingStateSeconds != 60 || ExternalDataMaxRetentionTimeHours != 24 ||
		TimeSyncMaxDifferentialSeconds != 5 || CstrPublishedNotificationLatencySeconds != 5 || OiMaxPlanHorizonDays != 30 {
		t.Error("a constant changed")
	}
}
