// Package regnum validates and normalises UAS operator registration
// numbers (2019/947 Art. 14): a configurable pattern defaulting to the EU
// shape (LESSONS G-07), the public part with the EU three-character secret
// suffix stripped, trimmed and upper-cased as the compare key (G-04), and
// the refusal to register a number carrying a hyphen.
//
// Vectors: vectors/testdata/serials_and_registration.json, the
// registration_number and public_registration_number kinds. Owned by WP-4.
package regnum
