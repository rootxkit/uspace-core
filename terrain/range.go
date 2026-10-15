package terrain

import (
	"math"
	"slices"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// Bounds of Store.RangeM. A box is walked cell by cell and, within a
// tile, sample by sample; both are bounded so that a caller's box cannot
// make the walk unbounded (E-10).
const (
	// MaxRangeCells bounds the 1 x 1 degree cells one box may touch.
	MaxRangeCells = 64
	// DefaultMaxRangeSamples is the samples RangeM reads when the caller
	// passes no bound: about 40 000 km² of GLO-30, four times F3548's
	// largest constraint (CstrMaxAreaKm2 = 10 000).
	DefaultMaxRangeSamples = 50_000_000
)

// Counter of Store.RangeM, beside those of Elevation.
const (
	// CounterRangeUnknown: a box whose range was left unknown, because
	// a cell is not in the index, a tile cannot be read or a sample is
	// NoData.
	CounterRangeUnknown = "range_unknown"
)

// Range is the least and greatest ground elevation over a box, with
// what it was read from (LESSONS D-05: the dataset and spacing are shown
// beside the number).
type Range struct {
	// MinM and MaxM bound the elevation Store.Elevation answers at any
	// position of the box: every sample a bilinear interpolation inside
	// the box can read, and 0 m for a sea cell.
	MinM, MaxM float64
	// Samples is the tile samples read; a sea cell reads none.
	Samples int
	// Cells is the 1 x 1 degree cells the box touches.
	Cells int
	// Datasets names every dataset read, sorted ("sea" for a sea cell).
	Datasets []string
	// SpacingM is the coarsest sample spacing read (0 at sea only).
	SpacingM float64
}

// RangeM returns the least and greatest ground elevation over box b, for
// a caller that must hold a limit above ground for a whole area rather
// than at one position: the least for a lower bound, the greatest for an
// upper, so that an absolute volume made from them never shrinks below
// the one above the ground.
//
// The range is a superset of Elevation's answers inside b, never a
// sample of them: in every tile it reads all the samples whose bilinear
// cells the box meets, including the enclosing ones past its edges, and
// a bilinear surface lies between the least and greatest of its corner
// samples. It is (nil, nil) when any position of b is unknown to
// Elevation: a cell that is not in the index, a tile that cannot be read
// or a NoData sample among those read (LESSONS D-04, never zero),
// counted range_unknown beside Elevation's own counter. A sea cell is
// 0 m with dataset "sea".
//
// b is degrees with MinLat <= MaxLat and MinLon <= MaxLon (a box across
// the antimeridian is refused), every edge a valid position. A box
// touching more than MaxRangeCells cells, or whose tiles hold more than
// maxSamples samples inside it (zero or less: DefaultMaxRangeSamples), is
// refused. Errors are *core.FieldError naming the box member.
func (s *Store) RangeM(b geodesy.BBox, maxSamples int) (*Range, error) {
	if maxSamples <= 0 {
		maxSamples = DefaultMaxRangeSamples
	}
	if err := validRangeBox(b); err != nil {
		s.counters.Inc(CounterInvalidPosition)
		return nil, err
	}
	lat0, lat1 := int(math.Floor(b.MinLat)), int(math.Floor(b.MaxLat))
	lon0, lon1 := int(math.Floor(b.MinLon)), int(math.Floor(b.MaxLon))
	// A box edge on +90 or +180 belongs to the cell below it, as no cell
	// starts there.
	lat1 = min(lat1, 89)
	lon1 = min(lon1, 179)
	if lat0 > lat1 || lon0 > lon1 {
		// A box lying wholly on the +90 parallel or the +180 meridian
		// touches no cell: Elevation answers unknown there (CellName
		// N90 or E180 is in no index), so the range is unknown too,
		// never the empty {+Inf, -Inf}.
		s.counters.Inc(CounterUnknownCell)
		s.counters.Inc(CounterRangeUnknown)
		return nil, nil
	}
	cells := (lat1 - lat0 + 1) * (lon1 - lon0 + 1)
	if cells > MaxRangeCells {
		return nil, core.Fieldf("bbox", "touches %d cells; at most %d", cells, MaxRangeCells)
	}
	out := &Range{MinM: math.Inf(1), MaxM: math.Inf(-1), Cells: cells}
	for la := lat0; la <= lat1; la++ {
		for ln := lon0; ln <= lon1; ln++ {
			// The cell's south-west corner names it (CellName).
			cell := CellName(core.LatLon{LatDeg: float64(la), LonDeg: float64(ln)})
			dataset, ok := s.index[cell]
			if !ok {
				s.counters.Inc(CounterUnknownCell)
				s.counters.Inc(CounterRangeUnknown)
				return nil, nil
			}
			if dataset == SeaDataset {
				out.MinM, out.MaxM = math.Min(out.MinM, 0), math.Max(out.MaxM, 0)
				out.Datasets = appendUnique(out.Datasets, SeaDataset)
				continue
			}
			t := s.tile(cell)
			if t == nil {
				s.counters.Inc(CounterTileUnavailable)
				s.counters.Inc(CounterRangeUnknown)
				return nil, nil
			}
			clip := geodesy.BBox{
				MinLat: math.Max(b.MinLat, float64(la)), MaxLat: math.Min(b.MaxLat, float64(la+1)),
				MinLon: math.Max(b.MinLon, float64(ln)), MaxLon: math.Min(b.MaxLon, float64(ln+1)),
			}
			r0, r1, c0, c1 := t.window(clip)
			n := (r1 - r0 + 1) * (c1 - c0 + 1)
			if out.Samples+n > maxSamples {
				return nil, core.Fieldf("bbox", "holds more than %d samples", maxSamples)
			}
			eMin, eMax, known := t.rawRange(r0, r1, c0, c1)
			if !known {
				s.counters.Inc(CounterNoData)
				s.counters.Inc(CounterRangeUnknown)
				return nil, nil
			}
			out.Samples += n
			out.MinM, out.MaxM = math.Min(out.MinM, eMin), math.Max(out.MaxM, eMax)
			out.Datasets = appendUnique(out.Datasets, t.Dataset())
			out.SpacingM = math.Max(out.SpacingM, t.SpacingM())
		}
	}
	slices.Sort(out.Datasets)
	return out, nil
}

func validRangeBox(b geodesy.BBox) error {
	for _, v := range []struct {
		field string
		deg   float64
		limit float64
	}{
		{"bbox.min_lat", b.MinLat, 90}, {"bbox.max_lat", b.MaxLat, 90},
		{"bbox.min_lon", b.MinLon, 180}, {"bbox.max_lon", b.MaxLon, 180},
	} {
		if !core.IsFinite(v.deg) || v.deg < -v.limit || v.deg > v.limit {
			return core.Fieldf(v.field, "%v is outside [-%v, %v]", v.deg, v.limit, v.limit)
		}
	}
	if b.MinLat > b.MaxLat {
		return core.Fieldf("bbox.max_lat", "%v is south of min_lat %v", b.MaxLat, b.MinLat)
	}
	if b.MinLon > b.MaxLon {
		return core.Fieldf("bbox.max_lon", "%v is west of min_lon %v; a box across the antimeridian is not supported", b.MaxLon, b.MinLon)
	}
	return nil
}

// window is the rows r0..r1 and columns c0..c1 of every sample
// ElevationM reads for a position inside b: the clamped fractional
// indices of b's edges, widened to the enclosing samples.
func (t *Tile) window(b geodesy.BBox) (r0, r1, c0, c1 int) {
	w, h := t.grid.Width, t.grid.Height
	fyTop := min(max((t.latFirstDeg-b.MaxLat)/t.latStepDeg, 0), float64(h-1))
	fyBot := min(max((t.latFirstDeg-b.MinLat)/t.latStepDeg, 0), float64(h-1))
	fxW := min(max((b.MinLon-t.lonFirstDeg)/t.lonStepDeg, 0), float64(w-1))
	fxE := min(max((b.MaxLon-t.lonFirstDeg)/t.lonStepDeg, 0), float64(w-1))
	r0 = min(int(fyTop), h-2)
	r1 = min(max(int(fyBot)+1, r0+1), h-1)
	c0 = min(int(fxW), w-2)
	c1 = min(max(int(fxE)+1, c0+1), w-1)
	return r0, r1, c0, c1
}

// rawRange is the least and greatest elevation of the samples in rows
// r0..r1 and columns c0..c1; known is false when one is NoData.
func (t *Tile) rawRange(r0, r1, c0, c1 int) (lo, hi float64, known bool) {
	minRaw, maxRaw := uint16(math.MaxUint16), uint16(0)
	for iy := r0; iy <= r1; iy++ {
		for ix := c0; ix <= c1; ix++ {
			v := t.grid.Raw(ix, iy)
			if v == NoData {
				return 0, 0, false
			}
			minRaw, maxRaw = min(minRaw, v), max(maxRaw, v)
		}
	}
	// The same rounding as ElevationM: one product, then the offset.
	return OffsetM + float64(ScaleM*float64(minRaw)), OffsetM + float64(ScaleM*float64(maxRaw)), true
}

func appendUnique(s []string, v string) []string {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}
