//nolint:misspell // Normalize is the API name fixed in docs/PLAN.md §3.6 (Go spelling)
package serial

import (
	"strconv"
	"strings"

	"github.com/rootxkit/uspace-core/core"
)

// MaxLen is the longest CTA-2063-A serial: a 4-character manufacturer
// code, one length character (at most F = 15) and 15 characters.
const MaxLen = 20

// Field is the name every serial error carries (*core.FieldError.Field).
const Field = "serial"

// FieldClassLabel is the field named when the class label itself is
// refused.
const FieldClassLabel = "class_label"

const (
	mfrLen = 4
	// reasonShape is the predecessor's wording for every shape refusal
	// (serials_and_registration.json pins "not a CTA" in it).
	reasonShape = "not a CTA-2063-A serial: 4-character manufacturer code, a length " +
		"character (1-9, A-F), then that many characters; digits and " +
		"upper-case letters without O and I"
	reasonRequired = "a serial number is required"
)

// ctaChar reports whether c is in the CTA-2063-A alphabet: digits and
// upper-case letters without O and I (they read as 0 and 1).
func ctaChar(c byte) bool {
	switch {
	case c >= '0' && c <= '9':
		return true
	case c >= 'A' && c <= 'Z':
		return c != 'O' && c != 'I'
	}
	return false
}

// lengthValue decodes the length character: 1-9, then A-F for 10-15. It
// returns 0 for anything else (0 is not a length).
func lengthValue(c byte) int {
	switch {
	case c >= '1' && c <= '9':
		return int(c - '0')
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}

// ValidateCTA2063A reports why serial is not an ANSI/CTA-2063-A serial,
// or nil when it is. The serial is checked as given: callers trim with
// the trimming function first. Upper case only, so "1a2b1x" is refused.
//
// The shape is checked first (alphabet, a length character 1-9 or A-F,
// 1 to 15 characters after it); a serial of the right shape whose length
// character disagrees with the count is refused with the reason "the
// length character says N characters follow, and M do". Every error is a
// *core.FieldError with Field "serial". Any input is safe: the work is
// bounded by MaxLen.
func ValidateCTA2063A(serial string) error {
	if reason := ctaProblem(serial); reason != "" {
		return &core.FieldError{Field: Field, Reason: reason}
	}
	return nil
}

// ctaProblem is the reason serial is not CTA-2063-A, or "" when it is.
func ctaProblem(serial string) string {
	n := len(serial)
	if n < mfrLen+2 || n > MaxLen {
		return reasonShape
	}
	for i := 0; i < n; i++ {
		if i != mfrLen && !ctaChar(serial[i]) {
			return reasonShape
		}
	}
	declared := lengthValue(serial[mfrLen])
	if declared == 0 {
		return reasonShape
	}
	if actual := n - mfrLen - 1; actual != declared {
		return "not a CTA-2063-A serial: the length character says " +
			strconv.Itoa(declared) + " characters follow, and " + strconv.Itoa(actual) + " do"
	}
	return ""
}

// classes lists the class labels of 2019/945 and whether each must carry
// a CTA-2063-A serial (the classes that broadcast direct Remote ID).
var classes = map[string]bool{
	"C0": false, "C1": true, "C2": true, "C3": true,
	"C4": false, "C5": true, "C6": true,
}

// RequiresCTA reports whether an aircraft of classLabel must carry a
// CTA-2063-A serial: C1, C2, C3, C5 and C6 (LESSONS G-06). An empty or
// unknown label does not.
func RequiresCTA(classLabel string) bool {
	return classes[classLabel]
}

// ValidateForClass reports why serial cannot be registered for an
// aircraft of classLabel, or nil. Every class needs a serial that is not
// empty after trimming ("a serial number is required"); C1, C2, C3, C5
// and C6 also need a valid CTA-2063-A serial ("class C1 requires a
// CTA-2063-A serial; " and the CTA reason). C0, C4 and an unlabelled
// aircraft ("") keep whatever serial the maker printed. A label other
// than "" and C0 to C6 is refused with Field "class_label".
func ValidateForClass(serial, classLabel string) error {
	cta, known := classes[classLabel]
	if !known && classLabel != "" {
		return &core.FieldError{Field: FieldClassLabel,
			Reason: "unknown class label " + strconv.Quote(classLabel) + ": want C0 to C6 or none"}
	}
	if strings.TrimSpace(serial) == "" {
		return &core.FieldError{Field: Field, Reason: reasonRequired}
	}
	if !cta {
		return nil
	}
	if reason := ctaProblem(serial); reason != "" {
		return &core.FieldError{Field: Field, Reason: "class " + classLabel + " requires a CTA-2063-A serial; " + reason}
	}
	return nil
}

// Normalize is a serial as it is stored and compared: surrounding space
// removed, case kept (LESSONS G-05). A legacy serial is whatever the maker
// printed, and folding its case could make two aircraft one.
func Normalize(serial string) string {
	return strings.TrimSpace(serial)
}

// FoldKey is the case-insensitive lookup key of a serial: Normalize, then
// its ASCII letters upper-cased. An exact match on Normalize wins; callers
// accept a match on FoldKey only when exactly one aircraft has it (G-05).
// Only ASCII is folded: strings.ToUpper maps look-alikes such as U+017F
// (long s) onto S, which would let a non-ASCII spelling fold onto one of
// our serials. A non-ASCII character is kept as it is and never meets an
// ASCII key.
func FoldKey(serial string) string {
	b := []byte(Normalize(serial))
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}
