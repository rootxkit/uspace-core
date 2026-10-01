package odid

import (
	"encoding/hex"
	"maps"
	"slices"
	"testing"
)

// LESSONS E-03: every offset is pinned by encoding two messages that
// differ in one field and asserting which bytes, and which bits of them,
// changed. A swapped or shifted field moves the diff and fails here even
// when the round trip of the reference frames would still agree.

func ptr(v float64) *float64 { return &v }

// authPage is an Authentication page of the one length the wire carries.
func authPage() Authentication { return Authentication{Raw: make([]byte, AuthDataSize)} }

func baseLocation() Location {
	return Location{
		Status:            StatusAirborne,
		DirectionDeg:      ptr(10),
		SpeedHorizontalMS: ptr(10),
		SpeedVerticalMS:   ptr(1),
		LatDeg:            ptr(41.7),
		LonDeg:            ptr(44.8),
		AltBaroM:          ptr(500),
		AltHAEM:           ptr(520),
		HeightReference:   HeightOverTakeoff,
		HeightM:           ptr(50),
		HorizAccuracy:     10,
		VertAccuracy:      4,
		BaroAccuracy:      3,
		SpeedAccuracy:     2,
		TSAccuracy:        1,
		SecondsAfterHour:  ptr(100),
	}
}

func baseSystem() System {
	return System{
		OperatorLocationType: 1,
		ClassificationType:   1,
		OperatorLatDeg:       ptr(41.7),
		OperatorLonDeg:       ptr(44.8),
		AreaCount:            1,
		AreaRadiusM:          100,
		AreaCeilingM:         ptr(120),
		AreaFloorM:           ptr(0),
		CategoryEU:           1,
		ClassEU:              2,
		OperatorAltHAEM:      ptr(450),
		TimestampS:           100,
	}
}

// diff returns, per byte offset that differs, the XOR of the two bytes.
func diff(t *testing.T, a, b Message) map[int]byte {
	t.Helper()
	ea, err := Encode(a)
	if err != nil {
		t.Fatalf("encode a: %v", err)
	}
	eb, err := Encode(b)
	if err != nil {
		t.Fatalf("encode b: %v", err)
	}
	d := map[int]byte{}
	for i := range ea {
		if x := ea[i] ^ eb[i]; x != 0 {
			d[i] = x
		}
	}
	return d
}

// assertDiff requires exactly the listed offsets to change. A mask of 0
// means "any bits of this byte"; otherwise the changed bits must lie
// within the mask.
func assertDiff(t *testing.T, got map[int]byte, want map[int]byte) {
	t.Helper()
	if !slices.Equal(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want))) {
		t.Fatalf("changed bytes %v, want %v", hexMap(got), hexMap(want))
	}
	for off, mask := range want {
		if mask != 0 && got[off]&^mask != 0 {
			t.Errorf("byte %d changed bits %08b, outside the field's bits %08b", off, got[off], mask)
		}
	}
}

func hexMap(m map[int]byte) map[int]string {
	out := make(map[int]string, len(m))
	for k, v := range m {
		out[k] = hex.EncodeToString([]byte{v})
	}
	return out
}

