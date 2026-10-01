// Package serial validates and normalises UAS serial numbers: the
// ANSI/CTA-2063-A rule (4-character manufacturer code, a length character
// 1-9 or A-F, exactly that many characters from 0-9 and A-Z without O and
// I, at most 20), the class rule that C1, C2, C3, C5 and C6 need a valid
// CTA serial while C0, C4 and unlabelled aircraft need only a non-empty
// one (LESSONS G-06), and the storage rule: trimmed, case kept, matched
// case-insensitively only when unambiguous (G-05).
//
// Vectors: vectors/testdata/serials_and_registration.json, the cta2063 and
// serial_for_class kinds. Owned by WP-4.
package serial
