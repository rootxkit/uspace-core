package ed269

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/vectors"
)

// tolMetres mirrors the file's `tolerance.metres` header.
const tolMetres = 1e-9

type parseInput struct {
	Document         json.RawMessage `json:"document"`
	Zone             json.RawMessage `json:"zone"`
	DocumentBOM64    string          `json:"document_utf8_with_bom_base64"`
	Bytes64          string          `json:"bytes_base64"`
	BytesDescription string          `json:"bytes_description"`
}

type zoneSummary struct {
	Identifier     string   `json:"identifier"`
	Restriction    string   `json:"restriction"`
	LowerM         *float64 `json:"lower_m"`
	LowerReference string   `json:"lower_reference"`
	UpperM         *float64 `json:"upper_m"`
	UpperReference string   `json:"upper_reference"`
	Shape          string   `json:"shape"`
	RadiusM        *float64 `json:"radius_m"`
	Periods        int      `json:"periods"`
}

type mustInclude struct {
	Field          string `json:"field"`
	FieldEndsWith  string `json:"field_endswith"`
	ReasonContains string `json:"reason_contains"`
}

type parseExpected struct {
	Accepted    bool            `json:"accepted"`
	Zones       []zoneSummary   `json:"zones"`
	Export      json.RawMessage `json:"export"`
	Problems    []Problem       `json:"problems"`
	MustInclude *mustInclude    `json:"must_include"`
}

// parseInputBytes returns the raw bytes a case feeds to Parse, or the raw
// zone for ParseZone.
func parseInputBytes(t *testing.T, in parseInput) (data []byte, zone bool) {
	t.Helper()
	switch {
	case in.Document != nil:
		return in.Document, false
	case in.Zone != nil:
		return in.Zone, true
	case in.DocumentBOM64 != "":
		b, err := base64.StdEncoding.DecodeString(in.DocumentBOM64)
		if err != nil {
			t.Fatalf("base64: %v", err)
		}
		if !bytes.HasPrefix(b, utf8BOM) {
			t.Fatalf("document_utf8_with_bom_base64 does not start with a BOM")
		}
		return b, false
	case in.Bytes64 != "":
		b, err := base64.StdEncoding.DecodeString(in.Bytes64)
		if err != nil {
			t.Fatalf("base64: %v", err)
		}
		return b, false
	case in.BytesDescription == "100000 '[' then 100000 ']'":
		return deepBrackets(100000), false
	}
	t.Fatalf("unknown input shape")
	return nil, false
}

func deepBrackets(n int) []byte {
	return append(bytes.Repeat([]byte("["), n), bytes.Repeat([]byte("]"), n)...)
}

var listDiffs atomic.Int32

func TestVectorsED269Parse(t *testing.T) {
	f := vectors.Load(t, "ed269_parse.json")
	var header struct {
		IdentifierMax   int `json:"identifier_max"`
		NameMax         int `json:"name_max"`
		MessageMax      int `json:"message_max"`
		ReasonsMax      int `json:"reasons_max"`
		MaxRingVertices int `json:"max_ring_vertices"`
		MaxProblems     int `json:"max_problems"`
	}
	f.Header(t, "limits", &header)
	d := DefaultLimits
	got := [6]int{d.IdentifierMax, d.NameMax, d.MessageMax, d.ReasonsMax, d.MaxRingVertices, d.MaxProblems}
	want := [6]int{header.IdentifierMax, header.NameMax, header.MessageMax, header.ReasonsMax, header.MaxRingVertices, header.MaxProblems}
	if got != want {
		t.Fatalf("DefaultLimits %v, header limits %v", got, want)
	}
	if tol, ok := f.FloatTolerance("metres"); !ok || tol != tolMetres {
		t.Fatalf("tolerance metres is %v (%v), the test uses %v", tol, ok, tolMetres)
	}
	listDiffs.Store(0)
	t.Cleanup(func() {
		t.Logf("cases whose full problem list differs from the vector: %d", listDiffs.Load())
	})

	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in parseInput
		var exp parseExpected
		c.Decode(t, &in, &exp)
		data, isZone := parseInputBytes(t, in)

		var doc *Document
		var probs *Problems
		if isZone {
			var z *GeoZone
			z, probs = ParseZone(data, DefaultLimits)
			if z != nil {
				doc = &Document{Zones: []GeoZone{*z}}
			}
		} else {
			doc, probs = Parse(data, DefaultLimits)
		}

		if !exp.Accepted {
			if probs == nil || doc != nil {
				t.Fatalf("accepted; want refused with %+v", exp.Problems)
			}
			checkProblems(t, exp, probs)
			return
		}
		if probs != nil {
			t.Fatalf("refused: %v", probs)
		}
		if exp.Zones != nil {
			checkSummaries(t, exp.Zones, doc.Zones)
		}
		if exp.Export != nil {
			var out []byte
			if isZone {
				m, err := Feature(&doc.Zones[0])
				if err != nil {
					t.Fatalf("Feature: %v", err)
				}
				out, err = json.Marshal(m)
				if err != nil {
					t.Fatalf("marshal: %v", err)
				}
			} else {
				var err error
				out, err = Export(doc)
				if err != nil {
					t.Fatalf("Export: %v", err)
				}
			}
			if g, w := canonical(t, out), canonical(t, exp.Export); !reflect.DeepEqual(g, w) {
				t.Errorf("export differs\n got: %s\nwant: %s", out, exp.Export)
			}
		}
	})
}

