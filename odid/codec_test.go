package odid

import (
	"bytes"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustEncode(t *testing.T, m Message) [MessageSize]byte {
	t.Helper()
	b, err := Encode(m)
	if err != nil {
		t.Fatalf("encode %T: %v", m, err)
	}
	return b
}

// wantFieldError requires a *core.FieldError naming field whose reason
// contains phrase.
func wantFieldError(t *testing.T, err error, field, phrase string) {
	t.Helper()
	var fe *core.FieldError
	if !errors.As(err, &fe) {
		t.Fatalf("error %v (%T), want a *core.FieldError on %q", err, err, field)
	}
	if fe.Field != field {
		t.Errorf("field %q, want %q (reason %q)", fe.Field, field, fe.Reason)
	}
	if !strings.Contains(fe.Reason, phrase) {
		t.Errorf("reason %q does not contain %q", fe.Reason, phrase)
	}
}

const refLocation = "1217b5ff7e1b72d7f41716b3fe0000000000001212ffff0000"

func pack(t *testing.T, msgs ...[MessageSize]byte) []byte {
	t.Helper()
	out := []byte{0xF2, MessageSize, byte(len(msgs))}
	for _, m := range msgs {
		out = append(out, m[:]...)
	}
	return out
}

func TestTypeOf(t *testing.T) {
	_, err := TypeOf(nil)
	wantFieldError(t, err, "frame", "empty")
	for _, c := range []struct {
		b    byte
		want MessageType
	}{{0x02, TypeBasicID}, {0x12, TypeLocation}, {0x22, TypeAuthentication}, {0x32, TypeSelfID}, {0x42, TypeSystem}, {0x52, TypeOperatorID}, {0x92, 9}, {0xF2, TypeMessagePack}} {
		got, err := TypeOf([]byte{c.b})
		if err != nil || got != c.want {
			t.Errorf("TypeOf(%#02x) = %d, %v; want %d", c.b, got, err, c.want)
		}
	}
}

// E-01: each lone-message refusal next to the frame that is accepted.
func TestDecodeLoneMessageLength(t *testing.T) {
	full := mustHex(t, refLocation)
	if ms, err := Decode(full, DecodeOptions{}); err != nil || len(ms) != 1 {
		t.Fatalf("25 bytes: %v, %d messages; want one Location", err, len(ms))
	}
	for _, n := range []int{1, 10, 24} {
		_, err := Decode(full[:n], DecodeOptions{})
		wantFieldError(t, err, "frame", strconv.Itoa(n)+" bytes, a message is 25")
	}
	_, err := Decode(append(full[:MessageSize:MessageSize], 0), DecodeOptions{})
	wantFieldError(t, err, "frame", "26 bytes, a message is 25")
}

func TestDecodePackRefusalsAndTwins(t *testing.T) {
	basic := mustEncode(t, BasicID{IDType: IDTypeSerial, UAID: "X"})
	loc := mustEncode(t, baseLocation())
	sys := mustEncode(t, baseSystem())
	op := mustEncode(t, OperatorID{OperatorID: "OP"})
	self := mustEncode(t, SelfID{Description: "survey"})
	auth := mustEncode(t, Authentication{AuthType: 1})
	var unknown [MessageSize]byte
	unknown[0] = 0x92

	accepted := map[string]struct {
		frame []byte
		n     int
	}{
		"two Basic IDs":           {pack(t, basic, basic, loc), 3},
		"one of each":             {pack(t, basic, loc, sys, op, self, auth), 4},
		"nine messages":           {pack(t, basic, basic, loc, sys, op, self, auth, auth, auth), 5},
		"one message":             {pack(t, loc), 1},
		"Self-ID and auth only":   {pack(t, self, auth), 0},
		"one System":              {pack(t, sys, loc), 2},
		"one Operator ID":         {pack(t, op, loc), 2},
		"one Self-ID":             {pack(t, self, loc), 1},
		"authentication repeated": {pack(t, auth, auth, auth), 0},
		"1 trailing byte":         {append(pack(t, basic, loc), 0), 2},
		"24 trailing bytes":       {append(pack(t, basic, loc), make([]byte, 24)...), 2},
		"a trailing message":      {append(pack(t, loc), loc[:]...), 1},
		"undefined types skipped": {pack(t, unknown, loc, unknown), 1},
	}
	for name, c := range accepted {
		ms, err := Decode(c.frame, DecodeOptions{})
		if err != nil || len(ms) != c.n {
			t.Errorf("%s: %v, %d messages; want %d", name, err, len(ms), c.n)
		}
	}

	refused := []struct {
		name, field, phrase string
		frame               []byte
	}{
		{"one byte", "frame", "1 bytes, a pack header is 3", []byte{0xF2}},
		{"two bytes", "frame", "2 bytes, a pack header is 3", []byte{0xF2, 0x19}},
		{"message size 26", "frame", "pack message size 26, expected 25", append([]byte{0xF2, 26, 1}, loc[:]...)},
		{"shorter by one byte", "frame", "pack shorter than it says", pack(t, basic, loc)[:52]},
		{"three Basic IDs", "pack[2]", "too many Basic ID messages in a pack", pack(t, basic, basic, basic)},
		{"two Systems", "pack[1]", "more than one SYSTEM message in a pack", pack(t, sys, sys)},
		{"two Self-IDs", "pack[1]", "more than one SELF_ID message in a pack", pack(t, self, self)},
		{"two Operator IDs", "pack[2]", "more than one OPERATOR_ID message in a pack", pack(t, op, loc, op)},
		{"two Locations", "pack[1]", "more than one LOCATION message in a pack", pack(t, loc, loc)},
	}
	for _, c := range refused {
		ms, err := Decode(c.frame, DecodeOptions{KeepSkipped: true})
		if ms != nil {
			t.Errorf("%s: a refusal returned %d messages", c.name, len(ms))
		}
		t.Run(c.name, func(t *testing.T) { wantFieldError(t, err, c.field, c.phrase) })
	}
}

// An undefined type inside a pack is skipped, not refused, and comes back
// as Unknown with KeepSkipped; the known messages around it decode.
func TestPackUndefinedTypeSkipped(t *testing.T) {
	loc := mustEncode(t, baseLocation())
	var unknown [MessageSize]byte
	unknown[0], unknown[5] = 0x92, 0xAB
	frame := pack(t, loc, unknown)

	ms, err := Decode(frame, DecodeOptions{})
	if err != nil || len(ms) != 1 {
		t.Fatalf("without KeepSkipped: %v, %d messages; want the Location alone", err, len(ms))
	}
	if _, ok := ms[0].(Location); !ok {
		t.Errorf("got %T, want Location", ms[0])
	}

	ms, err = Decode(frame, DecodeOptions{KeepSkipped: true})
	if err != nil || len(ms) != 2 {
		t.Fatalf("with KeepSkipped: %v, %d messages; want Location and Unknown", err, len(ms))
	}
	u, ok := ms[1].(Unknown)
	if !ok || u.Type() != 9 || u.Raw != unknown {
		t.Fatalf("second message %#v, want Unknown type 9 with its bytes", ms[1])
	}
	// The encoder accepts it back into a pack, byte for byte.
	enc, err := EncodePack(ms)
	if err != nil || !bytes.Equal(enc, frame) {
		t.Errorf("EncodePack: %v, got %x, want %x", err, enc, frame)
	}
}

func TestDecodeMessage(t *testing.T) {
	var b [MessageSize]byte
	copy(b[:], mustHex(t, refLocation))
	m, err := DecodeMessage(b, DecodeOptions{})
	if _, ok := m.(Location); !ok || err != nil {
		t.Fatalf("Location frame: %T, %v", m, err)
	}

	b[0] = 0xF2
	_, err = DecodeMessage(b, DecodeOptions{})
	wantFieldError(t, err, "frame", "a message pack is not a single message")

	// R-04: skipped types decode to nil, nil; KeepSkipped returns them.
	for _, hdr := range []byte{0x22, 0x32, 0x62, 0x92, 0xE2} {
		b = [MessageSize]byte{hdr, 0x13, 'a', 'b'}
		m, err := DecodeMessage(b, DecodeOptions{})
		if m != nil || err != nil {
			t.Errorf("%#02x skipped: %#v, %v; want nil, nil", hdr, m, err)
		}
		m, err = DecodeMessage(b, DecodeOptions{KeepSkipped: true})
		if m == nil || err != nil || m.Type() != typeNibble(hdr) {
			t.Fatalf("%#02x kept: %#v, %v", hdr, m, err)
		}
	}
}

func TestKeepSkippedValues(t *testing.T) {
	frame := make([]byte, MessageSize)
	frame[0], frame[1] = 0x32, 7
	copy(frame[2:], "Survey flight\x00")
	ms, err := Decode(frame, DecodeOptions{KeepSkipped: true})
	if err != nil || len(ms) != 1 {
		t.Fatal(err, len(ms))
	}
	if s, ok := ms[0].(SelfID); !ok || s.DescriptionType != 7 || s.Description != "Survey flight" {
		t.Errorf("Self-ID %#v", ms[0])
	}
	if ms, err := Decode(frame, DecodeOptions{}); err != nil || ms != nil {
		t.Errorf("Self-ID without KeepSkipped: %v, %v", ms, err)
	}

	frame[0], frame[1] = 0x22, 0x31 // auth type 3, page 1
	for i := 2; i < MessageSize; i++ {
		frame[i] = byte(i)
	}
	ms, _ = Decode(frame, DecodeOptions{KeepSkipped: true})
	a, ok := ms[0].(Authentication)
	if !ok || a.AuthType != 3 || a.PageNumber != 1 || !bytes.Equal(a.Raw, frame[2:]) {
		t.Fatalf("Authentication %#v", ms[0])
	}
	// The raw page is a copy: the caller's buffer may be reused (R-15).
	frame[2] = 0xEE
	if a.Raw[0] != 2 {
		t.Error("Authentication.Raw aliases the input frame")
	}

	frame[0] = 0x72
	ms, _ = Decode(frame, DecodeOptions{KeepSkipped: true})
	if u, ok := ms[0].(Unknown); !ok || u.Type() != 7 || !bytes.Equal(u.Raw[:], frame) {
		t.Errorf("Unknown %#v", ms[0])
	}
}

// TestStringsKeepCaseAndInnerBytes: only the trailing NUL padding goes.
func TestStringsKeepCaseAndInnerBytes(t *testing.T) {
	for _, id := range []string{"aBc", "x\x00y", "12345678901234567890", ""} {
		b := mustEncode(t, BasicID{IDType: IDTypeSerial, UAID: id})
		m, _ := DecodeMessage(b, DecodeOptions{})
		if got := m.(BasicID).UAID; got != id {
			t.Errorf("ua_id %q decoded as %q", id, got)
		}
		b = mustEncode(t, OperatorID{OperatorID: id})
		m, _ = DecodeMessage(b, DecodeOptions{})
		if got := m.(OperatorID).OperatorID; got != id {
			t.Errorf("operator_id %q decoded as %q", id, got)
		}
	}
}

func TestDecodeSentinelsAndPresence(t *testing.T) {
	// E-01: each sentinel decodes to nil; the value next to it does not.
	base := mustHex(t, "12205a33fdda128718134dc21a0000410df707000000000000")
	cases := []struct {
		name  string
		set   func(b []byte)
		field func(Location) *float64
		want  *float64
	}{
		{"direction 361", func(b []byte) { b[1] |= locFlagEWDirection; b[2] = 181 }, func(l Location) *float64 { return l.DirectionDeg }, nil},
		{"direction 360 is unknown", func(b []byte) { b[1] |= locFlagEWDirection; b[2] = 180 }, func(l Location) *float64 { return l.DirectionDeg }, nil},
		{"direction 435 is unknown", func(b []byte) { b[1] |= locFlagEWDirection; b[2] = 255 }, func(l Location) *float64 { return l.DirectionDeg }, nil},
		{"direction 359", func(b []byte) { b[1] |= locFlagEWDirection; b[2] = 179 }, func(l Location) *float64 { return l.DirectionDeg }, ptr(359)},
		{"direction 181 without the bit", func(b []byte) { b[2] = 181 }, func(l Location) *float64 { return l.DirectionDeg }, ptr(181)},
		{"speed 255 high", func(b []byte) { b[1] |= locFlagSpeedMult; b[3] = 255 }, func(l Location) *float64 { return l.SpeedHorizontalMS }, nil},
		{"speed 254 high", func(b []byte) { b[1] |= locFlagSpeedMult; b[3] = 254 }, func(l Location) *float64 { return l.SpeedHorizontalMS }, ptr(254.25)},
		{"speed 255 low", func(b []byte) { b[3] = 255 }, func(l Location) *float64 { return l.SpeedHorizontalMS }, ptr(63.75)},
		{"vertical 63", func(b []byte) { b[4] = 126 }, func(l Location) *float64 { return l.SpeedVerticalMS }, nil},
		{"vertical 63.5", func(b []byte) { b[4] = 127 }, func(l Location) *float64 { return l.SpeedVerticalMS }, ptr(63.5)},
		{"vertical -64", func(b []byte) { b[4] = 0x80 }, func(l Location) *float64 { return l.SpeedVerticalMS }, ptr(-64)},
		{"baro -1000", func(b []byte) { b[13], b[14] = 0, 0 }, func(l Location) *float64 { return l.AltBaroM }, nil},
		{"baro -999.5", func(b []byte) { b[13], b[14] = 1, 0 }, func(l Location) *float64 { return l.AltBaroM }, ptr(-999.5)},
		{"height -1000", func(b []byte) { b[17], b[18] = 0, 0 }, func(l Location) *float64 { return l.HeightM }, nil},
		{"geo top", func(b []byte) { b[15], b[16] = 0xFF, 0xFF }, func(l Location) *float64 { return l.AltHAEM }, ptr(31767.5)},
		{"timestamp 0xFFFF", func(b []byte) { b[21], b[22] = 0xFF, 0xFF }, func(l Location) *float64 { return l.SecondsAfterHour }, nil},
		{"timestamp 0xFFFE", func(b []byte) { b[21], b[22] = 0xFE, 0xFF }, func(l Location) *float64 { return l.SecondsAfterHour }, ptr(6553.4)},
		{"longitude 0 alone", func(b []byte) { copy(b[9:13], []byte{0, 0, 0, 0}) }, func(l Location) *float64 { return l.LonDeg }, ptr(0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := bytes.Clone(base)
			c.set(b)
			ms, err := Decode(b, DecodeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			got := c.field(ms[0].(Location))
			switch {
			case c.want == nil && got != nil:
				t.Errorf("got %v, want nil", *got)
			case c.want != nil && got == nil:
				t.Errorf("got nil, want %v", *c.want)
			case c.want != nil && math.Abs(*got-*c.want) > 1e-9:
				t.Errorf("got %v, want %v", *got, *c.want)
			}
		})
	}
}

func TestSystemOperatorPositionUnknown(t *testing.T) {
	s := baseSystem()
	s.OperatorLatDeg, s.OperatorLonDeg = nil, nil
	m, _ := DecodeMessage(mustEncode(t, s), DecodeOptions{})
	if got := m.(System); got.OperatorLatDeg != nil || got.OperatorLonDeg != nil {
		t.Errorf("operator 0,0 decoded as %v, %v", got.OperatorLatDeg, got.OperatorLonDeg)
	}
	s.OperatorLatDeg, s.OperatorLonDeg = ptr(0), ptr(10)
	m, _ = DecodeMessage(mustEncode(t, s), DecodeOptions{})
	if got := m.(System); got.OperatorLatDeg == nil || *got.OperatorLatDeg != 0 || *got.OperatorLonDeg != 10 {
		t.Errorf("operator 0,10 decoded as %v, %v", got.OperatorLatDeg, got.OperatorLonDeg)
	}
}

// TestEncodeQuantisation pins the encoder's rounding and range choices
// against the raw bytes, each refusal beside the value next to it.
func TestEncodeQuantisation(t *testing.T) {
	enc := func(change func(*Location)) ([MessageSize]byte, error) {
		l := baseLocation()
		change(&l)
		return Encode(l)
	}
	type raw struct{ off, val, flagMask, flags byte }
	ok := []struct {
		name   string
		change func(*Location)
		want   raw
	}{
		{"359.6 is north", func(l *Location) { l.DirectionDeg = ptr(359.6) }, raw{2, 0, locFlagEWDirection, 0}},
		{"360.4 is north", func(l *Location) { l.DirectionDeg = ptr(360.4) }, raw{2, 0, locFlagEWDirection, 0}},
		{"179.5 rounds to 180", func(l *Location) { l.DirectionDeg = ptr(179.5) }, raw{2, 0, locFlagEWDirection, locFlagEWDirection}},
		{"179.4 stays east", func(l *Location) { l.DirectionDeg = ptr(179.4) }, raw{2, 179, locFlagEWDirection, 0}},
		{"-0.4 is north", func(l *Location) { l.DirectionDeg = ptr(-0.4) }, raw{2, 0, locFlagEWDirection, 0}},
		{"nil direction", func(l *Location) { l.DirectionDeg = nil }, raw{2, 181, locFlagEWDirection, locFlagEWDirection}},
		{"63.5 m/s low", func(l *Location) { l.SpeedHorizontalMS = ptr(63.5) }, raw{3, 254, locFlagSpeedMult, 0}},
		{"63.75 m/s high", func(l *Location) { l.SpeedHorizontalMS = ptr(63.75) }, raw{3, 0, locFlagSpeedMult, locFlagSpeedMult}},
		{"63.7 m/s high", func(l *Location) { l.SpeedHorizontalMS = ptr(63.7) }, raw{3, 0, locFlagSpeedMult, locFlagSpeedMult}},
		{"254.25 m/s", func(l *Location) { l.SpeedHorizontalMS = ptr(254.25) }, raw{3, 254, locFlagSpeedMult, locFlagSpeedMult}},
		{"nil speed", func(l *Location) { l.SpeedHorizontalMS = nil }, raw{3, 255, locFlagSpeedMult, locFlagSpeedMult}},
		{"0 m/s", func(l *Location) { l.SpeedHorizontalMS = ptr(0) }, raw{3, 0, locFlagSpeedMult, 0}},
		{"nil vertical", func(l *Location) { l.SpeedVerticalMS = nil }, raw{4, 126, 0, 0}},
		{"vertical 63.5", func(l *Location) { l.SpeedVerticalMS = ptr(63.5) }, raw{4, 127, 0, 0}},
		{"vertical -64", func(l *Location) { l.SpeedVerticalMS = ptr(-64) }, raw{4, 0x80, 0, 0}},
		{"altitude -999.5", func(l *Location) { l.AltBaroM = ptr(-999.5) }, raw{13, 1, 0, 0}},
		{"nil altitude", func(l *Location) { l.AltBaroM = nil }, raw{13, 0, 0, 0}},
		{"timestamp 0.05 rounds up", func(l *Location) { l.SecondsAfterHour = ptr(0.05) }, raw{21, 1, 0, 0}},
		{"nil timestamp", func(l *Location) { l.SecondsAfterHour = nil }, raw{21, 0xFF, 0, 0}},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			b, err := enc(c.change)
			if err != nil {
				t.Fatal(err)
			}
			if b[c.want.off] != c.want.val || b[offLocFlags]&c.want.flagMask != c.want.flags {
				t.Errorf("byte %d = %d, flags %08b; want %d, flags %08b under %08b",
					c.want.off, b[c.want.off], b[offLocFlags], c.want.val, c.want.flags, c.want.flagMask)
			}
		})
	}

	refused := []struct {
		name, field, phrase string
		change              func(*Location)
	}{
		{"direction 360.6", "direction_deg", "outside 0 to 360", func(l *Location) { l.DirectionDeg = ptr(360.6) }},
		{"direction 435", "direction_deg", "outside 0 to 360", func(l *Location) { l.DirectionDeg = ptr(435) }},
		{"direction 435.5", "direction_deg", "outside 0 to 360", func(l *Location) { l.DirectionDeg = ptr(435.5) }},
		{"direction -0.5", "direction_deg", "outside 0 to 360", func(l *Location) { l.DirectionDeg = ptr(-0.5) }},
		{"direction NaN", "direction_deg", "not a finite number", func(l *Location) { l.DirectionDeg = ptr(math.NaN()) }},
		{"speed 254.7", "speed_horizontal_ms", "the unknown speed", func(l *Location) { l.SpeedHorizontalMS = ptr(254.7) }},
		{"speed -0.1", "speed_horizontal_ms", "negative", func(l *Location) { l.SpeedHorizontalMS = ptr(-0.1) }},
		{"speed +Inf", "speed_horizontal_ms", "not a finite number", func(l *Location) { l.SpeedHorizontalMS = ptr(math.Inf(1)) }},
		{"vertical 63", "speed_vertical_ms", "the unknown speed", func(l *Location) { l.SpeedVerticalMS = ptr(63) }},
		{"vertical 64", "speed_vertical_ms", "outside -64 to 63.5", func(l *Location) { l.SpeedVerticalMS = ptr(64) }},
		{"vertical -64.5", "speed_vertical_ms", "outside -64 to 63.5", func(l *Location) { l.SpeedVerticalMS = ptr(-64.5) }},
		{"vertical NaN", "speed_vertical_ms", "not a finite", func(l *Location) { l.SpeedVerticalMS = ptr(math.NaN()) }},
		{"latitude alone", "lon_deg", "nil while lat_deg is set", func(l *Location) { l.LonDeg = nil }},
		{"longitude alone", "lat_deg", "nil while lon_deg is set", func(l *Location) { l.LatDeg = nil }},
		{"0, 0", "lat_deg", "unknown position", func(l *Location) { l.LatDeg, l.LonDeg = ptr(0), ptr(0) }},
		{"latitude past int32", "lat_deg", "does not fit", func(l *Location) { l.LatDeg = ptr(215) }},
		{"longitude past int32", "lon_deg", "does not fit", func(l *Location) { l.LonDeg = ptr(-215) }},
		{"latitude NaN", "lat_deg", "not a finite", func(l *Location) { l.LatDeg = ptr(math.NaN()) }},
		{"baro -1000", "alt_baro_m", "unknown altitude", func(l *Location) { l.AltBaroM = ptr(-1000) }},
		{"geo below", "alt_hae_m", "outside -1000 to 31767.5", func(l *Location) { l.AltHAEM = ptr(-1001) }},
		{"height above", "height_m", "outside -1000 to 31767.5", func(l *Location) { l.HeightM = ptr(31768) }},
		{"height NaN", "height_m", "not a finite", func(l *Location) { l.HeightM = ptr(math.NaN()) }},
		{"timestamp 6553.5", "seconds_after_hour", "unknown timestamp", func(l *Location) { l.SecondsAfterHour = ptr(6553.5) }},
		{"timestamp 6553.6", "seconds_after_hour", "outside 0 to 6553.4", func(l *Location) { l.SecondsAfterHour = ptr(6553.6) }},
		{"timestamp -0.1", "seconds_after_hour", "outside 0 to 6553.4", func(l *Location) { l.SecondsAfterHour = ptr(-0.1) }},
		{"timestamp NaN", "seconds_after_hour", "not a finite", func(l *Location) { l.SecondsAfterHour = ptr(math.NaN()) }},
		{"status 16", "status", "4 bits", func(l *Location) { l.Status = 16 }},
		{"height reference 2", "height_reference", "1 bit", func(l *Location) { l.HeightReference = 2 }},
		{"horizontal accuracy 16", "horiz_accuracy", "4 bits", func(l *Location) { l.HorizAccuracy = 16 }},
		{"timestamp accuracy 16", "ts_accuracy", "4 bits", func(l *Location) { l.TSAccuracy = 16 }},
	}
	for _, c := range refused {
		t.Run("refuse "+c.name, func(t *testing.T) {
			_, err := enc(c.change)
			wantFieldError(t, err, c.field, c.phrase)
		})
	}
	// Twins at the edges: the values just inside are accepted.
	for _, change := range []func(*Location){
		func(l *Location) { l.LatDeg, l.LonDeg = ptr(0), ptr(10) },
		func(l *Location) { l.LatDeg, l.LonDeg = ptr(214.7), ptr(-214.7) },
		func(l *Location) { l.LatDeg, l.LonDeg = nil, nil },
		func(l *Location) { l.HeightM = ptr(31767.5) },
		func(l *Location) { l.SecondsAfterHour = ptr(6553.4) },
		func(l *Location) { l.Status, l.HeightReference, l.HorizAccuracy, l.TSAccuracy = 15, 1, 15, 15 },
	} {
		if _, err := enc(change); err != nil {
			t.Errorf("edge value refused: %v", err)
		}
	}
}

