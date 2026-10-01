package odid

import (
	"bytes"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/vectors"
)

type odidInput struct {
	Hex         string `json:"hex"`
	EncodedFrom *struct {
		DirectionDeg float64 `json:"direction_deg"`
	} `json:"encoded_from"`
}

type odidExpected struct {
	Messages      []expMsg `json:"messages"`
	Error         *string  `json:"error"`
	ErrorContains string   `json:"error_contains"`
}

// expMsg is the union of every message type's fields. Integers and
// strings are pointers so that a field the type needs but the vector lacks
// fails instead of reading as zero; nullable floats are nil for null.
type expMsg struct {
	Type string `json:"type"`

	// basic_id
	IDType *uint8  `json:"id_type"`
	UAType *uint8  `json:"ua_type"`
	UAID   *string `json:"ua_id"`

	// location
	Status            *uint8   `json:"status"`
	DirectionDeg      *float64 `json:"direction_deg"`
	SpeedHorizontalMS *float64 `json:"speed_horizontal_ms"`
	SpeedVerticalMS   *float64 `json:"speed_vertical_ms"`
	LatDeg            *float64 `json:"lat_deg"`
	LonDeg            *float64 `json:"lon_deg"`
	AltBaroM          *float64 `json:"alt_baro_m"`
	AltHAEM           *float64 `json:"alt_hae_m"`
	HeightReference   *uint8   `json:"height_reference"`
	HeightM           *float64 `json:"height_m"`
	HorizAccuracy     *uint8   `json:"horiz_accuracy"`
	VertAccuracy      *uint8   `json:"vert_accuracy"`
	BaroAccuracy      *uint8   `json:"baro_accuracy"`
	SpeedAccuracy     *uint8   `json:"speed_accuracy"`
	TSAccuracy        *uint8   `json:"ts_accuracy"`
	SecondsAfterHour  *float64 `json:"seconds_after_hour"`

	// system
	OperatorLocationType *uint8   `json:"operator_location_type"`
	ClassificationType   *uint8   `json:"classification_type"`
	OperatorLatDeg       *float64 `json:"operator_lat_deg"`
	OperatorLonDeg       *float64 `json:"operator_lon_deg"`
	AreaCount            *uint16  `json:"area_count"`
	AreaRadiusM          *uint32  `json:"area_radius_m"`
	AreaCeilingM         *float64 `json:"area_ceiling_m"`
	AreaFloorM           *float64 `json:"area_floor_m"`
	CategoryEU           *uint8   `json:"category_eu"`
	ClassEU              *uint8   `json:"class_eu"`
	OperatorAltHAEM      *float64 `json:"operator_alt_hae_m"`
	TimestampS           *uint32  `json:"timestamp_s"`

	// operator_id
	OperatorIDType *uint8  `json:"operator_id_type"`
	OperatorID     *string `json:"operator_id"`
}

