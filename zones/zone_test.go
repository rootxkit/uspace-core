package zones

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/geodesy"
)

func ll(latDeg, lonDeg float64) core.LatLon { return core.LatLon{LatDeg: latDeg, LonDeg: lonDeg} }

func f64(v float64) *float64 { return &v }

// square is a closed axis-aligned ring of half-size h degrees.
func square(c core.LatLon, h float64) []core.LatLon {
	return []core.LatLon{
		ll(c.LatDeg-h, c.LonDeg-h), ll(c.LatDeg-h, c.LonDeg+h),
		ll(c.LatDeg+h, c.LonDeg+h), ll(c.LatDeg+h, c.LonDeg-h),
		ll(c.LatDeg-h, c.LonDeg-h),
	}
}

var tbilisi = ll(41.7151, 44.8271)

// geoZone is a valid one-volume polygon zone in metres, AMSL to AMSL,
// unbounded, permanent.
func geoZone() *ed269.GeoZone {
	return &ed269.GeoZone{
		Identifier:    "Z1",
		Country:       "GEO",
		Type:          "COMMON",
		Restriction:   ed269.RestrictionProhibited,
		Applicability: []ed269.Period{{Permanent: true}},
		Geometry: []ed269.Volume{{
			Uom:      ed269.UomMetres,
			LowerRef: core.RefAMSL,
			UpperRef: core.RefAMSL,
			Projection: ed269.HorizontalProjection{
				Type:  ed269.ShapePolygon,
				Rings: [][]ed269.Position{square(tbilisi, 0.01)},
			},
		}},
	}
}

func mustZone(t *testing.T, gz *ed269.GeoZone) *Zone {
	t.Helper()
	z, err := FromED269(gz)
	if err != nil {
		t.Fatalf("FromED269: %v", err)
	}
	return z
}

func wantFieldError(t *testing.T, err error, field string) {
	t.Helper()
	var fe *core.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("got %v, want a *core.FieldError naming %q", err, field)
	}
	if !strings.HasPrefix(fe.Field, field) {
		t.Fatalf("field: got %q, want prefix %q (%v)", fe.Field, field, err)
	}
}

func TestFromED269AcceptsOneVolume(t *testing.T) {
	z := mustZone(t, geoZone())
	if z.Identifier != "Z1" || z.Country != "GEO" || z.Type != core.ZoneProhibited ||
		z.Restriction != ed269.RestrictionProhibited || z.Polygon == nil || z.Circle != nil ||
		z.Lower != nil || z.Upper != nil || len(z.Periods) != 1 {
		t.Fatalf("unexpected zone %+v", z)
	}
	if !z.BBox.Contains(tbilisi) {
		t.Fatalf("bbox %+v does not hold the centre", z.BBox)
	}
}

func TestFromED269RefusesTwoVolumes(t *testing.T) {
	gz := geoZone()
	gz.Geometry = append(gz.Geometry, gz.Geometry[0])
	_, err := FromED269(gz)
	wantFieldError(t, err, "geometry")
	gz.Geometry = nil
	_, err = FromED269(gz)
	wantFieldError(t, err, "geometry")
}

func TestFromED269ConvertsFeetExactly(t *testing.T) {
	gz := geoZone()
	gz.Geometry[0].Uom = ed269.UomFeet
	gz.Geometry[0].LowerLimit = f64(400)
	gz.Geometry[0].UpperLimit = f64(2000)
	z := mustZone(t, gz)
	if z.Upper.ValueM != 2000*core.FeetToMetres || math.Abs(z.Upper.ValueM-609.6) > 1e-9 {
		t.Fatalf("upper: got %v, want 609.6", z.Upper.ValueM)
	}
	if z.Lower.ValueM != 400*core.FeetToMetres || z.Lower.Ref != core.RefAMSL {
		t.Fatalf("lower: got %+v", *z.Lower)
	}

	// The presence pair: metres are kept as written.
	gz.Geometry[0].Uom = ed269.UomMetres
	z = mustZone(t, gz)
	if z.Upper.ValueM != 2000 {
		t.Fatalf("upper in metres: got %v", z.Upper.ValueM)
	}
}

