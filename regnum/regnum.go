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

// FieldSecret is the field named when the configured secret part
// (Options.SecretSep, Options.SecretLen) is invalid.
const FieldSecret = "registration_secret"

// FieldChecksum is the field named when a configured checksum name is
// unknown.
const FieldChecksum = "registration_checksum"

// DefaultSecretSep and DefaultSecretLen are the EU secret part: after a
// hyphen, three characters the operator uses to prove the number is
// theirs (spec 06 §5). Like DefaultPattern they are configuration: the
// Georgian format is an open question (spec 08 Q5).
const (
	DefaultSecretSep byte = '-'
	DefaultSecretLen int  = 3
)

// ChecksumNone is the configured name of no checksum, the default.
const ChecksumNone = "none"

const reasonHyphen = "only the public part of the registration number is registered; " +
	"leave out the hyphen and the secret characters after it"

const reasonChecksum = "the check character does not verify"

// Checksum reports whether a registration number, trimmed and without a
// secret part, carries a valid check character. It must be safe for
// concurrent use and must not panic on any input.
type Checksum func(number string) bool

// ChecksumByName returns the checksum configured by name. An empty name
// and ChecksumNone are no checksum (a nil Checksum). No algorithm is
// defined yet: the check character of AMC1 Art. 14(6) waits on the
// national format (spec 08 Q5, 09 §3), and an algorithm is not written
// from memory. Any other name is a *core.FieldError with Field
// "registration_checksum", so a configuration that names an algorithm
// this version does not have is refused rather than run unchecked.
func ChecksumByName(name string) (Checksum, error) {
	if name == "" || name == ChecksumNone {
		return nil, nil
	}
	return nil, core.Fieldf(FieldChecksum, "unknown checksum %q; known: %q", name, ChecksumNone)
}

// Options is a registration number format. The zero value is the EU
// default: DefaultPattern, the secret part DefaultSecretSep followed by
// DefaultSecretLen letters or digits, and no checksum.
type Options struct {
	// Pattern is the regular expression a registration number must match
	// as a whole (it is anchored); empty means DefaultPattern.
	Pattern string
	// SecretSep separates the public part from the secret part; zero
	// means DefaultSecretSep. It must be printable ASCII other than a
	// letter, a digit or a space.
	SecretSep byte
	// SecretLen is the number of ASCII letters or digits after SecretSep;
	// zero means DefaultSecretLen. It is 1 to MaxLen-2.
	SecretLen int
	// Checksum, when not nil, must accept a number for Validate to accept
	// it. Nil, the default, checks no check character. PublicPart does
	// not consult it: the secret part of a number with a bad check
	// character is stripped as well, so the secret is never kept (G-04).
	Checksum Checksum
}

// Validator checks registration numbers against one configured format
// and derives the public part under the same format. It is immutable and
// safe for concurrent use.
type Validator struct {
	re        *regexp.Regexp
	pattern   string
	sep       byte
	secretLen int
	checksum  Checksum
}

// defaultValidator compiles DefaultPattern, a constant known to be valid.
var defaultValidator = &Validator{
	re:        regexp.MustCompile(anchor(DefaultPattern)),
	pattern:   DefaultPattern,
	sep:       DefaultSecretSep,
	secretLen: DefaultSecretLen,
}

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
	return NewValidatorWith(Options{Pattern: pattern})
}

// NewValidatorWith compiles a format. The zero Options is the default
// format, the one the package-level functions use. An invalid pattern is
// a *core.FieldError with Field "registration_pattern", and an invalid
// secret separator or length one with Field "registration_secret".
func NewValidatorWith(opt Options) (*Validator, error) {
	sep, secretLen := opt.SecretSep, opt.SecretLen
	if sep == 0 {
		sep = DefaultSecretSep
	}
	if secretLen == 0 {
		secretLen = DefaultSecretLen
	}
	if sep <= ' ' || sep > '~' || alnum(sep) {
		return nil, core.Fieldf(FieldSecret, "separator %q is not printable ASCII other than a letter, a digit or a space", rune(sep))
	}
	if secretLen < 1 || secretLen > MaxLen-2 {
		return nil, core.Fieldf(FieldSecret, "length %d is outside 1..%d", secretLen, MaxLen-2)
	}
	if opt.Pattern == "" && sep == DefaultSecretSep && secretLen == DefaultSecretLen && opt.Checksum == nil {
		return defaultValidator, nil
	}
	pattern, re := DefaultPattern, defaultValidator.re
	if opt.Pattern != "" {
		var err error
		if re, err = regexp.Compile(anchor(opt.Pattern)); err != nil {
			return nil, core.Fieldf(FieldPattern, "not a valid regular expression %q: %v", opt.Pattern, err)
		}
		pattern = opt.Pattern
	}
	return &Validator{re: re, pattern: pattern, sep: sep, secretLen: secretLen, checksum: opt.Checksum}, nil
}