func checkSummaries(t *testing.T, want []zoneSummary, zones []GeoZone) {
	t.Helper()
	if len(zones) != len(want) {
		t.Fatalf("%d zones, want %d", len(zones), len(want))
	}
	for i, w := range want {
		z := zones[i]
		vol, ok := z.Volume()
		if !ok {
			t.Fatalf("zone %d: not one volume", i)
		}
		at := func(s string) string { return fmt.Sprintf("zones[%d].%s", i, s) }
		if z.Identifier != w.Identifier || string(z.Restriction) != w.Restriction {
			t.Errorf("%s: %s %s, want %s %s", at("identifier"), z.Identifier, z.Restriction, w.Identifier, w.Restriction)
		}
		if string(vol.LowerRef) != w.LowerReference || string(vol.UpperRef) != w.UpperReference {
			t.Errorf("%s: %s/%s, want %s/%s", at("references"), vol.LowerRef, vol.UpperRef, w.LowerReference, w.UpperReference)
		}
		if vol.Projection.Type != w.Shape {
			t.Errorf("%s: %s, want %s", at("shape"), vol.Projection.Type, w.Shape)
		}
		if len(z.Applicability) != w.Periods {
			t.Errorf("%s: %d, want %d", at("periods"), len(z.Applicability), w.Periods)
		}
		vectors.NearPtr(t, at("lower_m"), vol.LowerM(), w.LowerM, tolMetres)
		vectors.NearPtr(t, at("upper_m"), vol.UpperM(), w.UpperM, tolMetres)
		vectors.NearPtr(t, at("radius_m"), vol.RadiusM(), w.RadiusM, tolMetres)
	}
}

// checkProblems makes must_include binding and logs any difference in the
// full list (docs/PLAN.md section 11 gap 6). A case without must_include
// pins its listed problems exactly; they are its point (every problem
// reported; a repeated identifier names both places).
func checkProblems(t *testing.T, exp parseExpected, probs *Problems) {
	t.Helper()
	if m := exp.MustInclude; m != nil {
		found := false
		for _, p := range probs.List {
			fieldOK := (m.Field != "" && p.Field == m.Field) ||
				(m.Field == "" && strings.HasSuffix(p.Field, m.FieldEndsWith))
			if fieldOK && strings.Contains(p.Reason, m.ReasonContains) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no problem matches must_include %+v; got %+v", *m, probs.List)
		}
	} else {
		for _, w := range exp.Problems {
			found := false
			for _, p := range probs.List {
				if p == w {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("missing problem %+v; got %+v", w, probs.List)
			}
		}
	}
	missing, extra := diffProblems(exp.Problems, probs.List)
	if len(missing) > 0 || len(extra) > 0 {
		listDiffs.Add(1)
		t.Logf("full problem list differs (not binding, plan section 11 gap 6):\n only in vector: %+v\n only here:      %+v", missing, extra)
	}
}

func diffProblems(want, got []Problem) (missing, extra []Problem) {
	in := func(list []Problem, p Problem) bool {
		for _, q := range list {
			if q == p {
				return true
			}
		}
		return false
	}
	for _, w := range want {
		if !in(got, w) {
			missing = append(missing, w)
		}
	}
	for _, g := range got {
		if !in(want, g) {
			extra = append(extra, g)
		}
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Field < missing[j].Field })
	sort.Slice(extra, func(i, j int) bool { return extra[i].Field < extra[j].Field })
	return missing, extra
}

func TestVectorsZonesApplicability(t *testing.T) {
	f := vectors.Load(t, "zones_applicability.json")
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in struct {
			Applicability json.RawMessage `json:"applicability"`
			At            string          `json:"at"`
		}
		var exp struct {
			Applies bool `json:"applies"`
		}
		c.Decode(t, &in, &exp)
		periods, probs := ParseApplicability(in.Applicability)
		if probs != nil {
			t.Fatalf("refused: %v", probs)
		}
		at, err := time.Parse(time.RFC3339Nano, in.At)
		if err != nil {
			t.Fatalf("at: %v", err)
		}
		if got := Applies(periods, at); got != exp.Applies {
			t.Errorf("Applies(%s) = %v, want %v", in.At, got, exp.Applies)
		}
	})
}
