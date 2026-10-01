package core

// IdentStatus is one of the four identification statuses (04 §3.2,
// LESSONS G-01).
type IdentStatus string

const (
	IdentRegistered      IdentStatus = "registered"
	IdentSuspended       IdentStatus = "suspended"
	IdentUnknownOperator IdentStatus = "unknown_operator"
	IdentUnidentified    IdentStatus = "unidentified"
)

// IncidentStatus reports whether the status raises `identification`
// inside a PROHIBITED or REQ_AUTHORISATION zone (G-03).
func (s IdentStatus) IncidentStatus() bool {
	return s == IdentUnidentified || s == IdentUnknownOperator
}

// IdentReason is the stable reason code behind a status (04 §3.2).
type IdentReason string

const (
	ReasonMatched             IdentReason = "matched"
	ReasonSessionBinding      IdentReason = "session_binding"
	ReasonUASSuspended        IdentReason = "uas_suspended"
	ReasonUASRevoked          IdentReason = "uas_revoked"
	ReasonOperatorSuspended   IdentReason = "operator_suspended"
	ReasonOperatorRevoked     IdentReason = "operator_revoked"
	ReasonSerialUnknown       IdentReason = "serial_unknown"
	ReasonNotASerial          IdentReason = "not_a_serial"
	ReasonOperatorAbsent      IdentReason = "operator_absent"
	ReasonOperatorMismatch    IdentReason = "operator_mismatch"
	ReasonOwnerUnknown        IdentReason = "owner_unknown"
	ReasonNotInRegistry       IdentReason = "not_in_registry"
	ReasonSerialConflict      IdentReason = "serial_conflict"
	ReasonNoSerial            IdentReason = "no_serial"
	ReasonRegistryUnavailable IdentReason = "registry_unavailable"
)

// IdentBasis says what the resolution rests on (04 §3.2, LESSONS R-05).
type IdentBasis string

const (
	// BasisAuthenticated: an operator credential bound to the UAS and a
	// live session (USSP).
	BasisAuthenticated IdentBasis = "authenticated"
	// BasisAsBroadcast: direct or network Remote ID, "as broadcast and
	// unverified". Every display of such a status says so.
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
