package regnum

import (
	"strings"
	"testing"
)

// TestNewValidatorWithZeroIsDefault: the zero Options is today's format,
// the very validator the package-level functions use.
func TestNewValidatorWithZeroIsDefault(t *testing.T) {
	v, err := NewValidatorWith(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if v != defaultValidator {
		t.Error("the zero Options is not the default validator")
	}
	if v.Pattern() != DefaultPattern || v.SecretSep() != '-' || v.SecretLen() != 3 || v.HasChecksum() {
		t.Errorf("defaults: %q %q %d %v", v.Pattern(), v.SecretSep(), v.SecretLen(), v.HasChecksum())
	}
	// The defaults spelled out are the zero value too.
	v, err = NewValidatorWith(Options{Pattern: "", SecretSep: DefaultSecretSep, SecretLen: DefaultSecretLen})
	if err != nil || v != defaultValidator {
		t.Errorf("explicit defaults: %v %v", v, err)
	}
	// A pattern alone keeps the default secret part.
	v, err = NewValidatorWith(Options{Pattern: `GEO[0-9]{9}`})
	if err != nil || v.SecretSep() != '-' || v.SecretLen() != 3 {
		t.Fatalf("pattern alone: %v", err)
	}
	if p := v.PublicPart("GEO123456789-x9z"); p != "GEO123456789" {
		t.Errorf("default secret under a configured pattern: %q", p)
	}
}

// TestNewValidatorWithRefuses pairs each refused secret option with an
// accepted one next to it (E-01).
func TestNewValidatorWithRefuses(t *testing.T) {
	for _, sep := range []byte{'.', '/', '_', '!', '~', '#'} {
		if _, err := NewValidatorWith(Options{SecretSep: sep}); err != nil {
			t.Errorf("accept separator %q: %v", sep, err)
		}
	}
	for _, sep := range []byte{' ', '\t', 'a', 'Z', '5', 0x7f, 0x80, 0xff} {
		_, err := NewValidatorWith(Options{SecretSep: sep})
		fe := fieldError(t, err, FieldSecret)
		if !strings.Contains(fe.Reason, "separator") {
			t.Errorf("separator %q: %q", sep, fe.Reason)
		}
	}
	for _, n := range []int{1, 4, MaxLen - 2} {
		v, err := NewValidatorWith(Options{SecretLen: n})
		if err != nil || v.SecretLen() != n {
			t.Errorf("accept length %d: %v", n, err)
		}
	}
	for _, n := range []int{-1, MaxLen - 1, 1 << 30} {
		_, err := NewValidatorWith(Options{SecretLen: n})
		fe := fieldError(t, err, FieldSecret)
		if !strings.Contains(fe.Reason, "outside 1..62") {
			t.Errorf("length %d: %q", n, fe.Reason)
		}
	}
	_, err := NewValidatorWith(Options{Pattern: `[A-Z`, SecretSep: '.'})
	fieldError(t, err, FieldPattern)
}

// TestConfiguredSecretPart: a non-EU format with its own separator and
// length strips its own secret part, refuses it on registration, and no
// longer treats the EU tail as a secret.
func TestConfiguredSecretPart(t *testing.T) {
	v, err := NewValidatorWith(Options{Pattern: `GE[0-9]{10}`, SecretSep: '.', SecretLen: 4})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct{ in, public, key string }{
		{"GE0123456789.ab12", "GE0123456789", "GE0123456789"},
		{" ge0123456789.AB12 ", "ge0123456789", "GE0123456789"},
		{"GE0123456789", "GE0123456789", "GE0123456789"},
		// Not this format's secret part: kept.
		{"GE0123456789-abc", "GE0123456789-abc", "GE0123456789-ABC"},
		{"GE0123456789.abc", "GE0123456789.abc", "GE0123456789.ABC"},
		{"GE0123456789.abcde", "GE0123456789.abcde", "GE0123456789.ABCDE"},
		{"GE0123456789.ab!2", "GE0123456789.ab!2", "GE0123456789.AB!2"},
		{"GE0123456789.!b12", "GE0123456789.!b12", "GE0123456789.!B12"},
		// The head must be a number under this pattern.
		{"GE012345678.ab12", "GE012345678.ab12", "GE012345678.AB12"},
	}
	for _, tt := range tests {
		if p, k := v.Public(tt.in); p != tt.public || k != tt.key {
			t.Errorf("Public(%q) = %q, %q; want %q, %q", tt.in, p, k, tt.public, tt.key)
		}
	}
	if v.SecretSep() != '.' || v.SecretLen() != 4 || v.HasChecksum() {
		t.Errorf("effective: %q %d %v", v.SecretSep(), v.SecretLen(), v.HasChecksum())
	}
	if err := v.Validate("GE0123456789"); err != nil {
		t.Errorf("accept: %v", err)
	}
	// A separator alone, under the default pattern, is no default format.
	d, err := NewValidatorWith(Options{SecretSep: '.'})
	if err != nil || d == defaultValidator || d.SecretSep() != '.' || d.Pattern() != DefaultPattern {
		t.Fatalf("separator alone: %v", err)
	}
	if p := d.PublicPart("FIN87astrdge12k8.xyz"); p != "FIN87astrdge12k8" {
		t.Errorf("separator alone, strip: %q", p)
	}
	if p := d.PublicPart("FIN87astrdge12k8-xyz"); p != "FIN87astrdge12k8-xyz" {
		t.Errorf("separator alone, hyphen tail: %q", p)
	}
	fe := fieldError(t, v.Validate("GE0123456789.ab12"), Field)
	if fe.Reason != `only the public part of the registration number is registered; leave out the '.' and the secret characters after it` {
		t.Errorf("separator refusal: %q", fe.Reason)
	}
	// A hyphen is no longer the separator, so a pattern may contain one.
	h, err := NewValidatorWith(Options{Pattern: `GE-[0-9]{6}`, SecretSep: '.'})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Validate("GE-123456"); err != nil {
		t.Errorf("hyphen in a format with another separator: %v", err)
	}
	if p := h.PublicPart("GE-123456.x9z"); p != "GE-123456" {
		t.Errorf("strip under a hyphenated pattern: %q", p)
	}
	// Under the default the same number is refused for its hyphen.
	if fe := fieldError(t, defaultValidator.Validate("GE-123456"), Field); fe.Reason != reasonHyphen {
		t.Errorf("default hyphen refusal: %q", fe.Reason)
	}
}

// TestDefaultKeepsSecretOfAnotherFormat shows why the format must be
// configured: under the default a number of another shape keeps its
// secret part (G-04), under the configured format it is stripped.
func TestDefaultKeepsSecretOfAnotherFormat(t *testing.T) {
	const in = "GEO123456789012345678-x9z" // 18 digits: too long for DefaultPattern
	if p := PublicPart(in); p != in {
		t.Fatalf("default stripped %q", p)
	}
	v, err := NewValidatorWith(Options{Pattern: `GEO[0-9]{18}`})
	if err != nil {
		t.Fatal(err)
	}
	if p := v.PublicPart(in); p != "GEO123456789012345678" {
		t.Errorf("configured: %q", p)
	}
}

func TestChecksumByName(t *testing.T) {
	for _, name := range []string{"", ChecksumNone} {
		c, err := ChecksumByName(name)
		if err != nil || c != nil {
			t.Errorf("%q: %v %v", name, c != nil, err)
		}
	}
	for _, name := range []string{"luhn", "amc1", "NONE", " none"} {
		c, err := ChecksumByName(name)
		fe := fieldError(t, err, FieldChecksum)
		if c != nil || !strings.Contains(fe.Reason, `"`+name+`"`) {
			t.Errorf("%q: %q", name, fe.Reason)
		}
	}
}

// digitSum is a test checksum: the last character is the sum of the
// other digits modulo 10. It is no real algorithm.
func digitSum(number string) bool {
	sum := 0
	for i := 0; i < len(number)-1; i++ {
		if c := number[i]; c >= '0' && c <= '9' {
			sum += int(c - '0')
		}
	}
	return len(number) > 0 && number[len(number)-1] == byte('0'+sum%10)
}

// TestChecksumHook: with a checksum configured, Validate refuses a bad
// check character and accepts a good one; it runs after the pattern, on
// the trimmed number, and PublicPart still strips the secret of a number
// whose check character is bad.
func TestChecksumHook(t *testing.T) {
	calls := 0
	v, err := NewValidatorWith(Options{Pattern: `GEO[0-9]{9}`, Checksum: func(n string) bool { calls++; return digitSum(n) }})
	if err != nil {
		t.Fatal(err)
	}
	if !v.HasChecksum() || v == defaultValidator {
		t.Fatal("checksum not configured")
	}
	if err := v.Validate(" GEO123456786 "); err != nil { // 1+..+8 = 36
		t.Errorf("accept: %v", err)
	}
	fe := fieldError(t, v.Validate("GEO123456787"), Field)
	if fe.Reason != reasonChecksum {
		t.Errorf("refuse: %q", fe.Reason)
	}
	calls = 0
	fe = fieldError(t, v.Validate("GEO12345678"), Field)
	if !strings.HasPrefix(fe.Reason, "does not match") || calls != 0 {
		t.Errorf("pattern first: %q, %d checksum calls", fe.Reason, calls)
	}
	if p := v.PublicPart("GEO123456788-x9z"); p != "GEO123456788" {
		t.Errorf("secret kept on a bad check character: %q", p)
	}
	// A checksum alone, under the default pattern, is no default format.
	d, err := NewValidatorWith(Options{Checksum: digitSum})
	if err != nil || d == defaultValidator || !d.HasChecksum() {
		t.Fatalf("checksum alone: %v", err)
	}
	if fe := fieldError(t, d.Validate("FIN12345679"), Field); fe.Reason != reasonChecksum { // 1+..+7 = 28
		t.Errorf("checksum alone, refuse: %q", fe.Reason)
	}
	if err := d.Validate("FIN12345678"); err != nil {
		t.Errorf("checksum alone, accept: %v", err)
	}
	// Without a checksum the same bad number is accepted.
	plain, _ := NewValidatorWith(Options{Pattern: `GEO[0-9]{9}`})
	if err := plain.Validate("GEO123456788"); err != nil {
		t.Errorf("no checksum: %v", err)
	}
}

// FuzzPublicPartWith holds the PublicPart invariants under a non-default
// separator and length.
func FuzzPublicPartWith(f *testing.F) {
	for _, s := range []string{"GE0123456789.ab12", "GE0123456789-abc", "ge0123456789.AB12", ".abcd", "GE0123456789.", ""} {
		f.Add(s)
	}
	v, err := NewValidatorWith(Options{Pattern: `GE[0-9]{10}`, SecretSep: '.', SecretLen: 4})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, k := v.Public(s)
		if k != asciiUpper(p) || v.PublicPart(p) != p {
			t.Fatalf("Public(%q) = %q, %q", s, p, k)
		}
		if p != strings.TrimSpace(s) {
			if err := v.Validate(asciiUpper(p)); err != nil {
				t.Fatalf("stripped %q to %q, which is invalid: %v", s, p, err)
			}
			if len(strings.TrimSpace(s))-len(p) != 5 {
				t.Fatalf("stripped %q to %q: not a separator and four", s, p)
			}
		}
	})
}
