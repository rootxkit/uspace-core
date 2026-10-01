package ed269

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/vectors"
)

// FuzzParse feeds Parse any bytes, seeded with every document of
// ed269_parse.json and its byte cases. Parse never panics; when it
// accepts, Export succeeds and Parse(Export(doc)) accepts and equals doc.
func FuzzParse(f *testing.F) {
	vf := vectors.Load(f, "ed269_parse.json")
	for _, c := range vf.Cases {
		var in parseInput
		if err := json.Unmarshal(c.Input, &in); err != nil {
			f.Fatalf("%s: %v", c.Name, err)
		}
		switch {
		case in.Document != nil:
			f.Add([]byte(in.Document))
		case in.Zone != nil:
			f.Add([]byte(`{"features":[` + string(in.Zone) + `]}`))
		case in.DocumentBOM64 != "":
			b, _ := base64.StdEncoding.DecodeString(in.DocumentBOM64)
			f.Add(b)
		case in.Bytes64 != "":
			b, _ := base64.StdEncoding.DecodeString(in.Bytes64)
			f.Add(b)
		case in.BytesDescription != "":
			f.Add(deepBrackets(100000))
		}
	}
	f.Add(deepBrackets(33))
	f.Fuzz(func(t *testing.T, data []byte) {
		lim := DefaultLimits
		lim.MaxBytes = 1 << 20
		doc, probs := Parse(data, lim)
		if (doc == nil) == (probs == nil) {
			t.Fatalf("doc %v and problems %v", doc, probs)
		}
		if probs != nil {
			if len(probs.List) == 0 || len(probs.List) > lim.MaxProblems {
				t.Fatalf("%d problems listed", len(probs.List))
			}
			for _, p := range probs.List {
				if p.Field == "" || p.Reason == "" {
					t.Fatalf("unnamed problem %+v", p)
				}
			}
			return
		}
		out, err := Export(doc)
		if err != nil {
			t.Fatalf("Export of an accepted document: %v", err)
		}
		again, probs := Parse(out, lim)
		if probs != nil {
			t.Fatalf("Parse(Export(doc)) refused: %v\n%s", probs, out)
		}
		out2, err := Export(again)
		if err != nil || !bytes.Equal(out, out2) {
			t.Fatalf("round trip differs (%v)\n%s\n%s", err, out, out2)
		}
		if !reflect.DeepEqual(canonicalNoFail(out), canonicalNoFail(bytes.TrimPrefix(data, utf8BOM))) {
			t.Fatalf("Export differs from the input by value\n%s\n%s", out, data)
		}
		for i := range doc.Zones {
			Applies(doc.Zones[i].Applicability, time.Unix(0, 0))
		}
	})
}

// canonicalNoFail is canonical for the fuzz property, without null
// members; numbers compare by value and undecodable input yields nil.
func canonicalNoFail(raw []byte) any {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	return withoutNulls(byValue(v))
}

// withoutNulls drops null object members: Parse reads a null optional
// field as absent and Export leaves it out (LESSONS Z-01).
func withoutNulls(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			if e == nil {
				delete(t, k)
				continue
			}
			t[k] = withoutNulls(e)
		}
	case []any:
		for i, e := range t {
			t[i] = withoutNulls(e)
		}
	}
	return v
}

// FuzzParseApplicability checks that any applicability text either is
// refused with named problems or evaluates without panicking.
func FuzzParseApplicability(f *testing.F) {
	vf := vectors.Load(f, "zones_applicability.json")
	for _, c := range vf.Cases {
		var in struct {
			Applicability json.RawMessage `json:"applicability"`
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			f.Fatalf("%s: %v", c.Name, err)
		}
		f.Add([]byte(in.Applicability), int64(1_791_000_000))
	}
	f.Fuzz(func(_ *testing.T, data []byte, unixS int64) {
		ps, probs := ParseApplicability(data)
		if probs != nil {
			return
		}
		Applies(ps, time.Unix(unixS%(1<<40), 0))
	})
}

// zoneJSON is a zone with a ring of n vertices.
func zoneJSON(id string, n int) string {
	var b strings.Builder
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		fmt.Fprintf(&b, "[%.6f,%.6f],", 6.1+0.05*math.Cos(a), 49.6+0.03*math.Sin(a))
	}
	fmt.Fprintf(&b, "[%.6f,%.6f]", 6.1+0.05, 49.6)
	return strings.Replace(strings.Replace(baseFeature, `"TST001"`, `"`+id+`"`, 1),
		`[[[44.8,41.7],[44.82,41.7],[44.82,41.72],[44.8,41.72],[44.8,41.7]],[[44.805,41.705],[44.815,41.705],[44.815,41.715],[44.805,41.715],[44.805,41.705]]]`,
		"[["+b.String()+"]]", 1)
}

// BenchmarkParseDocument parses a Luxembourg-sized document: one zone of
// 1400 vertices (Luxembourg's largest) and 50 small ones.
func BenchmarkParseDocument(b *testing.B) {
	zones := []string{zoneJSON("BIG0001", 1400)}
	for i := range 50 {
		zones = append(zones, zoneJSON(fmt.Sprintf("SML%04d", i), 12))
	}
	data := []byte(`{"title":"bench","features":[` + strings.Join(zones, ",") + `]}`)
	if _, probs := Parse(data, DefaultLimits); probs != nil {
		b.Fatalf("refused: %v", probs)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, probs := Parse(data, DefaultLimits); probs != nil {
			b.Fatal(probs)
		}
	}
}

// BenchmarkApplies evaluates ten periods with schedules, none of which
// applies, so every one is tried.
func BenchmarkApplies(b *testing.B) {
	var periods []string
	for i := range 10 {
		periods = append(periods, fmt.Sprintf(
			`{"permanent":"NO","startDateTime":"2026-01-01T00:00:00Z","endDateTime":"2027-01-01T00:00:00+04:00","schedule":[{"day":["MON","WED","FRI"],"startTime":"%02d:00+04:00","endTime":"%02d:30+04:00"},{"day":["SAT"],"startTime":"22:00Z","endTime":"02:00Z"}]}`,
			i, i))
	}
	ps, probs := ParseApplicability(json.RawMessage("[" + strings.Join(periods, ",") + "]"))
	if probs != nil {
		b.Fatalf("refused: %v", probs)
	}
	at := time.Date(2026, 10, 6, 15, 0, 0, 0, time.UTC) // a Tuesday afternoon
	if Applies(ps, at) {
		b.Fatal("applies")
	}
	b.ReportAllocs()
	for b.Loop() {
		Applies(ps, at)
	}
}
