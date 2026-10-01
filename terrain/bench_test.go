package terrain

import (
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

var sinkElevation *float64

// BenchmarkTileElevation interpolates in a GLO-30-sized tile (3600 x
// 3600 samples at 1 arc-second), the per-track cost of a ground lookup.
func BenchmarkTileElevation(b *testing.B) {
	tile, err := ParseTile(encodeTile(tileSpec{
		width: 3600, height: 3600, latFirst: 42, lonFirst: 44, step: 1.0 / 3600,
		dataset:   "COP-DEM GLO-30",
		elevation: func(r, c int) float64 { return float64(400 + (r+c)%1000) },
	}))
	if err != nil {
		b.Fatal(err)
	}
	p := core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}
	b.ReportAllocs()
	for b.Loop() {
		sinkElevation = tile.ElevationM(p)
	}
}

var sinkStore *Elevation

// BenchmarkStoreElevation is a cached Store lookup: cell name, index,
// LRU touch under the lock and the interpolation.
func BenchmarkStoreElevation(b *testing.B) {
	data := cellTile(41, 44, 500)
	s := NewStore(Index{"N41E044": "COP-DEM GLO-90"}, StoreOptions{Open: func(string) ([]byte, error) { return data, nil }})
	p := core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}
	b.ReportAllocs()
	for b.Loop() {
		e, err := s.Elevation(p)
		if err != nil || e == nil {
			b.Fatal(e, err)
		}
		sinkStore = e
	}
}
