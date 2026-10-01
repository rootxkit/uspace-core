package odid

import (
	"errors"
	"math"

	"github.com/rootxkit/uspace-core/core"
)

// Encode is the inverse of DecodeMessage: it re-encodes every reference
// frame of odid_decode.json byte for byte (R-02). A nil field encodes the
// standard's unknown value. Encode refuses, with a *core.FieldError that
// names the field, a value the wire cannot carry: one outside its field,
// one that would encode as the unknown sentinel (pass nil instead), NaN
// or an infinity, and a string longer than its field or ending in NUL. It
// does not judge plausibility: a decoded message always re-encodes.
//
// Quantisation follows opendroneid-core-c: values round half away from
// zero; a direction that rounds to 360 is written as 0 (north); speed uses
// the 0.25 m/s range up to 63.5 m/s and the 0.75 m/s range above it; the
// area radius is truncated to tens of metres.
func Encode(m Message) ([MessageSize]byte, error) {
	var b [MessageSize]byte
	var err error
	switch v := m.(type) {
	case BasicID:
		err = encodeBasicID(&b, v)
	case Location:
		err = encodeLocation(&b, &v)
	case System:
		err = encodeSystem(&b, &v)
	case OperatorID:
		err = encodeOperatorID(&b, v)
	case SelfID:
		err = encodeSelfID(&b, v)
	case Authentication:
		err = encodeAuthentication(&b, v)
	case Unknown:
		err = encodeUnknown(&b, v)
	case nil:
		err = &core.FieldError{Field: "message", Reason: "nil"}
	default:
		err = core.Fieldf("message", "type %T is not an ODID message this codec encodes", m)
	}
	if err != nil {
		return [MessageSize]byte{}, err
	}
	return b, nil
}

// EncodePack encodes 1 to 9 messages as one message pack. It refuses what
// Decode refuses (R-03): an empty or oversized pack, a pack inside a pack,
// an undefined type, more than two Basic IDs and more than one Location,
// Self-ID, System or Operator ID. A message's refusal names it as
// "pack[i].<field>".
func EncodePack(ms []Message) ([]byte, error) {
	if len(ms) < 1 || len(ms) > PackMaxMessages {
		return nil, core.Fieldf("pack", "pack of %d messages", len(ms))
	}
	var kinds [PackMaxMessages]MessageType
	for i, m := range ms {
		if m == nil {
			return nil, &core.FieldError{Field: packField(i), Reason: "nil"}
		}
		kinds[i] = m.Type()
	}
	if err := checkPackKinds(kinds[:len(ms)]); err != nil {
		return nil, err
	}
	out := make([]byte, PackHeaderSize+len(ms)*MessageSize)
	out[offHeader] = header(TypeMessagePack)
	out[offPackMessageSize] = MessageSize
	out[offPackCount] = byte(len(ms))
	for i, m := range ms {
		b, err := Encode(m)
		if err != nil {
			var fe *core.FieldError
			if errors.As(err, &fe) {
				return nil, &core.FieldError{Field: packField(i) + "." + fe.Field, Reason: fe.Reason}
			}
			return nil, err
		}
		copy(out[offPackMessages+i*MessageSize:], b[:])
	}
	return out, nil
}

func encodeBasicID(b *[MessageSize]byte, m BasicID) error {
	if err := nibbleField("id_type", uint8(m.IDType)); err != nil {
		return err
	}
	if err := nibbleField("ua_type", m.UAType); err != nil {
		return err
	}
	b[offHeader] = header(TypeBasicID)
	b[offBasicIDTypes] = uint8(m.IDType)<<4 | m.UAType
	return putText(b[offBasicIDUASID:offBasicIDUASID+IDSize], "ua_id", m.UAID)
}

func encodeOperatorID(b *[MessageSize]byte, m OperatorID) error {
	b[offHeader] = header(TypeOperatorID)
	b[offOpIDType] = m.OperatorIDType
	return putText(b[offOpID:offOpID+IDSize], "operator_id", m.OperatorID)
}

