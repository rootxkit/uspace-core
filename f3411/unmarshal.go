package f3411

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/rootxkit/uspace-core/core"
)

// MaxMessageBytes bounds one F3411 message read by the Unmarshal helpers
// (E-10). A GET /uss/flights response for a 7 km view with recent
// positions is tens of kilobytes; 4 MiB leaves two orders of magnitude.
const MaxMessageBytes = 4 << 20

// UnmarshalRIDFlight reads one RIDFlight from untrusted bytes and checks
// the members a judgement rests on: required presence, enumeration
// membership and the latitude, longitude, altitude, speed, track and time
// ranges listed in validate.go (exactly those; nothing else is checked).
// Unknown members are ignored, never refused (spec 02 section 1: within a
// major, unknown fields are ignored). Invalid JSON, a member of the wrong
// JSON type, more than MaxMessageBytes or a failed check is refused with a
// *core.FieldError naming the member. It never panics. The special values
// are not converted here: read them through SpeedMS, TrackDeg, AltHAEM and
// the other accessors.
func UnmarshalRIDFlight(data []byte) (*RIDFlight, error) {
	var f RIDFlight
	if err := unmarshal(data, "flight", &f); err != nil {
		return nil, err
	}
	c := &checker{}
	c.flight("flight", &f)
	if c.err != nil {
		return nil, c.err
	}
	return &f, nil
}

// UnmarshalGetFlightsResponse reads a GET /uss/flights response with the
// rules of UnmarshalRIDFlight: its timestamp and every flight are checked.
func UnmarshalGetFlightsResponse(data []byte) (*GetFlightsResponse, error) {
	var r GetFlightsResponse
	if err := unmarshal(data, "response", &r); err != nil {
		return nil, err
	}
	c := &checker{}
	ts := r.Timestamp
	c.time("response.timestamp", &ts)
	if r.Flights != nil {
		for i := range *r.Flights {
			c.flight(index("response.flights", i), &(*r.Flights)[i])
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return &r, nil
}

// index is the path of element i of the list at path.
func index(path string, i int) string { return path + "[" + strconv.Itoa(i) + "]" }

// unmarshal decodes data into v, naming the field on a type error.
func unmarshal(data []byte, root string, v any) error {
	if len(data) > MaxMessageBytes {
		return core.Fieldf(root, "is %d bytes; at most %d", len(data), MaxMessageBytes)
	}
	if err := json.Unmarshal(data, v); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) && te.Field != "" {
			return core.Fieldf(root+"."+te.Field, "must be %s, not a JSON %s", te.Type.String(), te.Value)
		}
		return core.Fieldf(root, "%s", err.Error())
	}
	return nil
}