// TestVectorsOdidDecode runs odid_decode.json: every case decoded and
// compared field by field, every accepted frame re-encoded to the same
// bytes (R-02).
func TestVectorsOdidDecode(t *testing.T) {
	f := vectors.Load(t, "odid_decode.json")
	latLonTol, ok := f.FloatTolerance("lat_deg/lon_deg")
	if !ok {
		t.Fatal("no lat_deg/lon_deg tolerance in the header")
	}
	floatTol, ok := f.FloatTolerance("other floats")
	if !ok {
		t.Fatal("no 'other floats' tolerance in the header")
	}
	var order string
	f.Header(t, "byte_order", &order)
	if !strings.Contains(order, "little-endian") || !strings.Contains(order, "LSB first") {
		t.Fatalf("byte_order %q: the codec is written little-endian, bit fields LSB first", order)
	}

	passed := 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in odidInput
		var exp odidExpected
		c.Decode(t, &in, &exp)
		frame, err := hex.DecodeString(in.Hex)
		if err != nil {
			t.Fatalf("input.hex: %v", err)
		}
		got, err := Decode(frame, DecodeOptions{})
		if exp.Error != nil {
			if err == nil {
				t.Fatalf("decoded %d messages, want the error %q", len(got), *exp.Error)
			}
			if !strings.Contains(err.Error(), exp.ErrorContains) {
				t.Fatalf("error %q does not contain %q", err, exp.ErrorContains)
			}
			if !strings.Contains(err.Error(), *exp.Error) {
				t.Errorf("error %q does not contain the vector's full phrase %q", err, *exp.Error)
			}
			if got != nil {
				t.Errorf("refusal returned %d messages with the error", len(got))
			}
			passed++
			return
		}
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if exp.Messages == nil {
			t.Fatal("vector has neither messages nor error")
		}
		if len(got) != len(exp.Messages) {
			t.Fatalf("got %d messages, want %d: %#v", len(got), len(exp.Messages), got)
		}
		for i := range got {
			compareMessage(t, "messages["+strconv.Itoa(i)+"]", got[i], &exp.Messages[i], latLonTol, floatTol)
		}
		roundTrip(t, frame)
		if in.EncodedFrom != nil {
			loc, ok := got[0].(Location)
			if !ok {
				t.Fatalf("encoded_from on a %T", got[0])
			}
			d := in.EncodedFrom.DirectionDeg
			loc.DirectionDeg = &d
			enc, err := Encode(loc)
			if err != nil {
				t.Fatalf("encode direction %v: %v", d, err)
			}
			if !bytes.Equal(enc[:], frame) {
				t.Errorf("direction %v encodes to\n%x, want\n%x", d, enc, frame)
			}
		}
		if !t.Failed() {
			passed++
		}
	})
	if passed != len(f.Cases) {
		t.Errorf("%d/%d cases passed", passed, len(f.Cases))
	} else {
		t.Logf("%d/%d cases passed, round trips included", passed, len(f.Cases))
	}
}

// roundTrip re-encodes an accepted frame, skipped messages kept, and
// requires the same bytes (R-02).
func roundTrip(t *testing.T, frame []byte) {
	t.Helper()
	all, err := Decode(frame, DecodeOptions{KeepSkipped: true})
	if err != nil {
		t.Fatalf("decode with KeepSkipped: %v", err)
	}
	var enc []byte
	if typeNibble(frame[0]) == TypeMessagePack {
		enc, err = EncodePack(all)
	} else {
		if len(all) != 1 {
			t.Fatalf("a single frame kept %d messages", len(all))
		}
		var b [MessageSize]byte
		b, err = Encode(all[0])
		enc = b[:]
	}
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if !bytes.Equal(enc, frame) {
		t.Errorf("re-encoded\n%x, want\n%x", enc, frame)
	}
}

