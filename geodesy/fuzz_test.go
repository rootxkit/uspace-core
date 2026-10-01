package geodesy

import (
	"errors"
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// FuzzShapes feeds arbitrary floats (NaN, Inf, out of range included) to
// every exported function: none may panic, an invalid position is never
// inside, and a distance without an error is finite and not negative.
func FuzzShapes(f *testing.F) {
	// Seeds from geodesy.json.
	f.Add(-37.95103341666667, 144.42486788888888, -37.65282113888889, 143.92649552777777, 500.0)
	f.Add(0.0, 0.0, 0.0, 1.0, 1.0)
	f.Add(0.0, 0.0, 90.0, 0.0, 1.0)
	f.Add(41.71, 44.81, 41.71448761185305, 44.81, 500.0)
	f.Add(0.0, 179.999, 0.0, -179.999, 304.8)
	f.Add(0.0, 0.0, 0.5, 179.7, 1e9)
	f.Add(math.NaN(), math.Inf(1), -91.0, 181.0, -1.0)
	f.Fuzz(func(t *testing.T, lat1, lon1, lat2, lon2, radiusM float64) {
		a, b := ll(lat1, lon1), ll(lat2, lon2)
		d, b1, b2, err := Inverse(a, b)
		if err == nil {
			if !core.IsFinite(d) || d < 0 || !(b1 >= 0 && b1 < 360) || !(b2 >= 0 && b2 < 360) {
				t.Fatalf("Inverse(%v, %v) = %v %v %v", a, b, d, b1, b2)
			}
		} else {
			var fe *core.FieldError
			if !errors.As(err, &fe) && !errors.Is(err, ErrNoConvergence) {
				t.Fatalf("unexpected error %v", err)
			}
		}
		_ = HaversineM(a, b)
		_, _ = LocalOffsetM(a, b)
		_, _ = LocalOffsetAboutMidLatM(a, b)
		c := Circle{Center: a, RadiusM: radiusM}
		if in, _, err := c.Contains(b); in && (err != nil || !b.Valid()) {
			t.Fatalf("circle contains invalid %v (err %v)", b, err)
		}
		bb := c.BBox().PadM(radiusM)
		if bb.Contains(b) && !b.Valid() {
			t.Fatalf("box contains invalid %v", b)
		}
		r := Ring{a, b, ll(lat2, lon1), ll(radiusM, lon2), a}
		_ = ValidRing(r, 5000)
		p := Polygon{Rings: []Ring{r, r[:3]}}
		_ = p.BBox()
		pt := ll((lat1+lat2)/2, (lon1+lon2)/2)
		if p.Contains(pt) && !pt.Valid() {
			t.Fatalf("polygon contains invalid %v", pt)
		}
	})
}
