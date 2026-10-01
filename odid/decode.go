package odid

import (
	"bytes"
	"strconv"

	"github.com/rootxkit/uspace-core/core"
)

// Unknown is a message whose type nibble this codec does not define (6 to
// 14). Decode drops it (R-04); with DecodeOptions.KeepSkipped it is
// returned as the raw 25 bytes, never interpreted. It lives here rather
// than in the frozen types.go because only the codec produces it.
type Unknown struct {
	Raw [MessageSize]byte `json:"raw"`
}

// Type implements Message: the type nibble of Raw.
func (u Unknown) Type() MessageType { return typeNibble(u.Raw[offHeader]) }

// TypeOf returns the type nibble of a frame's first byte. An empty frame
// is refused; the length of the rest is not judged (Decode does that).
func TypeOf(frame []byte) (MessageType, error) {
	if len(frame) == 0 {
		return 0, &core.FieldError{Field: "frame", Reason: "empty"}
	}
	return typeNibble(frame[offHeader]), nil
}

// Decode decodes one 25-byte message or one message pack into the
// messages it holds, in order. Self-ID, Authentication and unknown types
// are dropped unless opts.KeepSkipped (R-04), so a valid frame can decode
// to no message. A malformed frame or pack is refused whole (R-03) with a
// *core.FieldError whose Field is "frame" or "pack[i]".
func Decode(frame []byte, opts DecodeOptions) ([]Message, error) {
	t, err := TypeOf(frame)
	if err != nil {
		return nil, err
	}
	if t == TypeMessagePack {
		return decodePack(frame, opts)
	}
	if len(frame) != MessageSize {
		return nil, core.Fieldf("frame", "%d bytes, a message is %d", len(frame), MessageSize)
	}
	m := decodeOne((*[MessageSize]byte)(frame), opts)
	if m == nil {
		return nil, nil
	}
	return []Message{m}, nil
}

// DecodeMessage decodes one message. It returns nil, nil for a type that
// is skipped (Self-ID, Authentication, unknown) unless opts.KeepSkipped,
// and refuses a message pack, which is not a single message.
func DecodeMessage(b [MessageSize]byte, opts DecodeOptions) (Message, error) {
	if typeNibble(b[offHeader]) == TypeMessagePack {
		return nil, &core.FieldError{Field: "frame", Reason: "a message pack is not a single message"}
	}
	return decodeOne(&b, opts), nil
}

// decodeOne decodes a message that is not a pack; nil for a skipped type.
func decodeOne(b *[MessageSize]byte, opts DecodeOptions) Message {
	switch typeNibble(b[offHeader]) {
	case TypeBasicID:
		return decodeBasicID(b)
	case TypeLocation:
		return decodeLocation(b)
	case TypeSystem:
		return decodeSystem(b)
	case TypeOperatorID:
		return decodeOperatorID(b)
	case TypeSelfID:
		if opts.KeepSkipped {
			return decodeSelfID(b)
		}
	case TypeAuthentication:
		if opts.KeepSkipped {
			return decodeAuthentication(b)
		}
	case TypeMessagePack:
		// Refused by the callers before they get here.
	default:
		if opts.KeepSkipped {
			return Unknown{Raw: *b}
		}
	}
	return nil
}

