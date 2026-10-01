package zones

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/geodesy"
)

var (
	sinkR Result
	sinkZ []*Zone
	sinkB bool
)

// ring100 is a closed ring of 100 distinct vertices around c.
func ring100(c core.LatLon, radiusDeg float64) geodesy.Ring {
	const n = 100
	r := make(geodesy.Ring, 0, n+1)
	for i := range n {
		a := 2 * math.Pi * float64(i) / n
		r = append(r, ll(c.LatDeg+radiusDeg*math.Sin(a), c.LonDeg+radiusDeg*math.Cos(a)))
	}
	return append(r, r[0])
}

func benchZone100() *Zone {
	p := &geodesy.Polygon{Rings: []geodesy.Ring{ring100(tbilisi, 0.05)}}
	return &Zone{
		Identifier: "B", Type: core.ZoneProhibited, Restriction: ed269.RestrictionProhibited,
		Lower: limit(0, core.RefAGL), Upper: limit(120, core.RefAGL),
		Polygon: p, BBox: p.BBox(), Periods: []ed269.Period{{Permanent: true}},
	}
}

// BenchmarkJudgeZone is one zone-track judgement: horizontal containment
// in a 100-vertex polygon, then both AGL limits over known ground.
func BenchmarkJudgeZone(b *testing.B) {
	z := benchZone100()
	pt := ll(tbilisi.LatDeg+0.01, tbilisi.LonDeg+0.01)
	ac := geodetic(600)
	pol := DefaultPolicy()
	for b.Loop() {
		in, err := z.ContainsHorizontally(pt)
		if err != nil || !in {
			b.Fatal("not inside")
		}
		sinkR = JudgeVertical(z, ac, ground500, pol)
	}
	if sinkR.Raise == nil {
		b.Fatal("no raise")
	}
}

// BenchmarkJudgeZoneCircle is the same with a circle (Vincenty inverse).
func BenchmarkJudgeZoneCircle(b *testing.B) {
	z := circleZone(tbilisi, 5000)
	z.Upper = limit(120, core.RefAGL)
	pt := ll(tbilisi.LatDeg+0.01, tbilisi.LonDeg+0.01)
	ac := geodetic(600)
	pol := DefaultPolicy()
	for b.Loop() {
		in, err := z.ContainsHorizontally(pt)
		if err != nil || !in {
			b.Fatal("not inside")
		}
		sinkR = JudgeVertical(z, ac, ground500, pol)
	}
}

// zones300 is 300 zones of about 1 km to 11 km in a 3 x 3 degree area,
// 100-vertex polygons and circles alternating.
func zones300() []*Zone {
	zs := make([]*Zone, 0, 300)
	for i := range 300 {
		c := ll(40.5+float64(i%20)*0.15, 43.5+float64(i/20)*0.2)
		size := 0.01 + float64(i%7)*0.015
		if i%2 == 0 {
			p := &geodesy.Polygon{Rings: []geodesy.Ring{ring100(c, size)}}
			zs = append(zs, &Zone{Identifier: "P", Type: core.ZoneProhibited, Upper: limit(120, core.RefAGL), Polygon: p, BBox: p.BBox()})
		} else {
			z := circleZone(c, size*100_000)
			z.Upper = limit(120, core.RefAGL)
			zs = append(zs, z)
		}
	}
	return zs
}

// BenchmarkJudgeZoneCandidates300 is the Index lookup over 300 zones.
func BenchmarkJudgeZoneCandidates300(b *testing.B) {
	ix := NewIndex(zones300())
	pt := ll(41.42, 44.31)
	buf := make([]*Zone, 0, 16)
	for b.Loop() {
		buf = ix.AppendCandidates(buf[:0], pt)
	}
	sinkZ = buf
	if len(buf) == 0 {
		b.Fatal("no candidate")
	}
}

// BenchmarkJudgeZoneAll300 judges one position against 300 zones: index
// lookup, containment of each candidate, vertical judgement of each
// zone it is inside.
func BenchmarkJudgeZoneAll300(b *testing.B) {
	ix := NewIndex(zones300())
	pt := ll(41.42, 44.31)
	ac := geodetic(600)
	pol := DefaultPolicy()
	buf := make([]*Zone, 0, 16)
	inside := 0
	for b.Loop() {
		buf = ix.AppendCandidates(buf[:0], pt)
		inside = 0
		for _, z := range buf {
			in, err := z.ContainsHorizontally(pt)
			if err != nil || !in {
				continue
			}
			inside++
			sinkR = JudgeVertical(z, ac, ground500, pol)
		}
	}
	b.ReportMetric(float64(len(buf)), "candidates")
	b.ReportMetric(float64(inside), "inside")
}

// BenchmarkJudgeZoneLinear300 is the same without the index: every
// bounding box tested, for comparison.
func BenchmarkJudgeZoneLinear300(b *testing.B) {
	zs := zones300()
	pt := ll(41.42, 44.31)
	for b.Loop() {
		for _, z := range zs {
			in, err := z.ContainsHorizontally(pt)
			sinkB = in && err == nil
		}
	}
}

func BenchmarkJudgeHeight(b *testing.B) {
	ac := geodetic(650)
	pol := heightPolicy(120)
	for b.Loop() {
		sinkR = JudgeHeightLimit(ac, ground500, pol)
	}
	if sinkR.Raise == nil {
		b.Fatal("no raise")
	}
}
