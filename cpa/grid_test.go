package cpa

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

func tangentM(a, b core.LatLon) float64 {
	n, e := geodesy.LocalOffsetAboutMidLatM(a, b)
	return math.Hypot(n, e)
}

// scatter returns n positions uniformly within halfM metres north and
// east of centre.
func scatter(rng *rand.Rand, centre core.LatLon, halfM float64, n int) []core.LatLon {
	northPerDegM, eastPerDegM := metresPerDegree(centre.LatDeg)
	out := make([]core.LatLon, n)
	for i := range out {
		out[i] = core.LatLon{
			LatDeg: math.Max(-90, math.Min(90, centre.LatDeg+(rng.Float64()*2-1)*halfM/northPerDegM)),
			LonDeg: core.WrapLonDeg(centre.LonDeg + (rng.Float64()*2-1)*halfM/eastPerDegM),
		}
	}
	return out
}

// TestGridMatchesBruteForce is the C-15 property: 300 aircraft, 100
// queries, zero misses against an exhaustive search, for cells as wide as
// the radius, wider, much narrower (the scan fallback), and over the
// antimeridian and a pole.
func TestGridMatchesBruteForce(t *testing.T) {
	const radiusM = 800.0
	for _, tc := range []struct {
		name   string
		centre core.LatLon
		halfM  float64
		cellM  float64
	}{
		{"tbilisi-cell-1000", origin, 5000, 1000},
		{"tbilisi-cell-800", origin, 5000, 800},
		{"tbilisi-cell-300", origin, 5000, 300},
		{"tbilisi-cell-1", origin, 5000, 1},
		{"tbilisi-cell-50km", origin, 5000, 50_000},
		{"antimeridian", core.LatLon{LatDeg: -16.5, LonDeg: 180}, 4000, 1000},
		{"north-pole", core.LatLon{LatDeg: 89.98, LonDeg: 10}, 4000, 1000},
		{"equator", core.LatLon{}, 4000, 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := rand.New(rand.NewPCG(1, 2))
			pos := scatter(rng, tc.centre, tc.halfM, 300)
			g := NewGrid(tc.cellM)
			for i, p := range pos {
				if !g.Upsert(fmt.Sprint(i), p) {
					t.Fatalf("Upsert(%v) refused", p)
				}
			}
			found, misses := 0, 0
			for _, q := range scatter(rng, tc.centre, tc.halfM, 100) {
				near := g.Near(q, radiusM)
				for i, p := range pos {
					if tangentM(q, p) > radiusM {
						continue
					}
					found++
					if !slices.Contains(near, fmt.Sprint(i)) {
						misses++
						t.Errorf("query %v: missed %d at %v (%.1f m)", q, i, p, tangentM(q, p))
					}
				}
			}
			if found == 0 || misses != 0 {
				t.Fatalf("%d within the radius, %d missed", found, misses)
			}
			t.Logf("%d neighbours within %v m, none missed", found, radiusM)
		})
	}
}

func TestGridNearPresenceAndAbsence(t *testing.T) {
	g := NewGrid(1000)
	northPerDegM, eastPerDegM := metresPerDegree(origin.LatDeg)
	north5km := core.LatLon{LatDeg: origin.LatDeg + 5000/northPerDegM, LonDeg: origin.LonDeg}
	g.Upsert("here", origin)
	g.Upsert("700m-east", core.LatLon{LatDeg: origin.LatDeg, LonDeg: origin.LonDeg + 700/eastPerDegM})
	g.Upsert("5km-north", north5km)
	got := g.Near(origin, DefaultPolicy.NeighbourRadiusM)
	if !slices.Contains(got, "here") || !slices.Contains(got, "700m-east") {
		t.Fatalf("Near = %v, want here and 700m-east", got)
	}
	if slices.Contains(got, "5km-north") {
		t.Fatalf("Near = %v: 5 km away is outside the 3x3 ring", got)
	}
	if got := g.Near(north5km, 800); !slices.Equal(got, []string{"5km-north"}) {
		t.Fatalf("Near at 5 km north = %v, want [5km-north]", got)
	}
}

func TestGridAntimeridian(t *testing.T) {
	g := NewGrid(1000)
	g.Upsert("west", core.LatLon{LatDeg: 0, LonDeg: 179.999})
	g.Upsert("east", core.LatLon{LatDeg: 0, LonDeg: -179.999})
	if got := g.Near(core.LatLon{LatDeg: 0, LonDeg: 179.999}, 800); !slices.Contains(got, "east") {
		t.Fatalf("Near = %v, want east across the antimeridian (223 m)", got)
	}
}

