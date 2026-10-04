package geoid

import (
	"os"
	"path/filepath"
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

// BenchmarkUndulationMapped is BenchmarkUndulation on a grid from
// LoadMapped: the same computation over mapped bytes.
func BenchmarkUndulationMapped(b *testing.B) {
	path := filepath.Join(b.TempDir(), "synthetic.pgm")
	if err := os.WriteFile(path, synthetic(), 0o600); err != nil {
		b.Fatal(err)
	}
	g, err := LoadMapped(path)
	if err != nil {
		b.Fatal(err)
	}
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