// decodePack checks the whole pack before decoding any message in it, so
// a refusal never leaves a partial result (R-03).
func decodePack(frame []byte, opts DecodeOptions) ([]Message, error) {
	if len(frame) < PackHeaderSize {
		return nil, core.Fieldf("frame", "%d bytes, a pack header is %d", len(frame), PackHeaderSize)
	}
	if size := frame[offPackMessageSize]; size != MessageSize {
		return nil, core.Fieldf("frame", "pack message size %d, expected %d", size, MessageSize)
	}
	count := int(frame[offPackCount])
	if count < 1 || count > PackMaxMessages {
		return nil, core.Fieldf("frame", "pack of %d messages", count)
	}
	want := PackHeaderSize + count*MessageSize
	if len(frame) < want {
		return nil, core.Fieldf("frame", "pack shorter than it says: %d bytes, %d messages need %d", len(frame), count, want)
	}
	// Bytes past the declared messages are transport padding (BLE and
	// Wi-Fi NAN pad their payloads): ignored, never decoded.
	frame = frame[:want]
	var kinds [PackMaxMessages]MessageType
	for i := range count {
		kinds[i] = typeNibble(frame[offPackMessages+i*MessageSize])
	}
	if err := checkPackKinds(kinds[:count]); err != nil {
		return nil, err
	}
	out := make([]Message, 0, count)
	for i := range count {
		off := offPackMessages + i*MessageSize
		if m := decodeOne((*[MessageSize]byte)(frame[off:off+MessageSize]), opts); m != nil {
			out = append(out, m)
		}
	}
	return out, nil
}

// checkPackKinds applies opendroneid-core-c checkPackContent: no pack and
// no undefined type inside a pack, at most two Basic IDs, at most one
// Location, Self-ID, System and Operator ID. Decode and EncodePack share
// it, so the encoder never builds a pack the decoder refuses.
func checkPackKinds(kinds []MessageType) error {
	var seen [TypeOperatorID + 1]int
	for i, k := range kinds {
		switch {
		case k == TypeMessagePack:
			return &core.FieldError{Field: packField(i), Reason: "a pack inside a pack"}
		case k > TypeOperatorID:
			return core.Fieldf(packField(i), "message type %d is not allowed in a pack", k)
		}
		seen[k]++
		switch k {
		case TypeBasicID:
			if seen[k] > PackMaxBasicID {
				return &core.FieldError{Field: packField(i), Reason: "too many Basic ID messages in a pack"}
			}
		case TypeLocation, TypeSelfID, TypeSystem, TypeOperatorID:
			if seen[k] > 1 {
				return core.Fieldf(packField(i), "more than one %s message in a pack", packNames[k])
			}
		case TypeAuthentication, TypeMessagePack:
			// Authentication pages (at most 16) cannot exceed a pack of 9;
			// a pack was refused above.
		}
	}
	return nil
}

// packNames are the reference decoder's names for the types in pack
// errors.
var packNames = [TypeOperatorID + 1]string{
	TypeBasicID: "BASIC_ID", TypeLocation: "LOCATION", TypeAuthentication: "AUTH",
	TypeSelfID: "SELF_ID", TypeSystem: "SYSTEM", TypeOperatorID: "OPERATOR_ID",
}

func packField(i int) string {
	return "pack[" + strconv.Itoa(i) + "]"
}

// text removes the trailing NUL padding and keeps every other byte as
// broadcast, case and all.
func text(b []byte) string {
	return string(bytes.TrimRight(b, "\x00"))
}

func decodeBasicID(b *[MessageSize]byte) BasicID {
	return BasicID{
		IDType: IDType(b[offBasicIDTypes] >> 4),
		UAType: b[offBasicIDTypes] & nibble,
		UAID:   text(b[offBasicIDUASID : offBasicIDUASID+IDSize]),
	}
}

func decodeOperatorID(b *[MessageSize]byte) OperatorID {
	return OperatorID{
		OperatorIDType: b[offOpIDType],
		OperatorID:     text(b[offOpID : offOpID+IDSize]),
	}
}

func decodeSelfID(b *[MessageSize]byte) SelfID {
	return SelfID{
		DescriptionType: b[offSelfIDDescType],
		Description:     text(b[offSelfIDDesc : offSelfIDDesc+StrSize]),
	}
}

func decodeAuthentication(b *[MessageSize]byte) Authentication {
	raw := make([]byte, AuthDataSize)
	copy(raw, b[offAuthData:])
	return Authentication{
		AuthType:   b[offAuthTypePage] >> 4,
		PageNumber: b[offAuthTypePage] & nibble,
		Raw:        raw,
	}
}

