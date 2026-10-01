package ed318

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/geodesy"
	"github.com/rootxkit/uspace-core/vectors"
)

// FuzzParseED318: any bytes are accepted or refused with problems, never a
// panic or an unbounded allocation (the input is capped by MaxBytes and
// the nesting by MaxDepth). An accepted collection exports, the export is
// accepted, and reads back to the same collection; ToZones, ToED269 and
// Applies on it never panic. Seeded with every document of
// ed318_roundtrip.json, the published examples and the ED-269 fixture
// mapped to ED-318.
func FuzzParseED318(f *testing.F) {
	vf := vectors.Load(f, "ed318_roundtrip.json")
	for _, c := range vf.Cases {
		var in struct {
			Document json.RawMessage `json:"document"`
		}
		if err := json.Unmarshal(c.Input, &in); err == nil && in.Document != nil {
			f.Add([]byte(in.Document))
		}
	}
	entries, err := os.ReadDir(filepath.Join("testdata", "published"))
	if err != nil {
		f.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			f.Add(readPublished(f, e.Name()))
		}
	}
	ef := vectors.Load(f, "ed269_parse.json")
	for _, c := range ef.Cases {
		var in struct {
			Document json.RawMessage `json:"document"`
		}
		if json.Unmarshal(c.Input, &in) != nil || in.Document == nil {
			continue
		}
		doc, probs := ed269.Parse(in.Document, ed269.Limits{})
		if probs != nil {
			continue
		}
		for i := range doc.Zones {
			one := *doc
			one.Zones = doc.Zones[i : i+1]
			if fc, err := FromED269(&one, Metadata{}, "en-GB"); err == nil {
				if raw, err := Export(fc); err == nil {
					f.Add(raw)
				}
			}
		}
	}
	f.Add(baseBytes(f))
	f.Add([]byte(`{"type":"FeatureCollection","features":[{"type":"Feature","geometry":{"type":"Point","coordinates":[0,0],"extent":{"subType":"Circle","radius":1}},"properties":{}}]}`))
	at := time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, data []byte) {
		fc, probs := Parse(data, Limits{})
		if fc == nil {
			if probs == nil || len(probs.List) == 0 {
				t.Fatal("refused without a problem")
			}
			return
		}
		if probs != nil {
			t.Fatal("accepted with problems")
		}
		out, err := Export(fc)
		if err != nil {
			t.Fatalf("an accepted collection does not export: %v", err)
		}
		again, probs := Parse(out, Limits{})
		if probs != nil {
			t.Fatalf("the export is refused: %v\n%s", probs, out)
		}
		if !reflect.DeepEqual(again, fc) {
			t.Fatalf("Parse(Export(c)) differs from c\n%s", out)
		}
		_, _ = ToZones(fc, FixedDaylight{})
		_, _ = ToED269(fc, "")
		for i := range fc.Features {
			_, _ = Applies(fc.Features[i].Properties.LimitedApplicability, at, center(boxOf(fc.Features[i].Geometry)), NOAADaylight{})
		}
	})
}

// boxOf is a rough centre box of a geometry for the fuzz target's
// Applies call.
func boxOf(g Geometry) geodesy.BBox {
	b := geodesy.BBox{MinLat: math.Inf(1), MaxLat: math.Inf(-1), MinLon: math.Inf(1), MaxLon: math.Inf(-1)}
	add := func(lat, lon float64) {
		b.MinLat, b.MaxLat = math.Min(b.MinLat, lat), math.Max(b.MaxLat, lat)
		b.MinLon, b.MaxLon = math.Min(b.MinLon, lon), math.Max(b.MaxLon, lon)
	}
	parts := g.parts()
	for i := range parts {
		p := &parts[i]
		if p.Center != nil {
			add(p.Center.LatDeg, p.Center.LonDeg)
		}
		for _, r := range p.Rings {
			for _, pt := range r {
				add(pt.LatDeg, pt.LonDeg)
			}
		}
	}
	return b
}

// zoneJSON is one ED-318 zone feature with a ring of n positions.
func zoneJSON(id string, n int) string {
	var pts []string
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		pts = append(pts, fmt.Sprintf("[%.6f,%.6f]", 44.8+0.05*math.Cos(a), 41.7+0.05*math.Sin(a)))
	}
	pts = append(pts, pts[0])
	return `{"type":"Feature","geometry":{"type":"Polygon","coordinates":[[` + strings.Join(pts, ",") +
		`]],"layer":{"upper":120,"upperReference":"AGL","lower":0,"lowerReference":"AGL","uom":"m"}},` +
		`"properties":{"identifier":"` + id + `","country":"GEO","name":[{"text":"Benchmark zone","lang":"en-GB"}],` +
		`"type":"PROHIBITED","variant":"COMMON","reason":["SENSITIVE"],` +
		`"limitedApplicability":[{"startDateTime":"2026-01-01T00:00:00Z","endDateTime":"2027-01-01T00:00:00+04:00",` +
		`"schedule":[{"day":["MON","TUE"],"startTime":"08:00:00+04:00","endTime":"18:00:00+04:00"}]}],` +
		`"zoneAuthority":[{"name":[{"text":"Test authority","lang":"en-GB"}],"purpose":"AUTHORIZATION"}]}}`
}

// BenchmarkParseED318 parses the shape of ed269's BenchmarkParseDocument:
// one zone of 1,400 vertices (Luxembourg's largest) and 50 of 12, the
// control-plane cost of one publication (docs/bench-targets.txt).
func BenchmarkParseED318(b *testing.B) {
	zones := []string{zoneJSON("BIG0001", 1400)}
	for i := range 50 {
		zones = append(zones, zoneJSON(fmt.Sprintf("SML%04d", i), 12))
	}
	data := []byte(`{"type":"FeatureCollection","name":"bench","features":[` + strings.Join(zones, ",") + `]}`)
	if _, probs := Parse(data, Limits{}); probs != nil {
		b.Fatalf("refused: %v", probs)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, probs := Parse(data, Limits{}); probs != nil {
			b.Fatal(probs)
		}
	}
}
