package geodesy

import (
	"errors"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// roundTripToleranceM is the promise of the WP-18 brief: Destination and
// Inverse agree to under 0.1 mm on the ground.
const roundTripToleranceM = 1e-4

// bearingGapM is the distance on the ground, at range d, between two
// bearings: the bearing agreement expressed in metres.
func bearingGapM(b1, b2, d float64) float64 {
	gap := math.Abs(math.Mod(b1-b2+540, 360) - 180)
	return radians(gap) * d
}

// checkRoundTrip asserts that Inverse from `from` to Destination(from,
// bearing, d) returns d and the bearing within the round-trip tolerance.
func checkRoundTrip(t *testing.T, from core.LatLon, bearing, d float64) core.LatLon {
	t.Helper()
	to := Destination(from, bearing, d)
	if !to.Valid() {
		t.Fatalf("Destination(%v, %v, %v) = %v, not valid", from, bearing, d, to)
	}
	got, b1, _, err := Inverse(from, to)
	if err != nil {
		t.Fatalf("Inverse(%v, %v): %v", from, to, err)
	}
	if math.Abs(got-d) > roundTripToleranceM {
		t.Errorf("from %v bearing %v: distance %v, want %v (off %v m)", from, bearing, got, d, got-d)
	}
	if gap := bearingGapM(b1, math.Mod(bearing+360, 360), d); gap > roundTripToleranceM {
		t.Errorf("from %v bearing %v d %v: bearing back %v, %v m off", from, bearing, d, b1, gap)
	}
	return to
}

// Geoscience Australia's worked example: Flinders Peak, 306 52 05.37,
// 54 972.271 m lands on Buninyong. The bearing is published to 0.01
// arc-second, which is up to 1.33 mm sideways at that range, so the
// published point bounds the result to that rounding; the round-trip
// tests below pin the sub-millimetre agreement with Inverse.
func TestDestinationMatchesPublishedReference(t *testing.T) {
	from := ll(-37.95103341666667, 144.42486788888888)
	bearing := 306 + 52.0/60 + 5.37/3600
	const distM = 54972.271
	got := Destination(from, bearing, distM)
	want := ll(-37.65282113888889, 143.92649552777777)
	d, err := DistanceM(got, want)
	if err != nil {
		t.Fatal(err)
	}
	if roundingM := radians(0.005/3600) * distM; d > roundingM {
		t.Errorf("Destination = %v, %v m from the published %v (bearing rounding %v m)", got, d, want, roundingM)
	}
}

func TestDestinationCardinal(t *testing.T) {
	// One degree of longitude on the equator, as Inverse measures it.
	got := Destination(ll(0, 0), 90, 111319.4907932264)
	if math.Abs(got.LatDeg) > 1e-12 || math.Abs(got.LonDeg-1) > 1e-9 {
		t.Errorf("due east = %v, want (0, 1)", got)
	}
	// Due south keeps the meridian.
	got = Destination(ll(10, 20), 180, 500000)
	if math.Abs(got.LonDeg-20) > 1e-12 || got.LatDeg >= 10 {
		t.Errorf("due south = %v", got)
	}
	// A bearing is taken modulo 360, negative or past a turn.
	for _, b := range []float64{-90, 630, 270 + 3600} {
		w := Destination(ll(0, 1), b, 111319.4907932264)
		if math.Abs(w.LatDeg) > 1e-12 || math.Abs(w.LonDeg) > 1e-9 {
			t.Errorf("bearing %v = %v, want (0, 0)", b, w)
		}
	}
}

// Both directions with the inverse over seeded random pairs, up to
// 10 000 km: Destination(a, Inverse(a, b)) lands on b, and Inverse(from,
// Destination(from, ..)) gives the distance and bearing back.
func TestDestinationRoundTripWithInverse(t *testing.T) {
	rng := rand.New(rand.NewPCG(18, 1))
	for i := range 2000 {
		from := ll(rng.Float64()*178-89, rng.Float64()*360-180)
		bearing := rng.Float64() * 360
		d := math.Pow(10, rng.Float64()*7) // 1 m to 10 000 km
		checkRoundTrip(t, from, bearing, d)

		b := ll(rng.Float64()*178-89, rng.Float64()*360-180)
		dist, az, _, err := Inverse(from, b)
		if errors.Is(err, ErrNoConvergence) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if dist > 1e7 {
			continue
		}
		got := Destination(from, az, dist)
		miss, err := DistanceM(got, b)
		if err != nil {
			t.Fatal(err)
		}
		if miss > roundTripToleranceM {
			t.Errorf("case %d: Destination(%v, %v, %v) = %v, %v m from %v", i, from, az, dist, got, miss, b)
		}
	}
}

// Past 10 000 km, up to the semi-circumference less a margin: seeded
// random lines from 10 000 to 18 000 km keep the full promise, distance
// and bearing back within 0.1 mm, and Destination(a, Inverse(a, b))
// lands on b. Up to 19 990 km the distance back still agrees; there the
// bearing is ill-conditioned (every geodesic to the antipode meets
// there), and a nearly antipodal pair Inverse cannot solve is skipped
// (D-09), never counted as agreement.
func TestDestinationRoundTripToHalfTurn(t *testing.T) {
	const fullPromiseM, distanceOnlyM = 1.8e7, 1.999e7
	rng := rand.New(rand.NewPCG(18, 2))
	solved := 0
	for i := range 2000 {
		from := ll(rng.Float64()*178-89, rng.Float64()*360-180)
		bearing := rng.Float64() * 360
		checkRoundTrip(t, from, bearing, 1e7+rng.Float64()*(fullPromiseM-1e7))

		d := fullPromiseM + rng.Float64()*(distanceOnlyM-fullPromiseM)
		to := Destination(from, bearing, d)
		got, _, _, err := Inverse(from, to)
		switch {
		case errors.Is(err, ErrNoConvergence):
		case err != nil:
			t.Fatalf("Inverse(%v, %v): %v", from, to, err)
		case math.Abs(got-d) > roundTripToleranceM:
			t.Errorf("case %d: from %v bearing %v: distance %v, want %v (off %v m)", i, from, bearing, got, d, got-d)
		default:
			solved++
		}

		b := ll(rng.Float64()*178-89, rng.Float64()*360-180)
		dist, az, _, err := Inverse(from, b)
		if errors.Is(err, ErrNoConvergence) || dist <= 1e7 || dist > fullPromiseM {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		landed := Destination(from, az, dist)
		miss, err := DistanceM(landed, b)
		if err != nil {
			t.Fatal(err)
		}
		if miss > roundTripToleranceM {
			t.Errorf("case %d: Destination(%v, %v, %v) = %v, %v m from %v", i, from, az, dist, landed, miss, b)
		}
	}
	// The skip must not swallow the check: almost every long line solves.
	if solved < 1900 {
		t.Errorf("only %d of 2000 lines past %v m were solved by Inverse", solved, fullPromiseM)
	}
}

func TestDestinationAcrossAntimeridian(t *testing.T) {
	east := checkRoundTrip(t, ll(0, 179.999), 90, 1000)
	if east.LonDeg > -179 || east.LonDeg < -180 {
		t.Errorf("eastwards across = %v, want just east of -180", east)
	}
	west := checkRoundTrip(t, ll(41.7, -179.995), 270, 2000)
	if west.LonDeg < 179 || west.LonDeg > 180 {
		t.Errorf("westwards across = %v, want just west of 180", west)
	}
	// Back again lands where it started.
	back := Destination(east, 270, 1000)
	if miss, err := DistanceM(back, ll(0, 179.999)); err != nil || miss > roundTripToleranceM {
		t.Errorf("back = %v, %v m off (%v)", back, miss, err)
	}
	// A long way round wraps more than once in the arithmetic.
	far := checkRoundTrip(t, ll(-10, 170), 80, 5e6)
	if !far.Valid() || far.LonDeg > 0 {
		t.Errorf("far = %v, want a western longitude", far)
	}
}

func TestDestinationNearAndAtPoles(t *testing.T) {
	for _, lat := range []float64{89.9999, -89.9999, 89.99999999} {
		for _, b := range []float64{0, 45, 90, 180, 270, 333.3} {
			checkRoundTrip(t, ll(lat, 44.8), b, 5000)
		}
	}
	// Due north across the north pole comes back down the opposite
	// meridian.
	from := ll(89.99, 10)
	over := Destination(from, 0, 3000)
	if math.Abs(over.LonDeg-(-170)) > 1e-9 || over.LatDeg >= 90 || over.LatDeg < 89.98 {
		t.Errorf("over the pole = %v, want on meridian -170", over)
	}
	if d, err := DistanceM(from, over); err != nil || math.Abs(d-3000) > roundTripToleranceM {
		t.Errorf("over the pole %v m (%v)", d, err)
	}
	// From a pole exactly: every bearing is south, measured from the
	// meridian of from.LonDeg, and the distance holds.
	for _, pole := range []core.LatLon{ll(90, 0), ll(-90, 30)} {
		for _, b := range []float64{0, 90, 200} {
			to := Destination(pole, b, 10000)
			if !to.Valid() {
				t.Fatalf("from %v bearing %v = %v", pole, b, to)
			}
			d, err := DistanceM(pole, to)
			if err != nil || math.Abs(d-10000) > roundTripToleranceM {
				t.Errorf("from %v bearing %v: %v m (%v)", pole, b, d, err)
			}
		}
	}
	// From the north pole a bearing b leaves along meridian lon+180-b.
	if to := Destination(ll(90, 0), 90, 10000); math.Abs(to.LonDeg-90) > 1e-6 {
		t.Errorf("from the pole bearing 90 = %v, want meridian 90", to)
	}
}

func TestDestinationZeroDistance(t *testing.T) {
	for _, p := range []core.LatLon{ll(41.71, 44.81), ll(90, 0), ll(-90, 180), ll(0, -180)} {
		if got := Destination(p, 123, 0); got != p {
			t.Errorf("Destination(%v, 123, 0) = %v, want it unchanged", p, got)
		}
	}
	// The accepted twin: the smallest step moves.
	if got := Destination(ll(41.71, 44.81), 0, 0.001); got.LatDeg <= 41.71 {
		t.Errorf("1 mm north = %v, did not move", got)
	}
}

// E-01: each refusal beside its accepted twin.
func TestDestinationRefusesInvalid(t *testing.T) {
	ok := ll(41.71, 44.81)
	cases := []struct {
		name          string
		from          core.LatLon
		bearing, dist float64
	}{
		{"lat NaN", ll(math.NaN(), 44.81), 0, 1},
		{"lat past 90", ll(90.0001, 44.81), 0, 1},
		{"lon past 180", ll(41.71, 180.0001), 0, 1},
		{"lon infinite", ll(41.71, math.Inf(-1)), 0, 1},
		{"bearing NaN", ok, math.NaN(), 1},
		{"bearing infinite", ok, math.Inf(1), 1},
		{"distance negative", ok, 0, -0.001},
		{"distance NaN", ok, 0, math.NaN()},
		{"distance infinite", ok, 0, math.Inf(1)},
		{"distance just past one turn", ok, 0, math.Nextafter(MaxDestinationDistanceM, math.Inf(1))},
		{"distance in millimetres by mistake", ok, 30, 5e9},
		{"distance 1e300", ok, 0, 1e300},
		{"distance MaxFloat64", ok, 0, math.MaxFloat64},
	}
	for _, c := range cases {
		got := Destination(c.from, c.bearing, c.dist)
		if !math.IsNaN(got.LatDeg) || !math.IsNaN(got.LonDeg) || got.Valid() {
			t.Errorf("%s: Destination = %v, want NaN", c.name, got)
		}
	}
	accepted := []struct {
		name          string
		from          core.LatLon
		bearing, dist float64
	}{
		{"lat 90", ll(90, 44.81), 0, 1},
		{"lon 180", ll(41.71, 180), 0, 1},
		{"lon -180", ll(41.71, -180), 0, 1},
		{"bearing large", ok, 1e6, 1},
		{"distance zero", ok, 0, 0},
		{"distance one turn", ok, 0, 4.0075e7},
		{"distance at the bound", ok, 0, MaxDestinationDistanceM},
		{"distance at the bound, oblique", ok, 30, MaxDestinationDistanceM},
	}
	for _, c := range accepted {
		if got := Destination(c.from, c.bearing, c.dist); !got.Valid() {
			t.Errorf("%s: Destination = %v, want a valid position", c.name, got)
		}
	}
}

func TestWrapLonDeg(t *testing.T) {
	for _, c := range [][2]float64{
		{0, 0}, {180, 180}, {-180, -180}, {181, -179}, {-181, 179},
		{540, 180}, {-540, -180}, {720.5, 0.5}, {-359, 1},
	} {
		got := wrapLonDeg(c[0])
		if math.Abs(got-c[1]) > 1e-9 && !(math.Abs(got) == 180 && math.Abs(c[1]) == 180) {
			t.Errorf("wrapLonDeg(%v) = %v, want %v", c[0], got, c[1])
		}
		if got < -180 || got > 180 {
			t.Errorf("wrapLonDeg(%v) = %v, out of range", c[0], got)
		}
	}
}
