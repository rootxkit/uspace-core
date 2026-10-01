// Package f3411 holds the ASTM F3411-22a network Remote ID types and
// constants, for the Service Provider, Display Provider and DSS client
// and server paths of the systems (spec 02 F6, F7).
//
// # Types
//
// types.gen.go is generated, types only, from the OpenAPI file InterUSS
// uas_standards generates its F3411-22a module from
// (uastech/standards remoteid/updated.yaml, "Standard Remote ID API
// Interfaces 2.1.0"), at the commits recorded in SOURCE; generate.go holds
// the generate directives. Every member name and type is the OpenAPI
// file's: nothing on the wire is written from memory. The file's
// one-element anyOf wrappers (a reference with a description) are
// generated as aliases of the referenced type (internal/oapialias), so the
// package needs only the standard library. Number formats are the
// standard's: `format: float` fields (alt, speed, track, height distance,
// radius) are float32 on the wire type, and the accessors below return the
// value as written, as float64.
//
// Unknown members are ignored, never refused (spec 02 section 1);
// UnmarshalRIDFlight and UnmarshalGetFlightsResponse read untrusted bytes
// with a size bound (MaxMessageBytes) and return a *core.FieldError
// naming the member on a type error. They never panic (fuzzed).
//
// # Special values
//
// Table 1's special values decode to nil, never to a number (spec 04
// section 3.1): SpeedMS (255), TrackDeg (361), VerticalSpeedMS (63),
// RIDHeight.DistanceM (-1000), AltHAEM (-1000) and PressureAltM (-1000).
// MaxSpeed (254.25) is a value meaning "254.25 m/s or more"; SpeedIsMax
// says so. F3411 calls the geodetic altitude `alt`; here it is AltHAEM,
// because the name carries the datum (E-13): height above the WGS84
// ellipsoid, which a consumer turns into AMSL through the geoid.
// Airborne applies odid's rule (R-11): only Ground is not airborne, and an
// absent or unknown status counts as airborne.
//
// # Conversions
//
// RIDAircraftPosition.LatLon and LatLngPoint.LatLon give a core.LatLon (a
// missing coordinate is NaN, never 0). Altitude.HAEM accepts only the
// reference and unit the standard allows (W84, M) and refuses anything
// else with a *core.FieldError. Volume4DToZonesEnvelope gives a
// conservative horizontal box and the time window of a Volume4D, for
// prefiltering zones and ISAs; a volume it cannot bound is an error, never
// an empty box.
//
// # Constants
//
// The Net* performance constants, the data-field constants (MaxSpeed,
// SpecialSpeed, SpecialHeight, SpecialTrackDirection and the rest) and the
// two scopes rid.service_provider and rid.display_provider are copied
// with their names from uas_standards constants.py.
//
// Pinned by the example messages in testdata/examples (assembled from the
// OpenAPI field examples: uas_standards has no whole-message examples),
// not by knowledge vectors. Milestone G-M2. Owned by WP-12.
package f3411
