package ed318

import (
	"encoding/json"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
)

// Limits bounds what one parse may cost and the lengths ED-318 sets
// (E-10). It is ed269's: IdentifierMax (7), NameMax (every textShortType
// text, 200), MessageMax (message texts, 200), ReasonsMax (9),
// MaxRingVertices, MaxProblems, MaxDepth and MaxBytes apply here;
// OtherReasonMax, USpaceClassMax and AuthorityTextMax are ED-269's and are
// not used (ED-318 bounds those texts by NameMax). A zero field takes its
// ed269.DefaultLimits value.
type Limits = ed269.Limits

// Text is an ED-318 textShortType: a text and the language it is in
// (`lang`, required, at most five characters, "en-GB"). Text is optional
// in the schema; nil is absent.
type Text struct {
	Text  *string
	Lang  string
	Extra map[string]json.RawMessage
}

// DateTime is an RFC 3339 date-time as published: the instant and the
// text it was written as, so that Export writes it back unchanged. A
// DateTime built in code may leave Text empty; Export then writes
// Time.Format(time.RFC3339Nano).
type DateTime struct {
	Time time.Time
	Text string
}

// Metadata is the collection's `metadata` (ED-318 schema
// Schema_GeoZoneCollectionMetadata.json; uas_standards DatasetMetadata).
// Every member is optional.
type Metadata struct {
	ValidFrom            *DateTime
	ValidTo              *DateTime
	Issued               *DateTime
	Provider             []Text
	Description          []Text
	OtherGeoid           *string
	TechnicalLimitations []Text
	// Extra holds members the schema does not define (it allows them),
	// kept as published so that Export writes them back.
	Extra map[string]json.RawMessage
}

// DataSource is a zone's `dataSource` (schema definition `metadata`).
// The schema names the creation time creationDate and uas_standards names
// it creationDateTime; both are read, each into its own field, and
// written back as read. Originator is a textShortType (schema;
// uas_standards has a plain string).
type DataSource struct {
	CreationDate     *DateTime
	CreationDateTime *DateTime
	UpdateDateTime   *DateTime
	Originator       *Text
	Extra            map[string]json.RawMessage
}

// Authority is one `zoneAuthority` entry. Purpose is required
// (AUTHORIZATION, NOTIFICATION or INFORMATION). SiteURL, Email and Phone
// are plain strings (schema; uas_standards writes them as textShortType).
type Authority struct {
	Name           []Text
	Service        []Text
	ContactName    []Text
	SiteURL        *string
	Email          *string
	Phone          *string
	Purpose        string
	IntervalBefore *string
	Extra          map[string]json.RawMessage
}

// The daylight events of ED-318 CodeDaylightEventType.
const (
	// EventBMCT is the beginning of morning civil twilight (sun centre 6
	// degrees below the horizon, rising).
	EventBMCT = "BMCT"
	// EventSR is sunrise.
	EventSR = "SR"
	// EventSS is sunset.
	EventSS = "SS"
	// EventEECT is the end of evening civil twilight (sun centre 6 degrees
	// below the horizon, setting).
	EventEECT = "EECT"
)

// DailyPeriod is one `schedule` entry: the days it applies on (MON..SUN or
// ANY) and a window that starts at a clock time or a daylight event and
// ends at a clock time or a daylight event. Exactly one of StartTime and
// StartEvent is set, and exactly one of EndTime and EndEvent. Times are
// RFC 3339 full-time with an offset ("16:00:00Z", "08:00:00+04:00"),
// kept as published.
type DailyPeriod struct {
	Day        []string
	StartTime  *string
	StartEvent *string
	EndTime    *string
	EndEvent   *string
}

// TimePeriod is one `limitedApplicability` entry. StartDateTime and
// EndDateTime (both included) bound it and Schedule, when given, narrows
// it to daily windows. A TimePeriod with none of them applies at all
// times. ED-318 has no `permanent`: a zone with no limitedApplicability
// applies always.
type TimePeriod struct {
	StartDateTime *DateTime
	EndDateTime   *DateTime
	Schedule      []DailyPeriod
}

// UASZone is a feature's `properties` (ED-318 Schema_GeoZoneProperties,
// uas_standards UASZone). Optional members are nil when absent; for the
// list members nil is absent and an empty non-nil slice is a published
// empty list.
type UASZone struct {
	Identifier string
	Country    string
	Name       []Text
	// Type is the ED-318 zone type, in ED-318's spelling
	// (REQ_AUTHORIZATION with a Z).
	Type core.ZoneType
	// Variant is COMMON or the customised variant (spelt with a Z, as
	// ED-318 publishes it).
	Variant               string
	RestrictionConditions *string
	Region                *int
	Reason                []string
	OtherReasonInfo       []Text
	RegulationExemption   *string
	Message               []Text
	// ExtendedProperties is ED-318's extension object, each member kept
	// as published (compact JSON). The U-space airspace requirements of
	// 2021/664 Art. 3(4) travel here (spec 02 F1, 04 section 4).
	ExtendedProperties   map[string]json.RawMessage
	LimitedApplicability []TimePeriod
	ZoneAuthority        []Authority
	DataSource           *DataSource
}

// The geometry types a zone may have.
const (
	GeometryPolygon    = "Polygon"
	GeometryPoint      = "Point"
	GeometryCollection = "GeometryCollection"
)

// The two units of a layer.
const (
	UomMetres = "m"
	UomFeet   = "ft"
)

// Layer is a geometry's `layer` (ED-318 Schema_LayeredGeoJSON.json): the
// vertical extent. Values are as written, in Uom ("m" or "ft"; absent
// means metres); a nil Lower is the surface and a nil Upper is unlimited.
// A value always has its reference; a reference may come without a value.
type Layer struct {
	Upper          *float64
	UpperReference core.VerticalRef
	Lower          *float64
	LowerReference core.VerticalRef
	Uom            *string
	Extra          map[string]json.RawMessage
}

// Geometry is a feature's GeoJSON geometry with ED-318's layer. A zone is
// a Polygon (Rings, exterior first, each closed), a Point with a circle
// extent (Center and RadiusM), or a GeometryCollection of those
// (Geometries, one per vertical layer, as ED-318's two-layer example).
// Positions are GeoJSON [longitude, latitude] on the wire and core.LatLon
// here.
type Geometry struct {
	Type       string
	Rings      [][]core.LatLon
	Center     *core.LatLon
	RadiusM    *float64
	Layer      *Layer
	Geometries []Geometry
	BBox       []float64
	Extra      map[string]json.RawMessage
	// ExtentExtra keeps members of a Point's `extent` beyond subType and
	// radius.
	ExtentExtra map[string]json.RawMessage
}

// Feature is one GeoJSON feature: a UASZone and its geometry. ID is the
// GeoJSON `id` (a string or a number) as published.
type Feature struct {
	Type       string
	ID         json.RawMessage
	Geometry   Geometry
	Properties UASZone
	BBox       []float64
	Extra      map[string]json.RawMessage
}

// FeatureCollection is a whole ED-318 file. Title and Description are not
// in the ED-318 schema but appear in its examples (`title`) and in the
// Swiss sample (`description`); they are read and written as published,
// as is any other top-level member, in Extra.
type FeatureCollection struct {
	Type        string
	Name        *string
	Title       *string
	Description *string
	BBox        []float64
	Metadata    *Metadata
	Features    []Feature
	Extra       map[string]json.RawMessage
}
