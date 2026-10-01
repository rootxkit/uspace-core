package f3411

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// flightTree is rid_flight.json as a plain JSON tree to mutate.
func flightTree(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(readExample(t, "rid_flight.json"), &m); err != nil {
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

// Each check refuses its member by path; the unchanged flight, and each
// special value at its member, is accepted (E-01).
func TestUnmarshalChecks(t *testing.T) {
	if _, err := UnmarshalRIDFlight(readExample(t, "rid_flight.json")); err != nil {
		t.Fatalf("the base flight: %v", err)
	}
	cases := []struct {
		name  string
		raw   string
		mut   func(m map[string]any)
		field string
	}{
		{"null", "null", nil, "flight.id"},
		{"empty object", "{}", nil, "flight.id"},
		{"no id", "", func(m map[string]any) { delete(m, "id") }, "flight.id"},
		{"unknown aircraft type", "", func(m map[string]any) { m["aircraft_type"] = "Dragon" }, "flight.aircraft_type"},
		{"neither state nor area", "", func(m map[string]any) { delete(m, "current_state") }, "flight"},
		{"empty area", "", func(m map[string]any) {
			delete(m, "current_state")
			m["operating_area"] = map[string]any{"volumes": []any{}}
		}, "flight"},
		{"timestamp format", "", func(m map[string]any) { obj(m, "current_state", "timestamp")["format"] = "ISO8601" }, "flight.current_state.timestamp.format"},
		{"no timestamp value", "", func(m map[string]any) { delete(obj(m, "current_state", "timestamp"), "value") }, "flight.current_state.timestamp.value"},
		{"negative accuracy", "", func(m map[string]any) { obj(m, "current_state")["timestamp_accuracy"] = -1 }, "flight.current_state.timestamp_accuracy"},
		{"no speed accuracy", "", func(m map[string]any) { delete(obj(m, "current_state"), "speed_accuracy") }, "flight.current_state.speed_accuracy"},
		{"unknown status", "", func(m map[string]any) { obj(m, "current_state")["operational_status"] = "Flying" }, "flight.current_state.operational_status"},
		{"speed above max", "", func(m map[string]any) { obj(m, "current_state")["speed"] = 254.5 }, "flight.current_state.speed"},
		{"negative speed", "", func(m map[string]any) { obj(m, "current_state")["speed"] = -1 }, "flight.current_state.speed"},
		{"track 360", "", func(m map[string]any) { obj(m, "current_state")["track"] = 360 }, "flight.current_state.track"},
		{"vertical speed", "", func(m map[string]any) { obj(m, "current_state")["vertical_speed"] = 62.5 }, "flight.current_state.vertical_speed"},
		{"lat 99", "", func(m map[string]any) { obj(m, "current_state", "position")["lat"] = 99 }, "flight.current_state.position.lat"},
		{"lng 500", "", func(m map[string]any) { obj(m, "current_state", "position")["lng"] = 500 }, "flight.current_state.position.lng"},
		{"accuracy h", "", func(m map[string]any) { obj(m, "current_state", "position")["accuracy_h"] = "HA2m" }, "flight.current_state.position.accuracy_h"},
		{"accuracy v", "", func(m map[string]any) { obj(m, "current_state", "position")["accuracy_v"] = "VA2m" }, "flight.current_state.position.accuracy_v"},
		{"height reference", "", func(m map[string]any) { obj(m, "current_state", "position", "height")["reference"] = "Sea" }, "flight.current_state.position.height.reference"},
		{"recent time", "", func(m map[string]any) {
			m["recent_positions"].([]any)[0].(map[string]any)["time"] = map[string]any{"value": "1985-04-12T23:20:49.52Z", "format": "Unix"}
		}, "flight.recent_positions[0].time.format"},
		{"recent lat", "", func(m map[string]any) {
			m["recent_positions"].([]any)[0].(map[string]any)["position"] = map[string]any{"lat": -91, "lng": 0}
		}, "flight.recent_positions[0].position.lat"},
	}
	for _, c := range cases {
		raw := []byte(c.raw)
		if c.mut != nil {
			m := flightTree(t)
			c.mut(m)
			raw, _ = json.Marshal(m)
		}
		_, err := UnmarshalRIDFlight(raw)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", c.name, err, c.field)
		}
	}
	// The ends of each range and the special values are accepted.
	for name, mut := range map[string]func(m map[string]any){
		"special values": func(m map[string]any) {
			s := obj(m, "current_state")
			s["speed"], s["track"], s["vertical_speed"] = 255, 361, 63
		},
		"range ends": func(m map[string]any) {
			s := obj(m, "current_state")
			s["speed"], s["track"], s["vertical_speed"], s["timestamp_accuracy"] = 254.25, 359.9, -62, 0
			p := obj(m, "current_state", "position")
			p["lat"], p["lng"] = -90, 180
		},
		"no position fix": func(m map[string]any) { obj(m, "current_state")["position"] = map[string]any{} },
		"no status":       func(m map[string]any) { delete(obj(m, "current_state"), "operational_status") },
	} {
		m := flightTree(t)
		mut(m)
		raw, _ := json.Marshal(m)
		if _, err := UnmarshalRIDFlight(raw); err != nil {
			t.Errorf("%s refused: %v", name, err)
		}
	}
}

func TestUnmarshalOperatingAreaChecks(t *testing.T) {
	if _, err := UnmarshalRIDFlight(readExample(t, "rid_flight_operating_area.json")); err != nil {
		t.Fatalf("the operating-area flight: %v", err)
	}
	for name, c := range map[string]struct {
		mut   func(v map[string]any)
		field string
	}{
		"no outline":     {func(v map[string]any) { v["volume"] = map[string]any{} }, "flight.operating_area.volumes[0]"},
		"altitude ref":   {func(v map[string]any) { obj(v, "volume", "altitude_upper")["reference"] = "SFC" }, "flight.operating_area.volumes[0].volume.altitude_upper"},
		"altitude range": {func(v map[string]any) { obj(v, "volume", "altitude_upper")["value"] = 100001 }, "flight.operating_area.volumes[0].volume.altitude_upper.value"},
	} {
		var m map[string]any
		_ = json.Unmarshal(readExample(t, "rid_flight_operating_area.json"), &m)
		v := obj(m, "operating_area")["volumes"].([]any)[0].(map[string]any)
		c.mut(v)
		raw, _ := json.Marshal(m)
		_, err := UnmarshalRIDFlight(raw)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, c.field)
		}
	}
}

func TestUnmarshalGetFlightsChecks(t *testing.T) {
	for name, c := range map[string]struct {
		raw   string
		field string
	}{
		"no timestamp": {`{"flights":[]}`, "response.timestamp.format"},
		"bad flight":   {`{"timestamp":{"value":"1985-04-12T23:20:50.52Z","format":"RFC3339"},"flights":[{}]}`, "response.flights[0].id"},
	} {
		_, err := UnmarshalGetFlightsResponse([]byte(c.raw))
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, c.field)
		}
	}
	if _, err := UnmarshalGetFlightsResponse([]byte(`{"timestamp":{"value":"1985-04-12T23:20:50.52Z","format":"RFC3339"}}`)); err != nil {
		t.Errorf("a response without flights: %v", err)
	}
}
