// Package f3548 holds the ASTM F3548-21 strategic coordination types and
// constants (operational intents, constraints, subscriptions, the DSS and
// USS-to-USS APIs), for the USSP, the ANSP and the authority (spec 02 F2,
// F6).
//
// # Types
//
// types.gen.go is generated, types only, from the OpenAPI file InterUSS
// uas_standards generates its F3548-21 module from
// (interuss/astm-utm-protocol utm.yaml, "UTM API (USS->DSS and USS->USS)
// 1.0.0"), at the commits recorded in SOURCE; generate.go holds the
// generate directives. Every member name and type is the OpenAPI file's.
// One-element anyOf wrappers are aliases of the referenced type
// (../f3411/internal/oapialias), uuid and duration strings are plain
// strings, and the package needs only the standard library.
//
// The four DSS states are the generated OperationalIntentState values
// Accepted, Activated, Nonconforming and Contingent, listed in DSSStates;
// any other state a USSP tracks is its local_state and never goes to the
// DSS (spec 04 section 4).
//
// Unknown members are ignored, never refused (spec 02 section 1);
// UnmarshalOperationalIntent reads untrusted bytes with a size bound
// (MaxMessageBytes) and returns a *core.FieldError naming the member on a
// type error. It never panics (fuzzed).
//
// # Conversions
//
// Altitude.HAEM accepts only reference W84 and units M, the only ones
// F3548 allows, and refuses anything else with a *core.FieldError; the
// USSP derives AMSL from it through the geoid and keeps both (spec 04
// section 3.1). Volume4DToZonesEnvelope gives a conservative horizontal
// box and the time window of a Volume4D for deconfliction prefilters; a
// volume it cannot bound is an error, never an empty box.
//
// # Constants
//
// The five utm.* scopes and the constants of uas_standards constants.py
// (Cstr*, Oi*, TimeSync*, MaxRecoverableTimeInNonconformingStateSeconds,
// ExternalDataMaxRetentionTimeHours and the rest) are copied with their
// names.
//
// Pinned by the example messages in testdata/examples (assembled from the
// OpenAPI field examples: uas_standards has no whole-message examples),
// not by knowledge vectors. Milestone G-M2. Owned by WP-12.
package f3548