// Pattern returns the configured pattern as given (DefaultPattern when
// none was).
func (v *Validator) Pattern() string { return v.pattern }

// SecretSep returns the effective secret separator (DefaultSecretSep when
// none was configured).
func (v *Validator) SecretSep() byte { return v.sep }

// SecretLen returns the effective secret length (DefaultSecretLen when
// none was configured).
func (v *Validator) SecretLen() int { return v.secretLen }

// HasChecksum reports whether Validate checks a check character.
func (v *Validator) HasChecksum() bool { return v.checksum != nil }

// Validate reports why value cannot be registered as an operator
// registration number, or nil. The value is trimmed first. It is refused
// when empty, when longer than MaxLen, when it contains the secret
// separator (by default a hyphen: "FIN87astrdge12k8-xyz" is refused, only
// the public part is registered), when it does not match the configured
// pattern ("does not match the configured format <pattern>") and, with a
// checksum configured, when the checksum refuses it ("the check character
// does not verify"). Every error is a *core.FieldError with Field
// "registration_number".
func (v *Validator) Validate(value string) error {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return &core.FieldError{Field: Field, Reason: "a registration number is required"}
	case len(value) > MaxLen:
		return &core.FieldError{Field: Field, Reason: "longer than " + strconv.Itoa(MaxLen) + " characters"}
	case strings.IndexByte(value, v.sep) >= 0:
		return &core.FieldError{Field: Field, Reason: v.reasonSep()}
	case !v.re.MatchString(value):
		return &core.FieldError{Field: Field, Reason: "does not match the configured format " + v.pattern}
	case v.checksum != nil && !v.checksum(value):
		return &core.FieldError{Field: Field, Reason: reasonChecksum}
	}
	return nil
}

// reasonSep is the refusal of a number that carries the secret
// separator; under the default separator it is reasonHyphen word for word.
func (v *Validator) reasonSep() string {
	if v.sep == DefaultSecretSep {
		return reasonHyphen
	}
	return "only the public part of the registration number is registered; " +
		"leave out the " + strconv.QuoteRune(rune(v.sep)) + " and the secret characters after it"
}

// PublicPart is the public part of a number as broadcast or typed: the
// value trimmed, without the secret part. The secret part is the secret
// separator and exactly the secret length of ASCII letters or digits at
// the end (by default a hyphen and three), and it is
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
	head, ok := v.splitSecret(value)
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
// part with its ASCII letters upper-cased. Only ASCII is folded, so a
// look-alike such as U+017F (long s) never compares equal to S. The secret part never takes part in a comparison.
func (v *Validator) CompareKey(value string) string {
	return asciiUpper(v.PublicPart(value))
}

// Public returns PublicPart and CompareKey in one call.
func (v *Validator) Public(value string) (public, compareKey string) {
	public = v.PublicPart(value)
	return public, asciiUpper(public)
}

// splitSecret splits value into the part before a trailing secret part
// (the separator and secretLen ASCII letters or digits) and reports
// whether there was one with something before it.
func (v *Validator) splitSecret(value string) (head string, ok bool) {
	n := len(value)
	if n < v.secretLen+2 || value[n-v.secretLen-1] != v.sep {
		return "", false
	}
	for i := n - v.secretLen; i < n; i++ {
		if !alnum(value[i]) {
			return "", false
		}
	}
	return value[:n-v.secretLen-1], true
}

func alnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// PublicPart is Validator.PublicPart under the default format (the zero
// Options: DefaultPattern, a hyphen and three, no checksum).
func PublicPart(value string) string { return defaultValidator.PublicPart(value) }

// CompareKey is Validator.CompareKey under the default format.
func CompareKey(value string) string { return defaultValidator.CompareKey(value) }

// Public is Validator.Public under the default format.
func Public(value string) (public, compareKey string) { return defaultValidator.Public(value) }
