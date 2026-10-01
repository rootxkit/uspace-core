package f3548

import (
	"encoding/json"
	"errors"

	"github.com/rootxkit/uspace-core/core"
)

// MaxMessageBytes bounds one F3548 message read by the Unmarshal helpers
// (E-10). An operational intent of OiMaxVertices vertices in one volume
// is about 600 kB; 4 MiB leaves room for several such volumes.
const MaxMessageBytes = 4 << 20

// UnmarshalOperationalIntent reads one OperationalIntent (reference and
// details, the body of GET /uss/v1/operational_intents/{entityid}) from
// untrusted bytes and checks the members a judgement rests on: required
// presence, the state and availability enumerations, the time order, and
// each volume's outline and altitude ranges, as listed in validate.go
// (exactly those; nothing else is checked). Unknown members are ignored,
// never refused (spec 02 section 1). Invalid JSON, a member of the wrong
// JSON type, more than MaxMessageBytes or a failed check is refused with a
// *core.FieldError naming the member. It never panics.
func UnmarshalOperationalIntent(data []byte) (*OperationalIntent, error) {
	var oi OperationalIntent
	if err := unmarshal(data, "operational_intent", &oi); err != nil {
		return nil, err
	}
	c := &checker{}
	c.intent("operational_intent", &oi)
	if c.err != nil {
		return nil, c.err
	}
	return &oi, nil
}

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
