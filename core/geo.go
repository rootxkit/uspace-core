package core

import "math"

// LatLon is a WGS84 position in degrees. Latitude is positive north,
// longitude positive east. JSON order follows the vectors (lat_deg,
// lon_deg); GeoJSON on the wire is [lon, lat] and is converted at the
// parser boundary (LESSONS Z-03), never here.
type LatLon struct {
	LatDeg float64 `json:"lat_deg"`
	LonDeg float64 `json:"lon_deg"`
}

// Valid reports whether both coordinates are finite and within range.
// Non-finite values must be refused before they reach any spatial index
// (LESSONS C-09: an inf latitude overflowed a floor() in the old grid).
func (p LatLon) Valid() bool {
	return isFinite(p.LatDeg) && isFinite(p.LonDeg) &&
		p.LatDeg >= -90 && p.LatDeg <= 90 &&
		p.LonDeg >= -180 && p.LonDeg <= 180
}

// WrapLonDeg wraps a longitude difference or an advanced longitude into
// (-180, 180] (LESSONS D-10: two aircraft straddling the antimeridian are
// 223 m apart, not 40,000 km).
func WrapLonDeg(lonDeg float64) float64 {
	if !isFinite(lonDeg) {
		return lonDeg
	}
	w := math.Mod(lonDeg+180, 360)
	if w < 0 {
		w += 360
	}
	w -= 180
	if w == -180 {
		return 180
	}
	return w
}

// FeetToMetres is exact (LESSONS Z-08).
const FeetToMetres = 0.3048

// MeanEarthRadiusM is the sphere radius of the haversine used for the
// spoof-distance check only (LESSONS D-11). Zone edges use Vincenty.
const MeanEarthRadiusM = 6_371_008.8

// WGS84 ellipsoid constants.
const (
	WGS84SemiMajorM = 6_378_137.0
	WGS84Flattening = 1 / 298.257223563
)

func isFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// IsFinite reports whether f is neither NaN nor infinite.
func IsFinite(f float64) bool { return isFinite(f) }
