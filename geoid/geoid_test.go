package geoid

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/pgm"
)

// synthetic builds terrain_geoid.json's synthetic grid: 36 x 19 at 10 deg,
// Offset -100, Scale 0.01, sample(row, col) = 1000 + 100*row + col.
func synthetic() []byte {
	s := make([]uint16, 36*19)
	for r := 0; r < 19; r++ {
		for c := 0; c < 36; c++ {
			s[r*36+c] = uint16(1000 + 100*r + c)
		}
	}
	return pgm.Encode(36, 19, []pgm.HeaderLine{
		{Key: "Description", Value: "synthetic 10-degree grid"},
		{Key: "Offset", Value: "-100"},
		{Key: "Scale", Value: "0.01"},
	}, s)
}

func mustParse(t testing.TB, data []byte) *Grid {
	t.Helper()
	g, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// sample is the synthetic grid's N at (row, col).
func sample(row, col int) float64 { return -100 + 0.01*float64(1000+100*row+col) }

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s: got %v, want %v", what, got, want)
	}
}

func TestUndulationAtSamples(t *testing.T) {
	g := mustParse(t, synthetic())
	for row := 0; row < 19; row++ {
		for col := 0; col < 36; col++ {
			p := core.LatLon{LatDeg: 90 - 10*float64(row), LonDeg: 10 * float64(col)}
			n, err := g.UndulationM(p)
			if err != nil {
				t.Fatal(err)
			}
			near(t, "sample", n, sample(row, col))
		}
	}
}

func TestUndulationWrapsAt360(t *testing.T) {
	g := mustParse(t, synthetic())
	// Halfway between the last column (350) and column 0 (360 = 0).
	want := (sample(9, 35) + sample(9, 0)) / 2
	for _, lon := range []float64{355, -5, 715, -365, 355 - 3600} {
		n, err := g.UndulationM(core.LatLon{LatDeg: 0, LonDeg: lon})
		if err != nil {
			t.Fatal(err)
		}
		near(t, "lon "+ftoa(lon), n, want)
	}
	// 360 is column 0 exactly, and so is -0.
	for _, lon := range []float64{360, 0, math.Copysign(0, -1), -360} {
		n, err := g.UndulationM(core.LatLon{LatDeg: 0, LonDeg: lon})
		if err != nil {
			t.Fatal(err)
		}
		near(t, "lon "+ftoa(lon), n, sample(9, 0))
	}
	// Just below 360 approaches column 0 from the last column.
	n, err := g.UndulationM(core.LatLon{LatDeg: 0, LonDeg: -1e-12})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "lon -1e-12", n, sample(9, 0))
}

func TestUndulationPoles(t *testing.T) {
	g := mustParse(t, synthetic())
	for _, c := range []struct {
		lat  float64
		row  int
		name string
	}{{90, 0, "north"}, {-90, 18, "south"}} {
		for _, lon := range []float64{0, 5, 123.4, 355} {
			n, err := g.UndulationM(core.LatLon{LatDeg: c.lat, LonDeg: lon})
			if err != nil {
				t.Fatal(err)
			}
			col := int(lon / 10)
			fx := lon/10 - float64(col)
			want := (1-fx)*sample(c.row, col) + fx*sample(c.row, (col+1)%36)
			near(t, c.name+" pole lon "+ftoa(lon), n, want)
		}
	}
	// Just inside the poles interpolates with the next row.
	n, err := g.UndulationM(core.LatLon{LatDeg: -85, LonDeg: 0})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "-85", n, (sample(17, 0)+sample(18, 0))/2)
}

func TestUndulationRefusesOutsideRange(t *testing.T) {
	g := mustParse(t, synthetic())
	for _, c := range []struct {
		p     core.LatLon
		field string
	}{
		{core.LatLon{LatDeg: 90.000001}, "lat_deg"},
		{core.LatLon{LatDeg: -90.5}, "lat_deg"},
		{core.LatLon{LatDeg: math.NaN()}, "lat_deg"},
		{core.LatLon{LatDeg: math.Inf(1)}, "lat_deg"},
		{core.LatLon{LonDeg: math.NaN()}, "lon_deg"},
		{core.LatLon{LonDeg: math.Inf(-1)}, "lon_deg"},
	} {
		_, err := g.UndulationM(c.p)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%+v: error %v, want field %s", c.p, err, c.field)
		}
	}
	// A huge but finite longitude still answers.
	if _, err := g.UndulationM(core.LatLon{LatDeg: 1, LonDeg: 1e300}); err != nil {
		t.Errorf("finite longitude refused: %v", err)
	}
}

