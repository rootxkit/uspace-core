package core

// Trust is the trust class on every track-like message (04 §2). A token
// proves who sent a message, not that its content is true (06 §1).
type Trust string

const (
	TrustAuthenticated Trust = "authenticated"
	TrustProvider      Trust = "provider"
	TrustSurveillance  Trust = "surveillance"
	TrustBroadcast     Trust = "broadcast"
	TrustSensor        Trust = "sensor"
	// TrustSimulated is lab only; production ingest refuses it (06 T11).
	TrustSimulated Trust = "simulated"
)

// Severity of an alert or violation.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// ZoneType is the ED-318 CodeZoneType. The canonical spelling is
// ED-318's (REQ_AUTHORIZATION); ED-269 spells the same value
// REQ_AUTHORISATION and the ed269 package converts at its boundary
// (LESSONS Z-04: the spelling must round-trip exactly, so it is never
// normalised inside a document).
type ZoneType string

const (
	ZoneProhibited       ZoneType = "PROHIBITED"
	ZoneReqAuthorization ZoneType = "REQ_AUTHORIZATION"
	ZoneConditional      ZoneType = "CONDITIONAL"
	ZoneNoRestriction    ZoneType = "NO_RESTRICTION"
	ZoneUSpace           ZoneType = "USPACE"
)

// Valid reports whether z is a known ED-318 zone type.
func (z ZoneType) Valid() bool {
	switch z {
	case ZoneProhibited, ZoneReqAuthorization, ZoneConditional, ZoneNoRestriction, ZoneUSpace:
		return true
	}
	return false
}

// ED269 returns the ED-269 `restriction` spelling of z. USPACE has no
// ED-269 restriction; it is returned unchanged.
func (z ZoneType) ED269() string {
	if z == ZoneReqAuthorization {
		return "REQ_AUTHORISATION"
	}
	return string(z)
}

// ZoneTypeFromED269 maps an ED-269 restriction spelling onto ZoneType.
// It accepts only the ED-269 spelling of REQ_AUTHORISATION; a file with
// the Z spelling is an ED-269 refusal (Z-04), which the ed269 package
// reports before calling this.
func ZoneTypeFromED269(restriction string) (ZoneType, bool) {
	switch restriction {
	case "PROHIBITED":
		return ZoneProhibited, true
	case "REQ_AUTHORISATION":
		return ZoneReqAuthorization, true
	case "CONDITIONAL":
		return ZoneConditional, true
	case "NO_RESTRICTION":
		return ZoneNoRestriction, true
	}
	return "", false
}

// IncidentZone reports whether an unidentified or unknown_operator
// aircraft inside this zone type raises the `identification` alert
// (LESSONS G-03): PROHIBITED and REQ_AUTHORISATION only.
func (z ZoneType) IncidentZone() bool {
	return z == ZoneProhibited || z == ZoneReqAuthorization
}