func encodeSelfID(b *[MessageSize]byte, m SelfID) error {
	b[offHeader] = header(TypeSelfID)
	b[offSelfIDDescType] = m.DescriptionType
	return putText(b[offSelfIDDesc:offSelfIDDesc+StrSize], "description", m.Description)
}

func encodeAuthentication(b *[MessageSize]byte, m Authentication) error {
	if err := nibbleField("auth_type", m.AuthType); err != nil {
		return err
	}
	if err := nibbleField("page_number", m.PageNumber); err != nil {
		return err
	}
	if len(m.Raw) > AuthDataSize {
		return core.Fieldf("raw", "%d bytes, a page holds %d", len(m.Raw), AuthDataSize)
	}
	b[offHeader] = header(TypeAuthentication)
	b[offAuthTypePage] = m.AuthType<<4 | m.PageNumber
	copy(b[offAuthData:], m.Raw)
	return nil
}

func encodeUnknown(b *[MessageSize]byte, m Unknown) error {
	switch t := m.Type(); t {
	case TypeBasicID, TypeLocation, TypeAuthentication, TypeSelfID, TypeSystem, TypeOperatorID:
		return core.Fieldf("raw", "type %d has its own message struct", t)
	case TypeMessagePack:
		return &core.FieldError{Field: "raw", Reason: "a message pack is not a single message"}
	}
	*b = m.Raw
	return nil
}

func encodeLocation(b *[MessageSize]byte, m *Location) error {
	if err := nibbleField("status", uint8(m.Status)); err != nil {
		return err
	}
	if m.HeightReference > 1 {
		return core.Fieldf("height_reference", "%d does not fit in 1 bit", m.HeightReference)
	}
	for _, f := range [...]struct {
		name string
		v    uint8
	}{
		{"horiz_accuracy", m.HorizAccuracy}, {"vert_accuracy", m.VertAccuracy},
		{"baro_accuracy", m.BaroAccuracy}, {"speed_accuracy", m.SpeedAccuracy},
		{"ts_accuracy", m.TSAccuracy},
	} {
		if err := nibbleField(f.name, f.v); err != nil {
			return err
		}
	}
	dir, ew, err := encodeDirection(m.DirectionDeg)
	if err != nil {
		return err
	}
	speed, mult, err := encodeSpeedH(m.SpeedHorizontalMS)
	if err != nil {
		return err
	}
	vspeed, err := encodeSpeedV(m.SpeedVerticalMS)
	if err != nil {
		return err
	}
	lat, lon, err := encodeLatLon("lat_deg", "lon_deg", m.LatDeg, m.LonDeg)
	if err != nil {
		return err
	}
	var alts [3]uint16
	for i, f := range [...]struct {
		name string
		v    *float64
	}{{"alt_baro_m", m.AltBaroM}, {"alt_hae_m", m.AltHAEM}, {"height_m", m.HeightM}} {
		if alts[i], err = encodeAltitude(f.name, f.v); err != nil {
			return err
		}
	}
	ts, err := encodeTimestamp(m.SecondsAfterHour)
	if err != nil {
		return err
	}

	b[offHeader] = header(TypeLocation)
	b[offLocFlags] = uint8(m.Status)<<locStatusShift | uint8(m.HeightReference)<<2 | ew<<1 | mult
	b[offLocDirection] = dir
	b[offLocSpeedH] = speed
	b[offLocSpeedV] = uint8(vspeed)
	putI32(b[offLocLat:], lat)
	putI32(b[offLocLon:], lon)
	putU16(b[offLocAltBaro:], alts[0])
	putU16(b[offLocAltGeo:], alts[1])
	putU16(b[offLocHeight:], alts[2])
	b[offLocAccHV] = m.VertAccuracy<<4 | m.HorizAccuracy
	b[offLocAccBS] = m.BaroAccuracy<<4 | m.SpeedAccuracy
	putU16(b[offLocTimestamp:], ts)
	b[offLocTSAccuracy] = m.TSAccuracy
	b[offLocReserved3] = 0
	return nil
}