func TestFromED269ConvertsCircleRadius(t *testing.T) {
	gz := geoZone()
	c := tbilisi
	gz.Geometry[0].Uom = ed269.UomFeet
	gz.Geometry[0].Projection = ed269.HorizontalProjection{Type: ed269.ShapeCircle, Center: &c, Radius: f64(1000)}
	z := mustZone(t, gz)
	if z.Circle == nil || z.Polygon != nil || z.Circle.RadiusM != 1000*core.FeetToMetres || z.Circle.Center != c {
		t.Fatalf("circle: got %+v", z.Circle)
	}
}

func TestFromED269Refusals(t *testing.T) {
	cases := []struct {
		name  string
		edit  func(gz *ed269.GeoZone)
		field string
	}{
		{"unknown restriction", func(gz *ed269.GeoZone) { gz.Restriction = "REQ_AUTHORIZATION" }, "restriction"},
		{"no period", func(gz *ed269.GeoZone) { gz.Applicability = nil }, "applicability"},
		{"lower NaN", func(gz *ed269.GeoZone) { gz.Geometry[0].LowerLimit = f64(math.NaN()) }, "geometry[0].lowerLimit"},
		{"upper Inf", func(gz *ed269.GeoZone) { gz.Geometry[0].UpperLimit = f64(math.Inf(1)) }, "geometry[0].upperLimit"},
		{"bad reference", func(gz *ed269.GeoZone) {
			gz.Geometry[0].UpperLimit = f64(100)
			gz.Geometry[0].UpperRef = "QNH"
		}, "geometry[0].upperLimit"},
		{"lower equal to upper, same reference", func(gz *ed269.GeoZone) {
			gz.Geometry[0].LowerLimit = f64(500)
			gz.Geometry[0].UpperLimit = f64(500)
		}, "geometry[0].upperLimit"},
		{"lower above upper, same reference", func(gz *ed269.GeoZone) {
			gz.Geometry[0].LowerLimit = f64(700)
			gz.Geometry[0].UpperLimit = f64(500)
		}, "geometry[0].upperLimit"},
		{"upper 0 with no lower", func(gz *ed269.GeoZone) { gz.Geometry[0].UpperLimit = f64(0) }, "geometry[0].upperLimit"},
		{"open ring", func(gz *ed269.GeoZone) {
			r := gz.Geometry[0].Projection.Rings[0]
			gz.Geometry[0].Projection.Rings[0] = r[:len(r)-1]
		}, "geometry[0].horizontalProjection.coordinates[0]"},
		{"no ring", func(gz *ed269.GeoZone) { gz.Geometry[0].Projection.Rings = nil }, "geometry[0].horizontalProjection.coordinates"},
		{"ring past the bound", func(gz *ed269.GeoZone) {
			r := make([]core.LatLon, 0, maxRingVertices+1)
			for i := range maxRingVertices {
				r = append(r, ll(41+float64(i)*1e-5, 44))
			}
			gz.Geometry[0].Projection.Rings[0] = append(r, r[0])
		}, "geometry[0].horizontalProjection.coordinates[0]"},
		{"circle without centre", func(gz *ed269.GeoZone) {
			gz.Geometry[0].Projection = ed269.HorizontalProjection{Type: ed269.ShapeCircle, Radius: f64(10)}
		}, "geometry[0].horizontalProjection.center"},
		{"circle radius 0", func(gz *ed269.GeoZone) {
			c := tbilisi
			gz.Geometry[0].Projection = ed269.HorizontalProjection{Type: ed269.ShapeCircle, Center: &c, Radius: f64(0)}
		}, "geometry[0].horizontalProjection.radius"},
		{"unknown shape", func(gz *ed269.GeoZone) { gz.Geometry[0].Projection.Type = "Point" }, "geometry[0].horizontalProjection.type"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gz := geoZone()
			tc.edit(gz)
			z, err := FromED269(gz)
			if z != nil {
				t.Fatalf("accepted %+v", z)
			}
			wantFieldError(t, err, tc.field)
		})
	}
	_, err := FromED269(nil)
	wantFieldError(t, err, "zone")
}

