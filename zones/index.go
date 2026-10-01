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

// IndexLimits bounds the memory of an Index (E-10). A zone that would
// exceed either bound is not put in the grid but on a list that every
// lookup checks by bounding box: it is still always found, it only costs
// one box comparison per lookup instead of a cell lookup.
type IndexLimits struct {
	// MaxCellsPerZone is the most 0.1 degree cells one zone is registered
	// in. 1,000 cells is a box of about 3 x 3 degrees, some 350 km a side
	// at the equator; a country-sized or whole-globe zone goes to the list.
	MaxCellsPerZone int
	// MaxEntries is the most (cell, zone) entries in the whole grid. An
	// entry costs about 50 bytes when each cell holds one zone (measured
	// on go1.27: 250,000 entries in 207,000 cells took 12 MB of heap), so
	// the default keeps the grid near 15 MB whatever the zone set.
	MaxEntries int
}

// DefaultIndexLimits are the limits NewIndex uses.
func DefaultIndexLimits() IndexLimits {
	return IndexLimits{MaxCellsPerZone: 1_000, MaxEntries: 250_000}
}

// Index finds the zones whose bounding box contains a position: a sparse
// grid of 0.1 degree cells over the bounding boxes, plus a list of the
// zones too large for the grid (IndexLimits). It is the prefilter before
// ContainsHorizontally (Z-06): every zone that contains a point is among
// its candidates, so an index miss is never a missed zone. Boxes that
// cross the antimeridian are registered on both sides.
//
// An Index is immutable once built and safe for concurrent use. Rebuild
// it when the zone set changes.
type Index struct {
	zones   []*Zone
	cells   map[int32][]int32
	large   []int32
	entries int
}

// NewIndex builds an index over zs with DefaultIndexLimits.
func NewIndex(zs []*Zone) *Index {
	return NewIndexLimits(zs, DefaultIndexLimits())
}

// NewIndexLimits builds an index over zs within lim; a zero or negative
// field takes its DefaultIndexLimits value. Nil zones and zones with an
// empty or non-finite bounding box are skipped (such a zone contains
// nothing). The slice is copied; the zones are not.
func NewIndexLimits(zs []*Zone, lim IndexLimits) *Index {
	d := DefaultIndexLimits()
	if lim.MaxCellsPerZone <= 0 {
		lim.MaxCellsPerZone = d.MaxCellsPerZone
	}
	if lim.MaxEntries <= 0 {
		lim.MaxEntries = d.MaxEntries
	}
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
		n *= lat1 - lat0 + 1
		if n > lim.MaxCellsPerZone || ix.entries+n > lim.MaxEntries {
			ix.large = append(ix.large, id)
			continue
		}
		ix.entries += n
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

// Spilled is the number of zones kept on the list instead of the grid,
// for a status line: many of them means the limits are too low for the
// zone set and every lookup checks them all.
func (i *Index) Spilled() int { return len(i.large) }

// Entries is the number of (cell, zone) entries in the grid.
func (i *Index) Entries() int { return i.entries }

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
