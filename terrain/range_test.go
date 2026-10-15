package terrain

import (
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// rangeStore is a store over N41E044 holding s, with the rest of the
// index as given.
func rangeStore(t *testing.T, s tileSpec, index Index) *Store {
	t.Helper()
	d := newDisk()
	d.put("N41E044", encodeTile(s))
	if index == nil {
		index = Index{}
	}
	index["N41E044"] = s.dataset
	return NewStore(index, StoreOptions{Open: d.open})
}

// slopeSpec is a 5 x 5 tile of N41E044 at 0.25 deg, elevation 400 +
// 10*row + col: highest in the south-east.
func slopeSpec() tileSpec {
	return tileSpec{
		width: 5, height: 5, latFirst: 42, lonFirst: 44, step: 0.25,
		dataset:   "COP-DEM GLO-30",
		elevation: func(r, c int) float64 { return float64(400 + 10*r + c) },
	}
}

func box(minLat, minLon, maxLat, maxLon float64) geodesy.BBox {
	return geodesy.BBox{MinLat: minLat, MinLon: minLon, MaxLat: maxLat, MaxLon: maxLon}
}

func TestRangeEnclosingSamples(t *testing.T) {
	s := rangeStore(t, slopeSpec(), nil)
	// Rows 1.2..1.6 and columns 1.2..1.6: the enclosing samples are rows
	// 1-2 and columns 1-2, so 411 (row 1, col 1) to 422 (row 2, col 2).
	got, err := s.RangeM(box(41.6, 44.3, 41.7, 44.4), 0)
	if err != nil {
		t.Fatal(err)
	}
	want := &Range{MinM: 411, MaxM: 422, Samples: 4, Cells: 1, Datasets: []string{"COP-DEM GLO-30"}, SpacingM: 0.25 * metresPerDegLat}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	wantCounters(t, s, map[string]uint64{CounterTilesLoaded: 1})
}

// A peak between the box's corners is in the range: sampling the corners
// (or the outline) would miss it, and an absolute ceiling made from the
// corners would cut through the ground.
func TestRangeFindsInteriorPeak(t *testing.T) {
	spec := tileSpec{
		width: 5, height: 5, latFirst: 42, lonFirst: 44, step: 0.25, dataset: "COP-DEM GLO-30",
		elevation: func(r, c int) float64 {
			if r == 2 && c == 2 {
				return 900
			}
			if r == 3 && c == 1 {
				return 100
			}
			return 400
		},
	}
	s := rangeStore(t, spec, nil)
	b := box(41.3, 44.1, 41.8, 44.9)
	corners := []core.LatLon{{LatDeg: b.MinLat, LonDeg: b.MinLon}, {LatDeg: b.MinLat, LonDeg: b.MaxLon}, {LatDeg: b.MaxLat, LonDeg: b.MinLon}, {LatDeg: b.MaxLat, LonDeg: b.MaxLon}}
	for _, p := range corners {
		e, err := s.Elevation(p)
		if err != nil || e == nil {
			t.Fatalf("corner %v: %v %v", p, e, err)
		}
		if e.ElevationM >= 900 || e.ElevationM <= 100 {
			t.Fatalf("corner %v is %v: the peak or the pit is at a corner, the test proves nothing", p, e.ElevationM)
		}
	}
	got, err := s.RangeM(b, 0)
	if err != nil || got == nil {
		t.Fatalf("%v %v", got, err)
	}
	if got.MaxM != 900 || got.MinM != 100 {
		t.Fatalf("range %v..%v, want 100..900", got.MinM, got.MaxM)
	}
}

// Every answer of Elevation inside a box lies in the box's range: random
// boxes on a random surface, sampled densely.
func TestRangeHoldsEveryElevation(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	heights := make([]float64, 9*9)
	for i := range heights {
		heights[i] = float64(rng.IntN(3000))
	}
	spec := tileSpec{
		width: 9, height: 9, latFirst: 42, lonFirst: 44, step: 0.125, dataset: "COP-DEM GLO-30",
		elevation: func(r, c int) float64 { return heights[r*9+c] },
	}
	s := rangeStore(t, spec, nil)
	for i := 0; i < 200; i++ {
		la0, la1 := 41+rng.Float64(), 41+rng.Float64()
		lo0, lo1 := 44+rng.Float64(), 44+rng.Float64()
		b := box(math.Min(la0, la1), math.Min(lo0, lo1), math.Max(la0, la1), math.Max(lo0, lo1))
		got, err := s.RangeM(b, 0)
		if err != nil || got == nil {
			t.Fatalf("box %+v: %v %v", b, got, err)
		}
		for k := 0; k <= 20; k++ {
			for j := 0; j <= 20; j++ {
				p := core.LatLon{LatDeg: b.MinLat + (b.MaxLat-b.MinLat)*float64(k)/20, LonDeg: b.MinLon + (b.MaxLon-b.MinLon)*float64(j)/20}
				e, err := s.Elevation(p)
				if err != nil || e == nil {
					t.Fatalf("%v: %v %v", p, e, err)
				}
				if e.ElevationM < got.MinM-1e-9 || e.ElevationM > got.MaxM+1e-9 {
					t.Fatalf("box %+v: elevation %v at %v outside the range %v..%v", b, e.ElevationM, p, got.MinM, got.MaxM)
				}
			}
		}
	}
}

func TestRangePointBox(t *testing.T) {
	s := rangeStore(t, slopeSpec(), nil)
	// A box of one position on a sample reads that sample and its
	// neighbours south and east, as ElevationM does there.
	got, err := s.RangeM(box(41.75, 44.25, 41.75, 44.25), 0)
	if err != nil || got == nil {
		t.Fatalf("%v %v", got, err)
	}
	if got.MinM != 411 || got.MaxM != 422 || got.Samples != 4 {
		t.Fatalf("%+v", got)
	}
}

// The tile's last row and column are clamped as ElevationM clamps them:
// a box past the last sample reads the edge samples, never outside.
func TestRangeClampedAtTileEdge(t *testing.T) {
	s := rangeStore(t, slopeSpec(), nil)
	got, err := s.RangeM(box(41.0, 44.95, 41.01, 44.999), 0)
	if err != nil || got == nil {
		t.Fatalf("%v %v", got, err)
	}
	// Rows 3-4 and columns 3-4: 433 to 444.
	if got.MinM != 433 || got.MaxM != 444 || got.Samples != 4 {
		t.Fatalf("%+v", got)
	}
}

func TestRangeSeaAndLand(t *testing.T) {
	s := rangeStore(t, slopeSpec(), Index{"N41E045": SeaDataset})
	got, err := s.RangeM(box(41.6, 44.9, 41.7, 45.2), 0)
	if err != nil || got == nil {
		t.Fatalf("%v %v", got, err)
	}
	want := &Range{MinM: 0, MaxM: 424, Samples: 4, Cells: 2, Datasets: []string{"COP-DEM GLO-30", SeaDataset}, SpacingM: 0.25 * metresPerDegLat}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestRangeSeaOnly(t *testing.T) {
	s := NewStore(Index{"N41E045": SeaDataset}, StoreOptions{})
	got, err := s.RangeM(box(41.2, 45.2, 41.3, 45.3), 0)
	if err != nil || got == nil {
		t.Fatalf("%v %v", got, err)
	}
	if got.MinM != 0 || got.MaxM != 0 || got.Samples != 0 || got.SpacingM != 0 || !reflect.DeepEqual(got.Datasets, []string{SeaDataset}) {
		t.Fatalf("%+v", got)
	}
}

// A box that reaches one cell outside the index is unknown as a whole,
// never the range of the part that is known (D-04).
func TestRangeUnknownCell(t *testing.T) {
	s := rangeStore(t, slopeSpec(), nil)
	got, err := s.RangeM(box(41.6, 44.9, 41.7, 45.1), 0)
	if err != nil || got != nil {
		t.Fatalf("want unknown, got %+v %v", got, err)
	}
	wantCounters(t, s, map[string]uint64{CounterTilesLoaded: 1, CounterUnknownCell: 1, CounterRangeUnknown: 1})
}

func TestRangeNoData(t *testing.T) {
	spec := slopeSpec()
	spec.nodata = map[[2]int]bool{{2, 2}: true}
	s := rangeStore(t, spec, nil)
	got, err := s.RangeM(box(41.6, 44.3, 41.7, 44.4), 0)
	if err != nil || got != nil {
		t.Fatalf("want unknown, got %+v %v", got, err)
	}
	wantCounters(t, s, map[string]uint64{CounterTilesLoaded: 1, CounterNoData: 1, CounterRangeUnknown: 1})
	// A NoData sample outside the box's enclosing samples leaves it known.
	got, err = s.RangeM(box(41.85, 44.05, 41.9, 44.1), 0)
	if err != nil || got == nil || got.MinM != 400 || got.MaxM != 411 {
		t.Fatalf("want 400..411, got %+v %v", got, err)
	}
}

func TestRangeTileUnavailable(t *testing.T) {
	s := NewStore(Index{"N41E044": "COP-DEM GLO-30"}, StoreOptions{Open: newDisk().open})
	got, err := s.RangeM(box(41.6, 44.3, 41.7, 44.4), 0)
	if err != nil || got != nil {
		t.Fatalf("want unknown, got %+v %v", got, err)
	}
	wantCounters(t, s, map[string]uint64{CounterTileReadFailed: 1, CounterTileUnavailable: 1, CounterRangeUnknown: 1})
}

func TestRangeSampleBound(t *testing.T) {
	s := rangeStore(t, slopeSpec(), nil)
	b := box(41.6, 44.3, 41.7, 44.4) // 4 samples
	if got, err := s.RangeM(b, 4); err != nil || got == nil {
		t.Fatalf("at the bound: %+v %v", got, err)
	}
	got, err := s.RangeM(b, 3)
	var fe *core.FieldError
	if got != nil || !errors.As(err, &fe) || fe.Field != "bbox" {
		t.Fatalf("past the bound: %+v %v", got, err)
	}
}

func TestRangeCellBound(t *testing.T) {
	index := Index{}
	for la := 0; la < 8; la++ {
		for lo := 0; lo < 9; lo++ {
			index[CellName(core.LatLon{LatDeg: float64(la), LonDeg: float64(lo)})] = SeaDataset
		}
	}
	s := NewStore(index, StoreOptions{})
	// 8 x 8 = 64 cells: at the bound.
	if got, err := s.RangeM(box(0.5, 0.5, 7.5, 7.5), 0); err != nil || got == nil || got.Cells != MaxRangeCells {
		t.Fatalf("at the bound: %+v %v", got, err)
	}
	got, err := s.RangeM(box(0.5, 0.5, 7.5, 8.5), 0)
	var fe *core.FieldError
	if got != nil || !errors.As(err, &fe) || fe.Field != "bbox" {
		t.Fatalf("past the bound: %+v %v", got, err)
	}
}

// A box edge on +90 or +180 belongs to the cell below it: no cell
// starts there.
func TestRangeTopEdges(t *testing.T) {
	s := NewStore(Index{"N89E179": SeaDataset}, StoreOptions{})
	got, err := s.RangeM(box(89.5, 179.5, 90, 180), 0)
	if err != nil || got == nil || got.Cells != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}

// A box wholly on the +90 parallel or the +180 meridian touches no
// cell: unknown, as Elevation is there, never an empty range.
func TestRangeBoxOnTheTopEdgesOnly(t *testing.T) {
	s := NewStore(Index{"N89E179": SeaDataset}, StoreOptions{})
	for _, b := range []geodesy.BBox{box(90, 179.5, 90, 179.6), box(89.5, 180, 89.6, 180), box(90, 180, 90, 180)} {
		got, err := s.RangeM(b, 0)
		if err != nil || got != nil {
			t.Fatalf("%+v: want unknown, got %+v %v", b, got, err)
		}
		if e, err := s.Elevation(core.LatLon{LatDeg: b.MinLat, LonDeg: b.MinLon}); err != nil || e != nil {
			t.Fatalf("%+v: Elevation answers %+v %v, the range must agree", b, e, err)
		}
	}
	if n := s.Counters().Snapshot()[CounterRangeUnknown]; n != 3 {
		t.Fatalf("range_unknown %d, want 3", n)
	}
}

func TestRangeInvalidBox(t *testing.T) {
	s := rangeStore(t, slopeSpec(), nil)
	for _, c := range []struct {
		b     geodesy.BBox
		field string
	}{
		{box(math.NaN(), 44, 41, 44.1), "bbox.min_lat"},
		{box(41, 44, 91, 44.1), "bbox.max_lat"},
		{box(41, math.Inf(-1), 41.1, 44.1), "bbox.min_lon"},
		{box(41, 44, 41.1, 181), "bbox.max_lon"},
		{box(41.2, 44, 41.1, 44.1), "bbox.max_lat"},
		{box(41, 179, 41.1, -179), "bbox.max_lon"},
	} {
		got, err := s.RangeM(c.b, 0)
		var fe *core.FieldError
		if got != nil || !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%+v: got %+v %v, want a problem on %s", c.b, got, err, c.field)
		}
	}
	if n := s.Counters().Snapshot()[CounterInvalidPosition]; n != 6 {
		t.Fatalf("invalid_position %d, want 6", n)
	}
}
