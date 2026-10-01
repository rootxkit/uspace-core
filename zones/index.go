package zones

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// Grid geometry of the Index: cells of cellsPerDeg^-1 degrees (0.1 deg,
// about 11 km of latitude).
const (
	cellsPerDeg = 10
	latCells    = 180 * cellsPerDeg
	lonCells    = 360 * cellsPerDeg
)

// MaxCellsPerZone bounds the grid cells one zone is registered in. A zone
// whose bounding box covers more (about 100 x 100 km at the equator) is
// kept on a short list that every lookup checks by its bounding box
// instead, so a country-sized or whole-globe zone costs one comparison
// per lookup rather than millions of map entries (E-10).
const MaxCellsPerZone = 10_000

// Index finds the zones whose bounding box contains a position: a sparse
// grid of 0.1 degree cells over the bounding boxes, plus a list of the
// zones too large for the grid. It is the prefilter before
// ContainsHorizontally (Z-06): every zone that contains a point is among
// its candidates, so an index miss is never a missed zone. Boxes that
// cross the antimeridian are registered on both sides.
//
// An Index is immutable once built and safe for concurrent use. Rebuild
// it when the zone set changes.
type Index struct {
	zones []*Zone
	cells map[int32][]int32
	large []int32
}

// NewIndex builds an index over zs. Nil zones and zones with an empty or
// non-finite bounding box are skipped (such a zone contains nothing).
// The slice is copied; the zones are not.
func NewIndex(zs []*Zone) *Index {
	ix := &Index{zones: make([]*Zone, 0, len(zs)), cells: make(map[int32][]int32)}
	for _, z := range zs {
		if z == nil {
			continue
		}
		b := z.BBox
		if !core.IsFinite(b.MinLat) || !core.IsFinite(b.MaxLat) ||
			!core.IsFinite(b.MinLon) || !core.IsFinite(b.MaxLon) || b.MinLat > b.MaxLat {
			continue
		}
		id := int32(len(ix.zones))
		ix.zones = append(ix.zones, z)
		lat0, lat1 := latCell(b.MinLat), latCell(b.MaxLat)
		lonRanges := lonCellRanges(b.MinLon, b.MaxLon)
		n := 0
		for _, r := range lonRanges {
			n += r[1] - r[0] + 1
		}
		if n*(lat1-lat0+1) > MaxCellsPerZone {
			ix.large = append(ix.large, id)
			continue
		}
		for la := lat0; la <= lat1; la++ {
			for _, r := range lonRanges {
				for lo := r[0]; lo <= r[1]; lo++ {
					k := cellKey(la, lo)
					ix.cells[k] = append(ix.cells[k], id)
				}
			}
		}
	}
	return ix
}

// Len is the number of zones indexed.
func (i *Index) Len() int { return len(i.zones) }

// Candidates returns the zones whose bounding box contains p, in the
// order they were given to NewIndex. An invalid position returns nil
// (C-09: a non-finite number never reaches the grid).
func (i *Index) Candidates(p core.LatLon) []*Zone {
	return i.AppendCandidates(nil, p)
}

// AppendCandidates appends the candidates for p to dst and returns it,
// so that a caller on the hot path can reuse one slice.
func (i *Index) AppendCandidates(dst []*Zone, p core.LatLon) []*Zone {
	if i == nil || !p.Valid() {
		return dst
	}
	if math.Abs(p.LonDeg) == 180 {
		// -180 and 180 are one meridian but two grid columns and, for a
		// box that does not cross the antimeridian, two answers: check
		// every zone, which this rare case can afford.
		for _, z := range i.zones {
			if bboxContains(z, p) {
				dst = append(dst, z)
			}
		}
		return dst
	}
	cell := i.cells[cellKey(latCell(p.LatDeg), lonCell(p.LonDeg))]
	li := 0
	// Merge the two sorted id lists so the result keeps the input order.
	for _, id := range cell {
		for li < len(i.large) && i.large[li] < id {
			dst = i.appendIf(dst, i.large[li], p)
			li++
		}
		dst = i.appendIf(dst, id, p)
	}
	for ; li < len(i.large); li++ {
		dst = i.appendIf(dst, i.large[li], p)
	}
	return dst
}

func (i *Index) appendIf(dst []*Zone, id int32, p core.LatLon) []*Zone {
	if z := i.zones[id]; z.BBox.Contains(p) {
		dst = append(dst, z)
	}
	return dst
}

// latCell is the grid row of a finite latitude, clamped into the grid.
func latCell(latDeg float64) int {
	return clampCell(math.Floor((latDeg+90)*cellsPerDeg), latCells)
}

// lonCell is the grid column of a finite longitude, clamped into the
// grid; 180 shares the last column with values just below it.
func lonCell(lonDeg float64) int {
	return clampCell(math.Floor((lonDeg+180)*cellsPerDeg), lonCells)
}

func clampCell(f float64, n int) int {
	switch {
	case f < 0:
		return 0
	case f >= float64(n):
		return n - 1
	}
	return int(f)
}

// lonCellRanges is the column ranges a box covers: one range, or two
// when the box crosses the antimeridian (MinLon > MaxLon).
func lonCellRanges(minLon, maxLon float64) [][2]int {
	lo, hi := lonCell(minLon), lonCell(maxLon)
	if minLon <= maxLon {
		return [][2]int{{lo, hi}}
	}
	return [][2]int{{lo, lonCells - 1}, {0, hi}}
}

func cellKey(la, lo int) int32 {
	return int32(la*lonCells + lo)
}