func TestDescription(t *testing.T) {
	if got := mustParse(t, synthetic()).Description(); got != "synthetic 10-degree grid" {
		t.Errorf("Description %q", got)
	}
	bare := pgm.Encode(2, 3, []pgm.HeaderLine{{Key: "Offset", Value: "0"}, {Key: "Scale", Value: "1"}}, nil)
	if got := mustParse(t, bare).Description(); got != "" {
		t.Errorf("no Description line gave %q", got)
	}
}

func TestHAEAMSLInverse(t *testing.T) {
	for _, c := range []struct{ hae, n float64 }{{100, 15.9}, {0, 22.5}, {-50, -30.15}, {1234.5, 0}} {
		amsl := AMSLFromHAE(c.hae, c.n)
		near(t, "amsl", amsl, c.hae-c.n)
		near(t, "inverse", HAEFromAMSL(amsl, c.n), c.hae)
	}
	// Tbilisi: an HAE of 515.9 m is 500 m AMSL with N = 15.9 m.
	near(t, "Tbilisi", AMSLFromHAE(515.9, 15.9), 500)
}

func TestParseRefusals(t *testing.T) {
	hdr := func(off, scale string) []pgm.HeaderLine {
		var h []pgm.HeaderLine
		if off != "" {
			h = append(h, pgm.HeaderLine{Key: "Offset", Value: off})
		}
		if scale != "" {
			h = append(h, pgm.HeaderLine{Key: "Scale", Value: scale})
		}
		return h
	}
	cases := []struct {
		name  string
		data  []byte
		field string
	}{
		{"not a pgm", []byte("P2\n"), "pgm[0]"},
		{"no offset", pgm.Encode(2, 3, hdr("", "1"), nil), "header.Offset"},
		{"no scale", pgm.Encode(2, 3, hdr("0", ""), nil), "header.Scale"},
		{"zero scale", pgm.Encode(2, 3, hdr("0", "0"), nil), "header.Scale"},
		{"negative scale", pgm.Encode(2, 3, hdr("0", "-1"), nil), "header.Scale"},
		{"odd columns", pgm.Encode(3, 3, hdr("0", "1"), nil), "width"},
		{"one column", pgm.Encode(1, 3, hdr("0", "1"), nil), "width"},
		{"even rows", pgm.Encode(2, 4, hdr("0", "1"), nil), "height"},
		{"one row", pgm.Encode(2, 1, hdr("0", "1"), nil), "height"},
	}
	for _, c := range cases {
		g, err := Parse(c.data)
		var fe *core.FieldError
		if g != nil || !errors.As(err, &fe) || !strings.HasPrefix(fe.Field, c.field) {
			t.Errorf("%s: grid %v error %v, want field %s", c.name, g != nil, err, c.field)
		}
	}
	// The smallest accepted grid (E-01 twin).
	if _, err := Parse(pgm.Encode(2, 3, hdr("0", "1"), nil)); err != nil {
		t.Errorf("2 x 3 grid refused: %v", err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "synthetic.pgm")
	if err := os.WriteFile(path, synthetic(), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	n, err := g.UndulationM(core.LatLon{})
	if err != nil {
		t.Fatal(err)
	}
	near(t, "equator", n, -81)
	if _, err := Load(filepath.Join(dir, "absent.pgm")); err == nil {
		t.Error("a missing file loaded")
	}
	if _, err := Load(dir); err == nil {
		t.Error("a directory loaded")
	}
	bad := filepath.Join(dir, "bad.pgm")
	if err := os.WriteFile(bad, []byte("P5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil {
		t.Error("a malformed file loaded")
	}
}

// TestRealGrids documents the R-07 figures (Tbilisi 15.9 m, Batumi
// 22.5 m on EGM2008) and asserts them only with GeographicLib's file.
func TestRealGrids(t *testing.T) {
	dir := os.Getenv("USPACE_GEOID_DIR")
	if dir == "" {
		t.Skip("needs egm2008-2_5.pgm in USPACE_GEOID_DIR")
	}
	g, err := Load(filepath.Join(dir, "egm2008-2_5.pgm"))
	if err != nil {
		t.Skipf("needs egm2008-2_5.pgm: %v", err)
	}
	for _, c := range []struct {
		name     string
		p        core.LatLon
		wantM    float64
		tolM     float64
		descPart string
	}{
		{"Tbilisi", core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}, 15.9, 0.05, "EGM2008"},
		{"Batumi", core.LatLon{LatDeg: 41.6168, LonDeg: 41.6367}, 22.5, 0.05, "EGM2008"},
	} {
		n, err := g.UndulationM(c.p)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(n-c.wantM) > c.tolM {
			t.Errorf("%s: N = %v, want %v", c.name, n, c.wantM)
		}
		if !strings.Contains(g.Description(), c.descPart) {
			t.Errorf("Description %q", g.Description())
		}
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
