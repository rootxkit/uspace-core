package geodesy

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// sigmaToleranceRad is the change in sigma below which the direct
// iteration has converged (about 0.006 mm on the ground), as
// lambdaToleranceRad is for Inverse.
const sigmaToleranceRad = 1e-12

// Destination solves the direct geodesic problem on the WGS84 ellipsoid
// with Vincenty's formulae (T. Vincenty, Survey Review 23, 1975), the
// counterpart of Inverse: the position reached from `from` after
// distanceM metres along the geodesic that starts at bearingDeg degrees
// clockwise from true north.
//
// Any finite bearing is taken modulo 360. A zero distance returns from
// unchanged. The longitude of the result is wrapped into [-180, 180]. From
// a pole the bearing is measured from the meridian of from.LonDeg.
//
// An invalid from (non-finite or out of range), a non-finite bearing, or
// a negative or non-finite distance returns NaN for both coordinates,
// which is not Valid(). It never panics.
func Destination(from core.LatLon, bearingDeg, distanceM float64) core.LatLon {
	if !from.Valid() || !core.IsFinite(bearingDeg) || !core.IsFinite(distanceM) || distanceM < 0 {
		return core.LatLon{LatDeg: math.NaN(), LonDeg: math.NaN()}
	}
	if distanceM == 0 {
		return from
	}
	const f = core.WGS84Flattening
	sinAlpha1, cosAlpha1 := math.Sincos(radians(math.Mod(bearingDeg, 360)))
	tanU1 := (1 - f) * math.Tan(radians(from.LatDeg))
	cosU1 := 1 / math.Sqrt(1+tanU1*tanU1)
	sinU1 := tanU1 * cosU1
	sigma1 := math.Atan2(tanU1, cosAlpha1)
	sinAlpha := cosU1 * sinAlpha1
	cos2Alpha := 1 - sinAlpha*sinAlpha

	const aSq = core.WGS84SemiMajorM * core.WGS84SemiMajorM
	const bSq = semiMinorM * semiMinorM
	uSq := cos2Alpha * (aSq - bSq) / bSq
	bigA := 1 + uSq/16384*(4096+uSq*(-768+uSq*(320-175*uSq)))
	bigB := uSq / 1024 * (256 + uSq*(-128+uSq*(74-47*uSq)))

	// sigma = s/(bA) + deltaSigma(sigma) is a contraction by about f: it
	// settles in a handful of steps for any distance, and maxIterations
	// only bounds the loop.
	sigma0 := distanceM / (semiMinorM * bigA)
	sigma := sigma0
	for range maxIterations {
		next := sigma0 + deltaSigmaDirect(bigB, sigma1, sigma)
		done := math.Abs(next-sigma) < sigmaToleranceRad
		sigma = next
		if done {
			break
		}
	}

	sinSigma, cosSigma := math.Sincos(sigma)
	cos2SigmaM := math.Cos(2*sigma1 + sigma)
	x := sinU1*sinSigma - cosU1*cosSigma*cosAlpha1
	lat := math.Atan2(sinU1*cosSigma+cosU1*sinSigma*cosAlpha1, (1-f)*math.Hypot(sinAlpha, x))
	lambda := math.Atan2(sinSigma*sinAlpha1, cosU1*cosSigma-sinU1*sinSigma*cosAlpha1)
	c := f / 16 * cos2Alpha * (4 + f*(4-3*cos2Alpha))
	bigL := lambda - (1-c)*f*sinAlpha*
		(sigma+c*sinSigma*(cos2SigmaM+c*cosSigma*(-1+2*cos2SigmaM*cos2SigmaM)))
	return core.LatLon{LatDeg: degrees(lat), LonDeg: wrapLonDeg(from.LonDeg + degrees(bigL))}
}

// deltaSigmaDirect is Vincenty's deltaSigma at sigma for the direct
// problem, where 2 sigma_m = 2 sigma1 + sigma.
func deltaSigmaDirect(bigB, sigma1, sigma float64) float64 {
	sinSigma, cosSigma := math.Sincos(sigma)
	cos2SigmaM := math.Cos(2*sigma1 + sigma)
	return bigB * sinSigma * (cos2SigmaM + bigB/4*
		(cosSigma*(-1+2*cos2SigmaM*cos2SigmaM)-
			bigB/6*cos2SigmaM*(-3+4*sinSigma*sinSigma)*(-3+4*cos2SigmaM*cos2SigmaM)))
}

// wrapLonDeg wraps a finite longitude into [-180, 180].
func wrapLonDeg(lonDeg float64) float64 {
	if lonDeg >= -180 && lonDeg <= 180 {
		return lonDeg
	}
	w := math.Mod(lonDeg+180, 360)
	if w < 0 {
		w += 360
	}
	return w - 180
}