func TestFromED269ParsedZone(t *testing.T) {
	raw := json.RawMessage(`{"identifier":"F1","country":"GEO","type":"COMMON","restriction":"REQ_AUTHORISATION",
	 "applicability":[{"permanent":"YES"}],"zoneAuthority":[],
	 "geometry":[{"uomDimensions":"FT","lowerVerticalReference":"AGL","upperVerticalReference":"WGS84","lowerLimit":0,"upperLimit":2000,
	 "horizontalProjection":{"type":"Circle","center":[44.8271,41.7151],"radius":1000}}]}`)
	gz, problems := ed269.ParseZone(raw, ed269.Limits{})
	if problems != nil {
		t.Fatalf("ParseZone: %v", problems)
	}
	z := mustZone(t, gz)
	if z.Type != core.ZoneReqAuthorization || z.Restriction != ed269.RestrictionReqAuthorisation {
		t.Fatalf("type %q restriction %q", z.Type, z.Restriction)
	}
	if z.Circle == nil || z.Circle.RadiusM != 1000*core.FeetToMetres || z.Circle.Center != tbilisi {
		t.Fatalf("circle %+v", z.Circle)
	}
	if z.Lower.Ref != core.RefAGL || z.Lower.ValueM != 0 || z.Upper.Ref != core.RefWGS84 {
		t.Fatalf("limits %+v %+v", *z.Lower, *z.Upper)
	}
}

func TestContainsPolygonInsideOutsideAndHole(t *testing.T) {
	gz := geoZone()
	gz.Geometry[0].Projection.Rings = append(gz.Geometry[0].Projection.Rings, square(tbilisi, 0.002))
	z := mustZone(t, gz)
	cases := []struct {
		name string
		p    core.LatLon
		want bool
	}{
		{"inside the ring, outside the hole", ll(tbilisi.LatDeg+0.005, tbilisi.LonDeg), true},
		{"inside the hole", tbilisi, false},
		{"on the outer vertex", ll(tbilisi.LatDeg-0.01, tbilisi.LonDeg-0.01), true},
		{"on the outer edge", ll(tbilisi.LatDeg-0.01, tbilisi.LonDeg), true},
		{"just outside the edge", ll(tbilisi.LatDeg-0.0101, tbilisi.LonDeg), false},
		{"far away (bbox)", ll(-33, 151), false},
	}
	for _, tc := range cases {
		got, err := z.ContainsHorizontally(tc.p)
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v %v, want %v", tc.name, got, err, tc.want)
		}
	}
}

// pointAtM is the position dist metres north of c, found on the
// ellipsoid by bisection with geodesy.DistanceM.
func pointAtM(t *testing.T, c core.LatLon, distM float64) core.LatLon {
	t.Helper()
	lo, hi := 0.0, 1.0
	for range 200 {
		mid := (lo + hi) / 2
		d, err := geodesy.DistanceM(c, ll(c.LatDeg+mid, c.LonDeg))
		if err != nil {
			t.Fatal(err)
		}
		if d < distM {
			lo = mid
		} else {
			hi = mid
		}
	}
	return ll(c.LatDeg+lo, c.LonDeg)
}

func circleZone(c core.LatLon, radiusM float64) *Zone {
	circle := &geodesy.Circle{Center: c, RadiusM: radiusM}
	return &Zone{Identifier: "C", Type: core.ZoneProhibited, Restriction: ed269.RestrictionProhibited, Circle: circle, BBox: circle.BBox()}
}

