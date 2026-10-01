package odid

import (
	"bytes"
	"encoding/hex"
	"math"
	"reflect"
	"testing"

	"github.com/rootxkit/uspace-core/vectors"
)

// seedFrames adds every input frame of odid_decode.json (refusals
// included) to the corpus.
func seedFrames(f *testing.F) {
	vf := vectors.Load(f, "odid_decode.json")
	for _, c := range vf.Cases {
		var in odidInput
		c.Decode(f, &in, nil)
		frame, err := hex.DecodeString(in.Hex)
		if err != nil {
			f.Fatalf("%s: %v", c.Name, err)
		}
		f.Add(frame)
	}
}

// FuzzDecode: Decode never panics on any frame; whatever it accepts
// re-encodes to messages that decode to the same values, and to the same
// bytes when the frame used only the canonical encodings (reserved bits
// zero, protocol version 2, see canonical).
func FuzzDecode(f *testing.F) {
	seedFrames(f)
	f.Fuzz(func(t *testing.T, frame []byte) {
		if _, err := Decode(frame, DecodeOptions{}); err != nil {
			if _, err2 := Decode(frame, DecodeOptions{KeepSkipped: true}); err2 == nil {
				t.Fatalf("refused only without KeepSkipped: %v", err)
			}
			return
		}
		all, err := Decode(frame, DecodeOptions{KeepSkipped: true})
		if err != nil {
			t.Fatalf("accepted only without KeepSkipped: %v", err)
		}
		var enc []byte
		if typeNibble(frame[0]) == TypeMessagePack {
			enc, err = EncodePack(all)
		} else {
			var b [MessageSize]byte
			b, err = Encode(all[0])
			enc = b[:]
		}
		if err != nil {
			t.Fatalf("decoded %#v but cannot re-encode it: %v", all, err)
		}
		again, err := Decode(enc, DecodeOptions{KeepSkipped: true})
		if err != nil {
			t.Fatalf("re-encoded frame refused: %v\n%x", err, enc)
		}
		if !reflect.DeepEqual(onCircle(all), onCircle(again)) {
			t.Fatalf("round trip changed the values:\n%#v\n%#v", all, again)
		}
		if canonical(frame) && !bytes.Equal(enc, frame) {
			t.Fatalf("canonical frame re-encoded differently:\n%x\n%x", frame, enc)
		}
	})
}

// onCircle folds every Location direction into [0, 360). The reference
// decoder (decodeDirection) turns raw 180 with the east/west bit into 360
// and raw 181-255 with it into 361-435 (361 aside); the encoder writes
// 360 as 0 (R-02), so those directions survive a round trip only as the
// same direction on the circle.
func onCircle(ms []Message) []Message {
	out := make([]Message, len(ms))
	for i, m := range ms {
		if l, ok := m.(Location); ok && l.DirectionDeg != nil {
			d := math.Mod(*l.DirectionDeg, 360)
			l.DirectionDeg = &d
			m = l
		}
		out[i] = m
	}
	return out
}

// canonical reports whether every message of an accepted frame uses the
// one encoding the encoder writes for its values: version 2 (the pack's
// and each message's), reserved
// bits and bytes zero, a direction of 180 or more carried with the
// east/west bit (and 360 as 0), a speed of 63.75 m/s in the high range.
func canonical(frame []byte) bool {
	if frame[0]&nibble != ProtocolVersion {
		return false
	}
	msgs := [][]byte{frame}
	if typeNibble(frame[0]) == TypeMessagePack {
		msgs = msgs[:0]
		for off := offPackMessages; off < len(frame); off += MessageSize {
			msgs = append(msgs, frame[off:off+MessageSize])
		}
	}
	for _, m := range msgs {
		if m[0]&nibble != ProtocolVersion {
			return false
		}
		switch typeNibble(m[0]) {
		case TypeBasicID, TypeOperatorID:
			if m[22]|m[23]|m[24] != 0 {
				return false
			}
		case TypeLocation:
			flags := m[offLocFlags]
			ew := flags&locFlagEWDirection != 0
			if flags&locFlagReserved != 0 || m[offLocTSAccuracy]>>4 != 0 || m[offLocReserved3] != 0 ||
				(!ew && m[offLocDirection] >= 180) || (ew && m[offLocDirection] == 180) ||
				(flags&locFlagSpeedMult == 0 && m[offLocSpeedH] == speedHRawUnknown) {
				return false
			}
		case TypeSystem:
			if m[offSysFlags]&sysReservedMask != 0 || m[offSysReserved2] != 0 {
				return false
			}
		case TypeAuthentication, TypeSelfID, TypeMessagePack:
		}
	}
	return true
}

