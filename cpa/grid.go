package cpa

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// Cell size bounds. NewGrid clamps into them, so a NaN, zero, negative or
// infinite size still gives a working grid (a lookup is always correct;
// only its cost depends on the size).
const (
	// MinCellM is the smallest cell side.
	MinCellM = 1.0
	// MaxCellM is the largest cell side, about half a meridian.
	MaxCellM = 20_000_000.0
)

// nearMarginRel widens every lookup by 1 %, so a caller measuring the
// exact distance with Vincenty rather than the tangent plane (they differ
// by far less over a few kilometres) never loses a candidate at the edge.
const nearMarginRel = 0.01

// Lower bounds on the metres per degree over the WGS84 ellipsoid: the
// meridional radius is smallest at the equator, a(1-e^2), and the prime
// vertical radius is never below a. A cell sized with them is at least
// as wide as intended everywhere.
var (
	minNorthPerDegM         = core.WGS84SemiMajorM * (1 - wgs84EccentricitySq) * math.Pi / 180
	minEastPerDegAtEquatorM = core.WGS84SemiMajorM * math.Pi / 180
)

const wgs84EccentricitySq = core.WGS84Flattening * (2 - core.WGS84Flattening)

// Grid counter names.
const (
	// CounterGridRejectedPosition counts Upsert calls refused for a
	// non-finite or out-of-range position (C-09).
	CounterGridRejectedPosition = "grid_rejected_invalid_position"
	// CounterGridRejectedQuery counts Near calls refused for an invalid
	// position or radius.
	CounterGridRejectedQuery = "grid_rejected_invalid_query"
)

type cellKey struct{ band, col int }

type slot struct {
	key cellKey
	idx int
}

// Grid is a neighbour index over WGS84 positions (C-15). The sphere is cut
// into latitude bands cellM metres high and, per band, into longitude
// columns at least cellM metres wide at the band's poleward edge; the
// columns divide 360 degrees exactly, so the column west of the first is
// the last and a pair straddling the antimeridian is found. Bands at a
// pole have one column.
//
// It holds one entry per id: bounded by the number of distinct ids
// upserted and not removed. Remove frees an entry and its cell.
//
// A Grid is not safe for concurrent use; use one per goroutine or guard
// it with a mutex.
type Grid struct {
	cellM    float64
	bandDeg  float64
	cells    map[cellKey][]string
	where    map[string]slot
	counters core.Counters
}

// NewGrid returns an empty grid of cells cellM metres on a side, clamped
// into [MinCellM, MaxCellM] (a NaN gives MinCellM). Size the cells at
// least one Policy.NeighbourRadiusM wide (C-15), so a lookup at that
// radius visits the 3x3 ring; a smaller cell is still correct, Near
// visits as many rings as the radius needs, but costs more.
func NewGrid(cellM float64) *Grid {
	if !(cellM >= MinCellM) {
		cellM = MinCellM
	}
	if cellM > MaxCellM {
		cellM = MaxCellM
	}
	return &Grid{
		cellM:   cellM,
		bandDeg: cellM / minNorthPerDegM,
		cells:   make(map[cellKey][]string),
		where:   make(map[string]slot),
	}
}

// CellM is the cell side in metres after clamping.
func (g *Grid) CellM() float64 { return g.cellM }

// Len is the number of ids in the grid.
func (g *Grid) Len() int { return len(g.where) }

// Counters returns the grid's refusal counters.
func (g *Grid) Counters() *core.Counters { return &g.counters }

func (g *Grid) bandOf(latDeg float64) int {
	return int(math.Floor(latDeg / g.bandDeg))
}

// columns is how many columns go round a band: as many as fit at its
// poleward edge, where a degree of longitude is shortest.
func (g *Grid) columns(band int) int {
	edgeDeg := math.Min(90, math.Max(math.Abs(float64(band)*g.bandDeg), math.Abs(float64(band+1)*g.bandDeg)))
	eastPerDegM := minEastPerDegAtEquatorM * math.Cos(edgeDeg*math.Pi/180)
	n := math.Floor(360 * eastPerDegM / g.cellM)
	if !(n >= 1) {
		return 1
	}
	return int(n)
}

// colOf is the unwrapped column index of a longitude in a band of n
// columns; the caller takes it modulo n.
func colOf(lonDeg float64, n int) int {
	return int(math.Floor((lonDeg + 180) / 360 * float64(n)))
}