func encodeSystem(b *[MessageSize]byte, m *System) error {
	if m.OperatorLocationType > sysOpLocTypeMask {
		return core.Fieldf("operator_location_type", "%d does not fit in 2 bits", m.OperatorLocationType)
	}
	if m.ClassificationType > sysClassMask {
		return core.Fieldf("classification_type", "%d does not fit in 3 bits", m.ClassificationType)
	}
	if err := nibbleField("category_eu", m.CategoryEU); err != nil {
		return err
	}
	if err := nibbleField("class_eu", m.ClassEU); err != nil {
		return err
	}
	if m.AreaRadiusM > sysAreaRadiusMaxRaw*sysAreaRadiusStepM {
		return core.Fieldf("area_radius_m", "%d m exceeds %d m", m.AreaRadiusM, sysAreaRadiusMaxRaw*sysAreaRadiusStepM)
	}
	lat, lon, err := encodeLatLon("operator_lat_deg", "operator_lon_deg", m.OperatorLatDeg, m.OperatorLonDeg)
	if err != nil {
		return err
	}
	var alts [3]uint16
	for i, f := range [...]struct {
		name string
		v    *float64
	}{{"area_ceiling_m", m.AreaCeilingM}, {"area_floor_m", m.AreaFloorM}, {"operator_alt_hae_m", m.OperatorAltHAEM}} {
		if alts[i], err = encodeAltitude(f.name, f.v); err != nil {
			return err
		}
	}

	b[offHeader] = header(TypeSystem)
	b[offSysFlags] = m.ClassificationType<<sysClassShift | m.OperatorLocationType
	putI32(b[offSysOpLat:], lat)
	putI32(b[offSysOpLon:], lon)
	putU16(b[offSysAreaCount:], m.AreaCount)
	b[offSysAreaRadius] = uint8(m.AreaRadiusM / sysAreaRadiusStepM)
	putU16(b[offSysCeiling:], alts[0])
	putU16(b[offSysFloor:], alts[1])
	b[offSysEU] = m.CategoryEU<<4 | m.ClassEU
	putU16(b[offSysOpAlt:], alts[2])
	putU32(b[offSysTimestamp:], m.TimestampS)
	b[offSysReserved2] = 0
	return nil
}

func nibbleField(name string, v uint8) error {
	if v > nibble {
		return core.Fieldf(name, "%d does not fit in 4 bits", v)
	}
	return nil
}

// putText writes s NUL-padded into dst.
func putText(dst []byte, name, s string) error {
	if len(s) > len(dst) {
		return core.Fieldf(name, "%d bytes, the field holds %d", len(s), len(dst))
	}
	if len(s) > 0 && s[len(s)-1] == 0 {
		return core.Fieldf(name, "ends in NUL, which the wire reads as padding")
	}
	copy(dst, s)
	return nil
}

// finite refuses NaN and the infinities.
func finite(name string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return core.Fieldf(name, "%v is not a finite number", v)
	}
	return nil
}

// encodeDirection follows opendroneid-core-c encodeDirection: round, wrap
// 360 to 0, then 0-179 with the east/west bit clear or 180 and over with
// it set and 180 subtracted.
func encodeDirection(p *float64) (raw, ew uint8, err error) {
	if p == nil {
		return directionRawUnknown, 1, nil
	}
	if err := finite("direction_deg", *p); err != nil {
		return 0, 0, err
	}
	d := math.Round(*p)
	switch {
	case d == 360:
		d = 0
	case d == SpecialDirection:
		return 0, 0, core.Fieldf("direction_deg", "%v rounds to %d, the unknown direction; pass nil", *p, SpecialDirection)
	case d < 0 || d > math.MaxUint8+180:
		return 0, 0, core.Fieldf("direction_deg", "%v is outside 0 to 360", *p)
	}
	if d >= 180 {
		return uint8(d - 180), 1, nil
	}
	return uint8(d), 0, nil
}

