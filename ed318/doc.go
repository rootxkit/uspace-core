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
// schema is followed and the difference written down:
//
//   - a zone authority's siteURL, email and phone are strings (the schema
//     and its examples); uas_standards types them as {text, lang};
//   - dataSource's creation time is creationDate in the schema and
//     creationDateTime in uas_standards: both are read, each into its own
//     field, and written back as read; its originator is {text, lang}
//     (schema), not a string;
//   - message texts are bounded at 200 characters like every
//     textShortType (schema); uas_standards declares a textLongType of
//     1,000 for them.
//
// The vertical limits of a zone, which the specification calls
// unverified (09 section 3), are the geometry's `layer` object: upper,
// upperReference, lower, lowerReference (AGL, AMSL or WGS84) and uom (m
// or ft, metres when absent). Both sources agree on these names; the
// EUROCAE text itself has not been seen, so they remain *unverified*
// against it. A circle is a GeoJSON Point with an `extent` of subType
// Circle; its radius is read as metres, which neither source states
// (unverified too). Several layers are a GeometryCollection of
// geometries, each with its layer, as in the schema's two-layer example.
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
// collection and on its members; a shape across the antimeridian.
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
// and ed269 periods; a schedule with events becomes one fixed window per
// day between its dates (at most MaxEventDays), resolved at the centre of
// the part's bounding box.
//
// Vector: vectors/testdata/ed318_roundtrip.json (22 cases), written by
// ed318/internal/genvectors from testdata/authority_collection.json and
// the cases in source.json; proposed upstream to uspace-lab. Milestone
// G-M2. Owned by WP-12.
package ed318