func TestEncodeSystemRefusals(t *testing.T) {
	for _, c := range []struct {
		name, field, phrase string
		change              func(*System)
	}{
		{"location type 4", "operator_location_type", "2 bits", func(s *System) { s.OperatorLocationType = 4 }},
		{"classification 8", "classification_type", "3 bits", func(s *System) { s.ClassificationType = 8 }},
		{"category 16", "category_eu", "4 bits", func(s *System) { s.CategoryEU = 16 }},
		{"class 16", "class_eu", "4 bits", func(s *System) { s.ClassEU = 16 }},
		{"radius 2560", "area_radius_m", "exceeds 2550", func(s *System) { s.AreaRadiusM = 2560 }},
		{"operator half known", "operator_lon_deg", "nil while", func(s *System) { s.OperatorLonDeg = nil }},
		{"ceiling -1000", "area_ceiling_m", "unknown altitude", func(s *System) { s.AreaCeilingM = ptr(-1000) }},
		{"floor below", "area_floor_m", "outside", func(s *System) { s.AreaFloorM = ptr(-2000) }},
		{"operator altitude NaN", "operator_alt_hae_m", "not a finite", func(s *System) { s.OperatorAltHAEM = ptr(math.NaN()) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := baseSystem()
			c.change(&s)
			_, err := Encode(s)
			wantFieldError(t, err, c.field, c.phrase)
		})
	}
	s := baseSystem()
	s.OperatorLocationType, s.ClassificationType, s.CategoryEU, s.ClassEU = 3, 7, 15, 15
	s.AreaRadiusM, s.TimestampS = 2549, math.MaxUint32
	s.AreaCeilingM, s.AreaFloorM, s.OperatorAltHAEM = nil, nil, nil
	b := mustEncode(t, s)
	m, _ := DecodeMessage(b, DecodeOptions{})
	got := m.(System)
	// The radius is truncated to tens of metres, as in encodeAreaRadius.
	if got.AreaRadiusM != 2540 || got.TimestampS != math.MaxUint32 || got.AreaCeilingM != nil {
		t.Errorf("edge System decoded as %+v", got)
	}
}

