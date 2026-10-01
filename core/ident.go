package core

// IdentStatus is one of the four identification statuses (04 §3.2,
// LESSONS G-01).
type IdentStatus string

const (
	// IdentRegistered is an aircraft matched to a registered UAS of a
	// registered operator in good standing.
	IdentRegistered IdentStatus = "registered"
	// IdentSuspended is an aircraft whose UAS or operator registration is
	// suspended or revoked.
	IdentSuspended IdentStatus = "suspended"
	// IdentUnknownOperator is an aircraft whose operator could not be
	// resolved to a registered one.
	IdentUnknownOperator IdentStatus = "unknown_operator"
	// IdentUnidentified is an aircraft that could not be identified at all.
	IdentUnidentified IdentStatus = "unidentified"
)

// IncidentStatus reports whether the status raises `identification`
// inside a PROHIBITED or REQ_AUTHORISATION zone (G-03).
func (s IdentStatus) IncidentStatus() bool {
	return s == IdentUnidentified || s == IdentUnknownOperator
}

// IdentReason is the stable reason code behind a status (04 §3.2).
type IdentReason string

const (
	// ReasonMatched means the serial matched a registered UAS and the
	// operator number, where checked, matched its owner.
	ReasonMatched IdentReason = "matched"
	// ReasonSessionBinding means an authenticated session bound the UAS.
	ReasonSessionBinding IdentReason = "session_binding"
	// ReasonUASSuspended means the UAS registration is suspended.
	ReasonUASSuspended IdentReason = "uas_suspended"
	// ReasonUASRevoked means the UAS registration is revoked.
	ReasonUASRevoked IdentReason = "uas_revoked"
	// ReasonOperatorSuspended means the operator registration is suspended.
	ReasonOperatorSuspended IdentReason = "operator_suspended"
	// ReasonOperatorRevoked means the operator registration is revoked.
	ReasonOperatorRevoked IdentReason = "operator_revoked"
	// ReasonSerialUnknown means the serial is not in the registry.
	ReasonSerialUnknown IdentReason = "serial_unknown"
	// ReasonNotASerial means the broadcast identity is not a serial number,
	// so it is never matched against the registry.
	ReasonNotASerial IdentReason = "not_a_serial"
	// ReasonOperatorAbsent means no operator registration was broadcast.
	ReasonOperatorAbsent IdentReason = "operator_absent"
	// ReasonOperatorMismatch means the broadcast operator number differs
	// from the registered owner's.
	ReasonOperatorMismatch IdentReason = "operator_mismatch"
	// ReasonOwnerUnknown means the UAS names an owner that is not in the
	// registry projection.
	ReasonOwnerUnknown IdentReason = "owner_unknown"
	// ReasonNotInRegistry means the aircraft is known but not (or not
	// yet) in the registry.
	ReasonNotInRegistry IdentReason = "not_in_registry"
	// ReasonSerialConflict means registry records conflict over the
	// serial; it is always flagged as a mismatch.
	ReasonSerialConflict IdentReason = "serial_conflict"
	// ReasonNoSerial means no serial was broadcast.
	ReasonNoSerial IdentReason = "no_serial"
	// ReasonRegistryUnavailable means the registry could not be consulted.
	ReasonRegistryUnavailable IdentReason = "registry_unavailable"
)

// IdentBasis says what the resolution rests on (04 §3.2, LESSONS R-05).
type IdentBasis string

const (
	// BasisAuthenticated is an operator credential bound to the UAS and a
	// live session (USSP).
	BasisAuthenticated IdentBasis = "authenticated"
	// BasisAsBroadcast is direct or network Remote ID, "as broadcast
	// and unverified". Every display of such a status says so.
	BasisAsBroadcast IdentBasis = "as_broadcast"
)

// Identification is the block carried in every track/telemetry/v1 and
// emitted on its own when it changes (04 §3.2). Pointer strings are nil
// for "unknown" or "none"; a vector's null is a nil pointer, never "".
type Identification struct {
	Status IdentStatus `json:"status"`
	Reason IdentReason `json:"reason"`
	// Serial as broadcast or bound (trimmed, case kept: LESSONS G-05).
	Serial *string `json:"serial"`
	// OperatorReg as broadcast (trimmed; may still carry the EU secret
	// suffix, which is compared away but reported as received).
	OperatorReg *string `json:"operator_reg"`
	// RegisteredOperatorReg is the owner's number when it differs from
	// the broadcast one (public part only).
	RegisteredOperatorReg *string `json:"registered_operator_reg"`
	// Mismatch is true for a registered serial with another operator's
	// number, or serial_conflict (LESSONS G-02).
	Mismatch bool `json:"mismatch"`
	// RegistryUASID is the registry's aircraft id when a serial resolved
	// to one; nil otherwise.
	RegistryUASID *string `json:"registry_uas_id,omitempty"`
	// Basis is the resolution basis; empty in vectors that predate it.
	Basis IdentBasis `json:"basis,omitempty"`
}
