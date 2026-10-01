package odid

// This file is FROZEN by docs/PLAN.md (WP-0). WP-3 implements the codec
// against it; WP-6 consumes it. Field names follow the vectors
// (knowledge/vectors/odid_decode.json), which follow the old decoder, which
// follow opendroneid-core-c. Units are in the names (E-13); wire units are
// converted at the parser boundary and nowhere else.

// MessageType is the ODID message type nibble.
type MessageType uint8

const (
	TypeBasicID        MessageType = 0x0
	TypeLocation       MessageType = 0x1
	TypeAuthentication MessageType = 0x2
	TypeSelfID         MessageType = 0x3
	TypeSystem         MessageType = 0x4
	TypeOperatorID     MessageType = 0x5
	TypeMessagePack    MessageType = 0xF
)

// MessageSize is the size of every ODID message on the wire.
const MessageSize = 25

// IDType is the Basic ID identity type. Only IDTypeSerial is matched
// against a registry (LESSONS I-05).
type IDType uint8

const (
	IDTypeNone            IDType = 0
	IDTypeSerial          IDType = 1 // ANSI/CTA-2063-A serial
	IDTypeCAARegistration IDType = 2
	IDTypeUTMAssigned     IDType = 3
	IDTypeSpecificSession IDType = 4
)

// Status is the Location message operational status. Only StatusGround
// is "not airborne" (LESSONS R-11).
type Status uint8

const (
	StatusUndeclared            Status = 0
	StatusGround                Status = 1
	StatusAirborne              Status = 2
	StatusEmergency             Status = 3
	StatusRemoteIDSystemFailure Status = 4
)

// Airborne reports whether the aircraft did not declare itself on the
// ground (R-11: erring towards flying is the safe side).
func (s Status) Airborne() bool { return s != StatusGround }

// HeightReference says what Location.HeightM is measured over (R-12).
type HeightReference uint8

const (
	HeightOverTakeoff HeightReference = 0
	HeightOverGround  HeightReference = 1
)

// Message is one decoded ODID message.
type Message interface {
	// Type returns the message type.
	Type() MessageType
}

// BasicID carries the aircraft identity.
type BasicID struct {
	IDType IDType `json:"id_type"`
	UAType uint8  `json:"ua_type"`
	// UAID is the identity with trailing NUL padding removed, case kept.
	UAID string `json:"ua_id"`
}

// Type implements Message.
func (BasicID) Type() MessageType { return TypeBasicID }

// Location is the dynamic message. Every pointer field is nil when the
// wire carried the standard's "unknown" value (R-01): direction 361,
// horizontal speed 255, vertical speed 63, altitude -1000 m, timestamp
// 0xFFFF, and latitude 0 together with longitude 0.
type Location struct {
	Status Status `json:"status"`
	// DirectionDeg is the track over the ground, degrees true, [0, 360).
	DirectionDeg *float64 `json:"direction_deg"`
	// SpeedHorizontalMS is the ground speed in m/s.
	SpeedHorizontalMS *float64 `json:"speed_horizontal_ms"`
	// SpeedVerticalMS is positive up.
	SpeedVerticalMS *float64 `json:"speed_vertical_ms"`
	LatDeg          *float64 `json:"lat_deg"`
	LonDeg          *float64 `json:"lon_deg"`
	// AltBaroM is the pressure altitude (ISA 1013.25 hPa, not AMSL).
	AltBaroM *float64 `json:"alt_baro_m"`
	// AltHAEM is the geodetic altitude above the WGS84 ellipsoid.
	AltHAEM         *float64        `json:"alt_hae_m"`
	HeightReference HeightReference `json:"height_reference"`
	// HeightM is over take-off or over ground, per HeightReference.
	HeightM *float64 `json:"height_m"`
	// Accuracy fields are the standard's enum codes, not metres.
	HorizAccuracy uint8 `json:"horiz_accuracy"`
	VertAccuracy  uint8 `json:"vert_accuracy"`
	BaroAccuracy  uint8 `json:"baro_accuracy"`
	SpeedAccuracy uint8 `json:"speed_accuracy"`
	TSAccuracy    uint8 `json:"ts_accuracy"`
	// SecondsAfterHour is the broadcast timestamp: seconds after the full
	// UTC hour (wire: tenths), nil for 0xFFFF.
	SecondsAfterHour *float64 `json:"seconds_after_hour"`
}

// Type implements Message.
func (Location) Type() MessageType { return TypeLocation }

// System carries the operator location and the operating area.
type System struct {
	OperatorLocationType uint8    `json:"operator_location_type"`
	ClassificationType   uint8    `json:"classification_type"`
	OperatorLatDeg       *float64 `json:"operator_lat_deg"`
	OperatorLonDeg       *float64 `json:"operator_lon_deg"`
	AreaCount            uint16   `json:"area_count"`
	AreaRadiusM          uint32   `json:"area_radius_m"`
	AreaCeilingM         *float64 `json:"area_ceiling_m"`
	AreaFloorM           *float64 `json:"area_floor_m"`
	CategoryEU           uint8    `json:"category_eu"`
	ClassEU              uint8    `json:"class_eu"`
	OperatorAltHAEM      *float64 `json:"operator_alt_hae_m"`
	// TimestampS is seconds since 2019-01-01T00:00:00Z.
	TimestampS uint32 `json:"timestamp_s"`
}

// Type implements Message.
func (System) Type() MessageType { return TypeSystem }

// OperatorID carries the operator registration number as broadcast.
type OperatorID struct {
	OperatorIDType uint8 `json:"operator_id_type"`
	// OperatorID is the number with trailing NUL padding removed.
	OperatorID string `json:"operator_id"`
}

// Type implements Message.
func (OperatorID) Type() MessageType { return TypeOperatorID }

// SelfID is free text the operator typed. It is skipped by default (R-04)
// and only returned when DecodeOptions.KeepSkipped is set.
type SelfID struct {
	DescriptionType uint8  `json:"description_type"`
	Description     string `json:"description"`
}

// Type implements Message.
func (SelfID) Type() MessageType { return TypeSelfID }

// Authentication needs the manufacturer's keys, which we do not have. It
// is skipped by default (R-04); when kept, the raw page is retained for
// the archive (R-15), never interpreted.
type Authentication struct {
	AuthType   uint8  `json:"auth_type"`
	PageNumber uint8  `json:"page_number"`
	Raw        []byte `json:"raw"`
}

// Type implements Message.
func (Authentication) Type() MessageType { return TypeAuthentication }

// DecodeOptions tune the decoder. The zero value is the production
// setting.
type DecodeOptions struct {
	// KeepSkipped returns Self-ID, Authentication and unknown-type
	// messages as values instead of dropping them.
	KeepSkipped bool
}
