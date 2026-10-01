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
// untrusted bytes. Unknown members are ignored, never refused (spec 02
// section 1: within a major, unknown fields are ignored); a member of the
// wrong JSON type, invalid JSON or more than MaxMessageBytes is refused
// with a *core.FieldError. It never panics. Altitudes are checked when
// read (Altitude.HAEM), not here.
func UnmarshalOperationalIntent(data []byte) (*OperationalIntent, error) {
	var oi OperationalIntent
	if err := unmarshal(data, "operational_intent", &oi); err != nil {
		return nil, err
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
