package cell

import (
	"math"
	"strconv"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/geodesy"
)

// Level is the size of a cell. The zero Level is invalid.
type Level int

// The two levels of spec 05 §3 (M35).
const (
	// Level3 is the 1 x 1 degree cell, named "c3".
	Level3 Level = 3
	// Level5 is the 0.1 x 0.1 degree cell, named "c5".
	Level5 Level = 5
)

// MaxCoverDefault is the cell count a caller of Cover passes when it has
// no tighter bound: room for a continent-sized query at Level5 with a
// hard stop.
const MaxCoverDefault = 10_000

// perDeg is the number of cells per degree of the level, 0 if invalid.
func (l Level) perDeg() int {
	switch l {
	case Level5:
		return 10
	case Level3:
		return 1
	}
	return 0
}

// Valid reports whether l is Level5 or Level3.
func (l Level) Valid() bool { return l.perDeg() != 0 }

// StepDeg is the side of a cell in degrees: 0.1 for Level5, 1 for
// Level3, 0 for an invalid level.
func (l Level) StepDeg() float64 {
	switch l {
	case Level5:
		return 0.1
	case Level3:
		return 1
	}
	return 0
}

// prefix is the name prefix of the level, "" if invalid.
func (l Level) prefix() string {
	switch l {
	case Level5:
		return "c5"
	case Level3:
		return "c3"
	}
	return ""
}

func (l Level) rows() int { return 180 * l.perDeg() }
func (l Level) cols() int { return 360 * l.perDeg() }

// latEdge is the south edge of row i in degrees: the nearest float64 to
// the decimal (i - 90n) / n.
func (l Level) latEdge(i int) float64 {
	n := l.perDeg()
	return float64(i-90*n) / float64(n)
}

// lonEdge is the west edge of column j in degrees.
func (l Level) lonEdge(j int) float64 {
	n := l.perDeg()
	return float64(j-180*n) / float64(n)
}

// latIndex is the row of a latitude in [-90, 90] at a valid level:
// floored, corrected by one against the row's own edges, and clamped so
// that +90 is in the last row.
func (l Level) latIndex(latDeg float64) int {
	n := l.perDeg()
	i := int(math.Floor(latDeg*float64(n))) + 90*n
	if latDeg < l.latEdge(i) {
		i--
	} else if latDeg >= l.latEdge(i+1) {
		i++
	}
	return clamp(i, l.rows()-1)
}

// lonIndex is the column of a longitude in [-180, 180) at a valid level.
func (l Level) lonIndex(lonDeg float64) int {
	n := l.perDeg()
	j := int(math.Floor(lonDeg*float64(n))) + 180*n
	if lonDeg < l.lonEdge(j) {
		j--
	} else if lonDeg >= l.lonEdge(j+1) {
		j++
	}
	return clamp(j, l.cols()-1)
}

func clamp(i, last int) int {
	if i < 0 {
		return 0
	}
	if i > last {
		return last
	}
	return i
}

// normLonDeg brings a finite longitude into [-180, 180). A longitude
// already in range is returned unchanged (core.WrapLonDeg would round
// it through an addition); 180 is -180.
func normLonDeg(lonDeg float64) float64 {
	if lonDeg < -180 || lonDeg > 180 {
		lonDeg = core.WrapLonDeg(lonDeg)
	}
	if lonDeg == 180 {
		return -180
	}
	return lonDeg
}

// ID is one cell: its level and its row and column indexes. The zero ID
// is invalid.
type ID struct {
	Level  Level
	LatIdx int
	LonIdx int
}

// Of returns the cell of level l that holds p. The latitude must be
// finite and in [-90, 90]; a finite longitude outside [-180, 180) is
// wrapped. An invalid level or position is refused with a
// *core.FieldError naming "level", "lat_deg" or "lon_deg".
func Of(p core.LatLon, l Level) (ID, error) {
	if !l.Valid() {
		return ID{}, core.Fieldf("level", "unknown cell level %d", int(l))
	}
	if !core.IsFinite(p.LatDeg) || p.LatDeg < -90 || p.LatDeg > 90 {
		return ID{}, core.Fieldf("lat_deg", "must be finite and in [-90, 90], got %v", p.LatDeg)
	}
	if !core.IsFinite(p.LonDeg) {
		return ID{}, core.Fieldf("lon_deg", "must be finite, got %v", p.LonDeg)
	}
	return ID{Level: l, LatIdx: l.latIndex(p.LatDeg), LonIdx: l.lonIndex(normLonDeg(p.LonDeg))}, nil
}

// Valid reports whether the level is valid and both indexes are in range
// for it.
func (c ID) Valid() bool {
	return c.Level.Valid() &&
		c.LatIdx >= 0 && c.LatIdx < c.Level.rows() &&
		c.LonIdx >= 0 && c.LonIdx < c.Level.cols()
}

