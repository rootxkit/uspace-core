package geodesy

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func TestLocalOffsetAcrossAntimeridianBothDirections(t *testing.T) {
	n, e := LocalOffsetM(ll(0, 179.999), ll(0, -179.999))
	if n != 0 || math.Abs(e-222.6389815876102) > 1e-6 {
		t.Errorf("east-going: %v %v", n, e)
	}
	n, e = LocalOffsetM(ll(0, -179.999), ll(0, 179.999))
	if n != 0 || math.Abs(e+222.6389815876102) > 1e-6 {
		t.Errorf("west-going: %v %v", n, e)
	}
	// Without crossing, the plain difference.
	_, e = LocalOffsetM(ll(0, 10), ll(0, 10.002))
	if math.Abs(e-222.6389815876102) > 1e-3 {
		t.Errorf("no crossing: %v", e)
	}
}

func TestLocalOffsetAboutMidLat(t *testing.T) {
	// cpa.json#head-on: d_horizontal_now_m 1000.0007861298747.
	a, b := ll(41.7151, 44.8271), ll(41.72410351324941, 44.8271)
	n, e := LocalOffsetAboutMidLatM(a, b)
	if math.Abs(math.Hypot(n, e)-1000.0007861298747) > 1e-6 {
		t.Errorf("head-on distance %v", math.Hypot(n, e))
	}
	n2, e2 := LocalOffsetAboutMidLatM(b, a)
	if n2 != -n || e2 != -e {
		t.Errorf("not antisymmetric: %v %v vs %v %v", n, e, n2, e2)
	}
	// Differs from the origin-latitude projection.
	n0, _ := LocalOffsetM(a, b)
	if n0 == n {
		t.Errorf("mid-latitude radius equals origin radius: %v", n)
	}
	_, e = LocalOffsetAboutMidLatM(ll(1, 179.999), ll(-1, -179.999))
	if e <= 0 || e > 300 {
		t.Errorf("antimeridian east %v", e)
	}
}

func square(minLon, minLat, maxLon, maxLat float64) Ring {
	return Ring{ll(minLat, minLon), ll(minLat, maxLon), ll(maxLat, maxLon), ll(maxLat, minLon), ll(minLat, minLon)}
}

