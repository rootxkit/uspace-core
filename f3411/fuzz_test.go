package f3411

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FuzzUnmarshalRIDFlight: any bytes either decode or are refused with an
// error, never a panic; an accepted flight satisfies the checks of
// validate.go; its accessors never panic; and it marshals and decodes back
// to an equal value.
func FuzzUnmarshalRIDFlight(f *testing.F) {
	entries, err := os.ReadDir(filepath.Join("testdata", "examples"))
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range entries {
		raw, err := os.ReadFile(filepath.Join("testdata", "examples", e.Name()))
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Add([]byte(`{"current_state":{"speed":255,"track":361,"position":{"alt":-1000,"height":{"distance":-1000}}}}`))
	f.Add([]byte(`{"operating_area":{"volumes":[{"volume":{"outline_polygon":{"vertices":[]}}}]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		fl, err := UnmarshalRIDFlight(data)
		if err != nil {
			return
		}
		// What an accepted flight guarantees (validate.go).
		if fl.Id == "" || !fl.AircraftType.Valid() {
			t.Fatalf("accepted without an id or a known aircraft type: %q %q", fl.Id, fl.AircraftType)
		}
		if s := fl.CurrentState; s != nil {
			p := s.Position
			if (p.Lat != nil && (*p.Lat < -90 || *p.Lat > 90)) || (p.Lng != nil && (*p.Lng < -180 || *p.Lng > 180)) {
				t.Fatalf("accepted a position out of range: %v %v", p.Lat, p.Lng)
			}
			if s.Timestamp.Format != RFC3339 || s.Timestamp.Value.IsZero() || !s.SpeedAccuracy.Valid() {
				t.Fatalf("accepted a state without a valid timestamp or speed accuracy: %+v", s)
			}
			if s.OperationalStatus != nil && !s.OperationalStatus.Valid() {
				t.Fatalf("accepted status %q", *s.OperationalStatus)
			}
		} else if fl.OperatingArea == nil || fl.OperatingArea.Volumes == nil || len(*fl.OperatingArea.Volumes) == 0 {
			t.Fatal("accepted a flight with neither current_state nor an operating area")
		}
		if s := fl.CurrentState; s != nil {
			_ = s.SpeedMS()
			_ = s.TrackDeg()
			_ = s.VerticalSpeedMS()
			_ = s.SpeedIsMax()
			_ = s.Airborne()
			_ = s.Position.AltHAEM()
			_ = s.Position.PressureAltM()
			_ = s.Position.LatLon()
			if s.Position.Height != nil {
				_ = s.Position.Height.DistanceM()
			}
		}
		if fl.OperatingArea != nil && fl.OperatingArea.Volumes != nil {
			for _, v := range *fl.OperatingArea.Volumes {
				_, _, _, _ = Volume4DToZonesEnvelope(v)
			}
		}
		out, err := json.Marshal(fl)
		if err != nil {
			return // a time.Time the encoder cannot write (a year past 9999 after its offset)
		}
		back, err := UnmarshalRIDFlight(out)
		if err != nil {
			t.Fatalf("the marshalled flight does not decode: %v\n%s", err, out)
		}
		again, err := json.Marshal(back)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(out, again) {
			t.Fatalf("round trip changed the flight:\n%s\n%s", out, again)
		}
	})
}
