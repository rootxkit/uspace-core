package geodesy

import (
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/vectors"
)

// Tolerances recorded in the geodesy.json header; TestVectorsGeodesy
// asserts the file still says so.
const (
	tolVincentyM = 0.001
	tolOtherM    = 1e-6
)

// geodesyInput is the union of every input shape in geodesy.json; the
// strict decoder refuses any field not named here.
type geodesyInput struct {
	Function    string         `json:"function"`
	From        *[2]float64    `json:"from"`
	To          *[2]float64    `json:"to"`
	Center      *[2]float64    `json:"center"`
	Radius      *float64       `json:"radius"`
	RadiusUnit  *string        `json:"radius_unit"`
	Point       *[2]float64    `json:"point"`
	RingsLonLat [][][2]float64 `json:"rings_lon_lat"`
	Origin      *[2]float64    `json:"origin"`
}

type geodesyExpected struct {
	DistanceM            *float64 `json:"distance_m"`
	PublishedReferenceM  *float64 `json:"published_reference_m"`
	VincentyM            *float64 `json:"vincenty_m"`
	HaversineMeanRadiusM *float64 `json:"haversine_mean_radius_m"`
	DifferenceM          *float64 `json:"difference_m"`
	Inside               *bool    `json:"inside"`
	NorthM               *float64 `json:"north_m"`
	EastM                *float64 `json:"east_m"`
}

func latLon(t *testing.T, field string, p *[2]float64) core.LatLon {
	t.Helper()
	if p == nil {
		t.Fatalf("input.%s missing", field)
		return core.LatLon{}
	}
	return core.LatLon{LatDeg: p[0], LonDeg: p[1]}
}

func want(t *testing.T, field string, v *float64) float64 {
	t.Helper()
	if v == nil {
		t.Fatalf("expected.%s missing", field)
		return 0
	}
	return *v
}

func TestVectorsGeodesy(t *testing.T) {
	f := vectors.Load(t, "geodesy.json")
	if tol, ok := f.FloatTolerance("vincenty distance_m"); !ok || tol != tolVincentyM {
		t.Fatalf("header tolerance vincenty distance_m = %v (%v), want %v", tol, ok, tolVincentyM)
	}
	if tol, ok := f.FloatTolerance("other distances"); !ok || tol != tolOtherM {
		t.Fatalf("header tolerance other distances = %v (%v), want %v", tol, ok, tolOtherM)
	}
	if v, ok := f.Tolerance["inside"]; !ok || v != "exact" {
		t.Fatalf("header tolerance inside = %v, want exact", v)
	}
	ran := 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in geodesyInput
		var exp geodesyExpected
		c.Decode(t, &in, &exp)
		ran++
		switch in.Function {
		case "vincenty_inverse":
			d, err := DistanceM(latLon(t, "from", in.From), latLon(t, "to", in.To))
			if err != nil {
				t.Fatalf("DistanceM: %v", err)
			}
			vectors.Near(t, "distance_m", d, want(t, "distance_m", exp.DistanceM), tolVincentyM)
			if exp.PublishedReferenceM != nil {
				t.Logf("published reference %v m, got %v m (off by %v m)", *exp.PublishedReferenceM, d, d-*exp.PublishedReferenceM)
			}
		case "compare":
			a, b := latLon(t, "from", in.From), latLon(t, "to", in.To)
			v, err := DistanceM(a, b)
			if err != nil {
				t.Fatalf("DistanceM: %v", err)
			}
			h := HaversineM(a, b)
			vectors.Near(t, "vincenty_m", v, want(t, "vincenty_m", exp.VincentyM), tolVincentyM)
			vectors.Near(t, "haversine_mean_radius_m", h, want(t, "haversine_mean_radius_m", exp.HaversineMeanRadiusM), tolOtherM)
			vectors.Near(t, "difference_m", v-h, want(t, "difference_m", exp.DifferenceM), tolOtherM)
		case "in_circle":
			if in.RadiusUnit == nil || *in.RadiusUnit != "m" {
				t.Fatalf("input.radius_unit = %v, want \"m\"", in.RadiusUnit)
			}
			c := Circle{Center: latLon(t, "center", in.Center), RadiusM: want(t, "radius", in.Radius)}
			inside, d, err := c.Contains(latLon(t, "point", in.Point))
			if err != nil {
				t.Fatalf("Circle.Contains: %v", err)
			}
			if exp.Inside == nil || inside != *exp.Inside {
				t.Errorf("inside: got %v, want %v", inside, exp.Inside)
			}
			vectors.Near(t, "distance_m", d, want(t, "distance_m", exp.DistanceM), tolOtherM)
		case "in_polygon":
			if len(in.RingsLonLat) == 0 {
				t.Fatalf("input.rings_lon_lat missing")
			}
			var p Polygon
			for i, coords := range in.RingsLonLat {
				r := RingFromLonLat(coords)
				if err := ValidRing(r, 5000); err != nil {
					t.Fatalf("rings_lon_lat[%d]: %v", i, err)
				}
				p.Rings = append(p.Rings, r)
			}
			pt := latLon(t, "point", in.Point)
			got := p.Contains(pt)
			if exp.Inside == nil || got != *exp.Inside {
				t.Errorf("inside: got %v, want %v", got, exp.Inside)
			}
			if got && !p.BBox().Contains(pt) {
				t.Errorf("the bounding box does not contain an inside point")
			}
		case "local_offset_m":
			n, e := LocalOffsetM(latLon(t, "origin", in.Origin), latLon(t, "point", in.Point))
			vectors.Near(t, "north_m", n, want(t, "north_m", exp.NorthM), tolOtherM)
			vectors.Near(t, "east_m", e, want(t, "east_m", exp.EastM), tolOtherM)
		case "haversine_6371008.8":
			d := HaversineM(latLon(t, "from", in.From), latLon(t, "to", in.To))
			vectors.Near(t, "distance_m", d, want(t, "distance_m", exp.DistanceM), tolOtherM)
		default:
			t.Fatalf("unknown input.function %q", in.Function)
		}
	})
	if ran != 17 {
		t.Errorf("ran %d cases, want 17", ran)
	}
}
