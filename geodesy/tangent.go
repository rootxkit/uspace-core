package geodesy

import (
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// radiiM returns the WGS84 meridional radius M and prime-vertical radius
// N, in metres, at a geodetic latitude in radians:
// M = a(1-e^2)/(1-e^2 sin^2)^1.5 and N = a/sqrt(1-e^2 sin^2).
func radiiM(phiRad float64) (meridionalM, primeVerticalM float64) {
	s := math.Sin(phiRad)
	d := math.Sqrt(1 - eccentricitySq*s*s)
	primeVerticalM = core.WGS84SemiMajorM / d
	meridionalM = core.WGS84SemiMajorM * (1 - eccentricitySq) / (d * d * d)
	return meridionalM, primeVerticalM
}

// offsetAtM projects the difference from a to b onto the tangent plane
// whose radii are taken at latitude phi0Deg.
func offsetAtM(phi0Deg float64, a, b core.LatLon) (northM, eastM float64) {
	phi0 := radians(phi0Deg)
	meridionalM, primeVerticalM := radiiM(phi0)
	northM = radians(b.LatDeg-a.LatDeg) * meridionalM
	dLonDeg := core.WrapLonDeg(b.LonDeg - a.LonDeg)
	eastM = radians(dLonDeg) * primeVerticalM * math.Cos(phi0)
	return northM, eastM
}

// LocalOffsetM is the north and east offset in metres of p from origin on
// the local tangent plane at the origin, using the WGS84 meridional and
// prime-vertical radii at the origin's latitude (D-10). The longitude
// difference is wrapped into (-180, 180], so a pair straddling the
// antimeridian is metres apart, not 40,000 km. Accurate to far better
// than a position fix over a few kilometres; not a geodesic. A
// non-finite input gives NaN; it never panics.
func LocalOffsetM(origin, p core.LatLon) (northM, eastM float64) {
	return offsetAtM(origin.LatDeg, origin, p)
}

// LocalOffsetAboutMidLatM is the north and east offset in metres of b
// from a, with the radii taken at the mid-latitude (a.lat + b.lat)/2. It
// is the CPA projection the cpa vectors were generated with: symmetric in
// a and b up to sign.
func LocalOffsetAboutMidLatM(a, b core.LatLon) (northM, eastM float64) {
	return offsetAtM((a.LatDeg+b.LatDeg)/2, a, b)
}
