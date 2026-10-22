// Package regnum validates and normalises UAS operator registration
// numbers (2019/947 Art. 14).
//
// A Validator holds the configured format (LESSONS G-07), an Options:
// the pattern, the secret separator and length, and an optional checksum.
// The zero Options is the EU shape: DefaultPattern (a three-letter
// country code, then 8 to 16 letters and digits), a secret part of a
// hyphen and three letters or digits, no checksum. The Georgian format is
// an open question (spec 08 Q5), so all of it is configuration. No
// checksum algorithm is defined until the format is decided
// (ChecksumByName knows only "none"); a system may pass its own. Validate refuses an empty or over-long number,
// a number with a hyphen (only the public part is registered, never the
// EU secret part after the hyphen; spec 06 §5) and a number that does not
// match the pattern. Errors are *core.FieldError naming
// "registration_number".
//
// PublicPart, CompareKey and Public turn a number as broadcast or typed
// into what identification compares (G-04): trimmed, the EU secret part
// (a hyphen and three letters or digits at the end) removed, upper-cased.
// The secret part is removed only when what precedes it is a registration
// number under the configured pattern. The predecessor removed any
// three-character tail after a hyphen, so the test id "GEO-OP-ABC"
// compared as "GEO-OP" (the G-04 pitfall); here it stays "GEO-OP-ABC"
// unless the pattern accepts "GEO-OP". The vector public-part-GEO-OP-ABC
// records the old value and is a known deviation of this package (see
// serial/vectors_test.go). Under a pattern that refuses a hyphen, as the
// default does, the correction cannot change a match against a registered
// number: a registered number never contains a hyphen.
//
// The package-level functions use the default format; a system configured
// with another format calls the methods of a Validator from
// NewValidatorWith (or NewValidator for a pattern alone).
//
// Vectors: vectors/testdata/serials_and_registration.json, the
// registration_number (4) and public_registration_number (8) kinds, run by
// serial/vectors_test.go. Owned by WP-4.
package regnum