func TestEncodeOtherRefusals(t *testing.T) {
	long := strings.Repeat("A", IDSize+1)
	for _, c := range []struct {
		name, field, phrase string
		m                   Message
	}{
		{"ua_id 21 bytes", "ua_id", "21 bytes, the field holds 20", BasicID{UAID: long}},
		{"ua_id trailing NUL", "ua_id", "ends in NUL", BasicID{UAID: "A\x00"}},
		{"id_type 16", "id_type", "4 bits", BasicID{IDType: 16}},
		{"ua_type 16", "ua_type", "4 bits", BasicID{UAType: 16}},
		{"operator_id 21 bytes", "operator_id", "21 bytes", OperatorID{OperatorID: long}},
		{"description 24 bytes", "description", "24 bytes, the field holds 23", SelfID{Description: strings.Repeat("d", 24)}},
		{"auth raw 24 bytes", "raw", "24 bytes, a page holds 23", Authentication{Raw: make([]byte, 24)}},
		{"auth type 16", "auth_type", "4 bits", Authentication{AuthType: 16}},
		{"auth page 16", "page_number", "4 bits", Authentication{PageNumber: 16}},
		{"unknown with a known type", "raw", "has its own message struct", Unknown{Raw: [MessageSize]byte{0x12}}},
		{"unknown pack", "raw", "a message pack is not a single message", Unknown{Raw: [MessageSize]byte{0xF2}}},
		{"nil", "message", "nil", nil},
		{"foreign type", "message", "is not an ODID message", foreign{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := Encode(c.m)
			wantFieldError(t, err, c.field, c.phrase)
		})
	}
	// Twins.
	for _, m := range []Message{
		BasicID{IDType: 15, UAType: 15, UAID: long[:IDSize]},
		OperatorID{OperatorIDType: 255, OperatorID: long[:IDSize]},
		SelfID{Description: strings.Repeat("d", StrSize)},
		Authentication{AuthType: 15, PageNumber: 15, Raw: make([]byte, AuthDataSize)},
		Unknown{Raw: [MessageSize]byte{0x62}},
	} {
		if _, err := Encode(m); err != nil {
			t.Errorf("%T at its edge refused: %v", m, err)
		}
	}
}