func TestContainsCircleByGeodesicDistance(t *testing.T) {
	z := circleZone(tbilisi, 500)
	in, err := z.ContainsHorizontally(pointAtM(t, tbilisi, 499.9))
	if err != nil || !in {
		t.Fatalf("499.9 m from the centre: got %v %v, want inside", in, err)
	}
	out, err := z.ContainsHorizontally(pointAtM(t, tbilisi, 500.1))
	if err != nil || out {
		t.Fatalf("500.1 m from the centre: got %v %v, want outside", out, err)
	}
	far, err := z.ContainsHorizontally(ll(0, 0))
	if err != nil || far {
		t.Fatalf("far: got %v %v", far, err)
	}
}

func TestContainsAcrossTheAntimeridian(t *testing.T) {
	gz := geoZone()
	gz.Geometry[0].Projection.Rings = [][]ed269.Position{{
		ll(-17, 179.5), ll(-17, -179.5), ll(-16, -179.5), ll(-16, 179.5), ll(-17, 179.5),
	}}
	z := mustZone(t, gz)
	if z.BBox.MinLon <= z.BBox.MaxLon {
		t.Fatalf("bbox %+v should cross the antimeridian", z.BBox)
	}
	for _, p := range []core.LatLon{ll(-16.5, 179.9), ll(-16.5, -179.9), ll(-16.5, 180), ll(-16.5, -180)} {
		if in, err := z.ContainsHorizontally(p); err != nil || !in {
			t.Errorf("%+v: got %v %v, want inside", p, in, err)
		}
	}
	for _, p := range []core.LatLon{ll(-16.5, 179.4), ll(-16.5, -179.4), ll(-16.5, 0)} {
		if in, err := z.ContainsHorizontally(p); err != nil || in {
			t.Errorf("%+v: got %v %v, want outside", p, in, err)
		}
	}

	c := circleZone(ll(-16.5, 180), 20_000)
	for _, p := range []core.LatLon{ll(-16.5, 179.9), ll(-16.5, -179.9), ll(-16.5, -180)} {
		if in, err := c.ContainsHorizontally(p); err != nil || !in {
			t.Errorf("circle %+v: got %v %v, want inside", p, in, err)
		}
	}
	if in, err := c.ContainsHorizontally(ll(-16.5, -179.5)); err != nil || in {
		t.Errorf("circle far side: got %v %v, want outside", in, err)
	}
}

// A polygon whose west edge is at -180 holds a point written as 180.
func TestContainsEdgeAtMinus180(t *testing.T) {
	gz := geoZone()
	gz.Geometry[0].Projection.Rings = [][]ed269.Position{{
		ll(10, -180), ll(10, -179), ll(11, -179), ll(11, -180), ll(10, -180),
	}}
	z := mustZone(t, gz)
	for _, p := range []core.LatLon{ll(10.5, -180), ll(10.5, 180)} {
		if in, err := z.ContainsHorizontally(p); err != nil || !in {
			t.Errorf("%+v: got %v %v, want inside", p, in, err)
		}
	}
}

func TestContainsInvalidPointIsNotJudged(t *testing.T) {
	poly := mustZone(t, geoZone())
	circ := circleZone(tbilisi, 500)
	for _, z := range []*Zone{poly, circ} {
		for _, p := range []core.LatLon{ll(math.NaN(), 44.8), ll(41.7, math.Inf(1)), ll(91, 0), ll(0, 181)} {
			in, err := z.ContainsHorizontally(p)
			if in || err == nil {
				t.Errorf("%+v: got %v %v, want false with an error", p, in, err)
			}
			wantFieldError(t, err, "point")
		}
		// The presence pair: a valid point inside is judged without error.
		if in, err := z.ContainsHorizontally(tbilisi); err != nil || !in {
			t.Errorf("valid point: got %v %v", in, err)
		}
	}
}

