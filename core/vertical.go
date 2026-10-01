package core

// VerticalRef names the datum an altitude or a zone limit is referenced
// to. Every altitude also names its datum in its field name (E-13); this
// type exists for zone limits and for values whose datum is data.
type VerticalRef string

const (
	// RefAGL is height above the ground surface (DEM). Used for the
	// 120 m rule and AGL-referenced zone limits only (LESSONS D-01).
	RefAGL VerticalRef = "AGL"
	// RefAMSL is orthometric height above mean sea level (EGM2008 by
	// default). Separation is judged in AMSL.
	RefAMSL VerticalRef = "AMSL"
	// RefWGS84 is height above the WGS84 ellipsoid (HAE). As an ED-269
	// vertical reference it is this project's extension (LESSONS Z-05).
	RefWGS84 VerticalRef = "WGS84"
)

// Valid reports whether r is one of the three references.
func (r VerticalRef) Valid() bool {
	switch r {
	case RefAGL, RefAMSL, RefWGS84:
		return true
	}
	return false
}

// AltSource says which rule produced a track's AMSL altitude (04 §3.1).
type AltSource string

const (
	// AltGeodetic: HAE through the geoid.
	AltGeodetic AltSource = "geodetic"
	// AltPressure: the broadcast pressure altitude (ISA 1013.25 hPa, not
	// AMSL) standing in for a missing or poor geodetic one (LESSONS R-08).
	// Every vertical judgement on it is widened (R-09).
	AltPressure AltSource = "pressure"
	// AltNetwork: an altitude a network provider asserted (F3411 alt).
	AltNetwork AltSource = "network"
	// AltNone: no usable altitude. Vertical judgements do not run.
	AltNone AltSource = "none"
)

// Altitude is a value with its datum, for zone limits and F3548 volumes.
type Altitude struct {
	ValueM float64     `json:"value_m"`
	Ref    VerticalRef `json:"ref"`
}
