package ed318

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/vectors"
)

type vectorInput struct {
	Kind          string                       `json:"kind"`
	Document      json.RawMessage              `json:"document"`
	ED269Document json.RawMessage              `json:"ed269_document"`
	Lang          string                       `json:"lang"`
	At            string                       `json:"at"`
	Where         *core.LatLon                 `json:"where"`
	Daylight      map[string]map[string]string `json:"daylight"`
}

type mustInclude struct {
	FieldEndsWith  string `json:"field_endswith"`
	ReasonContains string `json:"reason_contains"`
}

type vectorExpected struct {
	Accepted       *bool           `json:"accepted"`
	Export         json.RawMessage `json:"export"`
	MustInclude    *mustInclude    `json:"must_include"`
	Mapped         *bool           `json:"mapped"`
	ED269          json.RawMessage `json:"ed269"`
	ED318          json.RawMessage `json:"ed318"`
	FieldEndsWith  string          `json:"field_endswith"`
	ReasonContains string          `json:"reason_contains"`
	Applies        *bool           `json:"applies"`
	NotEvaluated   *bool           `json:"not_evaluated"`
}

func TestVectorsED318Roundtrip(t *testing.T) {
	f := vectors.Load(t, "ed318_roundtrip.json")
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in vectorInput
		var exp vectorExpected
		c.Decode(t, &in, &exp)
		switch in.Kind {
		case "parse":
			runParseCase(t, in, exp)
		case "to_ed269":
			runToED269Case(t, in, exp)
		case "from_ed269":
			runFromED269Case(t, in, exp)
		case "applies":
			runAppliesCase(t, in, exp)
		default:
			t.Fatalf("unknown kind %q", in.Kind)
		}
	})
}

func runParseCase(t *testing.T, in vectorInput, exp vectorExpected) {
	fc, probs := Parse(in.Document, Limits{})
	if exp.Accepted == nil {
		t.Fatal("no expected.accepted")
	}
	if !*exp.Accepted {
		if probs == nil {
			t.Fatal("accepted, want refused")
		}
		m := exp.MustInclude
		for _, p := range probs.List {
			if strings.HasSuffix(p.Field, m.FieldEndsWith) && strings.Contains(p.Reason, m.ReasonContains) {
				return
			}
		}
		t.Fatalf("no problem ending %q containing %q in %v", m.FieldEndsWith, m.ReasonContains, probs)
	}
	if probs != nil {
		t.Fatalf("refused: %v", probs)
	}
	out, err := Export(fc)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, out, exp.Export) {
		t.Errorf("export differs:\n got %s\nwant %s", out, exp.Export)
	}
}

func parseOrFail(t *testing.T, doc []byte) *FeatureCollection {
	t.Helper()
	fc, probs := Parse(doc, Limits{})
	if probs != nil {
		t.Fatalf("the input is refused: %v", probs)
	}
	return fc
}

func runToED269Case(t *testing.T, in vectorInput, exp vectorExpected) {
	doc, err := ToED269(parseOrFail(t, in.Document))
	if exp.Mapped == nil {
		t.Fatal("no expected.mapped")
	}
	if !*exp.Mapped {
		var fe *core.FieldError
		if err == nil || !errors.As(err, &fe) || !strings.HasSuffix(fe.Field, exp.FieldEndsWith) || !strings.Contains(fe.Reason, exp.ReasonContains) {
			t.Fatalf("got %v, want a refusal of a field ending %q containing %q", err, exp.FieldEndsWith, exp.ReasonContains)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	out, err := ed269.Export(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, out, exp.ED269) {
		t.Errorf("ED-269 differs:\n got %s\nwant %s", out, exp.ED269)
	}
}

func runFromED269Case(t *testing.T, in vectorInput, exp vectorExpected) {
	doc, probs := ed269.Parse(in.ED269Document, ed269.Limits{})
	if probs != nil {
		t.Fatalf("the ED-269 input is refused: %v", probs)
	}
	fc, err := FromED269(doc, Metadata{}, in.Lang)
	if exp.Mapped == nil || !*exp.Mapped {
		t.Fatal("from_ed269 cases pin a mapping")
	}
	if err != nil {
		t.Fatal(err)
	}
	out, err := Export(fc)
	if err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, out, exp.ED318) {
		t.Errorf("ED-318 differs:\n got %s\nwant %s", out, exp.ED318)
	}
	// The mapping is accepted by Parse and maps back to the input.
	back, err := ToED269(parseOrFail(t, out))
	if err != nil {
		t.Fatal(err)
	}
	sameED269(t, back, doc)
}

func runAppliesCase(t *testing.T, in vectorInput, exp vectorExpected) {
	fc := parseOrFail(t, in.Document)
	at, err := time.Parse(time.RFC3339, in.At)
	if err != nil {
		t.Fatal(err)
	}
	var dl Daylight
	if in.Daylight != nil {
		table := FixedDaylight{}
		for date, evs := range in.Daylight {
			table[date] = map[string]time.Time{}
			for ev, s := range evs {
				v, err := time.Parse(time.RFC3339, s)
				if err != nil {
					t.Fatal(err)
				}
				table[date][ev] = v
			}
		}
		dl = table
	}
	got, err := Applies(fc.Features[0].Properties.LimitedApplicability, at, *in.Where, dl)
	if got != *exp.Applies {
		t.Errorf("applies %v, want %v (error %v)", got, *exp.Applies, err)
	}
	if (err != nil) != *exp.NotEvaluated {
		t.Errorf("not evaluated: error %v, want not_evaluated %v", err, *exp.NotEvaluated)
	}
	if err != nil && !strings.Contains(err.Error(), exp.ReasonContains) {
		t.Errorf("reason %q does not contain %q", err, exp.ReasonContains)
	}
}
