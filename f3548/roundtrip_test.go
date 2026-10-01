package f3548

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
// whole-message F3548-21 examples (its tests only import the module) and
// the InterUSS dss repository's request bodies
// (build/dev/postman_collection.json) are pre-standard
// `operation_references` templates with {{placeholders}}. The files are
// therefore assembled here from the OpenAPI file pinned in SOURCE: every
// value that has an `example:` there uses it (EntityID
// 2f8343be-6482-4d1b-a474-16847e01af1e, EntityOVN
// 9d158f59-80b7-4c11-9c0c-8a2b4d936b2d, EntityVersion 1, SubscriptionID
// 78ea3fe8-71c2-4f5c-9b44-9c02f5563c6f, manager uss1, uss_base_url
// https://uss.example.com/utm, the time 1985-04-12T23:20:50.52Z, lat
// 34.123, lng -118.456, radius 300.183, altitude 19.5, speed 200.1, track
// 120, constraint type com.example.non_utm_aircraft_operations, the error
// message) and every other value is a member of its enumeration or range.
// All four DSS states appear.
var examples = map[string]func() any{
	"operational_intent.json":                         func() any { return new(OperationalIntent) },
	"operational_intent_activated_circle.json":        func() any { return new(OperationalIntent) },
	"operational_intent_contingent.json":              func() any { return new(OperationalIntent) },
	"operational_intent_reference_nonconforming.json": func() any { return new(GetOperationalIntentReferenceResponse) },
	"constraint.json":                                 func() any { return new(GetConstraintDetailsResponse) },
	"operational_intent_telemetry.json":               func() any { return new(GetOperationalIntentTelemetryResponse) },
	"error_response.json":                             func() any { return new(ErrorResponse) },
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
	raw := bytes.TrimSpace(readExample(t, "operational_intent.json"))
	withExtra := append([]byte(`{"x_vendor_extension":{"a":[1,2,3]},`), raw[1:]...)
	oi, err := UnmarshalOperationalIntent(withExtra)
	if err != nil {
		t.Fatalf("an unknown member was refused: %v", err)
	}
	if oi.Reference.State != Accepted || oi.Details.Volumes == nil || len(*oi.Details.Volumes) != 1 {
		t.Errorf("known members lost: %+v", oi)
	}
	if err := strict(withExtra, new(OperationalIntent)); err == nil {
		t.Error("the strict decoder accepted the unknown member; the test proves nothing")
	}
}