func TestPolygonContains(t *testing.T) {
	outer := square(44.8, 41.7, 44.82, 41.72)
	hole := square(44.805, 41.705, 44.815, 41.715)
	p := Polygon{Rings: []Ring{outer, hole}}
	cases := []struct {
		name string
		pt   core.LatLon
		want bool
	}{
		{"in-hole", ll(41.71, 44.81), false},
		{"between", ll(41.702, 44.802), true},
		{"north", ll(41.73, 44.81), false},
		{"west", ll(41.71, 44.79), false},
		{"outer-edge", ll(41.7, 44.81), true},
		{"outer-vertex", ll(41.7, 44.8), true},
		{"hole-edge", ll(41.705, 44.81), true},
		{"nan", ll(math.NaN(), 44.81), false},
		{"inf", ll(41.71, math.Inf(1)), false},
	}
	for _, c := range cases {
		if got := p.Contains(c.pt); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	// Open rings (first != last) are judged the same.
	open := Polygon{Rings: []Ring{outer[:4], hole[:4]}}
	if !open.Contains(ll(41.702, 44.802)) || open.Contains(ll(41.71, 44.81)) {
		t.Errorf("open rings judged differently")
	}
	// Degenerate polygons contain nothing.
	for _, d := range []Polygon{{}, {Rings: []Ring{outer[:2]}}} {
		if d.Contains(ll(41.71, 44.81)) {
			t.Errorf("degenerate polygon %v contains a point", d)
		}
	}
}

func TestPolygonAcrossAntimeridian(t *testing.T) {
	r := Ring{ll(-1, 179), ll(-1, -179), ll(1, -179), ll(1, 179), ll(-1, 179)}
	p := Polygon{Rings: []Ring{r}}
	for _, pt := range []core.LatLon{ll(0, 179.5), ll(0, -179.5), ll(0, 180), ll(0, -180)} {
		if !p.Contains(pt) {
			t.Errorf("%v should be inside", pt)
		}
		if !p.BBox().Contains(pt) {
			t.Errorf("bbox misses %v", pt)
		}
	}
	for _, pt := range []core.LatLon{ll(0, 0), ll(0, 178), ll(0, -178), ll(2, 180)} {
		if p.Contains(pt) {
			t.Errorf("%v should be outside", pt)
		}
	}
	b := p.BBox()
	if b.MinLon != 179 || b.MaxLon != -179 {
		t.Errorf("bbox %+v, want MinLon 179 MaxLon -179", b)
	}
	// Starting on the west side gives the same box.
	r2 := Ring{ll(-1, -179), ll(1, -179), ll(1, 179), ll(-1, 179), ll(-1, -179)}
	if b2 := (Polygon{Rings: []Ring{r2}}).BBox(); b2 != b {
		t.Errorf("bbox %+v, want %+v", b2, b)
	}
}

func TestPolygonBBox(t *testing.T) {
	p := Polygon{Rings: []Ring{square(44.8, 41.7, 44.82, 41.72)}}
	want := BBox{MinLat: 41.7, MinLon: 44.8, MaxLat: 41.72, MaxLon: 44.82}
	if got := p.BBox(); got != want {
		t.Errorf("bbox %+v, want %+v", got, want)
	}
	empty := Polygon{}.BBox()
	if empty.Contains(ll(0, 0)) {
		t.Errorf("empty box contains a point")
	}
	if (Polygon{Rings: []Ring{{}}}).BBox() != empty {
		t.Errorf("empty ring box not empty")
	}
	if empty.PadM(1000) != empty {
		t.Errorf("padding an empty box changed it")
	}
}

func TestBBoxContainsAndPad(t *testing.T) {
	b := BBox{MinLat: 41.7, MinLon: 44.8, MaxLat: 41.72, MaxLon: 44.82}
	if !b.Contains(ll(41.7, 44.8)) || b.Contains(ll(41.69, 44.81)) || b.Contains(ll(41.71, 44.83)) {
		t.Errorf("contains wrong")
	}
	if b.Contains(ll(math.NaN(), 44.81)) {
		t.Errorf("NaN inside")
	}
	// A point 990 m north of the box is inside the 1 km pad, not the box.
	n := ll(41.72+degrees(990/radiiMeridional(41.72)), 44.81)
	if b.Contains(n) || !b.PadM(1000).Contains(n) {
		t.Errorf("pad north wrong")
	}
	east := ll(41.71, 44.82+degrees(990/(core.WGS84SemiMajorM*math.Cos(radians(41.72)))))
	if b.Contains(east) || !b.PadM(1000).Contains(east) {
		t.Errorf("pad east wrong")
	}
	for _, m := range []float64{0, -5, math.NaN(), math.Inf(1)} {
		if b.PadM(m) != b {
			t.Errorf("PadM(%v) changed the box", m)
		}
	}
	// Padding across the antimeridian wraps.
	w := BBox{MinLat: 0, MinLon: 179.999, MaxLat: 0, MaxLon: 179.999}.PadM(1000)
	if w.MinLon <= w.MaxLon || !w.Contains(ll(0, -179.999)) || w.Contains(ll(0, 0)) {
		t.Errorf("antimeridian pad %+v", w)
	}
	// Reaching a pole gives every longitude.
	pole := BBox{MinLat: 89.99, MinLon: 10, MaxLat: 89.99, MaxLon: 10}.PadM(5000)
	if pole.MaxLat != 90 || pole.MinLon != -180 || pole.MaxLon != 180 {
		t.Errorf("polar pad %+v", pole)
	}
	// A pad wider than the globe gives every longitude.
	wide := BBox{MinLat: 0, MinLon: -170, MaxLat: 0, MaxLon: 170}.PadM(2_000_000)
	if wide.MinLon != -180 || wide.MaxLon != 180 {
		t.Errorf("wide pad %+v", wide)
	}
}

func radiiMeridional(latDeg float64) float64 {
	m, _ := radiiM(radians(latDeg))
	return m
}

func TestCircleContains(t *testing.T) {
	c := Circle{Center: ll(41.71, 44.81), RadiusM: 500}
	in, d, err := c.Contains(ll(41.71448761185305, 44.81))
	if err != nil || !in || math.Abs(d-498.4287233780669) > 1e-6 {
		t.Errorf("inside: %v %v %v", in, d, err)
	}
	in, d, err = c.Contains(ll(41.714505598273306, 44.81))
	if err != nil || in || math.Abs(d-500.4264344778979) > 1e-6 {
		t.Errorf("outside: %v %v %v", in, d, err)
	}
	in, d, err = c.Contains(c.Center)
	if err != nil || !in || d != 0 {
		t.Errorf("centre: %v %v %v", in, d, err)
	}
	// A radius in feet converted by the caller: 1000 ft.
	ft := Circle{Center: c.Center, RadiusM: 1000 * core.FeetToMetres}
	if in, _, _ := ft.Contains(ll(41.712697963037904, 44.81)); !in {
		t.Errorf("1000 ft inside at 300 m")
	}
}

func TestCircleRefusesInvalid(t *testing.T) {
	good := ll(41.71, 44.81)
	cases := []struct {
		c     Circle
		pt    core.LatLon
		field string
	}{
		{Circle{Center: ll(math.NaN(), 0), RadiusM: 1}, good, "center"},
		{Circle{Center: good, RadiusM: math.NaN()}, good, "radius_m"},
		{Circle{Center: good, RadiusM: math.Inf(1)}, good, "radius_m"},
		{Circle{Center: good, RadiusM: -1}, good, "radius_m"},
		{Circle{Center: good, RadiusM: 1}, ll(0, math.Inf(-1)), "point"},
	}
	for _, tc := range cases {
		in, _, err := tc.c.Contains(tc.pt)
		var fe *core.FieldError
		if in || !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%+v %v: inside %v err %v, want field %q", tc.c, tc.pt, in, err, tc.field)
		}
		if tc.field != "point" && !tc.c.BBox().empty() {
			t.Errorf("invalid circle has a non-empty box")
		}
	}
	in, _, err := Circle{Center: ll(0, 0), RadiusM: 1e9}.Contains(ll(0.5, 179.7))
	if in || !errors.Is(err, ErrNoConvergence) {
		t.Errorf("antipodal: %v %v", in, err)
	}
}

