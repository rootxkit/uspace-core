package terrain

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/pgm"
)

func TestCellName(t *testing.T) {
	for _, c := range []struct {
		lat, lon float64
		want     string
	}{
		{41.7151, 44.8271, "N41E044"},
		{-0.5, -0.5, "S01W001"},
		{0, 0, "N00E000"},
		{-33.9, 151.2, "S34E151"},
		{-1, -1, "S01W001"},
		{-1e-12, 179.999, "S01E179"},
		{90, 180, "N90E180"},
		{-90, -180, "S90W180"},
		{9.99, -99.5, "N09W100"},
	} {
		if got := CellName(core.LatLon{LatDeg: c.lat, LonDeg: c.lon}); got != c.want {
			t.Errorf("CellName(%v, %v) = %q, want %q", c.lat, c.lon, got, c.want)
		}
	}
	for _, p := range []core.LatLon{{LatDeg: math.NaN()}, {LonDeg: math.Inf(1)}, {LatDeg: 91}, {LonDeg: -181}} {
		if got := CellName(p); got != "" {
			t.Errorf("CellName(%+v) = %q, want \"\"", p, got)
		}
	}
}

func TestTileElevationSynthetic(t *testing.T) {
	tile := syntheticTile(t)
	if tile.Dataset() != "COP-DEM GLO-30" {
		t.Errorf("Dataset %q", tile.Dataset())
	}
	if got := tile.SpacingM(); math.Abs(got-27830) > 1e-9 {
		t.Errorf("SpacingM %v, want 0.25 * 111320", got)
	}
	// Every sample centre away from the nodata one reads its own value.
	for r := 0; r < 5; r++ {
		for c := 0; c < 5; c++ {
			p := core.LatLon{LatDeg: 42 - 0.25*float64(r), LonDeg: 44 + 0.25*float64(c)}
			got := tile.ElevationM(p)
			// The four corners are rows iy, iy+1 and columns ix, ix+1 with
			// the index clamped to n-2; a nodata corner gives nil even
			// with zero weight, as the reference does.
			iy, ix := min(r, 3), min(c, 3)
			wantNil := iy <= 1 && iy+1 >= 1 && ix <= 1 && ix+1 >= 1
			switch {
			case wantNil && got != nil:
				t.Errorf("sample (%d, %d) = %v, want nil (nodata corner)", r, c, *got)
			case !wantNil && got == nil:
				t.Errorf("sample (%d, %d) = nil, want %d", r, c, 400+10*r+c)
			case !wantNil && math.Abs(*got-float64(400+10*r+c)) > 1e-9:
				t.Errorf("sample (%d, %d) = %v", r, c, *got)
			}
		}
	}
	// Edge clamps: the far corner, and positions past each edge.
	for _, c := range []struct {
		lat, lon, want float64
	}{
		{41, 45, 444},
		{40, 46, 444},
		{41, 50, 444},
		{30, 45, 444},
		{41.5, 45.3, 424},
		{40.9, 44.75, 443},
	} {
		got := tile.ElevationM(core.LatLon{LatDeg: c.lat, LonDeg: c.lon})
		if got == nil || math.Abs(*got-c.want) > 1e-9 {
			t.Errorf("(%v, %v) = %v, want %v", c.lat, c.lon, got, c.want)
		}
	}
	for _, p := range []core.LatLon{{LatDeg: math.NaN(), LonDeg: 44}, {LatDeg: 42, LonDeg: math.Inf(-1)}} {
		if got := tile.ElevationM(p); got != nil {
			t.Errorf("%+v = %v, want nil", p, *got)
		}
	}
}

func TestParseTileRefusals(t *testing.T) {
	base := func(mod func(map[string]string)) []byte {
		h := map[string]string{
			"Dataset": "COP-DEM GLO-30", "Offset": "-500.0", "Scale": "0.2",
			"LatFirst": "42", "LonFirst": "44", "LatStep": "0.25", "LonStep": "0.25",
		}
		mod(h)
		var lines []pgm.HeaderLine
		for _, k := range []string{"Dataset", "Offset", "Scale", "LatFirst", "LonFirst", "LatStep", "LonStep"} {
			if v, ok := h[k]; ok {
				lines = append(lines, pgm.HeaderLine{Key: k, Value: v})
			}
		}
		return pgm.Encode(2, 2, lines, nil)
	}
	cases := []struct {
		name  string
		data  []byte
		field string
	}{
		{"not a pgm", []byte("P5\n"), "pgm["},
		{"no offset", base(func(h map[string]string) { delete(h, "Offset") }), "header.Offset"},
		{"other offset", base(func(h map[string]string) { h["Offset"] = "-108" }), "header.Offset"},
		{"no scale", base(func(h map[string]string) { delete(h, "Scale") }), "header.Scale"},
		{"other scale", base(func(h map[string]string) { h["Scale"] = "0.003" }), "header.Scale"},
		{"no dataset", base(func(h map[string]string) { delete(h, "Dataset") }), "header.Dataset"},
		{"no latfirst", base(func(h map[string]string) { delete(h, "LatFirst") }), "header.LatFirst"},
		{"no lonfirst", base(func(h map[string]string) { delete(h, "LonFirst") }), "header.LonFirst"},
		{"no latstep", base(func(h map[string]string) { delete(h, "LatStep") }), "header.LatStep"},
		{"no lonstep", base(func(h map[string]string) { delete(h, "LonStep") }), "header.LonStep"},
		{"zero latstep", base(func(h map[string]string) { h["LatStep"] = "0" }), "header.LatStep"},
		{"negative lonstep", base(func(h map[string]string) { h["LonStep"] = "-0.25" }), "header.LonStep"},
		{"text latfirst", base(func(h map[string]string) { h["LatFirst"] = "north" }), "header.LatFirst"},
		{"one row", pgm.Encode(2, 1, []pgm.HeaderLine{
			{Key: "Dataset", Value: "d"}, {Key: "Offset", Value: "-500"}, {Key: "Scale", Value: "0.2"},
			{Key: "LatFirst", Value: "1"}, {Key: "LonFirst", Value: "1"}, {Key: "LatStep", Value: "1"}, {Key: "LonStep", Value: "1"},
		}, nil), "size"},
	}
	for _, c := range cases {
		tile, err := ParseTile(c.data)
		var fe *core.FieldError
		if tile != nil || !errors.As(err, &fe) || !strings.HasPrefix(fe.Field, c.field) {
			t.Errorf("%s: tile %v error %v, want field %s", c.name, tile != nil, err, c.field)
		}
	}
	// E-01 twin: the unmodified header is accepted.
	if _, err := ParseTile(base(func(map[string]string) {})); err != nil {
		t.Errorf("valid 2 x 2 tile refused: %v", err)
	}
}
