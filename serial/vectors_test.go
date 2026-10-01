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
	FoldKey         *string `json:"fold_key"`
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
			if in.Pattern == nil {
				t.Fatal("case lacks pattern: the strip depends on it (G-04)")
			}
			v, err := regnum.NewValidator(*in.Pattern)
			if err != nil {
				t.Fatalf("pattern %q: %v", *in.Pattern, err)
			}
			wantPublic, wantKey := *exp.Public, *exp.CompareKey
			public, key := v.Public(str(in.Value))
			if public != wantPublic {
				t.Errorf("public %q, want %q", public, wantPublic)
			}
			if key != wantKey {
				t.Errorf("compare_key %q, want %q", key, wantKey)
			}
			if v.CompareKey(str(in.Value)) != key || v.PublicPart(str(in.Value)) != public {
				t.Error("Public disagrees with PublicPart/CompareKey")
			}
		case "serial_fold":
			if exp.FoldKey == nil {
				t.Fatal("case lacks fold_key")
			}
			if got := serial.FoldKey(str(in.Serial)); got != *exp.FoldKey {
				t.Errorf("fold_key %q, want %q", got, *exp.FoldKey)
			}
		default:
			t.Fatalf("unknown kind %q", in.Kind)
		}
		ran[in.Kind]++
	})
	want := map[string]int{"cta2063": 12, "serial_for_class": 9, "registration_number": 4, "public_registration_number": 12, "serial_fold": 4}
	for k, n := range want {
		if ran[k] != n {
			t.Errorf("kind %s: ran %d cases, want %d", k, ran[k], n)
		}
	}
	t.Logf("ran %v", ran)
}
