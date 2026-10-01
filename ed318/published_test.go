package ed318

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/ed269"
)

// The published ED-318 files in testdata/published:
//
//   - Example_*.json and InvalidExample_GeoZone_2_Layers.json are the
//     examples of the ED-318 schema repository (UASGeoZones/ED-318,
//     examples/, commit e98b292, MIT licence in LICENSE-ED-318), the files
//     its validate_examples.py checks against the schema;
//   - che_cis_source_sample_ed318.json is the Swiss sample InterUSS
//     monitoring validates against that schema
//     (monitoring/uss_qualifier/test_data/che/geoawareness/, commit
//     119dda5).
var publishedValid = []string{
	"Example_Collection.json",
	"Example_GeoZone_Circle.json",
	"Example_GeoZone_2_Layers.json",
	"Example_GeoZone_with_extension.json",
	"che_cis_source_sample_ed318.json",
}

func readPublished(t testing.TB, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "published", name))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// plain decodes JSON for a by-value comparison.
func plain(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%v\n%s", err, raw)
	}
	return v
}

// sameJSON compares two JSON texts by value.
func sameJSON(t *testing.T, got, want []byte) bool {
	t.Helper()
	return reflect.DeepEqual(plain(t, got), plain(t, want))
}

// Every published valid file is accepted and Export(Parse(f)) equals f by
// value, members this package does not interpret included.
func TestPublishedExamplesRoundTrip(t *testing.T) {
	for _, name := range publishedValid {
		t.Run(name, func(t *testing.T) {
			raw := readPublished(t, name)
			fc, probs := Parse(raw, Limits{})
			if probs != nil {
				t.Fatalf("refused: %v", probs)
			}
			out, err := Export(fc)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := plain(t, out), plain(t, raw); !reflect.DeepEqual(got, want) {
				t.Errorf("export differs from the file:\n got %s", out)
			}
			again, probs := Parse(out, Limits{})
			if probs != nil {
				t.Fatalf("the export is refused: %v", probs)
			}
			if !reflect.DeepEqual(again, fc) {
				t.Error("Parse(Export(Parse(f))) differs from Parse(f)")
			}
		})
	}
}

// The schema repository's invalid example is refused, each of its
// deliberate defects named.
func TestPublishedInvalidExampleRefused(t *testing.T) {
	fc, probs := Parse(readPublished(t, "InvalidExample_GeoZone_2_Layers.json"), Limits{})
	if fc != nil {
		t.Fatal("the invalid example was accepted")
	}
	for _, want := range []struct{ field, reason string }{
		{"metadata.validFrom", "must be an RFC 3339 date-time"},
		{"features[0].properties.name[0].lang", "missing"},
		{"features[0].properties.type", "ED-318 spells it REQ_AUTHORIZATION"},
	} {
		if !hasProblem(probs, want.field, want.reason) {
			t.Errorf("no problem %s: %s in %v", want.field, want.reason, probs)
		}
	}
	if len(probs.List) != 3 {
		t.Errorf("%d problems, the example has 3 defects: %v", len(probs.List), probs)
	}
}

func hasProblem(probs *ed269.Problems, field, reason string) bool {
	if probs == nil {
		return false
	}
	for _, p := range probs.List {
		if p.Field == field && strings.Contains(p.Reason, reason) {
			return true
		}
	}
	return false
}