func mod(i, n int) int {
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

func (g *Grid) keyOf(p core.LatLon) cellKey {
	band := g.bandOf(p.LatDeg)
	n := g.columns(band)
	return cellKey{band: band, col: mod(colOf(p.LonDeg, n), n)}
}

// Upsert inserts id at p or moves it there. A non-finite or out-of-range
// position is refused and counted (C-09): the grid is left unchanged,
// including any earlier position of id, and Upsert returns false.
func (g *Grid) Upsert(id string, p core.LatLon) bool {
	if !p.Valid() {
		g.counters.Inc(CounterGridRejectedPosition)
		return false
	}
	key := g.keyOf(p)
	if s, ok := g.where[id]; ok {
		if s.key == key {
			return true
		}
		g.detach(id, s)
	}
	g.cells[key] = append(g.cells[key], id)
	g.where[id] = slot{key: key, idx: len(g.cells[key]) - 1}
	return true
}

// Remove deletes id; removing an absent id does nothing.
func (g *Grid) Remove(id string) {
	if s, ok := g.where[id]; ok {
		g.detach(id, s)
	}
}

// detach removes id from its cell by swapping the cell's last id into
// its place, and frees the cell when it empties.
func (g *Grid) detach(id string, s slot) {
	ids := g.cells[s.key]
	last := len(ids) - 1
	if s.idx != last {
		moved := ids[last]
		ids[s.idx] = moved
		g.where[moved] = slot{key: s.key, idx: s.idx}
	}
	ids[last] = ""
	if last == 0 {
		delete(g.cells, s.key)
	} else {
		g.cells[s.key] = ids[:last]
	}
	delete(g.where, id)
}

// span is the band range and the half-width in degrees of longitude a
// lookup must cover; wholeLon means every column of each band.
type span struct {
	bandLo, bandHi int
	lonDeg         float64
	halfLonDeg     float64
	wholeLon       bool
}

func (g *Grid) spanOf(p core.LatLon, radiusM float64) span {
	r := radiusM * (1 + nearMarginRel)
	halfLatDeg := r / minNorthPerDegM
	sp := span{
		bandLo: g.bandOf(math.Max(-90, p.LatDeg-halfLatDeg)),
		bandHi: g.bandOf(math.Min(90, p.LatDeg+halfLatDeg)),
		lonDeg: p.LonDeg,
	}
	// The east distance the caller measures uses a latitude between the
	// two positions, at most halfLatDeg poleward of p.
	poleDeg := math.Abs(p.LatDeg) + halfLatDeg
	if poleDeg >= 90 {
		sp.wholeLon = true
		return sp
	}
	sp.halfLonDeg = r / (minEastPerDegAtEquatorM * math.Cos(poleDeg*math.Pi/180))
	if !(sp.halfLonDeg < 180) {
		sp.wholeLon = true
	}
	return sp
}

// colRange is the unwrapped column range of band covered by sp, and
// whether it is every column.
func (sp span) colRange(n int) (lo, hi int, all bool) {
	if sp.wholeLon {
		return 0, n - 1, true
	}
	lo = colOf(sp.lonDeg-sp.halfLonDeg, n)
	hi = colOf(sp.lonDeg+sp.halfLonDeg, n)
	if hi-lo+1 >= n {
		return 0, n - 1, true
	}
	return lo, hi, false
}

// Near returns every id whose position may be within radiusM of p: a
// superset of those within the radius by the tangent-plane distance
// (geodesy.LocalOffsetAboutMidLatM), widened by 1 % for other exact
// metrics. The caller computes the exact distance and excludes p's own
// id. The order is unspecified. An invalid p, or a radius that is NaN,
// negative or infinite, returns nil and is counted.
func (g *Grid) Near(p core.LatLon, radiusM float64) []string {
	return g.AppendNear(nil, p, radiusM)
}

// AppendNear is Near appending to dst, so a caller can reuse a buffer and
// avoid the allocation.
func (g *Grid) AppendNear(dst []string, p core.LatLon, radiusM float64) []string {
	if !p.Valid() || !(radiusM >= 0) || math.IsInf(radiusM, 1) {
		g.counters.Inc(CounterGridRejectedQuery)
		return dst
	}
	if len(g.cells) == 0 {
		return dst
	}
	sp := g.spanOf(p, radiusM)
	if g.costOf(sp) > len(g.cells) {
		return g.appendScan(dst, sp)
	}
	for band := sp.bandLo; band <= sp.bandHi; band++ {
		n := g.columns(band)
		lo, hi, _ := sp.colRange(n)
		for c := lo; c <= hi; c++ {
			dst = append(dst, g.cells[cellKey{band: band, col: mod(c, n)}]...)
		}
	}
	return dst
}

// costOf is the number of cells a lookup over sp visits, stopping early
// once it exceeds the number of occupied cells.
func (g *Grid) costOf(sp span) int {
	limit := len(g.cells)
	if sp.bandHi-sp.bandLo+1 > limit {
		return limit + 1
	}
	cost := 0
	for band := sp.bandLo; band <= sp.bandHi && cost <= limit; band++ {
		n := g.columns(band)
		lo, hi, _ := sp.colRange(n)
		cost += hi - lo + 1
	}
	return cost
}

// appendScan answers a lookup that would visit more cells than are
// occupied by scanning the occupied cells instead: the work is bounded by
// the grid's size, whatever the radius and cell size.
func (g *Grid) appendScan(dst []string, sp span) []string {
	for key, ids := range g.cells {
		if key.band < sp.bandLo || key.band > sp.bandHi {
			continue
		}
		n := g.columns(key.band)
		lo, hi, all := sp.colRange(n)
		if !all && mod(key.col-lo, n) > hi-lo {
			continue
		}
		dst = append(dst, ids...)
	}
	return dst
}
