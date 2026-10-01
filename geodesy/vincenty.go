package geodesy

import (
	"errors"
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// ErrNoConvergence is returned by Inverse when Vincenty's iteration on
// lambda does not settle within maxIterations, which happens only for
// nearly antipodal points, thousands of kilometres from any zone (D-09).
var ErrNoConvergence = errors.New("vincenty did not converge")

const (
	// semiMinorM is b = a(1 - f) of WGS84.
	semiMinorM = core.WGS84SemiMajorM * (1 - core.WGS84Flattening)
	// eccentricitySq is e^2 = f(2 - f) of WGS84.
	eccentricitySq = core.WGS84Flattening * (2 - core.WGS84Flattening)
	// lambdaToleranceRad is the change in lambda below which the
	// iteration has converged (about 0.006 mm on the ground).
	lambdaToleranceRad = 1e-12
	// maxIterations bounds the iteration; past it ErrNoConvergence.
	maxIterations = 200
)

// Inverse solves the inverse geodesic problem on the WGS84 ellipsoid with
// Vincenty's formulae (T. Vincenty, Survey Review 23, 1975): the distance
// in metres from a to b and the initial and final bearings in degrees
// clockwise from true north, in [0, 360).
//
// Coincident points return 0 for every value without iterating. An
// invalid position (non-finite or out of range) returns a
// *core.FieldError naming "a" or "b"; nearly antipodal points return
// ErrNoConvergence. It never panics.
func Inverse(a, b core.LatLon) (distanceM, initialBearingDeg, finalBearingDeg float64, err error) {
	if !a.Valid() {
		return 0, 0, 0, core.Fieldf("a", "not a valid WGS84 position (lat_deg %v, lon_deg %v)", a.LatDeg, a.LonDeg)
	}
	if !b.Valid() {
		return 0, 0, 0, core.Fieldf("b", "not a valid WGS84 position (lat_deg %v, lon_deg %v)", b.LatDeg, b.LonDeg)
	}
	if a == b {
		return 0, 0, 0, nil
	}
	const f = core.WGS84Flattening
	u1 := math.Atan((1 - f) * math.Tan(radians(a.LatDeg)))
	u2 := math.Atan((1 - f) * math.Tan(radians(b.LatDeg)))
	bigL := radians(b.LonDeg - a.LonDeg)
	sinU1, cosU1 := math.Sincos(u1)
	sinU2, cosU2 := math.Sincos(u2)

	lambda := bigL
	var sinLambda, cosLambda, sinSigma, cosSigma, sigma, cos2Alpha, cos2SigmaM float64
	converged := false
	for range maxIterations {
		sinLambda, cosLambda = math.Sincos(lambda)
		sinSigma = math.Hypot(cosU2*sinLambda, cosU1*sinU2-sinU1*cosU2*cosLambda)
		if sinSigma == 0 {
			// Coincident in a way equality did not catch (lon -180 and
			// 180, or both at a pole).
			return 0, 0, 0, nil
		}
		cosSigma = sinU1*sinU2 + cosU1*cosU2*cosLambda
		sigma = math.Atan2(sinSigma, cosSigma)
		sinAlpha := cosU1 * cosU2 * sinLambda / sinSigma
		cos2Alpha = 1 - sinAlpha*sinAlpha
		// On the equator cos2Alpha is 0 and the term is not used.
		cos2SigmaM = 0
		if cos2Alpha != 0 {
			cos2SigmaM = cosSigma - 2*sinU1*sinU2/cos2Alpha
		}
		c := f / 16 * cos2Alpha * (4 + f*(4-3*cos2Alpha))
		previous := lambda
		lambda = bigL + (1-c)*f*sinAlpha*
			(sigma+c*sinSigma*(cos2SigmaM+c*cosSigma*(-1+2*cos2SigmaM*cos2SigmaM)))
		if math.Abs(lambda-previous) < lambdaToleranceRad {
			converged = true
			break
		}
	}
	if !converged {
		return 0, 0, 0, ErrNoConvergence
	}

	const aSq = core.WGS84SemiMajorM * core.WGS84SemiMajorM
	const bSq = semiMinorM * semiMinorM
	uSq := cos2Alpha * (aSq - bSq) / bSq
	bigA := 1 + uSq/16384*(4096+uSq*(-768+uSq*(320-175*uSq)))
	bigB := uSq / 1024 * (256 + uSq*(-128+uSq*(74-47*uSq)))
	deltaSigma := bigB * sinSigma * (cos2SigmaM + bigB/4*
		(cosSigma*(-1+2*cos2SigmaM*cos2SigmaM)-
			bigB/6*cos2SigmaM*(-3+4*sinSigma*sinSigma)*(-3+4*cos2SigmaM*cos2SigmaM)))
	distanceM = semiMinorM * bigA * (sigma - deltaSigma)

	initialBearingDeg = bearingDeg(math.Atan2(cosU2*sinLambda, cosU1*sinU2-sinU1*cosU2*cosLambda))
	finalBearingDeg = bearingDeg(math.Atan2(cosU1*sinLambda, -sinU1*cosU2+cosU1*sinU2*cosLambda))
	return distanceM, initialBearingDeg, finalBearingDeg, nil
}

// DistanceM is the geodesic distance in metres from a to b on WGS84
// (Inverse without the bearings). Use it for every zone edge and circle.
func DistanceM(a, b core.LatLon) (float64, error) {
	d, _, _, err := Inverse(a, b)
	return d, err
}

// HaversineM is the great-circle distance in metres on the sphere of
// radius core.MeanEarthRadiusM. It exists for the spoof-distance check
// only (D-11, S-10), where a 300 m threshold tolerates the sphere's error
// of up to 0.5 %. Never use it for zone edges or circles: use DistanceM.
// A non-finite input gives NaN; it never panics.
func HaversineM(a, b core.LatLon) float64 {
	phi1, phi2 := radians(a.LatDeg), radians(b.LatDeg)
	sinDPhi := math.Sin((phi2 - phi1) / 2)
	sinDLambda := math.Sin(radians(b.LonDeg-a.LonDeg) / 2)
	h := sinDPhi*sinDPhi + math.Cos(phi1)*math.Cos(phi2)*sinDLambda*sinDLambda
	return 2 * core.MeanEarthRadiusM * math.Asin(math.Min(1, math.Sqrt(h)))
}

func radians(deg float64) float64 { return deg * math.Pi / 180 }

func degrees(rad float64) float64 { return rad * 180 / math.Pi }

// bearingDeg converts an azimuth in radians to degrees in [0, 360).
func bearingDeg(rad float64) float64 {
	return math.Mod(degrees(rad)+360, 360)
}
