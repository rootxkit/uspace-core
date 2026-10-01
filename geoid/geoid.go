package geoid

import (
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/pgm"
)

// Undulator gives the geoid undulation N (metres above the WGS84
// ellipsoid) at a position. *Grid implements it; zones and rid take it.
type Undulator interface {
	UndulationM(p core.LatLon) (float64, error)
}

// Grid is a GeographicLib geoid grid: row 0 at latitude +90, rows running
// south to -90 (an odd number of rows, so the equator is one), column 0 at
// longitude 0, columns running east through 360 (an even number), stored
// value v meaning Offset + Scale*v metres. A Grid is immutable and safe
// for concurrent use.
type Grid struct {
	grid     *pgm.Grid
	offsetM  float64
	scaleM   float64
	width    float64 // columns, spanning 360 deg
	rows     float64 // height-1 row intervals, spanning 180 deg
	halfRows int     // (height-1)/2: the equator's row
}

// Parse reads a geoid grid from the bytes of a GeographicLib .pgm file.
// It refuses what internal/pgm refuses, a missing or non-positive Scale, a
// missing Offset, and a raster that is not an even number of columns by an
// odd number of rows (at least 2 x 3). Errors are *core.FieldError.
func Parse(data []byte) (*Grid, error) {
	g, err := pgm.Parse(data, pgm.DefaultMaxBytes)
	if err != nil {
		return nil, err
	}
	offset, err := g.Number("Offset")
	if err != nil {
		return nil, err
	}
	scale, err := g.Number("Scale")
	if err != nil {
		return nil, err
	}
	if scale <= 0 {
		return nil, core.Fieldf("header.Scale", "%v is not positive", scale)
	}
	if g.Width < 2 || g.Width%2 != 0 {
		return nil, core.Fieldf("width", "%d columns, want an even number of at least 2 spanning 360 deg", g.Width)
	}
	if g.Height < 3 || g.Height%2 != 1 {
		return nil, core.Fieldf("height", "%d rows, want an odd number of at least 3 from +90 to -90", g.Height)
	}
	return &Grid{
		grid:     g,
		offsetM:  offset,
		scaleM:   scale,
		width:    float64(g.Width),
		rows:     float64(g.Height - 1),
		halfRows: (g.Height - 1) / 2,
	}, nil
}

// maxFileBytes bounds what Load reads: the sample bound plus room for the
// header lines.
const maxFileBytes = pgm.DefaultMaxBytes + 1<<20

// Load reads and parses the geoid grid file at path (for example
// egm2008-2_5.pgm from GeographicLib's distribution).
func Load(path string) (*Grid, error) {
	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // G703: the path is the caller's configured grid file; opening it is the purpose
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file; a close error changes nothing read
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileBytes {
		return nil, core.Fieldf("file", "larger than %d bytes", maxFileBytes)
	}
	return Parse(data)
}

// Description returns the grid's own "# Description" header line, for
// example "WGS84 EGM2008, 2.5-minute grid", or "" when it has none.
func (g *Grid) Description() string {
	return g.grid.Header["Description"]
}

// UndulationM returns N, the height of the geoid above the WGS84
// ellipsoid in metres, at p: bilinear between the four surrounding
// samples, as GeographicLib's Geoid class computes it without the cubic
// option. Longitude may be any finite number and wraps (column Width is
// column 0); latitude must lie in [-90, 90], and exactly +-90 reads the
// edge row. The error is a *core.FieldError naming lat_deg or lon_deg.
func (g *Grid) UndulationM(p core.LatLon) (float64, error) {
	if !core.IsFinite(p.LatDeg) || p.LatDeg < -90 || p.LatDeg > 90 {
		return 0, core.Fieldf("lat_deg", "%v is outside [-90, 90]", p.LatDeg)
	}
	if !core.IsFinite(p.LonDeg) {
		return 0, core.Fieldf("lon_deg", "%v is not finite", p.LonDeg)
	}
	lon := math.Mod(p.LonDeg, 360)
	if lon < 0 {
		lon += 360
	}
	// Multiply before dividing, as the reference implementation does: a
	// sample's own longitude then lands exactly on its column.
	fx := lon * g.width / 360
	fy := -p.LatDeg * g.rows / 180
	ix := int(math.Floor(fx))
	iy := min(g.halfRows-1, int(math.Floor(fy)))
	fx -= float64(ix)
	fy -= float64(iy)
	iy += g.halfRows
	w := g.grid.Width
	ix %= w
	ix1 := (ix + 1) % w
	v00 := float64(g.grid.Raw(ix, iy))
	v01 := float64(g.grid.Raw(ix1, iy))
	v10 := float64(g.grid.Raw(ix, iy+1))
	v11 := float64(g.grid.Raw(ix1, iy+1))
	a := (1-fx)*v00 + fx*v01
	b := (1-fx)*v10 + fx*v11
	return g.offsetM + g.scaleM*((1-fy)*a+fy*b), nil
}

// AMSLFromHAE converts a height above the WGS84 ellipsoid into a height
// above mean sea level: alt_amsl_m = alt_hae_m - N (LESSONS R-07).
func AMSLFromHAE(altHAEM, undulationM float64) float64 {
	return altHAEM - undulationM
}

// HAEFromAMSL converts a height above mean sea level into a height above
// the WGS84 ellipsoid: alt_hae_m = alt_amsl_m + N.
func HAEFromAMSL(altAMSLM, undulationM float64) float64 {
	return altAMSLM + undulationM
}