// encodeSpeedH uses the 0.25 m/s range while the raw value stays below
// 255 and the 0.75 m/s range above it. The reference library writes
// 63.75 m/s as 255 in the low range; this encoder writes it as 0 in the
// high range, which decodes to the same speed without looking like the
// 255 of a corrupted frame.
func encodeSpeedH(p *float64) (raw, mult uint8, err error) {
	if p == nil {
		return speedHRawUnknown, 1, nil
	}
	if err := finite("speed_horizontal_ms", *p); err != nil {
		return 0, 0, err
	}
	if *p < 0 {
		return 0, 0, core.Fieldf("speed_horizontal_ms", "%v is negative", *p)
	}
	if low := math.Round(*p / speedStepLowMS); low < speedHRawUnknown {
		return uint8(low), 0, nil
	}
	high := math.Max(0, math.Round((*p-speedHighBaseMS)/speedStepHighMS))
	if high >= speedHRawUnknown {
		return 0, 0, core.Fieldf("speed_horizontal_ms", "%v m/s is at or above %d m/s, the unknown speed", *p, SpecialSpeedH)
	}
	return uint8(high), 1, nil
}

func encodeSpeedV(p *float64) (int8, error) {
	if p == nil {
		return speedVRawUnknown, nil
	}
	if err := finite("speed_vertical_ms", *p); err != nil {
		return 0, err
	}
	r := math.Round(*p / speedVStepMS)
	switch {
	case r == speedVRawUnknown:
		return 0, core.Fieldf("speed_vertical_ms", "%v rounds to %d m/s, the unknown speed; pass nil", *p, SpecialSpeedV)
	case r < math.MinInt8 || r > math.MaxInt8:
		return 0, core.Fieldf("speed_vertical_ms", "%v is outside -64 to 63.5 m/s", *p)
	}
	return int8(r), nil
}

// encodeLatLon writes nil, nil as the unknown position 0, 0, and refuses
// a pair that is half known or that would encode as 0, 0.
func encodeLatLon(latName, lonName string, lat, lon *float64) (latRaw, lonRaw int32, err error) {
	switch {
	case lat == nil && lon == nil:
		return 0, 0, nil
	case lat == nil:
		return 0, 0, core.Fieldf(latName, "nil while %s is set; the wire has one unknown position for both", lonName)
	case lon == nil:
		return 0, 0, core.Fieldf(lonName, "nil while %s is set; the wire has one unknown position for both", latName)
	}
	if latRaw, err = latLonRaw(latName, *lat); err != nil {
		return 0, 0, err
	}
	if lonRaw, err = latLonRaw(lonName, *lon); err != nil {
		return 0, 0, err
	}
	if latRaw == 0 && lonRaw == 0 {
		return 0, 0, core.Fieldf(latName, "0, 0 is the unknown position; pass nil for both")
	}
	return latRaw, lonRaw, nil
}

func latLonRaw(name string, v float64) (int32, error) {
	if err := finite(name, v); err != nil {
		return 0, err
	}
	r := math.Round(v * latLonScale)
	if r < math.MinInt32 || r > math.MaxInt32 {
		return 0, core.Fieldf(name, "%v does not fit in an int32 of 1e-7 degrees", v)
	}
	return int32(r), nil
}

func encodeAltitude(name string, p *float64) (uint16, error) {
	if p == nil {
		return 0, nil
	}
	if err := finite(name, *p); err != nil {
		return 0, err
	}
	r := math.Round((*p + altOffsetM) / altStepM)
	switch {
	case r == 0:
		return 0, core.Fieldf(name, "%v rounds to %v m, the unknown altitude; pass nil", *p, SpecialAltitudeM)
	case r < 0 || r > math.MaxUint16:
		return 0, core.Fieldf(name, "%v is outside -1000 to 31767.5 m", *p)
	}
	return uint16(r), nil
}

func encodeTimestamp(p *float64) (uint16, error) {
	if p == nil {
		return SpecialTimestamp, nil
	}
	if err := finite("seconds_after_hour", *p); err != nil {
		return 0, err
	}
	r := math.Round(*p * timestampTenths)
	switch {
	case r == SpecialTimestamp:
		return 0, core.Fieldf("seconds_after_hour", "%v rounds to 0xFFFF tenths, the unknown timestamp; pass nil", *p)
	case r < 0 || r > SpecialTimestamp:
		return 0, core.Fieldf("seconds_after_hour", "%v is outside 0 to 6553.4 s", *p)
	}
	return uint16(r), nil
}
