package serial_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/regnum"
	"github.com/rootxkit/uspace-core/serial"
	"github.com/rootxkit/uspace-core/vectors"
)

type vecInput struct {
	Kind       string  `json:"kind"`
	Serial     *string `json:"serial"`
	ClassLabel *string `json:"class_label"`
	Value      *string `json:"value"`
	Pattern    *string `json:"pattern"`
}

type vecExpected struct {
	Valid           *bool   `json:"valid"`
	Problem         *string `json:"problem"`
	ProblemContains *string `json:"problem_contains"`
	Public          *string `json:"public"`
	CompareKey      *string `json:"compare_key"`
}

// deviation is a vector case this module deliberately answers differently
// from the predecessor that generated it. The vector file is unchanged
// (it is regenerated in uspace-lab, never edited); the test checks that
// the recorded value is the old rule's and that the module returns the
// corrected one.
type deviation struct {
	lesson, why        string
	public, compareKey string
}

var knownDeviations = map[string]deviation{
	"public-part-GEO-OP-ABC": {
		lesson: "G-04",
		why: "the predecessor stripped any three-alphanumeric tail after the last hyphen; " +
			"the EU secret part follows a public registration number, and GEO-OP is none " +
			"under the configured pattern, so nothing is stripped",
		public:     "GEO-OP-ABC",
		compareKey: "GEO-OP-ABC",
	},
}

// legacyPublicPart is the predecessor's rule (utm common/uas_identity.py
// public_registration_number), kept here only to show that a deviation's
// recorded value is that rule's output and not a different disagreement.
func legacyPublicPart(value string) string {
	s := strings.TrimSpace(value)
	i := strings.LastIndex(s, "-")
	if i <= 0 {
		return s
	}
	tail := s[i+1:]
	if len(tail) != 3 {
		return s
	}
	for _, r := range tail {
		if !isAlnum(r) {
			return s
		}
	}
	return s[:i]
}

func isAlnum(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// checkProblem compares an error with the case's expectation: valid
// exactly; problem_contains binding when present; the full old wording
// compared loosely (logged) otherwise, as the file's description says.
func checkProblem(t *testing.T, err error, field string, exp vecExpected) {
	t.Helper()
	if exp.Valid == nil {
		t.Fatal("case has no expected.valid")
	}
	if *exp.Valid {
		if err != nil {
			t.Errorf("want valid, got %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("want invalid (%s), got nil", str(exp.Problem))
	}
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != field {
		t.Errorf("error %v does not name field %q", err, field)
	}
	if exp.ProblemContains != nil {
		if !strings.Contains(err.Error(), *exp.ProblemContains) {
			t.Errorf("error %q does not contain %q", err, *exp.ProblemContains)
		}
	}
	if exp.Problem != nil && !strings.Contains(err.Error(), *exp.Problem) {
		t.Logf("wording differs from the old one (not binding):\n got  %q\n old  %q", err, *exp.Problem)
	}
}

func TestVectorsSerialsAndRegistration(t *testing.T) {
	f := vectors.Load(t, "serials_and_registration.json")
	var rule string
	f.Header(t, "cta2063_rule", &rule)
	if !strings.Contains(rule, "at most 20") || serial.MaxLen != 20 {
		t.Fatalf("cta2063_rule changed: %q (MaxLen %d)", rule, serial.MaxLen)
	}
	ran := map[string]int{}
	deviated := 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in vecInput
		var exp vecExpected
		c.Decode(t, &in, &exp)
		switch in.Kind {
		case "cta2063":
			checkProblem(t, serial.ValidateCTA2063A(str(in.Serial)), serial.Field, exp)
		case "serial_for_class":
			checkProblem(t, serial.ValidateForClass(str(in.Serial), str(in.ClassLabel)), serial.Field, exp)
		case "registration_number":
			v, err := regnum.NewValidator(str(in.Pattern))
			if err != nil {
				t.Fatalf("pattern %q: %v", str(in.Pattern), err)
			}
			checkProblem(t, v.Validate(str(in.Value)), regnum.Field, exp)
		case "public_registration_number":
			if exp.Public == nil || exp.CompareKey == nil {
				t.Fatal("case lacks public or compare_key")
			}
			wantPublic, wantKey := *exp.Public, *exp.CompareKey
			if d, ok := knownDeviations[c.Name]; ok {
				deviated++
				if legacyPublicPart(str(in.Value)) != wantPublic {
					t.Fatalf("the recorded value %q is not the old rule's output; the deviation needs review", wantPublic)
				}
				t.Logf("KNOWN DEVIATION (%s): vector says %q/%q, this module returns %q/%q: %s",
					d.lesson, wantPublic, wantKey, d.public, d.compareKey, d.why)
				wantPublic, wantKey = d.public, d.compareKey
			}
			public, key := regnum.Public(str(in.Value))
			if public != wantPublic {
				t.Errorf("public %q, want %q", public, wantPublic)
			}
			if key != wantKey {
				t.Errorf("compare_key %q, want %q", key, wantKey)
			}
			if regnum.CompareKey(str(in.Value)) != key || regnum.PublicPart(str(in.Value)) != public {
				t.Error("Public disagrees with PublicPart/CompareKey")
			}
		default:
			t.Fatalf("unknown kind %q", in.Kind)
		}
		ran[in.Kind]++
	})
	want := map[string]int{"cta2063": 12, "serial_for_class": 9, "registration_number": 4, "public_registration_number": 8}
	for k, n := range want {
		if ran[k] != n {
			t.Errorf("kind %s: ran %d cases, want %d", k, ran[k], n)
		}
	}
	if deviated != len(knownDeviations) {
		t.Errorf("%d known deviations met, %d declared", deviated, len(knownDeviations))
	}
	t.Logf("ran %v; %d known deviation(s)", ran, deviated)
}
