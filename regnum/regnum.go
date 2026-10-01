package regnum

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rootxkit/uspace-core/core"
)

// DefaultPattern is the EU operator registration number shape (LESSONS
// G-07): a three-letter upper-case country code, then 8 to 16 letters and
// digits. It is configuration, not law: the Georgian format is an open
// question (spec 08 Q5), so NewValidator takes the pattern a system is
// configured with.
const DefaultPattern = `^[A-Z]{3}[A-Za-z0-9]{8,16}$`

// MaxLen bounds a registration number before any pattern work. No known
// format comes near it; anything longer is refused outright.
const MaxLen = 64

// Field is the name every validation error carries.
const Field = "registration_number"

// FieldPattern is the field named when the configured pattern is invalid.
const FieldPattern = "registration_pattern"

// secretLen is the length of the EU secret part: after a hyphen, three
// characters the operator uses to prove the number is theirs (spec 06 §5).
const secretLen = 3

const reasonHyphen = "only the public part of the registration number is registered; " +
	"leave out the hyphen and the secret characters after it"

// Validator checks registration numbers against one configured pattern
// and derives the public part under the same pattern. It is immutable and
// safe for concurrent use.
type Validator struct {
	re      *regexp.Regexp
	pattern string
}

// defaultValidator compiles DefaultPattern, a constant known to be valid.
var defaultValidator = &Validator{re: regexp.MustCompile(anchor(DefaultPattern)), pattern: DefaultPattern}

// anchor makes pattern match the whole value, as the predecessor's
// fullmatch did, whether or not the pattern carries its own anchors.
func anchor(pattern string) string {
	return `^(?:` + pattern + `)$`
}

// NewValidator compiles pattern; an empty pattern means DefaultPattern.
// The pattern must match the whole number (it is anchored). An invalid
// regular expression is a *core.FieldError with Field
// "registration_pattern".
func NewValidator(pattern string) (*Validator, error) {
	if pattern == "" {
		return defaultValidator, nil
	}
	re, err := regexp.Compile(anchor(pattern))
	if err != nil {
		return nil, core.Fieldf(FieldPattern, "not a valid regular expression %q: %v", pattern, err)
	}
	return &Validator{re: re, pattern: pattern}, nil
}

// Pattern returns the configured pattern as given (DefaultPattern when
// none was).
func (v *Validator) Pattern() string { return v.pattern }

// Validate reports why value cannot be registered as an operator
// registration number, or nil. The value is trimmed first. It is refused
// when empty, when longer than MaxLen, when it contains a hyphen
// ("FIN87astrdge12k8-xyz" is refused: only the public part is registered)
// and when it does not match the configured pattern ("does not
// match the configured format <pattern>"). Every error is a
// *core.FieldError with Field "registration_number".
func (v *Validator) Validate(value string) error {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return &core.FieldError{Field: Field, Reason: "a registration number is required"}
	case len(value) > MaxLen:
		return &core.FieldError{Field: Field, Reason: "longer than " + strconv.Itoa(MaxLen) + " characters"}
	case strings.Contains(value, "-"):
		return &core.FieldError{Field: Field, Reason: reasonHyphen}
	case !v.re.MatchString(value):
		return &core.FieldError{Field: Field, Reason: "does not match the configured format " + v.pattern}
	}
	return nil
}

// PublicPart is the public part of a number as broadcast or typed: the
// value trimmed, without the EU secret part. The secret part is a hyphen
// and exactly three ASCII letters or digits at the end, and it is
// stripped only when what stands before the hyphen is itself a
// registration number under the configured pattern, as given or with its
// ASCII letters upper-cased (a broadcast's case is not significant: the
// compare key is upper case anyway). Anything else is returned trimmed
// and otherwise unchanged:
//
//	"FIN87astrdge12k8-xyz"   -> "FIN87astrdge12k8"
//	" FIN87astrdge12k8-XY1 " -> "FIN87astrdge12k8"
//	"geoabcd1234efgh-x9z"    -> "geoabcd1234efgh" (compares as GEOABCD1234EFGH)
//	"FIN87astrdge12k8-x!z"   -> unchanged (not alphanumeric)
//	"GEO-OP-SITL", "-xyz"    -> unchanged
//	"GEO-OP-ABC"             -> unchanged ("GEO-OP" is no registration number)
//
// The last line corrects the LESSONS G-04 pitfall: the predecessor
// stripped any three-alphanumeric tail after the last hyphen, so
// "GEO-OP-ABC" compared as "GEO-OP" (the vector public-part-GEO-OP-ABC
// records that old value). Under a pattern that accepts "GEO-OP" the tail
// is stripped.
func (v *Validator) PublicPart(value string) string {
	value = strings.TrimSpace(value)
	head, ok := splitSecret(value)
	if !ok || len(head) > MaxLen || !v.re.MatchString(head) && !v.re.MatchString(asciiUpper(head)) {
		return value
	}
	return head
}

// asciiUpper upper-cases ASCII letters only. strings.ToUpper would also
// map letters such as U+017F (long s) onto ASCII ones, letting a
// non-ASCII head pass an ASCII pattern.
func asciiUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}

// CompareKey is what identification compares (spec 06 §5): the public
// part, upper-cased. The secret part never takes part in a comparison.
func (v *Validator) CompareKey(value string) string {
	return strings.ToUpper(v.PublicPart(value))
}

// Public returns PublicPart and CompareKey in one call.
func (v *Validator) Public(value string) (public, compareKey string) {
	public = v.PublicPart(value)
	return public, strings.ToUpper(public)
}

// splitSecret splits value into the part before a trailing secret part
// (a hyphen and three ASCII letters or digits) and reports whether there
// was one with something before it.
func splitSecret(value string) (head string, ok bool) {
	n := len(value)
	if n < secretLen+2 || value[n-secretLen-1] != '-' {
		return "", false
	}
	for i := n - secretLen; i < n; i++ {
		if !alnum(value[i]) {
			return "", false
		}
	}
	return value[:n-secretLen-1], true
}

func alnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// PublicPart is Validator.PublicPart under DefaultPattern.
func PublicPart(value string) string { return defaultValidator.PublicPart(value) }

// CompareKey is Validator.CompareKey under DefaultPattern.
func CompareKey(value string) string { return defaultValidator.CompareKey(value) }

// Public is Validator.Public under DefaultPattern.
func Public(value string) (public, compareKey string) { return defaultValidator.Public(value) }