// String is the cell's name, "c5:<lat_idx>:<lon_idx>" or
// "c3:<lat_idx>:<lon_idx>"; "" for an invalid ID.
func (c ID) String() string {
	if !c.Valid() {
		return ""
	}
	var buf [16]byte
	b := append(buf[:0], c.Level.prefix()...)
	b = append(b, ':')
	b = strconv.AppendInt(b, int64(c.LatIdx), 10)
	b = append(b, ':')
	b = strconv.AppendInt(b, int64(c.LonIdx), 10)
	return string(b)
}

// maxNameLen bounds a name before it is read: "c5:1799:3599".
const maxNameLen = len("c5:1799:3599")

// Parse reads a name String produced and refuses every other spelling
// with a *core.FieldError naming "cell": an unknown or upper-case
// prefix, a sign, a leading zero, a space, a missing or extra part, or
// an index out of range for the level. Parse(c.String()) == c for every
// valid c.
func Parse(s string) (ID, error) {
	if len(s) > maxNameLen {
		return ID{}, core.Fieldf("cell", "%d bytes, longer than any cell name (%d)", len(s), maxNameLen)
	}
	var l Level
	switch {
	case len(s) >= 3 && s[:3] == "c5:":
		l = Level5
	case len(s) >= 3 && s[:3] == "c3:":
		l = Level3
	default:
		return ID{}, core.Fieldf("cell", "%q does not start with \"c5:\" or \"c3:\"", s)
	}
	rest := s[3:]
	latIdx, rest, ok := parseIndex(rest)
	if !ok || len(rest) == 0 || rest[0] != ':' {
		return ID{}, core.Fieldf("cell", "%q: the row is not a decimal index followed by ':'", s)
	}
	lonIdx, rest, ok := parseIndex(rest[1:])
	if !ok || len(rest) != 0 {
		return ID{}, core.Fieldf("cell", "%q: the column is not a decimal index ending the name", s)
	}
	c := ID{Level: l, LatIdx: latIdx, LonIdx: lonIdx}
	if !c.Valid() {
		return ID{}, core.Fieldf("cell", "%q: index out of range for %s (rows 0..%d, columns 0..%d)", s, l.prefix(), l.rows()-1, l.cols()-1)
	}
	return c, nil
}

// parseIndex reads "0" or a non-zero digit followed by digits from the
// start of s and returns the rest. Parse's length bound keeps the value
// far from overflow.
func parseIndex(s string) (v int, rest string, ok bool) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		v = v*10 + int(s[i]-'0')
		i++
	}
	if i == 0 || (i > 1 && s[0] == '0') {
		return 0, s, false
	}
	return v, s[i:], true
}

// Parent is the Level3 cell that holds c; a Level3 cell returns itself
// and an invalid ID returns the zero ID.
func (c ID) Parent() ID {
	if !c.Valid() {
		return ID{}
	}
	if c.Level == Level3 {
		return c
	}
	return ID{Level: Level3, LatIdx: c.LatIdx / 10, LonIdx: c.LonIdx / 10}
}

// Children are the 100 Level5 cells of a Level3 cell in row-major order
// (south to north, then west to east); a Level5 cell or an invalid ID
// returns nil.
func (c ID) Children() []ID {
	if !c.Valid() || c.Level != Level3 {
		return nil
	}
	out := make([]ID, 0, 100)
	for i := c.LatIdx * 10; i < c.LatIdx*10+10; i++ {
		for j := c.LonIdx * 10; j < c.LonIdx*10+10; j++ {
			out = append(out, ID{Level: Level5, LatIdx: i, LonIdx: j})
		}
	}
	return out
}

// Ring1 are the cells sharing an edge or a corner with c, sorted by
// (LatIdx, LonIdx): eight, or five on the first and last rows. Longitude
// wraps (the last column neighbours the first); latitude does not. The
// cell itself is never in the ring. An invalid ID returns nil.
func (c ID) Ring1() []ID {
	if !c.Valid() {
		return nil
	}
	last := c.Level.cols() - 1
	var cols [3]int
	switch c.LonIdx {
	case 0:
		cols = [3]int{0, 1, last}
	case last:
		cols = [3]int{0, last - 1, last}
	default:
		cols = [3]int{c.LonIdx - 1, c.LonIdx, c.LonIdx + 1}
	}
	n := 8
	if c.LatIdx == 0 || c.LatIdx == c.Level.rows()-1 {
		n = 5
	}
	out := make([]ID, 0, n)
	for i := c.LatIdx - 1; i <= c.LatIdx+1; i++ {
		if i < 0 || i >= c.Level.rows() {
			continue
		}
		for _, j := range cols {
			if i == c.LatIdx && j == c.LonIdx {
				continue
			}
			out = append(out, ID{Level: c.Level, LatIdx: i, LonIdx: j})
		}
	}
	return out
}

// emptyBBox contains nothing (MinLat > MaxLat), as geodesy's own.
var emptyBBox = geodesy.BBox{MinLat: math.Inf(1), MinLon: math.Inf(1), MaxLat: math.Inf(-1), MaxLon: math.Inf(-1)}

