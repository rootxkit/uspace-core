//nolint:misspell // Normalize is the API name fixed in docs/PLAN.md §3.6 (Go spelling)
package serial

import (
	"errors"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

const reasonMismatchPrefix = "not a CTA-2063-A serial: the length character says "

// fieldError asserts err is a *core.FieldError naming field and returns it.
func fieldError(t *testing.T, err error, field string) *core.FieldError {
	t.Helper()
	var fe *core.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("got %T %v, want *core.FieldError", err, err)
	}
	if fe.Field != field {
		t.Fatalf("field %q, want %q (%v)", fe.Field, field, err)
	}
	return fe
}

// TestValidateCTA2063A pairs each refusal with the accepted serial it
// differs from by one property (E-01).
func TestValidateCTA2063A(t *testing.T) {
	tests := []struct {
		name, accept, refuse, reason string
	}{
		{"length 1", "1A2B1X", "1A2B1", reasonShape},
		{"length 15 is 20 in all", "1A2BF123456789ABCDEF", "1A2BF123456789ABCDEFG", reasonShape},
		{"length character A", "1581A1234567890", "1581A123456789", reasonMismatchPrefix + "10 characters follow, and 9 do"},
		{"says 2, 3 follow", "1A2B2AB", "1A2B2ABC", reasonMismatchPrefix + "2 characters follow, and 3 do"},
		{"says 3, 2 follow", "1A2B3ABC", "1A2B3AB", reasonMismatchPrefix + "3 characters follow, and 2 do"},
		{"length 0", "1A2B1X", "1A2B0X", reasonShape},
		{"length G", "1A2BF123456789ABCDEF", "1A2BG123456789ABCDEF", reasonShape},
		{"O in manufacturer code", "1P2B1X", "1O2B1X", reasonShape},
		{"I after length", "1A2B1J", "1A2B1I", reasonShape},
		{"lower case", "1A2B1X", "1a2b1x", reasonShape},
		{"lower-case length character", "1A2BA1234567890", "1A2Ba1234567890", reasonShape},
		{"space", "1A2B1X", "1A2B1X ", reasonShape},
		{"hyphen", "1A2B2X1", "1A2B2X-", reasonShape},
		{"empty", "1A2B1X", "", reasonShape},
		{"non-ASCII", "1A2B1X", "1A2B1É", reasonShape},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateCTA2063A(tt.accept); err != nil {
				t.Errorf("accept %q: %v", tt.accept, err)
			}
			if tt.reason == "" {
				return
			}
			fe := fieldError(t, ValidateCTA2063A(tt.refuse), Field)
			if fe.Reason != tt.reason {
				t.Errorf("refuse %q: reason %q, want %q", tt.refuse, fe.Reason, tt.reason)
			}
			if !strings.HasPrefix(fe.Error(), "serial: ") {
				t.Errorf("error %q does not name the field", fe.Error())
			}
		})
	}
}

// TestValidateCTA2063ABound exceeds MaxLen (E-10): 21 characters of the
// right alphabet are refused, a very long input too, without work in
// proportion to its length mattering.
func TestValidateCTA2063ABound(t *testing.T) {
	if err := ValidateCTA2063A("1A2BF" + strings.Repeat("2", 15)); err != nil {
		t.Fatalf("20 characters: %v", err)
	}
	for _, s := range []string{"1A2BF" + strings.Repeat("2", 16), strings.Repeat("2", 1<<20)} {
		fe := fieldError(t, ValidateCTA2063A(s), Field)
		if fe.Reason != reasonShape {
			t.Errorf("len %d: %q", len(s), fe.Reason)
		}
	}
}

func TestRequiresCTA(t *testing.T) {
	want := map[string]bool{"C0": false, "C1": true, "C2": true, "C3": true, "C4": false, "C5": true, "C6": true, "": false, "C7": false, "c1": false}
	for label, w := range want {
		if got := RequiresCTA(label); got != w {
			t.Errorf("RequiresCTA(%q) = %v, want %v", label, got, w)
		}
	}
}

func TestValidateForClass(t *testing.T) {
	for _, label := range []string{"C1", "C2", "C3", "C5", "C6"} {
		t.Run(label, func(t *testing.T) {
			if err := ValidateForClass("1A2B1X", label); err != nil {
				t.Errorf("CTA serial refused: %v", err)
			}
			fe := fieldError(t, ValidateForClass("DJI-0042", label), Field)
			if want := "class " + label + " requires a CTA-2063-A serial; " + reasonShape; fe.Reason != want {
				t.Errorf("reason %q, want %q", fe.Reason, want)
			}
			fe = fieldError(t, ValidateForClass("1A2B2ABC", label), Field)
			if !strings.Contains(fe.Reason, "says 2 characters follow, and 3 do") {
				t.Errorf("mismatch reason lost: %q", fe.Reason)
			}
			fe = fieldError(t, ValidateForClass("", label), Field)
			if fe.Reason != reasonRequired {
				t.Errorf("empty: %q", fe.Reason)
			}
		})
	}
	for _, label := range []string{"C0", "C4", ""} {
		t.Run("no CTA "+label, func(t *testing.T) {
			if err := ValidateForClass("DJI-0042", label); err != nil {
				t.Errorf("legacy serial refused: %v", err)
			}
			for _, empty := range []string{"", "   ", "\t"} {
				fe := fieldError(t, ValidateForClass(empty, label), Field)
				if fe.Reason != reasonRequired {
					t.Errorf("empty %q: %q", empty, fe.Reason)
				}
			}
		})
	}
	t.Run("unknown class", func(t *testing.T) {
		fe := fieldError(t, ValidateForClass("1A2B1X", "C7"), FieldClassLabel)
		if !strings.Contains(fe.Reason, `"C7"`) {
			t.Errorf("reason %q does not name the label", fe.Reason)
		}
		fieldError(t, ValidateForClass("1A2B1X", "c1"), FieldClassLabel)
	})
}

