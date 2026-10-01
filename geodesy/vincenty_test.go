package geodesy

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func ll(lat, lon float64) core.LatLon { return core.LatLon{LatDeg: lat, LonDeg: lon} }

func TestInverseConvergesAndBearings(t *testing.T) {
	d, b1, b2, err := Inverse(ll(0, 0), ll(0, 1))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(d-111319.4907932264) > 1e-3 {
		t.Errorf("distance %v", d)
	}
	if math.Abs(b1-90) > 1e-9 || math.Abs(b2-90) > 1e-9 {
		t.Errorf("bearings %v %v, want 90 90", b1, b2)
	}
	// Due south: initial and final bearing 180.
	_, b1, b2, err = Inverse(ll(10, 20), ll(5, 20))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(b1-180) > 1e-9 || math.Abs(b2-180) > 1e-9 {
		t.Errorf("bearings %v %v, want 180 180", b1, b2)
	}
	// Due west: 270, never negative.
	_, b1, _, err = Inverse(ll(0, 1), ll(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(b1-270) > 1e-9 {
		t.Errorf("initial bearing %v, want 270", b1)
	}
	// Flinders Peak to Buninyong: Geoscience Australia gives 306 52 05.37
	// and 127 10 25.07 (reverse azimuth), so final = 307.17 deg.
	_, b1, b2, err = Inverse(ll(-37.95103341666667, 144.42486788888888), ll(-37.65282113888889, 143.92649552777777))
	if err != nil {
		t.Fatal(err)
	}
	if want := 306 + 52.0/60 + 5.37/3600; math.Abs(b1-want) > 1e-5 {
		t.Errorf("initial bearing %v, want %v", b1, want)
	}
	if want := 127 + 10.0/60 + 25.07/3600 + 180; math.Abs(b2-want) > 1e-5 {
		t.Errorf("final bearing %v, want %v", b2, want)
	}
}

// Geoscience Australia's published value for its worked example, to the
// millimetre (D-09), independent of the code that generated the vectors.
func TestInverseMatchesPublishedReference(t *testing.T) {
	d, err := DistanceM(ll(-37.95103341666667, 144.42486788888888), ll(-37.65282113888889, 143.92649552777777))
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(d-54972.271) > 0.001 {
		t.Errorf("got %v, want 54972.271 to 1 mm", d)
	}
}

func TestInverseNoConvergenceNearlyAntipodal(t *testing.T) {
	_, _, _, err := Inverse(ll(0, 0), ll(0.5, 179.7))
	if !errors.Is(err, ErrNoConvergence) {
		t.Fatalf("err = %v, want ErrNoConvergence", err)
	}
	if _, err := DistanceM(ll(0, 0), ll(0.5, 179.7)); !errors.Is(err, ErrNoConvergence) {
		t.Fatalf("DistanceM err = %v, want ErrNoConvergence", err)
	}
	// The twin: the same pair moved off antipodal converges.
	if _, err := DistanceM(ll(0, 0), ll(0.5, 170)); err != nil {
		t.Fatalf("err = %v, want convergence", err)
	}
}

func TestInverseCoincidentAndDegenerate(t *testing.T) {
	d, b1, b2, err := Inverse(ll(41.7, 44.8), ll(41.7, 44.8))
	if err != nil || d != 0 || b1 != 0 || b2 != 0 {
		t.Errorf("same point = %v %v %v %v, want zeros", d, b1, b2, err)
	}
	// The same place written two ways: nanometres, never an error.
	for _, c := range []struct{ a, b core.LatLon }{
		{ll(10, -180), ll(10, 180)},
		{ll(90, 0), ll(90, 120)},
		{ll(-90, 0), ll(-90, -45)},
	} {
		d, _, _, err := Inverse(c.a, c.b)
		if err != nil || d > 1e-6 {
			t.Errorf("Inverse(%v, %v) = %v %v, want ~0", c.a, c.b, d, err)
		}
	}
	// sinSigma exactly 0 inside the loop: positions that differ by a
	// subnormal longitude, which underflows to 0 in radians.
	d, _, _, err = Inverse(ll(0, 0), ll(0, math.SmallestNonzeroFloat64))
	if err != nil || d != 0 {
		t.Errorf("signed zero: %v %v", d, err)
	}
}

func TestInverseRefusesInvalid(t *testing.T) {
	for _, bad := range []core.LatLon{
		ll(math.NaN(), 0), ll(0, math.NaN()), ll(math.Inf(1), 0), ll(0, math.Inf(-1)),
		ll(91, 0), ll(-91, 0), ll(0, 181), ll(0, -181),
	} {
		for _, tc := range []struct {
			a, b  core.LatLon
			field string
		}{
			{bad, ll(0, 0), "a"},
			{ll(0, 0), bad, "b"},
		} {
			_, _, _, err := Inverse(tc.a, tc.b)
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Errorf("Inverse(%v, %v) err = %v, want field %q", tc.a, tc.b, err, tc.field)
			}
		}
	}
}

// geodesy.json#tbilisi-short-there and -back.
func TestInverseSymmetric(t *testing.T) {
	a, b := ll(41.7151, 44.8271), ll(41.7239, 44.8401)
	d1, err1 := DistanceM(a, b)
	d2, err2 := DistanceM(b, a)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if math.Abs(d1-d2) > 1e-9 {
		t.Errorf("there %v back %v, differ by %v", d1, d2, d1-d2)
	}
}

// Within 10 km the sphere and the ellipsoid agree to about 0.5 % (D-11):
// the sphere is good enough for a 300 m spoof threshold, not for edges.
// The exact worst case is a north-south line at the equator, where the
// meridional radius a(1-e^2) = 6,335,439 m is 0.561 % below the sphere's
// 6,371,008.8 m, so the bound asserted is that, not a round 0.5 %, which
// north-south pairs near the equator exceed.
func TestHaversineAgreesWithVincentyWithin10km(t *testing.T) {
	// 0.561 % plus 0.01 % for the step from a 10 km chord to the radius.
	sphereWorstRelErr := core.MeanEarthRadiusM/(core.WGS84SemiMajorM*(1-eccentricitySq)) - 1 + 1e-4
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 2000 {
		a := ll(rng.Float64()*170-85, rng.Float64()*360-180)
		bearing := rng.Float64() * 2 * math.Pi
		distM := 1 + rng.Float64()*9999
		n, e := distM*math.Cos(bearing), distM*math.Sin(bearing)
		// Close to distM: the sphere's degree, not the ellipsoid's.
		b := ll(a.LatDeg+degrees(n/core.MeanEarthRadiusM), core.WrapLonDeg(a.LonDeg+degrees(e/(core.MeanEarthRadiusM*math.Cos(radians(a.LatDeg))))))
		if !b.Valid() {
			continue
		}
		v, err := DistanceM(a, b)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		h := HaversineM(a, b)
		if math.Abs(h-v)/v > sphereWorstRelErr {
			t.Errorf("case %d %v -> %v: vincenty %v haversine %v (%.3f %%)", i, a, b, v, h, 100*math.Abs(h-v)/v)
		}
	}
}

func TestHaversineNonFiniteIsNaN(t *testing.T) {
	if d := HaversineM(ll(math.NaN(), 0), ll(0, 0)); !math.IsNaN(d) {
		t.Errorf("got %v, want NaN", d)
	}
	if d := HaversineM(ll(0, 0), ll(0, 180)); math.Abs(d-math.Pi*core.MeanEarthRadiusM) > 1e-6 {
		t.Errorf("half circumference %v", d)
	}
}

func TestBearingDegRange(t *testing.T) {
	for _, rad := range []float64{-math.Pi, -1e-18, 0, math.Pi / 2, math.Pi} {
		d := bearingDeg(rad)
		if d < 0 || d >= 360 {
			t.Errorf("bearingDeg(%v) = %v", rad, d)
		}
	}
}
