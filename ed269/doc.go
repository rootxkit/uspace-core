// Package ed269 reads and writes EUROCAE ED-269 UAS geographical zone
// documents strictly, and evaluates when a zone applies.
//
// # Reading
//
// Parse reads a whole document and ParseZone one `UASZoneVersion`. Both
// are strict (LESSONS Z-01): an unknown field, a value of the wrong JSON
// type (a number written as a string, "0", is refused, never converted),
// a value outside its enumeration, an over-length string, an unclosed or
// too short ring, a latitude out of range, a period that cannot be
// evaluated and a repeated member name are all refused. A document is
// accepted whole or refused whole (Z-02): the result is either a
// *Document or *Problems listing every problem with its JSON path
// (`features[3].geometry[0].upperLimit`, `$` for the document) and a
// reason, capped at Limits.MaxProblems with the rest counted in
// Truncated. A repeated zone identifier names both places.
//
// The near-misses are refused by name (Z-04): REQ_AUTHORIZATION with a Z
// ("ED-269 spells it REQ_AUTHORISATION"), a zone with more than one
// volume, a daily schedule whose start and end have different offsets or
// are equal, a permanent period with dates, a non-permanent period with
// nothing that says when it applies, and a date-time without an offset.
// A polygon spanning more than 180 degrees of longitude, or a circle
// reaching past ±180 degrees, is refused too: geodesy's containment would
// judge such a shape wrongly, and no zone this system serves crosses the
// antimeridian.
//
// What an import may cost is bounded (Z-06, E-10) by Limits: input bytes
// (checked before parsing), nesting depth (counted while tokenising,
// without recursion, so a hundred thousand brackets are refused rather
// than exhausting a stack), positions per ring and problems per report.
// DefaultLimits equals the `limits` header of the vectors. The input may
// start with a UTF-8 byte order mark (Luxembourg's live file does) and
// may use either published wrapper, `features` (InterUSS ED269Schema,
// Luxembourg) or `UASZoneList` (the Swiss sample).
//
// # Field names
//
// EUROCAE's text is paywalled and has no published JSON schema (Z-03).
// Field names, types, lengths and enumerations are pinned to InterUSS
// `uas_standards` (src/uas_standards/eurocae_ed269.py), Luxembourg's live
// national file and the Swiss BAZL INTERLIS profile
// (UASGeographicalZone_V1.ili, which gives the rule that a permanent
// period has no start or end), and to the vectors in
// vectors/testdata/ed269_parse.json. Coordinates are GeoJSON
// [longitude, latitude], as every published file writes them whatever the
// prose says; in memory they are core.LatLon.
//
// # WGS84 is this project's extension
//
// Every published source has only AGL and AMSL as vertical references.
// This package also reads and writes WGS84 (height above the WGS84
// ellipsoid) because the owner's field list adds it (Z-05). A file that
// uses WGS84 is this project's file, not a published ED-269 file, and
// other ED-269 readers will refuse it.
//
// # Writing
//
// Feature and Export write zones back so that Export(Parse(f)) equals f
// by value: nothing is normalised on the way in except that a null
// optional field is read as absent and written back absent; numbers are
// written by value (5.0 as 5); date-times, clock times and day lists are
// written as published; document keys (title, description,
// formatVersion, createdAt) and extendedProperties are kept. Limits are
// kept in the volume's unit; Volume.LowerM, UpperM and RadiusM convert
// feet with core.FeetToMetres exactly.
//
// # Applicability
//
// Applies evaluates a zone's periods at an instant (Z-07, T-09): both
// ends of a window are included; offsets are converted, never read as
// UTC; a daily window whose end is before its start runs past midnight
// and belongs to the day it starts on; the weekday is judged in the
// schedule's own offset; dates bound a schedule; a zone applies when any
// period does. Published files end days at 23:59:59, which leaves the
// last second before midnight uncovered; that is kept, not corrected.
// The comparison is in UTC and never consults time.Local; evaluate at the
// aircraft's placed time, not its arrival time.
//
// Offsets are fixed, as written in the file: ED-269 times carry a UTC
// offset, not a time zone, so a schedule written 08:00+04:00 stays at
// 04:00Z all year and does not follow daylight saving time. An authority
// whose rules follow local summer time publishes a period per season.
//
// Vectors: vectors/testdata/ed269_parse.json (52 cases) and
// zones_applicability.json (32). Consumers: zones, alerting, ed318 and
// the CISP's import. Owned by WP-5.
package ed269
