package f3548

import (
	"strconv"

	"github.com/rootxkit/uspace-core/core"
)

// wire converts a value the generated types hold as float32 (the OpenAPI
// `format: float`) to the float64 the sender wrote: the shortest decimal
// that reads back as the float32.
func wire(f float32) float64 {
	// FormatFloat's output always parses (Inf and NaN included).
	v, _ := strconv.ParseFloat(strconv.FormatFloat(float64(f), 'g', -1, 32), 64)
	return v
}

// LatLon is the point as a core.LatLon.
func (p LatLngPoint) LatLon() core.LatLon {
	return core.LatLon{LatDeg: p.Lat, LonDeg: p.Lng}
}

// DSSStates are the four OperationalIntentState values the DSS holds
// (spec 09 section 1.5); states beyond them are a USSP's local_state, never
// written to the DSS.
var DSSStates = []OperationalIntentState{Accepted, Activated, Nonconforming, Contingent}
