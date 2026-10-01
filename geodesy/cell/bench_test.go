package cell

import (
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

var (
	sinkID  ID
	sinkIDs []ID
)

func boxOf(minLat, minLon, maxLat, maxLon float64) geodesy.BBox {
	return geodesy.BBox{MinLat: minLat, MinLon: minLon, MaxLat: maxLat, MaxLon: maxLon}
}

// BenchmarkCellOf is the ingest's call, once per sample.
func BenchmarkCellOf(b *testing.B) {
	p := core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}
	for b.Loop() {
		c, err := Of(p, Level5)
		if err != nil {
			b.Fatal(err)
		}
		sinkID = c
	}
}

func BenchmarkCellRing1(b *testing.B) {
	c := ID{Level: Level5, LatIdx: 1317, LonIdx: 2248}
	for b.Loop() {
		sinkIDs = c.Ring1()
	}
}

func BenchmarkCellString(b *testing.B) {
	c := ID{Level: Level5, LatIdx: 1317, LonIdx: 2248}
	for b.Loop() {
		if c.String() == "" {
			b.Fatal("empty name")
		}
	}
}

func BenchmarkCellParse(b *testing.B) {
	for b.Loop() {
		c, err := Parse("c5:1317:2248")
		if err != nil {
			b.Fatal(err)
		}
		sinkID = c
	}
}

func BenchmarkCellCoverGeorgia(b *testing.B) {
	box := boxOf(41.0, 40.0, 43.6, 46.7)
	for b.Loop() {
		ids, err := Cover(box, Level5, MaxCoverDefault)
		if err != nil {
			b.Fatal(err)
		}
		sinkIDs = ids
	}
}
