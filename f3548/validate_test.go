package f3548

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func intentTree(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readExample(t, "operational_intent.json"), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func obj(m map[string]any, keys ...string) map[string]any {
	for _, k := range keys {
		m = m[k].(map[string]any)
	}
	return m
}

func volume(m map[string]any) map[string]any {
	return obj(m, "details")["volumes"].([]any)[0].(map[string]any)
}

// Each check refuses its member by path; the unchanged intent and every
// example of the corpus are accepted (E-01).
func TestUnmarshalChecks(t *testing.T) {
	for _, name := range []string{"operational_intent.json", "operational_intent_activated_circle.json", "operational_intent_contingent.json"} {
		if _, err := UnmarshalOperationalIntent(readExample(t, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	ref := func(m map[string]any) map[string]any { return obj(m, "reference") }
	cases := []struct {
		name  string
		raw   string
		mut   func(m map[string]any)
		field string
	}{
		{"null", "null", nil, "operational_intent.reference.id"},
		{"empty object", "{}", nil, "operational_intent.reference.id"},
		{"no manager", "", func(m map[string]any) { delete(ref(m), "manager") }, "operational_intent.reference.manager"},
		{"no base url", "", func(m map[string]any) { delete(ref(m), "uss_base_url") }, "operational_intent.reference.uss_base_url"},
		{"no subscription", "", func(m map[string]any) { delete(ref(m), "subscription_id") }, "operational_intent.reference.subscription_id"},
		{"unknown state", "", func(m map[string]any) { ref(m)["state"] = "Ended" }, "operational_intent.reference.state"},
		{"unknown availability", "", func(m map[string]any) { ref(m)["uss_availability"] = "Up" }, "operational_intent.reference.uss_availability"},
		{"time format", "", func(m map[string]any) { obj(m, "reference", "time_start")["format"] = "Unix" }, "operational_intent.reference.time_start.format"},
		{"no time value", "", func(m map[string]any) { delete(obj(m, "reference", "time_end"), "value") }, "operational_intent.reference.time_end.value"},
		{"end before start", "", func(m map[string]any) {
			obj(m, "reference", "time_end")["value"] = "1985-04-12T23:00:00Z"
		}, "operational_intent.reference.time_end"},
		{"vertex lat", "", func(m map[string]any) {
			obj(volume(m), "volume", "outline_polygon")["vertices"].([]any)[0].(map[string]any)["lat"] = 99
		}, "operational_intent.details.volumes[0]"},
		{"altitude reference", "", func(m map[string]any) { obj(volume(m), "volume", "altitude_lower")["reference"] = "AMSL" }, "operational_intent.details.volumes[0].volume.altitude_lower"},
		{"altitude range", "", func(m map[string]any) { obj(volume(m), "volume", "altitude_upper")["value"] = 100001 }, "operational_intent.details.volumes[0].volume.altitude_upper.value"},
		{"lower above upper", "", func(m map[string]any) { obj(volume(m), "volume", "altitude_lower")["value"] = 200 }, "operational_intent.details.volumes[0].volume.altitude_upper"},
		{"off-nominal outline", "", func(m map[string]any) {
			obj(m, "details")["off_nominal_volumes"] = []any{map[string]any{"volume": map[string]any{}}}
		}, "operational_intent.details.off_nominal_volumes[0]"},
	}
	for _, c := range cases {
		raw := []byte(c.raw)
		if c.mut != nil {
			m := intentTree(t)
			c.mut(m)
			raw, _ = json.Marshal(m)
		}
		_, err := UnmarshalOperationalIntent(raw)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", c.name, err, c.field)
		}
	}
	// The ends of the ranges, and equal altitudes and times, are accepted.
	m := intentTree(t)
	obj(volume(m), "volume", "altitude_lower")["value"] = -8000
	obj(volume(m), "volume", "altitude_upper")["value"] = 100000
	obj(m, "reference", "time_end")["value"] = obj(m, "reference", "time_start")["value"]
	raw, _ := json.Marshal(m)
	if _, err := UnmarshalOperationalIntent(raw); err != nil {
		t.Errorf("range ends refused: %v", err)
	}
}
