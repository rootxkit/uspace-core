package zones

import (
	"errors"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// polyZone is a square polygon zone of half-size h degrees around c.
func polyZone(id string, c core.LatLon, h float64) *Zone {
	p := &geodesy.Polygon{Rings: []geodesy.Ring{square(c, h)}}
	return &Zone{Identifier: id, Type: core.ZoneProhibited, Polygon: p, BBox: p.BBox()}
}

func ids(zs []*Zone) []string {
	out := make([]string, len(zs))
	for i, z := range zs {
		out[i] = z.Identifier
	}
	return out
}

// testZones covers the cases the grid must get right: overlapping zones,
// a zone across the antimeridian, a circle at the antimeridian, a zone
// near the pole, a zone too large for the grid and one on a cell edge.
func testZones() []*Zone {
	anti := &geodesy.Polygon{Rings: []geodesy.Ring{{
		ll(-17, 179.5), ll(-17, -179.5), ll(-16, -179.5), ll(-16, 179.5), ll(-17, 179.5),
	}}}
	big := &geodesy.Polygon{Rings: []geodesy.Ring{{
		ll(30, 40), ll(30, 50), ll(45, 50), ll(45, 40), ll(30, 40),
	}}}
	c1 := circleZone(ll(-16.5, 180), 20_000)
	c1.Identifier = "circle-anti"
	c2 := circleZone(ll(89.9, 10), 30_000)
	c2.Identifier = "circle-pole"
	return []*Zone{
		polyZone("a", tbilisi, 0.01),
		polyZone("b", tbilisi, 0.05),
		{Identifier: "anti", Polygon: anti, BBox: anti.BBox()},
		c1,
		c2,
		{Identifier: "big", Polygon: big, BBox: big.BBox()},
		polyZone("edge", ll(10.05, 20.05), 0.05),
	}
}

func TestIndexHitAndMiss(t *testing.T) {
	ix := NewIndex(testZones())
	if ix.Len() != 7 {
		t.Fatalf("Len %d", ix.Len())
	}
	cases := []struct {
		name string
		p    core.LatLon
		want []string
	}{
		{"Tbilisi: both nested squares and the big zone, in input order", tbilisi, []string{"a", "b", "big"}},
		{"outside the small square", ll(tbilisi.LatDeg+0.03, tbilisi.LonDeg), []string{"b", "big"}},
		{"far from everything", ll(0, 0), nil},
		{"east of the antimeridian", ll(-16.5, -179.9), []string{"anti", "circle-anti"}},
		{"west of the antimeridian", ll(-16.5, 179.9), []string{"anti", "circle-anti"}},
		{"on it, written 180", ll(-16.5, 180), []string{"anti", "circle-anti"}},
		{"on it, written -180", ll(-16.5, -180), []string{"anti", "circle-anti"}},
		{"near the pole", ll(89.95, 100), []string{"circle-pole"}},
		{"on a cell edge", ll(10.1, 20.1), []string{"edge"}},
		{"just past the cell edge", ll(10.1000001, 20.1), nil},
	}
	for _, tc := range cases {
		if got := ids(ix.Candidates(tc.p)); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestIndexRefusesInvalidPoints(t *testing.T) {
	ix := NewIndex(testZones())
	for _, p := range []core.LatLon{ll(math.NaN(), 0), ll(0, math.Inf(1)), ll(math.Inf(-1), 0), ll(90.5, 0), ll(0, -180.5)} {
		if got := ix.Candidates(p); got != nil {
			t.Errorf("%+v: got %v, want nil", p, ids(got))
		}
	}
	// The presence pair: a valid point finds its zone.
	if got := ix.Candidates(tbilisi); len(got) == 0 {
		t.Errorf("valid point found nothing")
	}
	var nilIndex *Index
	if got := nilIndex.Candidates(tbilisi); got != nil {
		t.Errorf("nil index: %v", got)
	}
}

func TestIndexSkipsZonesWithoutABox(t *testing.T) {
	empty := &Zone{Identifier: "empty", BBox: (&geodesy.Polygon{}).BBox()}
	nan := &Zone{Identifier: "nan", BBox: geodesy.BBox{MinLat: math.NaN(), MaxLat: 1, MinLon: 0, MaxLon: 1}}
	ix := NewIndex([]*Zone{nil, empty, nan, polyZone("a", tbilisi, 0.01)})
	if ix.Len() != 1 {
		t.Fatalf("Len %d, want 1", ix.Len())
	}
	if got := ids(ix.Candidates(tbilisi)); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("got %v", got)
	}
}

// E-10: a zone past MaxCellsPerZone goes to the large list, and is still
// found; one just under it goes into the grid.
func TestIndexLargeZoneBound(t *testing.T) {
	lim := DefaultIndexLimits()
	if lim.MaxCellsPerZone != 1_000 || lim.MaxEntries != 250_000 {
		t.Fatalf("DefaultIndexLimits %+v", lim)
	}
	under := polyZone("under", ll(5.15, 5.15), 1.54) // 31 x 31 = 961 cells
	over := polyZone("over", ll(-30, -60), 1.65)     // 34 x 34 cells
	globe := &Zone{Identifier: "globe", BBox: geodesy.BBox{MinLat: -90, MinLon: -180, MaxLat: 90, MaxLon: 180}}
	ix := NewIndex([]*Zone{under, over, globe})
	if !slices.Equal(ix.large, []int32{1, 2}) || ix.Spilled() != 2 || ix.Entries() != 961 {
		t.Fatalf("large %v spilled %d entries %d, want [1 2], 2, 961", ix.large, ix.Spilled(), ix.Entries())
	}
	if got := ids(ix.Candidates(ll(-30, -60))); !slices.Equal(got, []string{"over", "globe"}) {
		t.Errorf("over: got %v", got)
	}
	if got := ids(ix.Candidates(ll(5, 5))); !slices.Equal(got, []string{"under", "globe"}) {
		t.Errorf("under: got %v", got)
	}
	if got := ids(ix.Candidates(ll(-89, 170))); !slices.Equal(got, []string{"globe"}) {
		t.Errorf("globe: got %v", got)
	}
}

// E-10: past the total MaxEntries every further zone spills to the list,
// and every zone is still found.
func TestIndexTotalEntriesBound(t *testing.T) {
	var zs []*Zone
	for i := range 5 {
		// 0.45 deg boxes away from cell edges: 5 x 5 = 25 cells each.
		zs = append(zs, polyZone(string(rune('a'+i)), ll(10.25, 10.25+float64(i)), 0.2))
	}
	ix := NewIndexLimits(zs, IndexLimits{MaxEntries: 60})
	if ix.Entries() != 50 || ix.Spilled() != 3 || !slices.Equal(ix.large, []int32{2, 3, 4}) {
		t.Fatalf("entries %d spilled %d large %v, want 50, 3, [2 3 4]", ix.Entries(), ix.Spilled(), ix.large)
	}
	for i, z := range zs {
		if got := ix.Candidates(ll(10.25, 10.25+float64(i))); !slices.Equal(got, []*Zone{z}) {
			t.Errorf("zone %s: got %v", z.Identifier, ids(got))
		}
	}
	// The presence pair: with the default ceiling nothing spills.
	if d := NewIndexLimits(zs, IndexLimits{}); d.Spilled() != 0 || d.Entries() != 125 {
		t.Fatalf("defaults: spilled %d entries %d", d.Spilled(), d.Entries())
	}
}

// Large zones and grid zones interleave in input order.
func TestIndexKeepsInputOrderAcrossLists(t *testing.T) {
	zs := []*Zone{
		{Identifier: "g0", BBox: geodesy.BBox{MinLat: -90, MinLon: -180, MaxLat: 90, MaxLon: 180}},
		polyZone("p1", tbilisi, 0.01),
		{Identifier: "g2", BBox: geodesy.BBox{MinLat: -90, MinLon: -180, MaxLat: 90, MaxLon: 180}},
		polyZone("p3", tbilisi, 0.02),
		{Identifier: "g4", BBox: geodesy.BBox{MinLat: -90, MinLon: -180, MaxLat: 90, MaxLon: 180}},
	}
	got := ids(NewIndex(zs).Candidates(tbilisi))
	if want := []string{"g0", "p1", "g2", "p3", "g4"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppendCandidatesReusesTheSlice(t *testing.T) {
	ix := NewIndex(testZones())
	buf := make([]*Zone, 0, 8)
	got := ix.AppendCandidates(buf[:0], tbilisi)
	if len(got) != 3 || &got[0] != &buf[:1][0] {
		t.Fatalf("did not append into the given slice: %v", ids(got))
	}
}

// shapeContains is the oracle for the index tests: the shape itself, with
// no bounding box in the way (ContainsHorizontally prefilters with the
// box, so comparing against it would be circular). -180 and 180 are one
// meridian for the polygon too.
func shapeContains(t testing.TB, z *Zone, p core.LatLon) bool {
	t.Helper()
	switch {
	case z.Polygon != nil:
		if z.Polygon.Contains(p) {
			return true
		}
		return math.Abs(p.LonDeg) == 180 && z.Polygon.Contains(core.LatLon{LatDeg: p.LatDeg, LonDeg: -p.LonDeg})
	case z.Circle != nil:
		in, _, err := z.Circle.Contains(p)
		if errors.Is(err, geodesy.ErrNoConvergence) {
			// Vincenty fails only for nearly antipodal points, half the
			// Earth away from any circle the tests build.
			return false
		}
		if err != nil && p.Valid() {
			t.Fatalf("%s: circle at %+v: %v", z.Identifier, p, err)
		}
		return in
	}
	t.Fatalf("%s has no shape", z.Identifier)
	return false
}

// Completeness: every zone that contains a point is among its candidates.
// A miss here would be a missed zone, so it is checked over random points
// concentrated around the zones.
func TestIndexNeverMissesAContainingZone(t *testing.T) {
	zs := testZones()
	ix := NewIndex(zs)
	rng := rand.New(rand.NewPCG(1, 2))
	centres := []core.LatLon{tbilisi, ll(-16.5, 180), ll(89.9, 10), ll(37, 45), ll(10.05, 20.05)}
	hits := 0
	for i := range 20000 {
		c := centres[i%len(centres)]
		p := ll(c.LatDeg+(rng.Float64()-0.5)*1.5, core.WrapLonDeg(c.LonDeg+(rng.Float64()-0.5)*3))
		if !p.Valid() {
			continue
		}
		cands := ix.Candidates(p)
		for _, z := range zs {
			in := shapeContains(t, z, p)
			// The bounding-box prefilter of ContainsHorizontally never
			// loses a point the shape holds either.
			if got, err := z.ContainsHorizontally(p); err != nil || got != in {
				t.Fatalf("%s at %+v: ContainsHorizontally %v %v, shape says %v", z.Identifier, p, got, err, in)
			}
			if in {
				hits++
				if !slices.Contains(cands, z) {
					t.Fatalf("%s contains %+v but is not a candidate", z.Identifier, p)
				}
			}
		}
	}
	if hits < 1000 {
		t.Fatalf("only %d containing pairs: the test is not exercising presence", hits)
	}
}
