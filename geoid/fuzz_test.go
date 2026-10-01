package geoid

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/pgm"
)

// FuzzParseGrid: no panic on any file, and an accepted grid answers
// every in-range position with a finite value inside the stored range.
func FuzzParseGrid(f *testing.F) {
	f.Add(synthetic())
	f.Add(pgm.Encode(2, 3, []pgm.HeaderLine{{Key: "Offset", Value: "-108"}, {Key: "Scale", Value: "0.003"}}, []uint16{0, 1, 2, 3, 0xFFFF, 5}))
	f.Add([]byte("P5\n# Offset 0\n# Scale 1\n2 3\n65535\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := Parse(data)
		if err != nil {
			return
		}
		lo, hi := g.offsetM, g.offsetM+g.scaleM*65535
		for _, p := range []core.LatLon{
			{LatDeg: 90}, {LatDeg: -90}, {LatDeg: 0, LonDeg: 359.999}, {LatDeg: 12.3, LonDeg: -45.6},
			{LatDeg: -89.99, LonDeg: 1e10}, {LatDeg: 45, LonDeg: -1e-300},
		} {
			n, err := g.UndulationM(p)
			if err != nil {
				t.Fatalf("%+v: %v", p, err)
			}
			if math.IsNaN(n) || n < lo-1e-6*math.Abs(hi-lo)-1e-9 || n > hi+1e-6*math.Abs(hi-lo)+1e-9 {
				t.Fatalf("%+v: N %v outside [%v, %v]", p, n, lo, hi)
			}
		}
		_ = g.Description()
	})
}