// FuzzEncodeLocation: any Location inside the operational ranges encodes,
// and decodes back within one quantum of each field.
func FuzzEncodeLocation(f *testing.F) {
	f.Add(359.6, 12.75, -1.5, 41.150537, 44.8941331, 696.5, 19.5, 3599.9, uint8(2), true)
	f.Add(0.0, 0.0, 0.0, 0.0, 10.0, -999.5, 0.0, 0.0, uint8(0), false)
	f.Add(179.5, 63.7, 62.0, -90.0, -180.0, 31767.5, -500.0, 0.05, uint8(15), true)
	f.Add(-0.4, 254.25, -62.0, 90.0, 180.0, 0.0, 120.0, 3600.0, uint8(1), false)
	f.Fuzz(func(t *testing.T, dir, speed, vspeed, lat, lon, alt, height, ts float64, status uint8, overGround bool) {
		dir = math.Mod(math.Abs(clampF(dir, -1e6, 1e6)), 360)
		speed = math.Mod(math.Abs(clampF(speed, -1e6, 1e6)), 254.25)
		vspeed = clampF(vspeed, -62, 62)
		lat, lon = clampF(lat, -90, 90), clampF(lon, -180, 180)
		alt, height, ts = clampF(alt, -999.5, 31767.5), clampF(height, -999.5, 31767.5), clampF(ts, 0, 3600)
		if math.Round(lat*latLonScale) == 0 && math.Round(lon*latLonScale) == 0 {
			lon = 1 // 0, 0 is the unknown position, refused by design.
		}
		in := Location{
			Status: Status(status & nibble), DirectionDeg: &dir, SpeedHorizontalMS: &speed, SpeedVerticalMS: &vspeed,
			LatDeg: &lat, LonDeg: &lon, AltBaroM: &alt, AltHAEM: &alt, HeightM: &height, SecondsAfterHour: &ts,
		}
		if overGround {
			in.HeightReference = HeightOverGround
		}
		b, err := Encode(in)
		if err != nil {
			t.Fatalf("in-range Location refused: %v (%+v)", err, in)
		}
		m, err := DecodeMessage(b, DecodeOptions{})
		if err != nil {
			t.Fatal(err)
		}
		out := m.(Location)
		if out.Status != in.Status || out.HeightReference != in.HeightReference {
			t.Fatalf("status/height reference %d/%d, want %d/%d", out.Status, out.HeightReference, in.Status, in.HeightReference)
		}
		// Half a step, plus float noise; direction compares on the circle.
		d := math.Abs(*out.DirectionDeg - dir)
		within(t, "direction_deg", math.Min(d, 360-d), 0.5)
		within(t, "speed_horizontal_ms", *out.SpeedHorizontalMS-speed, speedStepHighMS/2)
		within(t, "speed_vertical_ms", *out.SpeedVerticalMS-vspeed, speedVStepMS/2)
		within(t, "lat_deg", *out.LatDeg-lat, 0.5/latLonScale)
		within(t, "lon_deg", *out.LonDeg-lon, 0.5/latLonScale)
		within(t, "alt_baro_m", *out.AltBaroM-alt, altStepM/2)
		within(t, "alt_hae_m", *out.AltHAEM-alt, altStepM/2)
		within(t, "height_m", *out.HeightM-height, altStepM/2)
		within(t, "seconds_after_hour", *out.SecondsAfterHour-ts, 0.05)
	})
}

func clampF(v, lo, hi float64) float64 {
	if math.IsNaN(v) {
		return lo
	}
	return math.Max(lo, math.Min(hi, v))
}

func within(t *testing.T, field string, d, tol float64) {
	t.Helper()
	if math.Abs(d) > tol+1e-9 {
		t.Fatalf("%s off by %v, more than %v", field, d, tol)
	}
}