func TestCircleBBox(t *testing.T) {
	c := Circle{Center: ll(41.71, 44.81), RadiusM: 500}
	b := c.BBox()
	for _, brg := range []float64{0, 45, 90, 135, 180, 225, 270, 315} {
		phi := radians(brg)
		m, n := radiiM(radians(41.71))
		pt := ll(41.71+degrees(499*math.Cos(phi)/m), 44.81+degrees(499*math.Sin(phi)/(n*math.Cos(radians(41.71)))))
		if !b.Contains(pt) {
			t.Errorf("bearing %v: %v outside box %+v", brg, pt, b)
		}
	}
	if b.Contains(ll(41.72, 44.81)) {
		t.Errorf("box too large: %+v", b)
	}
}

func TestValidRing(t *testing.T) {
	good := square(44.8, 41.7, 44.82, 41.72)
	if err := ValidRing(good, 5000); err != nil {
		t.Fatalf("good ring refused: %v", err)
	}
	if err := ValidRing(good, 5); err != nil {
		t.Fatalf("ring at the bound refused: %v", err)
	}
	cases := []struct {
		name  string
		r     Ring
		max   int
		field string
		why   string
	}{
		{"too-many", good, 4, "ring", "more than the maximum"},
		{"too-few", good[:3], 5000, "ring", "at least 4"},
		{"empty", nil, 5000, "ring", "at least 4"},
		{"open", good[:4], 5000, "ring", "not closed"},
		{"nan", Ring{good[0], ll(math.NaN(), 44.82), good[2], good[3], good[4]}, 5000, "ring[1]", "not a valid"},
		{"inf", Ring{good[0], good[1], ll(41.72, math.Inf(1)), good[3], good[4]}, 5000, "ring[2]", "not a valid"},
		{"out-of-range", Ring{good[0], good[1], good[2], ll(91, 44.8), good[4]}, 5000, "ring[3]", "not a valid"},
	}
	for _, c := range cases {
		err := ValidRing(c.r, c.max)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field || !strings.Contains(fe.Reason, c.why) {
			t.Errorf("%s: err %v, want field %q reason containing %q", c.name, err, c.field, c.why)
		}
	}
	// Past the bound by far: refused before it is walked.
	big := make(Ring, 5001)
	big[3] = ll(math.NaN(), 0)
	var fe *core.FieldError
	if err := ValidRing(big, 5000); !errors.As(err, &fe) || fe.Field != "ring" {
		t.Errorf("5001 positions: %v", err)
	}
}

func TestRingFromLonLat(t *testing.T) {
	r := RingFromLonLat([][2]float64{{44.8, 41.7}, {44.82, 41.71}})
	if len(r) != 2 || r[0] != ll(41.7, 44.8) || r[1] != ll(41.71, 44.82) {
		t.Errorf("got %v", r)
	}
	if len(RingFromLonLat(nil)) != 0 {
		t.Errorf("nil input gave positions")
	}
}
