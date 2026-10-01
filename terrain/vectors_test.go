package terrain

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geoid"
	"github.com/rootxkit/uspace-core/internal/pgm"
	"github.com/rootxkit/uspace-core/vectors"
)

// Tolerances of terrain_geoid.json's header, mirrored here so a change in
// the lab shows in this diff (docs/PLAN.md section 6).
const (
	tolSyntheticM = 1e-9
	tolGeoLibM    = 1e-9
)

type vectorInput struct {
	Function string  `json:"function"`
	LatDeg   float64 `json:"lat_deg"`
	LonDeg   float64 `json:"lon_deg"`
	Grid     string  `json:"grid,omitempty"`
	Tile     string  `json:"tile,omitempty"`
}

// syntheticGeoid builds the header's synthetic grid: 36 x 19 at 10 deg,
// Offset -100, Scale 0.01, sample(row, col) = 1000 + 100*row + col, row 0
// = 90N, col 0 = 0E.
func syntheticGeoid() []byte {
	s := make([]uint16, 36*19)
	for r := 0; r < 19; r++ {
		for c := 0; c < 36; c++ {
			s[r*36+c] = uint16(1000 + 100*r + c)
		}
	}
	return pgm.Encode(36, 19, []pgm.HeaderLine{{Key: "Offset", Value: "-100"}, {Key: "Scale", Value: "0.01"}}, s)
}

// realGrids loads GeographicLib's grids once from USPACE_GEOID_DIR.
var realGrids = struct {
	sync.Mutex
	grids map[string]*geoid.Grid
	errs  map[string]error
}{grids: map[string]*geoid.Grid{}, errs: map[string]error{}}

func realGrid(name string) (*geoid.Grid, error) {
	realGrids.Lock()
	defer realGrids.Unlock()
	if g, ok := realGrids.grids[name]; ok {
		return g, realGrids.errs[name]
	}
	dir := os.Getenv("USPACE_GEOID_DIR")
	var g *geoid.Grid
	err := os.ErrNotExist
	if dir != "" {
		g, err = geoid.Load(filepath.Join(dir, name+".pgm"))
	}
	realGrids.grids[name], realGrids.errs[name] = g, err
	return g, err
}

func TestVectorsTerrainGeoid(t *testing.T) {
	f := vectors.Load(t, "terrain_geoid.json")
	for key, want := range map[string]float64{"synthetic": tolSyntheticM, "geographiclib references": tolGeoLibM} {
		if got, ok := f.FloatTolerance(key); !ok || got != want {
			t.Fatalf("tolerance %q: header says %v (%v), test applies %v", key, got, ok, want)
		}
	}
	tile := syntheticTile(t)
	grid, err := geoid.Parse(syntheticGeoid())
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	ran := map[string]int{}
	skipped := 0
	t.Cleanup(func() {
		t.Logf("terrain_geoid.json: ran %v, skipped %d (GeographicLib grids absent)", ran, skipped)
	})
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in vectorInput
		c.Decode(t, &in, nil)
		p := core.LatLon{LatDeg: in.LatDeg, LonDeg: in.LonDeg}
		switch in.Function {
		case "cell_name":
			var exp struct {
				Cell string `json:"cell"`
			}
			c.Decode(t, nil, &exp)
			if got := CellName(p); got != exp.Cell {
				t.Errorf("CellName(%+v) = %q, want %q", p, got, exp.Cell)
			}
		case "terrain_tile_elevation":
			if in.Tile != "synthetic" {
				t.Fatalf("unknown tile %q", in.Tile)
			}
			var exp struct {
				ElevationM *float64 `json:"elevation_m"`
			}
			c.Decode(t, nil, &exp)
			vectors.NearPtr(t, "elevation_m", tile.ElevationM(p), exp.ElevationM, tolSyntheticM)
		case "geoid_undulation":
			var exp struct {
				UndulationM float64 `json:"undulation_m"`
			}
			c.Decode(t, nil, &exp)
			g, tol := grid, tolSyntheticM
			if in.Grid != "synthetic" {
				rg, err := realGrid(in.Grid)
				if err != nil {
					mu.Lock()
					skipped++
					mu.Unlock()
					t.Skipf("needs %s.pgm in USPACE_GEOID_DIR (%v)", in.Grid, err)
				}
				g, tol = rg, tolGeoLibM
			}
			n, err := g.UndulationM(p)
			if err != nil {
				t.Fatal(err)
			}
			vectors.Near(t, "undulation_m", n, exp.UndulationM, tol)
		default:
			t.Fatalf("unknown function %q", in.Function)
		}
		mu.Lock()
		ran[strings.TrimSpace(in.Function)]++
		mu.Unlock()
	})
}
