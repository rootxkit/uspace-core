package f3411

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The example messages in testdata/examples. uas_standards publishes no
// whole-message F3411-22a examples (its tests only import the module) and
// the InterUSS dss repository's request bodies
// (build/dev/postman_collection.json) are F3411-19 and pre-standard SCD
// templates with {{placeholders}}. The files are therefore assembled here
// from the OpenAPI file pinned in SOURCE: every value that has an
// `example:` there uses it (the flight id uss1.JA6kHYCcByQ-6AfU, details
// id a3423b-213401-0023, lat 34.123, lng -118.456, alt 1321.2, speed 1.9,
// vertical_speed 0.2, the UASID examples, owner myuss, uss_base_url
// https://example.com/rid, the UUIDv4 example, the time
// 1985-04-12T23:20:50.52Z, radius 300.183) and every other value is a
// member of its enumeration or range.
var examples = map[string]func() any{
	"rid_flight.json":                  func() any { return new(RIDFlight) },
	"rid_flight_special_values.json":   func() any { return new(RIDFlight) },
	"rid_flight_max_speed.json":        func() any { return new(RIDFlight) },
	"rid_flight_operating_area.json":   func() any { return new(RIDFlight) },
	"get_flights_response.json":        func() any { return new(GetFlightsResponse) },
	"get_flight_details_response.json": func() any { return new(GetFlightDetailsResponse) },
	"identification_service_area.json": func() any { return new(IdentificationServiceArea) },
	"put_isa_parameters.json":          func() any { return new(CreateIdentificationServiceAreaParameters) },
	"subscription.json":                func() any { return new(Subscription) },
}

func readExample(t testing.TB, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// strict decodes refusing unknown members: a member name the generated
// types do not know fails here instead of reading as absent.
func strict(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// generic decodes raw as plain JSON values for a by-value comparison.
func generic(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// Every example decodes strictly, marshals back to the same JSON by value,
// and decodes again to an equal value.
func TestExamplesRoundTrip(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("testdata", "examples"))
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		seen++
		mk, ok := examples[e.Name()]
		if !ok {
			t.Errorf("%s has no type in the examples table", e.Name())
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			raw := readExample(t, e.Name())
			first := mk()
			if err := strict(raw, first); err != nil {
				t.Fatalf("strict decode: %v", err)
			}
			out, err := json.Marshal(first)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := generic(t, out), generic(t, raw); !reflect.DeepEqual(got, want) {
				t.Errorf("marshal changed the message:\n got %s\nwant %s", out, raw)
			}
			second := mk()
			if err := strict(out, second); err != nil {
				t.Fatalf("strict decode of the marshalled form: %v", err)
			}
			if !reflect.DeepEqual(first, second) {
				t.Errorf("decode(marshal(decode(x))) differs from decode(x)")
			}
		})
	}
	if seen != len(examples) {
		t.Errorf("%d example files, %d in the table", seen, len(examples))
	}
}

// A member the standard does not define is ignored on the wire (spec 02
// section 1), and the known members still read. The strict decoder, which
// the round-trip test uses, refuses the same message: the member really is
// unknown.
func TestUnknownMembersIgnoredOnTheWire(t *testing.T) {
	raw := readExample(t, "rid_flight.json")
	withExtra := append([]byte(`{"x_vendor_extension":{"a":[1,2,3]},`), raw[1:]...)
	f, err := UnmarshalRIDFlight(withExtra)
	if err != nil {
		t.Fatalf("an unknown member was refused: %v", err)
	}
	if f.Id != "uss1.JA6kHYCcByQ-6AfU" || f.CurrentState == nil || f.CurrentState.SpeedMS() == nil {
		t.Errorf("known members lost: %+v", f)
	}
	if err := strict(withExtra, new(RIDFlight)); err == nil {
		t.Error("the strict decoder accepted the unknown member; the test proves nothing")
	}
}
