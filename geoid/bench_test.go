package geoid

import (
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

var sinkM float64

func BenchmarkUndulation(b *testing.B) {
	g := mustParse(b, synthetic())
	p := core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}
	b.ReportAllocs()
	for b.Loop() {
		n, err := g.UndulationM(p)
		if err != nil {
			b.Fatal(err)
		}
		sinkM = n
	}
}