func TestGridUpsertMovesAndRemoveFrees(t *testing.T) {
	g := NewGrid(1000)
	g.Upsert("a", origin)
	g.Upsert("b", origin)
	g.Upsert("c", origin)
	far := core.LatLon{LatDeg: origin.LatDeg + 1, LonDeg: origin.LonDeg}
	g.Upsert("a", far) // moved: swapped out of the shared cell
	if got := g.Near(origin, 800); len(got) != 2 || slices.Contains(got, "a") {
		t.Fatalf("Near(origin) = %v, want b and c", got)
	}
	if got := g.Near(far, 800); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("Near(far) = %v, want [a]", got)
	}
	g.Upsert("a", far) // same cell: no change
	g.Remove("b")
	g.Remove("b") // absent: no-op
	if got := g.Near(origin, 800); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("after Remove(b): %v, want [c]", got)
	}
	g.Remove("c")
	g.Remove("a")
	if g.Len() != 0 || len(g.cells) != 0 {
		t.Fatalf("Len %d, %d cells after removing everything, want 0, 0", g.Len(), len(g.cells))
	}
	if got := g.Near(origin, 800); got != nil {
		t.Fatalf("Near on an empty grid = %v", got)
	}
}

// TestGridBoundedByIDs is E-10: far more upserts than ids, across many
// cells, leave one entry per id and no empty cells.
func TestGridBoundedByIDs(t *testing.T) {
	g := NewGrid(100)
	rng := rand.New(rand.NewPCG(3, 4))
	pos := scatter(rng, origin, 20_000, 10_000)
	for i, p := range pos {
		g.Upsert(fmt.Sprint(i%7), p)
	}
	if g.Len() != 7 {
		t.Fatalf("Len = %d after 10000 upserts of 7 ids, want 7", g.Len())
	}
	entries := 0
	for _, ids := range g.cells {
		if len(ids) == 0 {
			t.Fatal("an empty cell was kept")
		}
		entries += len(ids)
	}
	if entries != 7 || len(g.cells) > 7 {
		t.Fatalf("%d entries in %d cells, want 7 in at most 7", entries, len(g.cells))
	}
	for id, s := range g.where {
		if g.cells[s.key][s.idx] != id {
			t.Fatalf("index of %s is stale", id)
		}
	}
}

func TestGridRefusesInvalidInput(t *testing.T) {
	g := NewGrid(1000)
	g.Upsert("a", origin)
	for _, p := range []core.LatLon{
		{LatDeg: math.NaN(), LonDeg: 0}, {LatDeg: math.Inf(1), LonDeg: 0},
		{LatDeg: 0, LonDeg: math.Inf(-1)}, {LatDeg: 91, LonDeg: 0}, {LatDeg: 0, LonDeg: 181},
	} {
		if g.Upsert("a", p) {
			t.Fatalf("Upsert(%v) accepted", p)
		}
		if g.Near(p, 800) != nil {
			t.Fatalf("Near(%v) answered", p)
		}
	}
	if got := g.Counters().Get(CounterGridRejectedPosition); got != 5 {
		t.Fatalf("%s = %d, want 5", CounterGridRejectedPosition, got)
	}
	// The refused upserts left the earlier position of a in place.
	if got := g.Near(origin, 800); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("Near(origin) = %v, want [a]", got)
	}
	for _, r := range []float64{math.NaN(), -1, math.Inf(1)} {
		if g.Near(origin, r) != nil {
			t.Fatalf("Near radius %v answered", r)
		}
	}
	if got := g.Counters().Get(CounterGridRejectedQuery); got != 8 {
		t.Fatalf("%s = %d, want 8", CounterGridRejectedQuery, got)
	}
	// A zero radius is valid: the query's own cell.
	if got := g.Near(origin, 0); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("Near radius 0 = %v, want [a]", got)
	}
}

func TestNewGridClampsCellSize(t *testing.T) {
	for _, tc := range []struct{ in, want float64 }{
		{math.NaN(), MinCellM}, {0, MinCellM}, {-5, MinCellM}, {0.5, MinCellM},
		{800, 800}, {math.Inf(1), MaxCellM}, {1e9, MaxCellM},
	} {
		g := NewGrid(tc.in)
		if g.CellM() != tc.want {
			t.Fatalf("NewGrid(%v).CellM() = %v, want %v", tc.in, g.CellM(), tc.want)
		}
		g.Upsert("a", origin)
		if got := g.Near(origin, 800); !slices.Equal(got, []string{"a"}) {
			t.Fatalf("NewGrid(%v): Near = %v, want [a]", tc.in, got)
		}
	}
}

func TestGridHugeRadiusFindsEverything(t *testing.T) {
	g := NewGrid(1000)
	pts := map[string]core.LatLon{
		"np": {LatDeg: 90, LonDeg: 0}, "sp": {LatDeg: -90, LonDeg: 0},
		"am": {LatDeg: 0, LonDeg: 180}, "o": origin,
	}
	for id, p := range pts {
		g.Upsert(id, p)
	}
	if got := g.Near(core.LatLon{}, 3e7); len(got) != len(pts) {
		t.Fatalf("Near with a radius past the antipode = %v, want all %d", got, len(pts))
	}
}

func TestAppendNearReusesBuffer(t *testing.T) {
	g := NewGrid(1000)
	g.Upsert("a", origin)
	buf := make([]string, 0, 8)
	got := g.AppendNear(buf, origin, 800)
	if !slices.Equal(got, []string{"a"}) || &got[0] != &buf[:1][0] {
		t.Fatalf("AppendNear = %v, want [a] in the given buffer", got)
	}
}
