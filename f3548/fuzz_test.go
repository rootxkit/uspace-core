package f3548

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FuzzUnmarshalOperationalIntent: any bytes either decode or are refused
// with an error, never a panic; the altitude and envelope helpers never
// panic on what decodes; and a decoded intent marshals and decodes back to
// the same JSON.
func FuzzUnmarshalOperationalIntent(f *testing.F) {
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
	f.Add([]byte(`{"details":{"volumes":[{"volume":{"outline_circle":{"radius":{"value":-1,"units":"M"}}}}]}}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		oi, err := UnmarshalOperationalIntent(data)
		if err != nil {
			return
		}
		// What an accepted intent guarantees (validate.go).
		r := oi.Reference
		if r.Id == "" || r.Manager == "" || !r.State.Valid() || !r.UssAvailability.Valid() ||
			r.TimeStart.Format != RFC3339 || r.TimeEnd.Format != RFC3339 || r.TimeEnd.Value.Before(r.TimeStart.Value) {
			t.Fatalf("accepted an invalid reference: %+v", r)
		}
		for _, vs := range []*[]Volume4D{oi.Details.Volumes, oi.Details.OffNominalVolumes} {
			if vs == nil {
				continue
			}
			for _, v := range *vs {
				if _, _, _, err := Volume4DToZonesEnvelope(v); err != nil {
					t.Fatalf("accepted a volume without an envelope: %v", err)
				}
				if v.Volume.AltitudeLower != nil {
					_, _ = v.Volume.AltitudeLower.HAEM()
				}
				if v.Volume.AltitudeUpper != nil {
					_, _ = v.Volume.AltitudeUpper.HAEM()
				}
			}
		}
		out, err := json.Marshal(oi)
		if err != nil {
			return // a time.Time the encoder cannot write (a year past 9999 after its offset)
		}
		back, err := UnmarshalOperationalIntent(out)
		if err != nil {
			t.Fatalf("the marshalled intent does not decode: %v\n%s", err, out)
		}
		again, err := json.Marshal(back)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(out, again) {
			t.Fatalf("round trip changed the intent:\n%s\n%s", out, again)
		}
	})
}
