package cell

import (
	"errors"
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// FuzzParse feeds arbitrary strings to Parse: it never panics, refuses
// with a *core.FieldError naming "cell", and whatever it accepts is a
// valid ID whose String parses back to the same ID (and is the input:
// Parse accepts only the spelling String produces).
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"c5:1317:2248", "c3:131:224", "c5:0:0", "c5:1799:3599", "c3:179:359",
		"c5:041:5", "C5:1:2", "c5:1:2:3", "h3:8a2a1072b59ffff", "c5:-1:2",
		"c5:+1:2", "c5: 1:2", "c5:1800:0", "", "c5:", "c5:99999999999:1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, err := Parse(s)
		if err != nil {
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != "cell" {
				t.Fatalf("Parse(%q) error %v is not a FieldError on \"cell\"", s, err)
			}
			if c != (ID{}) {
				t.Fatalf("Parse(%q) returned %v beside its error", s, c)
			}
			return
		}
		if !c.Valid() || c.String() != s {
			t.Fatalf("Parse(%q) = %v (String %q)", s, c, c.String())
		}
		again, err := Parse(c.String())
		if err != nil || again != c {
			t.Fatalf("Parse(String(Parse(%q))) = %v, %v; want %v", s, again, err, c)
		}
	})
}

// FuzzOf feeds arbitrary floats to Of and Cover: no panic, an error is a
// *core.FieldError, an accepted position is inside its cell, and a cover
// never exceeds its bound.
func FuzzOf(f *testing.F) {
	f.Add(41.7151, 44.8271, 41.0, 40.0, 43.6, 46.7, 5, 100)
	f.Add(90.0, 180.0, -90.0, -180.0, 90.0, 180.0, 3, 10_000)
	f.Add(41.7, 44.8, 0.05, 179.85, 0.15, -179.85, 5, 8)
	f.Add(math.NaN(), math.Inf(1), math.Inf(-1), 1e308, -1e308, math.NaN(), 4, -1)
	f.Fuzz(func(t *testing.T, lat, lon, minLat, minLon, maxLat, maxLon float64, level, maxCells int) {
		l := Level(level)
		p := core.LatLon{LatDeg: lat, LonDeg: lon}
		c, err := Of(p, l)
		if err != nil {
			var fe *core.FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("Of(%v, %d) error %v", p, level, err)
			}
		} else {
			if !c.Valid() || len(c.Ring1()) < 5 {
				t.Fatalf("Of(%v, %d) = %v", p, level, c)
			}
			b := c.BBox()
			w := lon
			if w < -180 || w >= 180 {
				w = core.WrapLonDeg(w)
				if w == 180 {
					w = -180
				}
			}
			if lat < b.MinLat || lat > b.MaxLat || w < b.MinLon || w > b.MaxLon {
				t.Fatalf("Of(%v, %d) = %v, box %+v does not hold it", p, level, c, b)
			}
		}
		if maxCells > 50_000 {
			maxCells = 50_000
		}
		ids, err := Cover(boxOf(minLat, minLon, maxLat, maxLon), l, maxCells)
		if err != nil {
			var fe *core.FieldError
			if !errors.As(err, &fe) || ids != nil {
				t.Fatalf("Cover error %v with %d cells", err, len(ids))
			}
			return
		}
		if len(ids) == 0 || len(ids) > maxCells {
			t.Fatalf("Cover returned %d cells for max %d", len(ids), maxCells)
		}
	})
}
