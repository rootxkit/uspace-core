package odid

import "encoding/binary"

// Wire layout. Every offset, bit position and scale below is read from
// the packed structs and helpers of opendroneid-core-c (see doc.go for the
// commit) and pinned by the reference frames of odid_decode.json and by
// the frame-differencing tests in wire_test.go (LESSONS E-03). Multi-byte
// fields are little-endian; bit fields are listed least significant bit
// first, as in the C structs.

// The standard's "unknown" values, in decoded units (opendroneid.h
// INV_DIR, INV_SPEED_H, INV_SPEED_V, INV_ALT, INV_TIMESTAMP). The decoder
// turns each into nil (R-01); the encoder writes each for a nil field.
const (
	// SpecialDirection is the unknown direction, in degrees.
	SpecialDirection = 361
	// SpecialSpeedH is the unknown horizontal speed, in m/s.
	SpecialSpeedH = 255
	// SpecialSpeedV is the unknown vertical speed, in m/s.
	SpecialSpeedV = 63
	// SpecialAltitudeM is the unknown altitude or height, in metres.
	SpecialAltitudeM = -1000.0
	// SpecialTimestamp is the unknown Location timestamp, as the raw
	// uint16 of tenths of a second.
	SpecialTimestamp = 0xFFFF
)

// ProtocolVersion is the version nibble the encoder writes
// (ODID_PROTOCOL_VERSION). The decoder does not judge it.
const ProtocolVersion = 2

const (
	// PackMaxMessages is the most messages a pack holds
	// (ODID_PACK_MAX_MESSAGES).
	PackMaxMessages = 9
	// PackMaxBasicID is the most Basic ID messages a pack holds
	// (ODID_BASIC_ID_MAX_MESSAGES).
	PackMaxBasicID = 2
	// PackHeaderSize is the pack header: type byte, message size, count.
	PackHeaderSize = 3
	// IDSize is the Basic ID and Operator ID string field (ODID_ID_SIZE).
	IDSize = 20
	// StrSize is the Self-ID description field (ODID_STR_SIZE).
	StrSize = 23
	// AuthDataSize is the Authentication page after the two header
	// bytes; Authentication.Raw holds it.
	AuthDataSize = 23
)

// Byte 0 of every message: [MessageType:4][ProtoVersion:4].
const offHeader = 0

// ODID_BasicID_encoded.
const (
	offBasicIDTypes = 1 // [IDType:4][UAType:4]
	offBasicIDUASID = 2 // char UASID[20], bytes 2-21; 22-24 reserved
)

// ODID_Location_encoded.
const (
	offLocFlags      = 1  // [Status:4][Reserved:1][HeightType:1][EWDirection:1][SpeedMult:1]
	offLocDirection  = 2  // uint8
	offLocSpeedH     = 3  // uint8
	offLocSpeedV     = 4  // int8
	offLocLat        = 5  // int32
	offLocLon        = 9  // int32
	offLocAltBaro    = 13 // uint16
	offLocAltGeo     = 15 // uint16
	offLocHeight     = 17 // uint16
	offLocAccHV      = 19 // [VertAccuracy:4][HorizAccuracy:4]
	offLocAccBS      = 20 // [BaroAccuracy:4][SpeedAccuracy:4]
	offLocTimestamp  = 21 // uint16 tenths of a second after the hour
	offLocTSAccuracy = 23 // [Reserved2:4][TSAccuracy:4]
	offLocReserved3  = 24
)

// Location flag bits (byte 1).
const (
	locFlagSpeedMult   = 0x01
	locFlagEWDirection = 0x02
	locFlagHeightType  = 0x04
	locFlagReserved    = 0x08
	locStatusShift     = 4
)

// ODID_System_encoded.
const (
	offSysFlags      = 1  // [Reserved:3][ClassificationType:3][OperatorLocationType:2]
	offSysOpLat      = 2  // int32
	offSysOpLon      = 6  // int32
	offSysAreaCount  = 10 // uint16
	offSysAreaRadius = 12 // uint8, tens of metres
	offSysCeiling    = 13 // uint16
	offSysFloor      = 15 // uint16
	offSysEU         = 17 // [CategoryEU:4][ClassEU:4]
	offSysOpAlt      = 18 // uint16
	offSysTimestamp  = 20 // uint32 seconds since 2019-01-01
	offSysReserved2  = 24
)

// System flag bits (byte 1).
const (
	sysOpLocTypeMask    = 0x03
	sysClassShift       = 2
	sysClassMask        = 0x07
	sysReservedMask     = 0xE0
	sysAreaRadiusStepM  = 10
	sysAreaRadiusMaxRaw = 0xFF
)

// ODID_OperatorID_encoded.
const (
	offOpIDType = 1 // uint8
	offOpID     = 2 // char OperatorId[20], bytes 2-21; 22-24 reserved
)

// ODID_SelfID_encoded.
const (
	offSelfIDDescType = 1 // uint8
	offSelfIDDesc     = 2 // char Desc[23], bytes 2-24
)

// ODID_Auth_encoded (both page layouts share the first two bytes).
const (
	offAuthTypePage = 1 // [AuthType:4][DataPage:4]
	offAuthData     = 2 // 23 bytes, interpreted only with the maker's keys
)

// ODID_MessagePack_encoded.
const (
	offPackMessageSize = 1
	offPackCount       = 2
	offPackMessages    = 3
)

// Scales (opendroneid.c SPEED_DIV, VSPEED_DIV, LATLON_MULT, ALT_DIV,
// ALT_ADDER).
const (
	speedStepLowMS  = 0.25
	speedStepHighMS = 0.75
	// speedHighBaseMS is where the 0.75 m/s range starts: 255 * 0.25.
	speedHighBaseMS = 255 * speedStepLowMS
	speedVStepMS    = 0.5
	latLonScale     = 1e7
	altStepM        = 0.5
	altOffsetM      = 1000.0
	timestampTenths = 10
)

const (
	nibble = 0x0F
	// speedHRawUnknown is the raw horizontal speed, with the multiplier
	// bit set, that decodes to SpecialSpeedH.
	speedHRawUnknown = 0xFF
	// speedVRawUnknown is the raw vertical speed that decodes to
	// SpecialSpeedV (63 / 0.5).
	speedVRawUnknown = SpecialSpeedV * 2
	// directionRawUnknown is the raw direction, with the east/west bit
	// set, that decodes to SpecialDirection.
	directionRawUnknown = SpecialDirection - 180
)

func typeNibble(b byte) MessageType { return MessageType(b >> 4) }

func header(t MessageType) byte { return byte(t)<<4 | ProtocolVersion }

func u16(b []byte) uint16 { return binary.LittleEndian.Uint16(b) }
func u32(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
func i32(b []byte) int32  { return int32(binary.LittleEndian.Uint32(b)) }

func putU16(b []byte, v uint16) { binary.LittleEndian.PutUint16(b, v) }
func putU32(b []byte, v uint32) { binary.LittleEndian.PutUint32(b, v) }
func putI32(b []byte, v int32)  { binary.LittleEndian.PutUint32(b, uint32(v)) }