func compareMessage(t *testing.T, at string, got Message, want *expMsg, latLonTol, floatTol float64) {
	t.Helper()
	switch m := got.(type) {
	case BasicID:
		wantType(t, at, want, "basic_id")
		eqU8(t, at+".id_type", uint8(m.IDType), want.IDType)
		eqU8(t, at+".ua_type", m.UAType, want.UAType)
		eqStr(t, at+".ua_id", m.UAID, want.UAID)
	case Location:
		wantType(t, at, want, "location")
		eqU8(t, at+".status", uint8(m.Status), want.Status)
		vectors.NearPtr(t, at+".direction_deg", m.DirectionDeg, want.DirectionDeg, floatTol)
		vectors.NearPtr(t, at+".speed_horizontal_ms", m.SpeedHorizontalMS, want.SpeedHorizontalMS, floatTol)
		vectors.NearPtr(t, at+".speed_vertical_ms", m.SpeedVerticalMS, want.SpeedVerticalMS, floatTol)
		vectors.NearPtr(t, at+".lat_deg", m.LatDeg, want.LatDeg, latLonTol)
		vectors.NearPtr(t, at+".lon_deg", m.LonDeg, want.LonDeg, latLonTol)
		vectors.NearPtr(t, at+".alt_baro_m", m.AltBaroM, want.AltBaroM, floatTol)
		vectors.NearPtr(t, at+".alt_hae_m", m.AltHAEM, want.AltHAEM, floatTol)
		eqU8(t, at+".height_reference", uint8(m.HeightReference), want.HeightReference)
		vectors.NearPtr(t, at+".height_m", m.HeightM, want.HeightM, floatTol)
		eqU8(t, at+".horiz_accuracy", m.HorizAccuracy, want.HorizAccuracy)
		eqU8(t, at+".vert_accuracy", m.VertAccuracy, want.VertAccuracy)
		eqU8(t, at+".baro_accuracy", m.BaroAccuracy, want.BaroAccuracy)
		eqU8(t, at+".speed_accuracy", m.SpeedAccuracy, want.SpeedAccuracy)
		eqU8(t, at+".ts_accuracy", m.TSAccuracy, want.TSAccuracy)
		vectors.NearPtr(t, at+".seconds_after_hour", m.SecondsAfterHour, want.SecondsAfterHour, floatTol)
	case System:
		wantType(t, at, want, "system")
		eqU8(t, at+".operator_location_type", m.OperatorLocationType, want.OperatorLocationType)
		eqU8(t, at+".classification_type", m.ClassificationType, want.ClassificationType)
		vectors.NearPtr(t, at+".operator_lat_deg", m.OperatorLatDeg, want.OperatorLatDeg, latLonTol)
		vectors.NearPtr(t, at+".operator_lon_deg", m.OperatorLonDeg, want.OperatorLonDeg, latLonTol)
		if want.AreaCount == nil || m.AreaCount != *want.AreaCount {
			t.Errorf("%s.area_count: got %d, want %v", at, m.AreaCount, deref(want.AreaCount))
		}
		if want.AreaRadiusM == nil || m.AreaRadiusM != *want.AreaRadiusM {
			t.Errorf("%s.area_radius_m: got %d, want %v", at, m.AreaRadiusM, deref(want.AreaRadiusM))
		}
		vectors.NearPtr(t, at+".area_ceiling_m", m.AreaCeilingM, want.AreaCeilingM, floatTol)
		vectors.NearPtr(t, at+".area_floor_m", m.AreaFloorM, want.AreaFloorM, floatTol)
		eqU8(t, at+".category_eu", m.CategoryEU, want.CategoryEU)
		eqU8(t, at+".class_eu", m.ClassEU, want.ClassEU)
		vectors.NearPtr(t, at+".operator_alt_hae_m", m.OperatorAltHAEM, want.OperatorAltHAEM, floatTol)
		if want.TimestampS == nil || m.TimestampS != *want.TimestampS {
			t.Errorf("%s.timestamp_s: got %d, want %v", at, m.TimestampS, deref(want.TimestampS))
		}
	case OperatorID:
		wantType(t, at, want, "operator_id")
		eqU8(t, at+".operator_id_type", m.OperatorIDType, want.OperatorIDType)
		eqStr(t, at+".operator_id", m.OperatorID, want.OperatorID)
	default:
		t.Errorf("%s: decoded a %T, the vectors hold no such type", at, got)
	}
}

func wantType(t *testing.T, at string, want *expMsg, typ string) {
	t.Helper()
	if want.Type != typ {
		t.Errorf("%s.type: got %s, want %s", at, typ, want.Type)
	}
}

func eqU8(t *testing.T, field string, got uint8, want *uint8) {
	t.Helper()
	if want == nil {
		t.Errorf("%s: got %d, the vector has no value", field, got)
		return
	}
	if got != *want {
		t.Errorf("%s: got %d, want %d", field, got, *want)
	}
}

func eqStr(t *testing.T, field, got string, want *string) {
	t.Helper()
	if want == nil {
		t.Errorf("%s: got %q, the vector has no value", field, got)
		return
	}
	if got != *want {
		t.Errorf("%s: got %q, want %q", field, got, *want)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return "absent"
	}
	return *p
}