func TestLocationOffsetsByDifference(t *testing.T) {
	cases := []struct {
		name   string
		change func(*Location)
		want   map[int]byte
	}{
		{"status", func(l *Location) { l.Status = StatusEmergency }, map[int]byte{1: 0xF0}},
		{"direction within a half", func(l *Location) { l.DirectionDeg = ptr(11) }, map[int]byte{2: 0}},
		// 10 -> 190: the raw byte stays 10, only the east/west bit moves.
		{"direction east/west bit", func(l *Location) { l.DirectionDeg = ptr(190) }, map[int]byte{1: locFlagEWDirection}},
		{"speed within the low range", func(l *Location) { l.SpeedHorizontalMS = ptr(10.25) }, map[int]byte{3: 0}},
		// 10 m/s low (raw 40) -> 93.75 m/s high (raw 40): only the multiplier bit.
		{"speed multiplier", func(l *Location) { l.SpeedHorizontalMS = ptr(93.75) }, map[int]byte{1: locFlagSpeedMult}},
		{"vertical speed", func(l *Location) { l.SpeedVerticalMS = ptr(-1) }, map[int]byte{4: 0}},
		{"latitude", func(l *Location) { l.LatDeg = ptr(41.7 + 3*1e-7) }, map[int]byte{5: 0}},
		{"latitude high byte", func(l *Location) { l.LatDeg = ptr(-41.7) }, map[int]byte{5: 0, 6: 0, 7: 0, 8: 0}},
		{"longitude", func(l *Location) { l.LonDeg = ptr(44.8 + 3*1e-7) }, map[int]byte{9: 0}},
		// 448000000 and -448000000 share their low byte.
		{"longitude high bytes", func(l *Location) { l.LonDeg = ptr(-44.8) }, map[int]byte{10: 0, 11: 0, 12: 0}},
		{"baro altitude", func(l *Location) { l.AltBaroM = ptr(500.5) }, map[int]byte{13: 0}},
		{"geodetic altitude", func(l *Location) { l.AltHAEM = ptr(520.5) }, map[int]byte{15: 0}},
		{"height", func(l *Location) { l.HeightM = ptr(50.5) }, map[int]byte{17: 0}},
		{"height high byte", func(l *Location) { l.HeightM = ptr(50 + 128) }, map[int]byte{18: 0}},
		{"height type", func(l *Location) { l.HeightReference = HeightOverGround }, map[int]byte{1: locFlagHeightType}},
		{"horizontal accuracy", func(l *Location) { l.HorizAccuracy = 11 }, map[int]byte{19: 0x0F}},
		{"vertical accuracy", func(l *Location) { l.VertAccuracy = 5 }, map[int]byte{19: 0xF0}},
		{"speed accuracy", func(l *Location) { l.SpeedAccuracy = 3 }, map[int]byte{20: 0x0F}},
		{"baro accuracy", func(l *Location) { l.BaroAccuracy = 4 }, map[int]byte{20: 0xF0}},
		{"timestamp", func(l *Location) { l.SecondsAfterHour = ptr(100.1) }, map[int]byte{21: 0}},
		{"timestamp high byte", func(l *Location) { l.SecondsAfterHour = ptr(100 + 25.6) }, map[int]byte{22: 0}},
		{"timestamp accuracy", func(l *Location) { l.TSAccuracy = 2 }, map[int]byte{23: 0x0F}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := baseLocation(), baseLocation()
			c.change(&b)
			assertDiff(t, diff(t, a, b), c.want)
		})
	}
}

func TestSystemOffsetsByDifference(t *testing.T) {
	cases := []struct {
		name   string
		change func(*System)
		want   map[int]byte
	}{
		{"operator location type", func(s *System) { s.OperatorLocationType = 2 }, map[int]byte{1: 0x03}},
		{"classification type", func(s *System) { s.ClassificationType = 0 }, map[int]byte{1: 0x1C}},
		{"operator latitude", func(s *System) { s.OperatorLatDeg = ptr(-41.7) }, map[int]byte{2: 0, 3: 0, 4: 0, 5: 0}},
		{"operator longitude", func(s *System) { s.OperatorLonDeg = ptr(44.8 + 3*1e-7) }, map[int]byte{6: 0}},
		{"operator longitude high bytes", func(s *System) { s.OperatorLonDeg = ptr(-44.8) }, map[int]byte{7: 0, 8: 0, 9: 0}},
		{"area count", func(s *System) { s.AreaCount = 2 }, map[int]byte{10: 0}},
		{"area count high byte", func(s *System) { s.AreaCount = 257 }, map[int]byte{11: 0}},
		{"area radius", func(s *System) { s.AreaRadiusM = 110 }, map[int]byte{12: 0}},
		{"area ceiling", func(s *System) { s.AreaCeilingM = ptr(120.5) }, map[int]byte{13: 0}},
		{"area floor", func(s *System) { s.AreaFloorM = ptr(0.5) }, map[int]byte{15: 0}},
		{"class EU", func(s *System) { s.ClassEU = 3 }, map[int]byte{17: 0x0F}},
		{"category EU", func(s *System) { s.CategoryEU = 2 }, map[int]byte{17: 0xF0}},
		{"operator altitude", func(s *System) { s.OperatorAltHAEM = ptr(450.5) }, map[int]byte{18: 0}},
		{"timestamp", func(s *System) { s.TimestampS = 101 }, map[int]byte{20: 0}},
		{"timestamp top byte", func(s *System) { s.TimestampS = 100 + 1<<24 }, map[int]byte{23: 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a, b := baseSystem(), baseSystem()
			c.change(&b)
			assertDiff(t, diff(t, a, b), c.want)
		})
	}
}

