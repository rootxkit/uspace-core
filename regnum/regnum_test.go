package regnum

import (
	"errors"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

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

func TestNewValidator(t *testing.T) {
	v, err := NewValidator("")
	if err != nil || v.Pattern() != DefaultPattern {
		t.Fatalf("empty pattern: %v %v", v, err)
	}
	v, err = NewValidator(`GEO[0-9]{6}`)
	if err != nil || v.Pattern() != `GEO[0-9]{6}` {
		t.Fatalf("custom pattern: %v", err)
	}
	// The pattern is anchored even without ^ and $.
	if err := v.Validate("GEO123456"); err != nil {
		t.Errorf("accept: %v", err)
	}
	if err := v.Validate("xGEO1234567"); err == nil {
		t.Error("an unanchored pattern matched a substring")
	}
	_, err = NewValidator(`[A-Z`)
	fe := fieldError(t, err, FieldPattern)
	if !strings.Contains(fe.Reason, `"[A-Z"`) {
		t.Errorf("reason %q does not quote the pattern", fe.Reason)
	}
}

// TestValidate pairs every refusal with the accepted value it differs
// from (E-01).
func TestValidate(t *testing.T) {
	v, _ := NewValidator("")
	tests := []struct{ name, accept, refuse, reason string }{
		{"hyphen tail", "FIN87astrdge12k8", "FIN87astrdge12k8-xyz", reasonHyphen},
		{"hyphen anywhere", "GEOabcd1234efgh", "GEO-abcd1234efgh", reasonHyphen},
		{"lower-case country", "FIN87astrdge12k8", "fin87astrdge12k8", "does not match the configured format " + DefaultPattern},
		{"too short", "FIN87astrdge", "FIN87", "does not match the configured format " + DefaultPattern},
		{"too long for the pattern", "FIN" + strings.Repeat("a", 16), "FIN" + strings.Repeat("a", 17), "does not match the configured format " + DefaultPattern},
		{"over MaxLen", "FIN87astrdge12k8", "FIN" + strings.Repeat("a", MaxLen), "longer than 64 characters"},
		{"empty", "FIN87astrdge12k8", "", "a registration number is required"},
		{"blank", " FIN87astrdge12k8 ", " \t ", "a registration number is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := v.Validate(tt.accept); err != nil {
				t.Errorf("accept %q: %v", tt.accept, err)
			}
			fe := fieldError(t, v.Validate(tt.refuse), Field)
			if fe.Reason != tt.reason {
				t.Errorf("refuse %q: %q, want %q", tt.refuse, fe.Reason, tt.reason)
			}
			if !strings.HasPrefix(fe.Error(), Field+": ") {
				t.Errorf("error %q does not name the field", fe.Error())
			}
		})
	}
}

// TestPublicPart covers the strip, every way of not stripping, and the
// G-04 pitfall in both readings.
func TestPublicPart(t *testing.T) {
	tests := []struct{ in, public, key string }{
		{"FIN87astrdge12k8-xyz", "FIN87astrdge12k8", "FIN87ASTRDGE12K8"},
		{" FIN87astrdge12k8-XY1 ", "FIN87astrdge12k8", "FIN87ASTRDGE12K8"},
		{"GEOabcd1234efgh-x9z", "GEOabcd1234efgh", "GEOABCD1234EFGH"},
		{"FIN87astrdge12k8", "FIN87astrdge12k8", "FIN87ASTRDGE12K8"},
		{"fin87astrdge12k8", "fin87astrdge12k8", "FIN87ASTRDGE12K8"},
		{"GEO-OP-SITL", "GEO-OP-SITL", "GEO-OP-SITL"},
		// G-04 pitfall corrected: "GEO-OP" is no registration number.
		{"GEO-OP-ABC", "GEO-OP-ABC", "GEO-OP-ABC"},
		{"FIN87astrdge12k8-", "FIN87astrdge12k8-", "FIN87ASTRDGE12K8-"},
		{"-xyz", "-xyz", "-XYZ"},
		{"FIN87astrdge12k8-x!z", "FIN87astrdge12k8-x!z", "FIN87ASTRDGE12K8-X!Z"},
		{"FIN87astrdge12k8-xy", "FIN87astrdge12k8-xy", "FIN87ASTRDGE12K8-XY"},
		{"FIN87astrdge12k8-wxyz", "FIN87astrdge12k8-wxyz", "FIN87ASTRDGE12K8-WXYZ"},
		{"FIN87astrdge12k8-xé", "FIN87astrdge12k8-xé", "FIN87ASTRDGE12K8-XÉ"},
		// Stripped once only.
		{"FIN87astrdge12k8-abc-xyz", "FIN87astrdge12k8-abc-xyz", "FIN87ASTRDGE12K8-ABC-XYZ"},
		{"", "", ""},
	}
	for _, tt := range tests {
		if got := PublicPart(tt.in); got != tt.public {
			t.Errorf("PublicPart(%q) = %q, want %q", tt.in, got, tt.public)
		}
		if got := CompareKey(tt.in); got != tt.key {
			t.Errorf("CompareKey(%q) = %q, want %q", tt.in, got, tt.key)
		}
		if p, k := Public(tt.in); p != tt.public || k != tt.key {
			t.Errorf("Public(%q) = %q, %q", tt.in, p, k)
		}
	}
}