func TestContainsZoneWithoutShapeOrInvalidCircle(t *testing.T) {
	_, err := (&Zone{Identifier: "X"}).ContainsHorizontally(tbilisi)
	wantFieldError(t, err, "zone")

	bad := &Zone{Circle: &geodesy.Circle{Center: tbilisi, RadiusM: math.NaN()}, BBox: geodesy.BBox{MinLat: -90, MinLon: -180, MaxLat: 90, MaxLon: 180}}
	in, err := bad.ContainsHorizontally(tbilisi)
	if in || err == nil {
		t.Fatalf("invalid circle: got %v %v, want false with an error", in, err)
	}
}

func TestAppliesAt(t *testing.T) {
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	z := &Zone{Periods: []ed269.Period{{Start: &start, End: &end}}}
	cases := []struct {
		name string
		at   time.Time
		want bool
	}{
		{"before", start.Add(-time.Second), false},
		{"at the start (included)", start, true},
		{"inside, written in another offset", start.Add(30 * time.Minute).In(time.FixedZone("+04", 4*3600)), true},
		{"at the end (included)", end, true},
		{"after", end.Add(time.Second), false},
		{"zero time: unknown, so enforced", time.Time{}, true},
	}
	for _, tc := range cases {
		if got := z.AppliesAt(tc.at); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
	if !(&Zone{}).AppliesAt(start) {
		t.Errorf("a zone without periods must apply (fail-safe)")
	}
	if !(&Zone{Periods: []ed269.Period{{}}}).AppliesAt(start) {
		t.Errorf("the zero-value period applies always (ed269)")
	}
}

func TestNeedsTerrainAndGeoid(t *testing.T) {
	agl := func(v float64) *Limit { return &Limit{ValueM: v, Ref: core.RefAGL} }
	amsl := func(v float64) *Limit { return &Limit{ValueM: v, Ref: core.RefAMSL} }
	wgs := func(v float64) *Limit { return &Limit{ValueM: v, Ref: core.RefWGS84} }
	cases := []struct {
		name         string
		lower, upper *Limit
		terrain      bool
		geoid        bool
	}{
		{"no limits", nil, nil, false, false},
		{"AMSL band", amsl(500), amsl(700), false, false},
		{"AGL floor at 0", agl(0), amsl(700), false, false},
		{"AGL floor below 0", agl(-5), nil, false, false},
		{"AGL floor above 0", agl(50), nil, true, false},
		{"AGL ceiling", nil, agl(120), true, false},
		{"AGL ceiling 0", amsl(0), agl(0), true, false},
		{"AGL floor 0 and AGL ceiling", agl(0), agl(120), true, false},
		{"WGS84 ceiling", amsl(0), wgs(600), false, true},
		{"WGS84 floor", wgs(0), nil, false, true},
		{"AGL and WGS84", agl(30), wgs(600), true, true},
	}
	for _, tc := range cases {
		z := &Zone{Lower: tc.lower, Upper: tc.upper}
		if got := z.NeedsTerrain(); got != tc.terrain {
			t.Errorf("%s: NeedsTerrain %v, want %v", tc.name, got, tc.terrain)
		}
		if got := z.NeedsGeoid(); got != tc.geoid {
			t.Errorf("%s: NeedsGeoid %v, want %v", tc.name, got, tc.geoid)
		}
	}
}

// The pair of the limit-order refusals: a lower limit below the upper one
// in the same reference, or above it in another reference, is accepted
// (an AGL floor and an AMSL ceiling are not comparable without terrain).
func TestFromED269AcceptsOrderedLimits(t *testing.T) {
	gz := geoZone()
	gz.Geometry[0].LowerLimit = f64(500)
	gz.Geometry[0].UpperLimit = f64(500.1)
	mustZone(t, gz)
	gz.Geometry[0].LowerRef = core.RefAGL
	gz.Geometry[0].LowerLimit = f64(700)
	mustZone(t, gz)
	gz = geoZone()
	gz.Geometry[0].LowerLimit = f64(0)
	gz.Geometry[0].UpperLimit = f64(0.5)
	mustZone(t, gz)
}
