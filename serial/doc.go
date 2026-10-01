// Package serial validates and normalises UAS serial numbers.
//
// ValidateCTA2063A applies the ANSI/CTA-2063-A rule: a 4-character
// manufacturer code, a length character (1-9, then A-F for 10-15), exactly
// that many characters, all from 0-9 and A-Z without O and I, upper case
// only, at most MaxLen (20) in all.
//
// ValidateForClass applies the 2019/945 class rule (LESSONS G-06): C1, C2,
// C3, C5 and C6, the classes that must broadcast direct Remote ID, need a
// valid CTA-2063-A serial; C0, C4 and unlabelled aircraft need only a
// serial that is not empty.
//
// The storage and lookup rule (G-05): a serial is stored trimmed with its
// case kept; an exact match wins, and a match on the upper-cased FoldKey
// is accepted by callers only when exactly one aircraft has it.
//
// Every refusal is a *core.FieldError naming the field ("serial" or
// "class_label") with a reason that contains the phrases the vectors pin.
// No input panics.
//
// Vectors: vectors/testdata/serials_and_registration.json (33 cases), run
// by this package's vectors_test.go, which also runs the registration_number
// and public_registration_number kinds through package regnum. Owned by
// WP-4.
package serial
