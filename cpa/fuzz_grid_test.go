package cpa

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// FuzzGridNear compares Near with a brute-force search: 60 aircraft
// scattered around an arbitrary centre (the poles and the antimeridian
// included), an arbitrary cell size (NaN, zero and Inf are clamped) and
// radius. Every aircraft within the radius by the tangent-plane distance
// must be returned; nothing may panic; an invalid query returns nil.
func FuzzGridNear(f *testing.F) {
	f.Add(uint64(1), 41.7151, 44.8271, 5000.0, 1000.0, 800.0)
	f.Add(uint64(2), -16.5, 180.0, 4000.0, 1000.0, 800.0)
	f.Add(uint64(3), 89.98, 10.0, 4000.0, 800.0, 800.0)
	f.Add(uint64(4), -89.999, -179.9, 20000.0, 1.0, 3000.0)
	f.Add(uint64(5), 0.0, 0.0, 1000.0, math.NaN(), 0.0)
	f.Add(uint64(6), 45.0, -179.99, 30000.0, math.Inf(1), 20000.0)
	f.Fuzz(func(t *testing.T, seed uint64, latDeg, lonDeg, spreadM, cellM, radiusM float64) {
		if !core.IsFinite(latDeg) || !core.IsFinite(lonDeg) || !core.IsFinite(spreadM) || !core.IsFinite(radiusM) {
			if g := NewGrid(cellM); g.Near(core.LatLon{LatDeg: latDeg, LonDeg: lonDeg}, radiusM) != nil {
				t.Fatal("an empty grid answered")
			}
			return
		}
		centre := core.LatLon{LatDeg: math.Mod(latDeg, 90), LonDeg: core.WrapLonDeg(lonDeg)}
		spreadM = math.Abs(math.Mod(spreadM, 50_000))
		radiusM = math.Abs(math.Mod(radiusM, 20_000))
		rng := rand.New(rand.NewPCG(seed, 7))
		pos := scatter(rng, centre, spreadM, 60)
		g := NewGrid(cellM)
		for i, p := range pos {
			if !g.Upsert(fmt.Sprint(i), p) {
				t.Fatalf("Upsert(%v) refused a valid position", p)
			}
		}
		for _, q := range scatter(rng, centre, spreadM, 5) {
			near := g.Near(q, radiusM)
			if len(near) > g.Len() {
				t.Fatalf("Near returned %d ids from a grid of %d", len(near), g.Len())
			}
			for i, p := range pos {
				if tangentM(q, p) <= radiusM && !slices.Contains(near, fmt.Sprint(i)) {
					t.Fatalf("cell %v m, radius %v m: query %v missed %d at %v (%v m)", g.CellM(), radiusM, q, i, p, tangentM(q, p))
				}
			}
		}
	})
}