// decodeLocation turns the wire into units and the standard's unknown
// values into nil (R-01). The optional values share one allocation.
func decodeLocation(b *[MessageSize]byte) Location {
	v := new([9]float64)
	flags := b[offLocFlags]
	l := Location{
		Status:          Status(flags >> locStatusShift),
		HeightReference: HeightReference((flags & locFlagHeightType) >> 2),
		HorizAccuracy:   b[offLocAccHV] & nibble,
		VertAccuracy:    b[offLocAccHV] >> 4,
		SpeedAccuracy:   b[offLocAccBS] & nibble,
		BaroAccuracy:    b[offLocAccBS] >> 4,
		TSAccuracy:      b[offLocTSAccuracy] & nibble,
	}

	v[0] = float64(b[offLocDirection])
	if flags&locFlagEWDirection != 0 {
		v[0] += 180
	}
	// 361 is the standard's unknown; 360 and 362-435 are not directions
	// either, only a malformed frame carries them: all decode as unknown.
	if v[0] < 360 {
		l.DirectionDeg = &v[0]
	}

	if flags&locFlagSpeedMult != 0 {
		v[1] = float64(b[offLocSpeedH])*speedStepHighMS + speedHighBaseMS
	} else {
		v[1] = float64(b[offLocSpeedH]) * speedStepLowMS
	}
	if v[1] != SpecialSpeedH {
		l.SpeedHorizontalMS = &v[1]
	}

	v[2] = float64(int8(b[offLocSpeedV])) * speedVStepMS
	if v[2] != SpecialSpeedV {
		l.SpeedVerticalMS = &v[2]
	}

	lat, lon := i32(b[offLocLat:]), i32(b[offLocLon:])
	if lat != 0 || lon != 0 {
		v[3], v[4] = float64(lat)/latLonScale, float64(lon)/latLonScale
		l.LatDeg, l.LonDeg = &v[3], &v[4]
	}

	l.AltBaroM = altitude(u16(b[offLocAltBaro:]), &v[5])
	l.AltHAEM = altitude(u16(b[offLocAltGeo:]), &v[6])
	l.HeightM = altitude(u16(b[offLocHeight:]), &v[7])

	if ts := u16(b[offLocTimestamp:]); ts != SpecialTimestamp {
		v[8] = float64(ts) / timestampTenths
		l.SecondsAfterHour = &v[8]
	}
	return l
}

// altitude decodes an altitude or height field into dst, or nil for the
// unknown value.
func altitude(raw uint16, dst *float64) *float64 {
	*dst = float64(raw)*altStepM - altOffsetM
	if *dst == SpecialAltitudeM {
		return nil
	}
	return dst
}

func decodeSystem(b *[MessageSize]byte) System {
	v := new([5]float64)
	flags := b[offSysFlags]
	s := System{
		OperatorLocationType: flags & sysOpLocTypeMask,
		ClassificationType:   (flags >> sysClassShift) & sysClassMask,
		AreaCount:            u16(b[offSysAreaCount:]),
		AreaRadiusM:          uint32(b[offSysAreaRadius]) * sysAreaRadiusStepM,
		CategoryEU:           b[offSysEU] >> 4,
		ClassEU:              b[offSysEU] & nibble,
		TimestampS:           u32(b[offSysTimestamp:]),
	}
	lat, lon := i32(b[offSysOpLat:]), i32(b[offSysOpLon:])
	if lat != 0 || lon != 0 {
		v[0], v[1] = float64(lat)/latLonScale, float64(lon)/latLonScale
		s.OperatorLatDeg, s.OperatorLonDeg = &v[0], &v[1]
	}
	s.AreaCeilingM = altitude(u16(b[offSysCeiling:]), &v[2])
	s.AreaFloorM = altitude(u16(b[offSysFloor:]), &v[3])
	s.OperatorAltHAEM = altitude(u16(b[offSysOpAlt:]), &v[4])
	return s
}
