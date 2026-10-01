package terrain

import (
	"strconv"
	"testing"

	"github.com/rootxkit/uspace-core/internal/pgm"
)

// tileSpec describes a tile in the lab tool's format.
type tileSpec struct {
	width, height      int
	latFirst, lonFirst float64
	step               float64
	dataset            string
	elevation          func(row, col int) float64 // NaN-free metres
	nodata             map[[2]int]bool            // {row, col}
}

func encodeTile(s tileSpec) []byte {
	samples := make([]uint16, s.width*s.height)
	for r := 0; r < s.height; r++ {
		for c := 0; c < s.width; c++ {
			if s.nodata[[2]int{r, c}] {
				samples[r*s.width+c] = NoData
				continue
			}
			samples[r*s.width+c] = uint16((s.elevation(r, c) - OffsetM) / ScaleM)
		}
	}
	f := func(x float64) string { return strconv.FormatFloat(x, 'g', -1, 64) }
	return pgm.Encode(s.width, s.height, []pgm.HeaderLine{
		{Key: "Description", Value: "test tile"},
		{Key: "Dataset", Value: s.dataset},
		{Key: "Offset", Value: "-500.0"},
		{Key: "Scale", Value: "0.2"},
		{Key: "Nodata", Value: "65535"},
		{Key: "LatFirst", Value: f(s.latFirst)},
		{Key: "LonFirst", Value: f(s.lonFirst)},
		{Key: "LatStep", Value: f(s.step)},
		{Key: "LonStep", Value: f(s.step)},
	}, samples)
}

// syntheticTileBytes is terrain_geoid.json's synthetic tile: 5 x 5 at
// 0.25 deg, first sample 42.0N 44.0E, rows south, elevation = 400 +
// 10*row + col, sample (1, 1) nodata.
func syntheticTileBytes() []byte {
	return encodeTile(tileSpec{
		width: 5, height: 5, latFirst: 42, lonFirst: 44, step: 0.25,
		dataset:   "COP-DEM GLO-30",
		elevation: func(r, c int) float64 { return float64(400 + 10*r + c) },
		nodata:    map[[2]int]bool{{1, 1}: true},
	})
}

func syntheticTile(t testing.TB) *Tile {
	t.Helper()
	tile, err := ParseTile(syntheticTileBytes())
	if err != nil {
		t.Fatal(err)
	}
	return tile
}

// cellTile is a 1 x 1 degree tile for cell (lat, lon south-west corner),
// 5 x 5 samples at 0.25 deg, flat at elevationM.
func cellTile(latSW, lonSW int, elevationM float64) []byte {
	return encodeTile(tileSpec{
		width: 5, height: 5, latFirst: float64(latSW + 1), lonFirst: float64(lonSW), step: 0.25,
		dataset:   "COP-DEM GLO-90",
		elevation: func(int, int) float64 { return elevationM },
	})
}