func TestNormalizeFoldKey(t *testing.T) {
	tests := []struct{ in, norm, fold string }{
		{" dji-0042 ", "dji-0042", "DJI-0042"},
		{"DJI-0042", "DJI-0042", "DJI-0042"},
		{"\t1A2B1X\n", "1A2B1X", "1A2B1X"},
		{"", "", ""},
		// Only ASCII folds: U+017F (long s) and U+0131 (dotless i) are
		// kept, so they never meet the ASCII key SN-FLEET or I.
		{"ſn-fleet", "ſn-fleet", "ſN-FLEET"},
		{"ı", "ı", "ı"},
		{"éa", "éa", "éA"},
	}
	for _, tt := range tests {
		if got := Normalize(tt.in); got != tt.norm {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.norm)
		}
		if got := FoldKey(tt.in); got != tt.fold {
			t.Errorf("FoldKey(%q) = %q, want %q", tt.in, got, tt.fold)
		}
	}
	// G-05: two legacy serials that differ only in case stay distinct when
	// stored, and share a fold key.
	if Normalize("abc1") == Normalize("ABC1") || FoldKey("abc1") != FoldKey("ABC1") {
		t.Error("case must be kept by Normalize and folded by FoldKey")
	}
	// A non-ASCII look-alike never folds onto an ASCII key; its ASCII
	// twin does.
	if FoldKey("ſn-fleet") == FoldKey("SN-FLEET") {
		t.Error("a non-ASCII look-alike folds onto an ASCII serial")
	}
	if FoldKey("sn-fleet") != FoldKey("SN-FLEET") {
		t.Error("the ASCII spelling does not fold")
	}
}

// FuzzFoldKey: a fold key changes only ASCII lower-case letters, and a
// key with a non-ASCII byte is never an ASCII key.
func FuzzFoldKey(f *testing.F) {
	for _, s := range []string{"sn-fleet", "ſn-fleet", " dji-0042 ", "", "ÿ"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, k := Normalize(s), FoldKey(s)
		if len(n) != len(k) {
			t.Fatalf("FoldKey(%q) changed the length", s)
		}
		for i := range len(n) {
			c, d := n[i], k[i]
			if c != d && (c < 'a' || c > 'z' || d != c-('a'-'A')) {
				t.Fatalf("FoldKey(%q) changed byte %d from %q to %q", s, i, c, d)
			}
		}
		if FoldKey(k) != k {
			t.Fatalf("not idempotent: %q", s)
		}
	})
}

func FuzzValidateCTA2063A(f *testing.F) {
	for _, s := range []string{
		"1A2B1X", "1A2B9123456789", "1A2BF123456789ABCDEF", "1581A1234567890",
		"1A2B3AB", "1A2B2ABC", "1O2B1X", "1A2B1I", "1a2b1x", "1A2B0X",
		"1A2BG123456789ABCDEFG", "", "DJI-0042",
	} {
		f.Add(s, "C1")
	}
	f.Add("DJI-0042", "")
	f.Add("", "C0")
	f.Fuzz(func(t *testing.T, s, label string) {
		err := ValidateCTA2063A(s)
		if err == nil {
			if len(s) > MaxLen || strings.ToUpper(s) != s {
				t.Fatalf("accepted %q", s)
			}
			if FoldKey(s) != s {
				t.Fatalf("valid CTA serial %q is not its own fold key", s)
			}
		} else {
			fe := fieldError(t, err, Field)
			if !strings.Contains(fe.Reason, "not a CTA") {
				t.Fatalf("reason %q", fe.Reason)
			}
		}
		cerr := ValidateForClass(s, label)
		if RequiresCTA(label) && err != nil && cerr == nil {
			t.Fatalf("class %s accepted non-CTA %q", label, s)
		}
		if cerr != nil {
			var fe *core.FieldError
			if !errors.As(cerr, &fe) || fe.Field == "" {
				t.Fatalf("error without a field: %v", cerr)
			}
		}
	})
}

func BenchmarkValidateCTA(b *testing.B) {
	serials := []string{"1A2BF123456789ABCDEF", "1A2B3AB", "1a2b1x"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_ = ValidateCTA2063A(serials[i%len(serials)])
	}
}
