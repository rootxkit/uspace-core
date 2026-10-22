// Package ed318 reads, validates and writes EUROCAE ED-318 UAS
// geographical zone collections (GeoJSON FeatureCollections of UASZone
// features), maps ED-269 documents onto ED-318 and back, evaluates when a
// zone applies with ED-318's daylight events, and builds the zones
// package's judgement view of a collection. Consumers: the CISP
// (publication), the authority (zone authoring), the USSP and the ANSP
// (spec 02 F1-F3).
//
// # Field names and their sources
//
// EUROCAE's text is not public. Member names, types, lengths and
// enumerations come from the ED-318 JSON schema of the UASGeoZones/ED-318
// repository at commit e98b292c5665a04989d62e32fd93829f161a89a9 (the
// schema InterUSS monitoring pins and validates ED-318 documents
// against), checked against InterUSS uas_standards
// src/uas_standards/eurocae_ed318.py at commit
// 6e182f43ec960b3bccf131c9006ff9979dd8a56e. Where the two disagree the
// schema is followed, because it is what InterUSS validates against
// (owner decision on PR #16). Every disagreement found:
//
//  1. A zone authority's siteURL, email and phone: strings in the schema
//     and its examples; {text, lang} objects in uas_standards. Strings.
//  2. dataSource's creation time: creationDate in the schema,
//     creationDateTime in uas_standards. Both are read, each into its own
//     field, and written back as read (the schema leaves the object open,
//     so its validator accepts either).
//  3. dataSource's originator: {text, lang} in the schema, a string in
//     uas_standards. {text, lang}.
//  4. message: a list of textShortType (200 characters) in the schema; of
//     TextLongType (1,000) in uas_standards. 200.
//  5. zoneAuthority: required with at least one entry in the schema; a
//     list with no minimum in uas_standards. At least one.
//  6. The collection's metadata: optional in the schema, required in
//     uas_standards' ED318Schema. Optional.
//  7. The collection's title: a member of uas_standards' ED318Schema, not
//     of the schema (which leaves the collection open). Read and written
//     as published.
//  8. identifier: at most 7 characters in the schema; no bound in
//     uas_standards. 7.
//  9. A daily period's day list: the schema writes minItems 1 and
//     maxItems 7 inside `items`, where they constrain nothing; no bound in
//     uas_standards. 1 to 7, as the schema evidently intends.
//
// # UNVERIFIED: the layer names and the circle radius unit
//
// The EUROCAE ED-318 text is not available to this project (spec 09
// section 3). Three things this package reads are therefore UNVERIFIED
// against the standard itself, and stay so until a licensed copy is
// checked (owner decision on PR #16):
//
//   - UNVERIFIED: the names of a zone's vertical limits. They are the
//     geometry's `layer` object with upper, upperReference, lower,
//     lowerReference (AGL, AMSL or WGS84) and uom (m or ft), the table
//     LayerMembers. Every public source read agrees (the list below),
//     and none of them is the standard's text.
//     ED-269's names for the same limits (upperLimit, lowerLimit,
//     upperVerticalReference, lowerVerticalReference, uomDimensions with
//     M or FT; sources 6 and 7 below) are the members of an ED-269 UASZoneAirspaceVolume, read by package
//     ed269 and mapped by FromED269. Found inside an ED-318 layer they are
//     refused by name (LayerVariants, UomVariants), never mapped (spec 06
//     T9): kept as extras, a layer of them would be a zone from the
//     surface to unlimited, and an uomDimensions beside upper would leave
//     the limit read as metres. So are the spellings of each of the five
//     members that differ from it in case only (Upper, LOWER,
//     UpperReference, lowerreference, UOM and the rest; LayerVariants):
//     member names are case-sensitive and no source spells them
//     otherwise, and kept as an extra an Upper would leave the layer read
//     as lower to unlimited. A layer with none of the five members is
//     refused as empty for the same reason. Should the licensed text name
//     the limits otherwise, the change is to the table and its vectors.
//   - UNVERIFIED: an absent `uom` means metres. The schema says so for
//     upper and lower (`"default": "m"`, and "If this member is not
//     specified, the units should be assumed to be metres"); the EUROCAE
//     text has not been checked. The sources disagree: uas_standards
//     (VerticalLayer.uom is a required field) and Swiss V2 (`uom :
//     MANDATORY`) make uom mandatory, against the schema's default. This
//     package follows the schema, as above; every layer of the Swiss and
//     German samples read gives its uom, so no producer read relies on
//     the default.
//   - UNVERIFIED: the unit of a circle's radius. A circle is a GeoJSON
//     Point with an `extent` of subType Circle and a `radius`; neither the
//     schema (Schema_GeoJSONGeometries.json, where radius is a bare
//     number) nor uas_standards (ExtentCircle) states its unit. The radius
//     is always in metres (owner decision on PR #16), following GeoJSON,
//     whose distances are metres, and InterUSS practice (F3411 and F3548
//     radii are metres, and the schema's own circle example is 3500 with a
//     metres layer). LayeredGeoJSON (extent.radius "expressed in metres")
//     and Swiss V2 (HorizontalExtent.radius, "Distance, in metres") say
//     the same. The layer's uom governs the vertical limits only, so
//     a circle is accepted whatever it says. A radius that is not finite,
//     not above 0, or above MaxCircleRadiusM (1000 km) is refused.
//
// The public sources of the layer, read for the check of 2026-10-22:
//
//  1. The schema: https://github.com/UASGeoZones/ED-318
//     schema/Schema_LayeredGeoJSON.json at
//     e98b292c5665a04989d62e32fd93829f161a89a9 (2025-05-21), the commit
//     interuss/monitoring's interfaces/ed318 submodule pins. Every layer
//     member optional; uom m or ft, default m.
//  2. https://github.com/interuss/uas_standards
//     src/uas_standards/eurocae_ed318.py at
//     6e182f43ec960b3bccf131c9006ff9979dd8a56e: VerticalLayer and
//     Layered (interuss/uas_standards #34 and #36, in releases from
//     v4.1.0 of 2025-10-01). upper, lower and uom required.
//  3. The LayeredGeoJSON specification, the origin of the layer:
//     https://github.com/LayeredGeoJSON/LayeredGeoJSON-Specification
//     README.md at 0acc0b5 (2023-11-16).
//  4. The Swiss FOCA INTERLIS model V2, a national profile:
//     https://models.geo.admin.ch/BAZL/UASGeographicalZone_V2.ili
//     (VERSION "2026-06-29"), CLASS VerticalLayer.
//  5. The German DFS ED-318 sample:
//     https://dipul.bund.de/drohnenhersteller/de/beispieldatened318.zip
//     (234 of 234 layers read carry the five names and uom m).
//  6. ED-269's names: uas_standards src/uas_standards/eurocae_ed269.py
//     at the commit of 2, UASZoneAirspaceVolume (UomDimensions M, FT).
//  7. ED-269 national data: the Cyprus DCA file
//     https://drones.gov.cy/wp-content/uploads/2025/05/CYP_GeoZones_06_05_25.json
//     ("ver 06MAY25"), 141 of 141 volumes with uomDimensions M.
//
// Free-text members the schema leaves unbounded are bounded at
// MaxFreeTextChars characters.
//
// Several layers are a GeometryCollection of geometries, each with its
// layer, as in the schema's two-layer example.
//
// # Reading
//
// Parse validates on receipt and never repairs (spec 06 T9): a collection
// is accepted whole or refused whole with every problem listed by JSON
// path (`features[2].properties.type`) and reason, as ed269 does, in
// *ed269.Problems, bounded by ed269.Limits (input bytes, nesting depth
// counted without recursion, positions per ring, problems per report).
// The UASZone properties, TimePeriod and DailyPeriod are closed in the
// schema: an unknown member there is refused. Elsewhere the schema allows
// other members (the examples' `title`, the Swiss sample's
// `description` and `technicalLimitation`); they are kept as published
// and written back.
//
// Refused by name: ED-269's REQ_AUTHORISATION spelling and its
// FOREIGN_TERRITORY reason; a daylight event outside BMCT, SR, SS, EECT;
// a window end given both as a time and as an event, or neither; a
// limit without its vertical reference (D-01); a date-time without an
// offset (T-09); an empty zoneAuthority; a feature without properties or
// geometry (the schema allows null; a zone needs both); a geometry other
// than a Polygon, a Point with a Circle extent, or a GeometryCollection
// of those (a line has no inside; a MultiPolygon is refused whole in
// this release, with a problem that names it, never imported part by
// part, owner decision on PR #16); a
// position with a third (altitude) member; a layer given both on a
// collection and on its members; an ED-269 name inside a layer, or a
// layer member spelt in another case (LayerVariants), or an ED-269 unit
// as its uom
// (UomVariants); a layer
// with none of ED-318's members; a shape across the antimeridian.
//
// # Writing
//
// Export(Parse(f)) equals f by value: members in schema order, the kept
// members after them, null and absent optional members absent, numbers
// by value, date-times and times as published. The published examples of
// the schema repository and the Swiss sample round-trip so
// (testdata/published).
//
// # ED-269
//
// FromED269 and ToED269 map the formats both ways (spec 02 F1):
// restriction and type, REQ_AUTHORISATION and REQ_AUTHORIZATION,
// applicability and limitedApplicability (a lone permanent period is no
// limitedApplicability), strings and one-language text lists, M/FT and
// m/ft, a Circle and a Point with a Circle extent. ED-269 fields with no
// ED-318 member (uSpaceClass, a zone title, restrictionConditions
// published as a list) travel in extendedProperties under ED269Key and
// return. What one side cannot hold is refused with a *core.FieldError,
// never dropped or approximated: from ED-269 a FOREIGN_TERRITORY reason
// and a zone without an authority or a purpose; to ED-269 USPACE, DAR,
// daylight events and two-layer zones. A text in several languages is
// not refused: ToED269 writes the one in the caller's language, else
// English, else the first, and carries the whole list in the ED-269
// zone's extendedProperties.ed269.texts, from which FromED269 restores
// it.
// ed269 -> ED-318 -> ed269 is the identity on the mappable zones of
// ed269_parse.json's valid file.
//
// # The extendedProperties.ed269 carrier
//
// The extendedProperties member ED269Key ("ed269") is this project's
// carrier for what one format has no member for, approved by the owner
// on PR #16. In an ED-318 zone written by FromED269 it holds the ED-269
// fields uSpaceClass, the zone-level title and a list-form
// restrictionConditions; in an ED-269 zone written by ToED269 it holds
// texts, the ED-318 text lists whose other languages ED-269's single
// string cannot hold. Each mapping reads back what the other wrote and
// refuses anything else under the key, so the carrier cannot silently
// pass unknown content through. An authority publishing ED-318 should not
// use the key for its own extensions.
//
// # Applicability and daylight
//
// Applies has ed269's semantics (Z-07, T-09) plus ED-318's events: each
// end of a daily window is a clock time or an event resolved per day
// through a Daylight at the zone's place. An event that cannot be
// resolved makes the answer "not evaluated" with an error, never
// "applies" and never a silent "does not apply". NOAADaylight computes
// the events with NOAA's algorithm in pure Go (sunrise and sunset at a
// zenith of 90.833 degrees, civil twilight at 96): within 7 s of astropy
// at Tbilisi, Oslo and Sydney in the tests, within a minute between 72
// degrees north and south by NOAA's statement, and an error wrapping
// ErrNoEvent in polar day or night. FixedDaylight takes the times from a
// table.
//
// ToZones gives one zones.Zone per geometry part with limits in metres
// and ed269 periods; the layers of a GeometryCollection zone get distinct
// identifiers, "<identifier>/L<index>" (PartIdentifier), so that alerts
// keyed by country and identifier hold one key per layer; a schedule with events becomes one fixed window per
// day between its dates (at most MaxEventDays), resolved at the centre of
// the part's bounding box.
//
// Vector: vectors/testdata/ed318_roundtrip.json (22 cases), written by
// ed318/internal/genvectors from testdata/authority_collection.json and
// the cases in source.json; proposed upstream to uspace-lab. Milestone
// G-M2. Owned by WP-12.
package ed318
