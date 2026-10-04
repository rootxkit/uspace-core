package terrain

import (
	"math"
	"strconv"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/pgm"
)

// The tile format written by the lab's terrain fetch tool: elevation =
// OffsetM + ScaleM*raw, NoData reserved, and the dataset name used for a
// cell the index marks as sea.
const (
	OffsetM    = -500.0
	ScaleM     = 0.2
	NoData     = 0xFFFF
	SeaDataset = "sea"
)

// metresPerDegLat converts a latitude step into the sample spacing along
// a meridian, as the reference implementation reports it.
const metresPerDegLat = 111_320.0

// Attribution is the notice the Copernicus DEM licence requires wherever
// elevations derived from it are shown (LESSONS D-05).
const Attribution = "Produced using Copernicus WorldDEM-30 (c) DLR e.V. 2010-2014 and (c) Airbus Defence and Space GmbH 2014-2018 provided under COPERNICUS by the European Union and ESA; all rights reserved."

// CellName names the 1 x 1 degree cell holding p after its south-west
// corner, by floor (not truncation): 41.7N 44.8E is "N41E044", 0.5S 0.5W
// is "S01W001". It returns "" for a position that is not valid
// (core.LatLon.Valid), which no index holds.
func CellName(p core.LatLon) string {
	if !p.Valid() {
		return ""
	}
	lat := int(math.Floor(p.LatDeg))
	lon := int(math.Floor(p.LonDeg))
	b := make([]byte, 0, 7)
	if lat >= 0 {
		b = append(b, 'N')
	} else {
		b = append(b, 'S')
		lat = -lat
	}
	b = appendPadded(b, lat, 2)
	if lon >= 0 {
		b = append(b, 'E')
	} else {
		b = append(b, 'W')
		lon = -lon
	}
	b = appendPadded(b, lon, 3)
	return string(b)
}

func appendPadded(b []byte, n, width int) []byte {
	s := strconv.Itoa(n)
	for i := len(s); i < width; i++ {
		b = append(b, '0')
	}
	return append(b, s...)
}

// Tile is one DEM tile: a grid of samples whose first (north-west) sample
// is at LatFirst, LonFirst, rows running south by LatStep and columns
// east by LonStep. A Tile is immutable and safe for concurrent use.
type Tile struct {
	grid        *pgm.Grid
	dataset     string
	latFirstDeg float64
	lonFirstDeg float64
	latStepDeg  float64
	lonStepDeg  float64
}

// ParseTile reads a tile in the lab tool's PGM format. It refuses what
// internal/pgm refuses, an Offset or Scale other than OffsetM and ScaleM,
// a missing Dataset, missing georeferencing (LatFirst, LonFirst, LatStep,
// LonStep), a step that is not positive and a raster smaller than 2 x 2.
// Errors are *core.FieldError naming the header key or byte.
func ParseTile(data []byte) (*Tile, error) {
	g, err := pgm.Parse(data, pgm.DefaultMaxBytes)
	if err != nil {
		return nil, err
	}
	return tileFromGrid(g)
}

// tileFromGrid applies the tile checks of ParseTile to a parsed PGM.
func tileFromGrid(g *pgm.Grid) (*Tile, error) {
	offset, err := g.Number("Offset")
	if err != nil {
		return nil, err
	}
	if offset != OffsetM {
		return nil, core.Fieldf("header.Offset", "%v, this format's is %v", offset, OffsetM)
	}
	scale, err := g.Number("Scale")
	if err != nil {
		return nil, err
	}
	if scale != ScaleM {
		return nil, core.Fieldf("header.Scale", "%v, this format's is %v", scale, ScaleM)
	}
	dataset := g.Header["Dataset"]
	if dataset == "" {
		return nil, core.Fieldf("header.Dataset", "missing")
	}
	t := &Tile{grid: g, dataset: dataset}
	for _, f := range []struct {
		key string
		dst *float64
	}{
		{"LatFirst", &t.latFirstDeg}, {"LonFirst", &t.lonFirstDeg},
		{"LatStep", &t.latStepDeg}, {"LonStep", &t.lonStepDeg},
	} {
		v, err := g.Number(f.key)
		if err != nil {
			return nil, err
		}
		*f.dst = v
	}
	if t.latStepDeg <= 0 {
		return nil, core.Fieldf("header.LatStep", "%v is not positive", t.latStepDeg)
	}
	if t.lonStepDeg <= 0 {
		return nil, core.Fieldf("header.LonStep", "%v is not positive", t.lonStepDeg)
	}
	if g.Width < 2 || g.Height < 2 {
		return nil, core.Fieldf("size", "%d x %d samples, want at least 2 x 2", g.Width, g.Height)
	}
	return t, nil
}

// ElevationM returns the elevation in metres at p, bilinear between the
// four surrounding samples, or nil when it is unknown: p is not finite,
// or any of the four samples is NoData (never zero, LESSONS D-04).
// Positions are clamped into the tile: past the last sample the edge
// sample is used (Copernicus drops each tile's shared east and south
// edge), before the first sample the first one.
func (t *Tile) ElevationM(p core.LatLon) *float64 {
	if !core.IsFinite(p.LatDeg) || !core.IsFinite(p.LonDeg) {
		return nil
	}
	w, h := t.grid.Width, t.grid.Height
	fy := (t.latFirstDeg - p.LatDeg) / t.latStepDeg
	fx := (p.LonDeg - t.lonFirstDeg) / t.lonStepDeg
	fy = min(max(fy, 0), float64(h-1))
	fx = min(max(fx, 0), float64(w-1))
	iy := min(int(fy), h-2)
	ix := min(int(fx), w-2)
	fy -= float64(iy)
	fx -= float64(ix)
	v00 := t.grid.Raw(ix, iy)
	v01 := t.grid.Raw(ix+1, iy)
	v10 := t.grid.Raw(ix, iy+1)
	v11 := t.grid.Raw(ix+1, iy+1)
	if v00 == NoData || v01 == NoData || v10 == NoData || v11 == NoData {
		return nil
	}
	// Every product is rounded by an explicit float64 conversion before it
	// is added: the Go spec lets a compiler fuse x*y + z into one
	// multiply-add (arm64 does; amd64 does not), which skips that rounding
	// and moves the answer by an ulp (400.00000000000006 for 400). The
	// conversions forbid the fusion, so every architecture returns what
	// amd64 always has.
	top := float64((1-fx)*float64(v00)) + float64(fx*float64(v01))
	bottom := float64((1-fx)*float64(v10)) + float64(fx*float64(v11))
	e := OffsetM + float64(ScaleM*(float64((1-fy)*top)+float64(fy*bottom)))
	return &e
}

// SpacingM is the distance between samples along a meridian in metres
// (about 30 for GLO-30, 90 for GLO-90).
func (t *Tile) SpacingM() float64 { return t.latStepDeg * metresPerDegLat }

// Dataset names the source of the tile, for example "COP-DEM GLO-30".
func (t *Tile) Dataset() string { return t.dataset }