type foreign struct{}

// Type claims a Location so that only Encode, not the pack check, refuses it.
func (foreign) Type() MessageType { return TypeLocation }

func TestEncodePackRefusals(t *testing.T) {
	loc := baseLocation()
	basic := BasicID{UAID: "X"}
	nine := []Message{basic, basic, loc, baseSystem(), OperatorID{}, SelfID{}, Authentication{}, Authentication{}, Authentication{}}
	if _, err := EncodePack(nine); err != nil {
		t.Fatalf("nine messages: %v", err)
	}
	bad := loc
	bad.DirectionDeg = ptr(361)
	for _, c := range []struct {
		name, field, phrase string
		ms                  []Message
	}{
		{"empty", "pack", "pack of 0 messages", nil},
		{"ten", "pack", "pack of 10 messages", append(nine[:9:9], Authentication{})},
		{"nested", "pack[1]", "a pack inside a pack", []Message{loc, Unknown{Raw: [MessageSize]byte{0xF2}}}},
		{"three Basic IDs", "pack[2]", "too many Basic ID messages in a pack", []Message{basic, basic, basic}},
		{"two Locations", "pack[1]", "more than one LOCATION message in a pack", []Message{loc, loc}},
		{"nil message", "pack[1]", "nil", []Message{loc, nil}},
		{"a message refused", "pack[1].direction_deg", "outside 0 to 360", []Message{basic, bad}},
		{"a foreign message", "pack[0].message", "is not an ODID message", []Message{foreign{}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := EncodePack(c.ms)
			if out != nil {
				t.Errorf("refusal returned %d bytes", len(out))
			}
			wantFieldError(t, err, c.field, c.phrase)
		})
	}
}

// TestEncodePackDecodes: what EncodePack builds, Decode accepts.
func TestEncodePackDecodes(t *testing.T) {
	in := []Message{BasicID{IDType: IDTypeSerial, UAID: "1581F4XFC233L00B00A9"}, baseLocation(), baseSystem(), OperatorID{OperatorID: "GEO-OP-1"}}
	b, err := EncodePack(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != PackHeaderSize+4*MessageSize {
		t.Fatalf("pack of %d bytes", len(b))
	}
	out, err := Decode(b, DecodeOptions{})
	if err != nil || len(out) != 4 {
		t.Fatalf("decode: %v, %d", err, len(out))
	}
	for i := range in {
		if a, b := mustEncode(t, in[i]), mustEncode(t, out[i]); a != b {
			t.Errorf("message %d changed through the pack", i)
		}
	}
}
