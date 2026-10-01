package ed269

import (
	"encoding/json"

	"github.com/rootxkit/uspace-core/core"
)

// Restriction is the ED-269 `restriction` of a zone, in ED-269's spelling
// (REQ_AUTHORISATION with an S). The spelling is kept exactly so that a
// file round-trips; ZoneType converts to the ED-318 value (LESSONS Z-04).
type Restriction string

// The four ED-269 restrictions.
const (
	RestrictionProhibited       Restriction = "PROHIBITED"
	RestrictionReqAuthorisation Restriction = "REQ_AUTHORISATION"
	RestrictionConditional      Restriction = "CONDITIONAL"
	RestrictionNoRestriction    Restriction = "NO_RESTRICTION"
)

// ZoneType returns the ED-318 zone type of r, or "" when r is not one of
// the four ED-269 values.
func (r Restriction) ZoneType() core.ZoneType {
	t, ok := core.ZoneTypeFromED269(string(r))
	if !ok {
		return ""
	}
	return t
}

// Reason is one entry of a zone's `reason` list (InterUSS uas_standards).
type Reason string

// The ED-269 reasons.
const (
	ReasonAirTraffic       Reason = "AIR_TRAFFIC"
	ReasonSensitive        Reason = "SENSITIVE"
	ReasonPrivacy          Reason = "PRIVACY"
	ReasonPopulation       Reason = "POPULATION"
	ReasonNature           Reason = "NATURE"
	ReasonNoise            Reason = "NOISE"
	ReasonForeignTerritory Reason = "FOREIGN_TERRITORY"
	ReasonEmergency        Reason = "EMERGENCY"
	ReasonOther            Reason = "OTHER"
)

// Uom is a volume's `uomDimensions`: the unit of its limits and radius.
type Uom string

// The two units.
const (
	UomMetres Uom = "M"
	UomFeet   Uom = "FT"
)

// Purpose is a zone authority's `purpose`, in ED-269's spelling
// (AUTHORIZATION with a Z, unlike the restriction).
type Purpose string

// The three purposes.
const (
	PurposeAuthorization Purpose = "AUTHORIZATION"
	PurposeNotification  Purpose = "NOTIFICATION"
	PurposeInformation   Purpose = "INFORMATION"
)

// YesNo is ED-269's YESNO enumeration.
type YesNo string

// The two YesNo values.
const (
	Yes YesNo = "YES"
	No  YesNo = "NO"
)

// VerticalRef is a volume's vertical reference: AGL, AMSL or, as this
// project's extension, WGS84 (LESSONS Z-05).
type VerticalRef = core.VerticalRef

// Position is a WGS84 position. ED-269 files write it as GeoJSON
// [longitude, latitude]; it is converted at the parser boundary (Z-03).
type Position = core.LatLon

// The two horizontal projection types.
const (
	ShapePolygon = "Polygon"
	ShapeCircle  = "Circle"
)

// HorizontalProjection is a volume's `horizontalProjection`: a Polygon
// (Rings, exterior first then holes, each closed) or a Circle (Center and
// Radius). Radius is as written, in the volume's Uom; Volume.RadiusM
// converts it.
type HorizontalProjection struct {
	Type   string
	Rings  [][]Position
	Center *Position
	Radius *float64
}

// Volume is one `UASZoneAirspaceVolume`. Limits are kept as written, in
// Uom; a nil lower limit is the surface and a nil upper limit is
// unlimited. LowerM, UpperM and RadiusM convert to metres.
type Volume struct {
	Uom        Uom
	LowerLimit *float64
	UpperLimit *float64
	LowerRef   VerticalRef
	UpperRef   VerticalRef
	Projection HorizontalProjection
}

// toM converts a value in v.Uom to metres; feet use core.FeetToMetres
// exactly (LESSONS Z-08).
func (v Volume) toM(value *float64) *float64 {
	if value == nil {
		return nil
	}
	m := *value
	if v.Uom == UomFeet {
		m *= core.FeetToMetres
	}
	return &m
}

// LowerM is the lower limit in metres, nil when absent.
func (v Volume) LowerM() *float64 { return v.toM(v.LowerLimit) }

// UpperM is the upper limit in metres, nil when absent.
func (v Volume) UpperM() *float64 { return v.toM(v.UpperLimit) }

// RadiusM is a circle's radius in metres, nil for a polygon.
func (v Volume) RadiusM() *float64 {
	if v.Projection.Type != ShapeCircle {
		return nil
	}
	return v.toM(v.Projection.Radius)
}

// Authority is one `zoneAuthority` entry. Absent and null members are nil.
type Authority struct {
	Name           *string
	Service        *string
	ContactName    *string
	SiteURL        *string
	Email          *string
	Phone          *string
	Purpose        *Purpose
	IntervalBefore *string
}

// GeoZone is one ED-269 `UASZoneVersion` as published. Optional fields
// are pointers (nil: absent or null in the file, and absent on export).
// For the list fields, nil is absent and an empty non-nil slice is a
// published empty list.
type GeoZone struct {
	Identifier string
	Country    string
	Name       *string
	// Type is the ED-269 zone type: COMMON or the customised type (spelt
	// with a Z in ED-269).
	Type            string
	Restriction     Restriction
	Reason          []Reason
	Message         *string
	OtherReasonInfo *string
	// RestrictionConditions is published as a string or a list of
	// strings; RestrictionConditionsIsText says it was a single string
	// (then it holds one element), so that it is written back the same way.
	RestrictionConditions       []string
	RestrictionConditionsIsText bool
	Region                      *int
	RegulationExemption         *YesNo
	USpaceClass                 *string
	// Title is the zone-level `title` of uas_standards' UASZoneVersion.
	Title         *string
	Applicability []Period
	ZoneAuthority []Authority
	// Geometry holds exactly one volume when parsed (LESSONS Z-04: a zone
	// with more than one volume is refused rather than half-imported).
	Geometry []Volume
	// ExtendedProperties is any JSON value, kept as published (compact,
	// key order and number spelling preserved); nil when absent.
	ExtendedProperties json.RawMessage
}

// Volume returns the zone's single volume, or false when Geometry does
// not hold exactly one.
func (z *GeoZone) Volume() (Volume, bool) {
	if len(z.Geometry) != 1 {
		return Volume{}, false
	}
	return z.Geometry[0], true
}

// The two published document wrappers (LESSONS Z-03): InterUSS's
// ED269Schema and Luxembourg's file use `features`; the Swiss sample uses
// `UASZoneList`.
const (
	WrapperFeatures    = "features"
	WrapperUASZoneList = "UASZoneList"
)

// Document is a whole ED-269 file. Title and Description belong to the
// `features` wrapper; FormatVersion and CreatedAt to `UASZoneList`.
// Export writes the wrapper the document was read with.
type Document struct {
	Title         *string
	Description   *string
	FormatVersion *string
	CreatedAt     *string
	Zones         []GeoZone
	Wrapper       string
}