// BBox is the cell's extent in degrees, holding [MinLat, MaxLat) x
// [MinLon, MaxLon) (the last row also holds latitude 90). Each edge is
// the nearest float64 to its decimal value; MaxLon is 180 exactly for
// the last column. An invalid ID gives an empty box.
func (c ID) BBox() geodesy.BBox {
	if !c.Valid() {
		return emptyBBox
	}
	return geodesy.BBox{
		MinLat: c.Level.latEdge(c.LatIdx),
		MinLon: c.Level.lonEdge(c.LonIdx),
		MaxLat: c.Level.latEdge(c.LatIdx + 1),
		MaxLon: c.Level.lonEdge(c.LonIdx + 1),
	}
}

// Centre is the middle of the cell in degrees. An invalid ID gives NaN
// coordinates, which core.LatLon.Valid refuses.
func (c ID) Centre() core.LatLon {
	if !c.Valid() {
		return core.LatLon{LatDeg: math.NaN(), LonDeg: math.NaN()}
	}
	n := c.Level.perDeg()
	return core.LatLon{
		LatDeg: float64(2*(c.LatIdx-90*n)+1) / float64(2*n),
		LonDeg: float64(2*(c.LonIdx-180*n)+1) / float64(2*n),
	}
}

// colRange is an inclusive range of columns.
type colRange struct{ lo, hi int }

// Cover returns every cell of level l that intersects b (edges
// included), sorted by (LatIdx, LonIdx). A box with MinLon > MaxLon
// crosses the antimeridian and is covered as two longitude ranges; a
// box reaching MaxLon == 180 includes the last column once (not the
// first). The box must have finite edges, latitudes in [-90, 90] with
// MinLat <= MaxLat, and longitudes in [-180, 180].
//
// The count is computed before anything is allocated: more than
// maxCells cells is refused with a *core.FieldError naming "bbox" and a
// nil slice (E-10); pass MaxCoverDefault without a tighter bound. A
// maxCells below 1 is refused naming "max", an invalid level naming
// "level".
func Cover(b geodesy.BBox, l Level, maxCells int) ([]ID, error) {
	if !l.Valid() {
		return nil, core.Fieldf("level", "unknown cell level %d", int(l))
	}
	if maxCells < 1 {
		return nil, core.Fieldf("max", "must be at least 1, got %d", maxCells)
	}
	if !core.IsFinite(b.MinLat) || !core.IsFinite(b.MaxLat) || !core.IsFinite(b.MinLon) || !core.IsFinite(b.MaxLon) {
		return nil, core.Fieldf("bbox", "edges must be finite, got %+v", b)
	}
	if b.MinLat < -90 || b.MaxLat > 90 || b.MinLat > b.MaxLat {
		return nil, core.Fieldf("bbox", "latitudes must satisfy -90 <= min_lat <= max_lat <= 90, got %v..%v", b.MinLat, b.MaxLat)
	}
	if b.MinLon < -180 || b.MinLon > 180 || b.MaxLon < -180 || b.MaxLon > 180 {
		return nil, core.Fieldf("bbox", "longitudes must be in [-180, 180], got %v..%v", b.MinLon, b.MaxLon)
	}
	loLat, hiLat := l.latIndex(b.MinLat), l.latIndex(b.MaxLat)

	var ranges [2]colRange
	var nr int
	if b.MinLon <= b.MaxLon {
		ranges[0] = colRange{l.coverLonIndex(b.MinLon), l.coverLonIndex(b.MaxLon)}
		nr = 1
	} else {
		// Crossing the antimeridian: [-180, MaxLon] then [MinLon, 180].
		west, east := l.coverLonIndex(b.MaxLon), l.coverLonIndex(b.MinLon)
		if east <= west+1 {
			ranges[0] = colRange{0, l.cols() - 1}
			nr = 1
		} else {
			ranges[0] = colRange{0, west}
			ranges[1] = colRange{east, l.cols() - 1}
			nr = 2
		}
	}
	perRow := 0
	for _, r := range ranges[:nr] {
		perRow += r.hi - r.lo + 1
	}
	count := (hiLat - loLat + 1) * perRow
	if count > maxCells {
		return nil, core.Fieldf("bbox", "covers %d cells at %s, more than the maximum of %d", count, l.prefix(), maxCells)
	}
	out := make([]ID, 0, count)
	for i := loLat; i <= hiLat; i++ {
		for _, r := range ranges[:nr] {
			for j := r.lo; j <= r.hi; j++ {
				out = append(out, ID{Level: l, LatIdx: i, LonIdx: j})
			}
		}
	}
	return out, nil
}

// coverLonIndex is the column of a box edge in [-180, 180]: 180 is the
// east edge of the last column, not the west edge of the first.
func (l Level) coverLonIndex(lonDeg float64) int {
	if lonDeg == 180 {
		return l.cols() - 1
	}
	return l.lonIndex(lonDeg)
}