// TestPublicPartConfiguredPattern shows the format is configuration: a
// pattern that accepts "GEO-OP" strips the tail, the default does not.
func TestPublicPartConfiguredPattern(t *testing.T) {
	v, err := NewValidator(`[A-Z]{3}-[A-Z]{2}`)
	if err != nil {
		t.Fatal(err)
	}
	if p, k := v.Public("GEO-OP-abc"); p != "GEO-OP" || k != "GEO-OP" {
		t.Errorf("configured: %q %q", p, k)
	}
	if got := PublicPart("GEO-OP-abc"); got != "GEO-OP-abc" {
		t.Errorf("default: %q", got)
	}
	// The head is bounded by MaxLen before the pattern runs (E-10).
	long, _ := NewValidator(`A+`)
	in := strings.Repeat("A", MaxLen+1) + "-xyz"
	if got := long.PublicPart(in); got != in {
		t.Errorf("over-long head stripped: %q", got)
	}
	in = strings.Repeat("A", MaxLen) + "-xyz"
	if got := long.PublicPart(in); got != strings.Repeat("A", MaxLen) {
		t.Errorf("head at MaxLen not stripped: %q", got)
	}
}

func TestConcurrentUse(t *testing.T) {
	v, _ := NewValidator("")
	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 200 {
				if err := v.Validate("FIN87astrdge12k8"); err != nil {
					t.Error(err)
					return
				}
				if k := v.CompareKey("FIN87astrdge12k8-xyz"); k != "FIN87ASTRDGE12K8" {
					t.Errorf("compare key %q", k)
					return
				}
			}
		}()
	}
	for range 8 {
		<-done
	}
}

func FuzzPublicPart(f *testing.F) {
	for _, s := range []string{
		"FIN87astrdge12k8-xyz", " FIN87astrdge12k8-XY1 ", "FIN87astrdge12k8", "GEO-OP-SITL",
		"GEO-OP-ABC", "FIN87astrdge12k8-", "-xyz", "FIN87astrdge12k8-x!z", "fin87astrdge12k8", "FIN87", "",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		p, k := Public(s)
		upper := strings.ToUpper(p)
		if p != PublicPart(s) || k != CompareKey(s) || k != upper {
			t.Fatalf("Public(%q) = %q, %q disagrees", s, p, k)
		}
		if PublicPart(p) != p {
			t.Fatalf("not idempotent: %q -> %q -> %q", s, p, PublicPart(p))
		}
		if p != strings.TrimSpace(s) {
			// Something was stripped: the rest is a valid registration number.
			if err := defaultValidator.Validate(p); err != nil {
				t.Fatalf("stripped %q to %q, which is invalid: %v", s, p, err)
			}
		}
		if err := defaultValidator.Validate(s); err != nil {
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != Field {
				t.Fatalf("error without the field: %v", err)
			}
		} else if CompareKey(s) != strings.ToUpper(strings.TrimSpace(s)) {
			t.Fatalf("a valid number %q changed under CompareKey", s)
		}
	})
}

func BenchmarkCompareKey(b *testing.B) {
	in := []string{"FIN87astrdge12k8-xyz", "GEO-OP-SITL", "GEOabcd1234efgh"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		_ = CompareKey(in[i%len(in)])
	}
}
