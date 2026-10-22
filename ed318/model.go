package ed318

import (
	"encoding/json"
	"maps"
	"strings"
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

// The members of a layer, ED-318's names for a zone's vertical limits
// (UNVERIFIED against the EUROCAE text; the package doc names the
// sources). This is the one table of them: the parser reads these and
// no other, the exporter writes these, and a system that writes or reads
// a layer itself compares its names with LayerMembers.
const (
	LayerKeyUpper          = "upper"
	LayerKeyUpperReference = "upperReference"
	LayerKeyLower          = "lower"
	LayerKeyLowerReference = "lowerReference"
	LayerKeyUom            = "uom"
)

// LayerMembers is ED-318's layer members in the schema's order (a new
// slice on each call).
func LayerMembers() []string {
	return []string{LayerKeyUpper, LayerKeyUpperReference, LayerKeyLower, LayerKeyLowerReference, LayerKeyUom}
}

// UomValues is the units a layer's uom may take (a new slice on each
// call).
func UomValues() []string { return []string{UomMetres, UomFeet} }

// ed269LayerNames maps ED-269's name for each vertical limit, found where
// a layer member belongs, to the ED-318 name it stands for. They are the
// members of an ED-269 UASZoneAirspaceVolume (InterUSS uas_standards
// src/uas_standards/eurocae_ed269.py at
// 6e182f43ec960b3bccf131c9006ff9979dd8a56e; the Cyprus DCA ED-269 file
// CYP_GeoZones_06_05_25.json writes all five).
var ed269LayerNames = map[string]string{
	"upperLimit":             LayerKeyUpper,
	"upperVerticalReference": LayerKeyUpperReference,
	"lowerLimit":             LayerKeyLower,
	"lowerVerticalReference": LayerKeyLowerReference,
	"uomDimensions":          LayerKeyUom,
}

// caseSpelt is the layer members whose spellings that differ from them
// in ASCII letter case only (UOM, Uom, Upper, LOWER, UpperReference,
// lowerreference and the rest; caseSpellings) are refused inside a
// layer: all five. JSON member names are case-sensitive and every source
// read names the members as LayerMembers does (doc.go), so a producer
// that spells one otherwise has made a mistake that would silently lose
// a vertical bound: kept as an extra, an Upper leaves the layer read as
// lower to unlimited, a Lower as the surface to upper, and a UOM a limit
// in feet read as metres (owner decisions 2026-10-22 for uom, from the
// review of PR #54, and 2026-10-23 for the other four).
var caseSpelt = []string{LayerKeyUpper, LayerKeyUpperReference, LayerKeyLower, LayerKeyLowerReference, LayerKeyUom}

// caseSpellings is every spelling of member that differs from it in
// ASCII letter case only, member itself not among them: 2^n - 1 of them
// for n letters (7 for uom, 31 for upper, 16383 for upperReference).
func caseSpellings(member string) []string {
	var letters []int
	for i := range len(member) {
		if lowerASCII(member[i]) != upperASCII(member[i]) {
			letters = append(letters, i)
		}
	}
	out := make([]string, 0, 1<<len(letters)-1)
	for mask := 1; mask < 1<<len(letters); mask++ {
		b := []byte(member)
		for bit, i := range letters {
			if mask&(1<<bit) != 0 {
				if b[i] == lowerASCII(b[i]) {
					b[i] = upperASCII(b[i])
				} else {
					b[i] = lowerASCII(b[i])
				}
			}
		}
		out = append(out, string(b))
	}
	return out
}

// caseSpelling reports the member of caseSpelt that k spells in another
// case: k differs from it in ASCII letter case only and is not it.
func caseSpelling(k string) (string, bool) {
	for _, m := range caseSpelt {
		if k != m && equalFoldASCII(k, m) {
			return m, true
		}
	}
	return "", false
}

// equalFoldASCII reports whether a and b are equal but for ASCII letter
// case. Unlike strings.EqualFold it folds no other character, so it
// matches exactly what caseSpellings enumerates.
func equalFoldASCII(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if lowerASCII(a[i]) != lowerASCII(b[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func upperASCII(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

// layerVariant reports the ED-318 name that k stands for when k is a
// known name that is not ED-318's, found where a layer member belongs:
// one of ED-269's names (ed269LayerNames), or a member spelt in another
// case (caseSpelt). Inside an ED-318 layer they are refused by name,
// never read as their ED-318 counterpart and never kept as extras: kept,
// a layer of them would be a zone from the surface to unlimited, an
// uomDimensions or a UOM beside upper would leave the limit read as
// metres, and an Upper or a Lower would lose its bound. Any other unknown
// member is kept as an extra: the schema leaves the layer open.
func layerVariant(k string) (string, bool) {
	if canonical, ok := ed269LayerNames[k]; ok {
		return canonical, true
	}
	return caseSpelling(k)
}

// variantReason is why a variant is refused, naming the ED-318 member it
// stands for.
func variantReason(variant, canonical string) string {
	if _, ok := ed269LayerNames[variant]; ok {
		return "is ED-269's name; ED-318 writes " + canonical
	}
	if canonical == strings.ToLower(canonical) {
		return "is not ED-318's spelling; ED-318 writes " + canonical + ", in lower case (member names are case-sensitive)"
	}
	return "is not ED-318's spelling; ED-318 writes " + canonical + " (member names are case-sensitive)"
}

// uomVariants maps ED-269's spelling of a unit (UomDimensions, upper
// case) to ED-318's.
var uomVariants = map[string]string{
	"M":  UomMetres,
	"FT": UomFeet,
}

// LayerVariants is the known names that are not ED-318's, each with the
// ED-318 layer member it stands for (a new map on each call): ED-269's
// names for the limits, and every spelling of each of the five members
// that differs from it in ASCII letter case only (32835 of them, most
// for upperReference and lowerReference). Parse refuses each of them
// inside a layer by name.
func LayerVariants() map[string]string {
	out := maps.Clone(ed269LayerNames)
	for _, m := range caseSpelt {
		for _, s := range caseSpellings(m) {
			out[s] = m
		}
	}
	return out
}

// UomVariants is ED-269's spelling of each unit with ED-318's (a new map
// on each call). Parse refuses them as a layer's uom.
func UomVariants() map[string]string { return maps.Clone(uomVariants) }

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