func TestIDOffsetsByDifference(t *testing.T) {
	assertDiff(t, diff(t, BasicID{IDType: 1, UAType: 2, UAID: "A"}, BasicID{IDType: 2, UAType: 2, UAID: "A"}), map[int]byte{1: 0xF0})
	assertDiff(t, diff(t, BasicID{IDType: 1, UAType: 2, UAID: "A"}, BasicID{IDType: 1, UAType: 3, UAID: "A"}), map[int]byte{1: 0x0F})
	assertDiff(t, diff(t, BasicID{UAID: "A"}, BasicID{UAID: "B"}), map[int]byte{2: 0})
	assertDiff(t, diff(t, BasicID{UAID: "A"}, BasicID{UAID: "A2345678901234567890"}), map[int]byte{
		3: 0, 4: 0, 5: 0, 6: 0, 7: 0, 8: 0, 9: 0, 10: 0, 11: 0, 12: 0, 13: 0, 14: 0, 15: 0, 16: 0, 17: 0, 18: 0, 19: 0, 20: 0, 21: 0,
	})
	assertDiff(t, diff(t, OperatorID{OperatorIDType: 0, OperatorID: "A"}, OperatorID{OperatorIDType: 1, OperatorID: "A"}), map[int]byte{1: 0})
	assertDiff(t, diff(t, OperatorID{OperatorID: "A"}, OperatorID{OperatorID: "AAAAAAAAAAAAAAAAAAAB"}), map[int]byte{
		3: 0, 4: 0, 5: 0, 6: 0, 7: 0, 8: 0, 9: 0, 10: 0, 11: 0, 12: 0, 13: 0, 14: 0, 15: 0, 16: 0, 17: 0, 18: 0, 19: 0, 20: 0, 21: 0,
	})
}

// TestHeaderByte pins byte 0 as [type:4][version:4] for every type the
// encoder writes.
func TestHeaderByte(t *testing.T) {
	for _, c := range []struct {
		m    Message
		want byte
	}{
		{BasicID{}, 0x02}, {baseLocation(), 0x12}, {authPage(), 0x22},
		{SelfID{}, 0x32}, {baseSystem(), 0x42}, {OperatorID{}, 0x52},
	} {
		b, err := Encode(c.m)
		if err != nil {
			t.Fatalf("%T: %v", c.m, err)
		}
		if b[0] != c.want {
			t.Errorf("%T: byte 0 = %#02x, want %#02x", c.m, b[0], c.want)
		}
	}
	p, err := EncodePack([]Message{BasicID{}})
	if err != nil {
		t.Fatal(err)
	}
	if p[0] != 0xF2 || p[1] != MessageSize || p[2] != 1 {
		t.Errorf("pack header %x, want f2 19 01", p[:3])
	}
}

// TestReferenceFrameFields reads two Location fields straight from a
// reference frame (reference-pack-001, message 2) at the offsets the codec
// uses, so the constants and the vectors agree independently of Decode.
func TestReferenceFrameFields(t *testing.T) {
	frame, _ := hex.DecodeString("12205a33fdda128718134dc21a0000410df707000000000000")
	if frame[offLocDirection] != 90 || frame[offLocSpeedH] != 51 || int8(frame[offLocSpeedV]) != -3 {
		t.Errorf("direction/speed bytes %x", frame[offLocDirection:offLocSpeedV+1])
	}
	if lat := i32(frame[offLocLat:]); lat != 411505370 {
		t.Errorf("latitude raw %d, want 411505370 (41.150537 in the vector)", lat)
	}
	if lon := i32(frame[offLocLon:]); lon != 448941331 {
		t.Errorf("longitude raw %d, want 448941331 (44.8941331 in the vector)", lon)
	}
	if geo := u16(frame[offLocAltGeo:]); geo != 3393 {
		t.Errorf("geodetic altitude raw %d, want 3393 (696.5 m in the vector)", geo)
	}
	if h := u16(frame[offLocHeight:]); h != 2039 {
		t.Errorf("height raw %d, want 2039 (19.5 m in the vector)", h)
	}
}
