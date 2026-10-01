//nolint:misspell // serial.Normalize is the API name fixed in docs/PLAN.md §3.6 (Go spelling)
package identify

import (
	"strings"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/odid"
	"github.com/rootxkit/uspace-core/regnum"
	"github.com/rootxkit/uspace-core/serial"
)

// RemoteIDIdentity is the identity a direct Remote ID observation carries:
// its Basic ID and Operator ID, as decoded.
type RemoteIDIdentity struct {
	// Identified is false for a transmitter that sent no Basic ID.
	Identified bool
	// UAID is the Basic ID identity, case kept.
	UAID string
	// IDType is the Basic ID identity type; only odid.IDTypeSerial is
	// looked up (I-05).
	IDType odid.IDType
	// OperatorID is the Operator ID as broadcast; nil when none was.
	OperatorID *string
}

// clean trims a broadcast value; empty after the trim is nil.
func clean(value *string) *string {
	if value == nil {
		return nil
	}
	return nonEmpty(strings.TrimSpace(*value))
}

func nonEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// cleanSerial is serial.Normalize on an optional serial; empty is nil.
func cleanSerial(value *string) *string {
	if value == nil {
		return nil
	}
	return nonEmpty(serial.Normalize(*value))
}

// ResolveBroadcast resolves a broadcast serial and operator registration
// number against the registry (G-01, G-02, G-04, G-05) on the broadcast
// basis. Both inputs are trimmed and echoed as cleaned; an empty value is
// nil. A nil reg is Unavailable.
func ResolveBroadcast(reg Lookup, sn, operatorReg *string) core.Identification {
	sn, operatorReg = cleanSerial(sn), clean(operatorReg)
	if sn == nil {
		return noSerial(operatorReg)
	}
	if reg == nil {
		return Unavailable(sn, operatorReg)
	}
	id := core.Identification{Serial: sn, OperatorReg: operatorReg, Basis: core.BasisAsBroadcast}
	uas, m := reg.UASBySerial(*sn)
	if m != MatchExact && m != MatchFolded {
		// No aircraft, or more than one candidate: nothing registered is
		// flying, whatever operator is claimed.
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonSerialUnknown
		return id
	}
	id.RegistryUASID = nonEmpty(uas.DroneID)
	if !uas.InRegistry {
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonNotInRegistry
		return id
	}
	var owner *OperatorFacts
	if uas.OperatorID != nil {
		if o, ok := reg.Operator(*uas.OperatorID); ok {
			owner = &o
		}
	}
	if operatorReg != nil && owner != nil &&
		regnum.CompareKey(owner.RegistrationNumber) != regnum.CompareKey(*operatorReg) {
		id.Mismatch = true
		id.RegisteredOperatorReg = nonEmpty(regnum.PublicPart(owner.RegistrationNumber))
	}
	switch status, reason, ok := notInGoodStanding(uas, owner); {
	case ok:
		id.Status, id.Reason = status, reason
	case uas.OperatorID == nil:
		// Our own fleet: registered on its serial alone (G-01).
		id.Status, id.Reason = core.IdentRegistered, core.ReasonMatched
	case owner == nil:
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonOwnerUnknown
	case operatorReg == nil:
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonOperatorAbsent
	case id.Mismatch:
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonOperatorMismatch
	default:
		id.Status, id.Reason = core.IdentRegistered, core.ReasonMatched
	}
	return id
}

// ResolveRemoteID resolves a direct Remote ID identity block on the
// broadcast basis. Only a serial (ID type 1) is looked up (I-05): another
// type with an identity is unknown_operator / not_a_serial and never
// names a registry aircraft; a blank identity of any type, or no Basic ID
// at all, is unidentified.
func ResolveRemoteID(reg Lookup, id RemoteIDIdentity) core.Identification {
	if !id.Identified {
		return ResolveBroadcast(reg, nil, id.OperatorID)
	}
	if id.IDType != odid.IDTypeSerial {
		if strings.TrimSpace(id.UAID) == "" {
			return ResolveBroadcast(reg, nil, id.OperatorID)
		}
		return core.Identification{
			Status:      core.IdentUnknownOperator,
			Reason:      core.ReasonNotASerial,
			OperatorReg: clean(id.OperatorID),
			Basis:       core.BasisAsBroadcast,
		}
	}
	return ResolveBroadcast(reg, &id.UAID, id.OperatorID)
}

// ResolveBound resolves a track whose aircraft an authenticated session
// binding names, on the authenticated basis: the binding is the proof,
// not anything the aircraft broadcasts (G-01). A bound aircraft the
// projection does not hold yet is registered (the read may predate the
// registration); one the projection marks as not in the registry is
// unknown_operator / not_in_registry; a suspended or revoked UAS or owner
// is suspended; a UAS whose owner the projection does not hold is
// unknown_operator / owner_unknown, as for a broadcast (no vector pins a
// bound aircraft with a missing owner). Serial and operator number are the registry's. An empty
// drone id names no aircraft and is unknown_operator / not_in_registry.
//
// A nil reg is unknown_operator / registry_unavailable, not registered:
// the session binding proves which aircraft this is, not that its
// registration is valid. A registry read that predates the registration
// is a registry that was consulted and holds no row yet; a registry that
// could not be consulted cannot say the UAS or its operator is not
// suspended or revoked.
func ResolveBound(reg Lookup, droneID string) core.Identification {
	id := core.Identification{Basis: core.BasisAuthenticated}
	droneID = strings.TrimSpace(droneID)
	if droneID == "" {
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonNotInRegistry
		return id
	}
	id.RegistryUASID = &droneID
	if reg == nil {
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonRegistryUnavailable
		return id
	}
	uas, ok := reg.UASByID(droneID)
	if !ok {
		id.Status, id.Reason = core.IdentRegistered, core.ReasonSessionBinding
		return id
	}
	id.Serial = nonEmpty(serial.Normalize(uas.Serial))
	if !uas.InRegistry {
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonNotInRegistry
		return id
	}
	var owner *OperatorFacts
	if uas.OperatorID != nil {
		if o, found := reg.Operator(*uas.OperatorID); found {
			owner = &o
			id.OperatorReg = nonEmpty(regnum.PublicPart(o.RegistrationNumber))
		}
	}
	switch status, reason, bad := notInGoodStanding(uas, owner); {
	case bad:
		id.Status, id.Reason = status, reason
	case uas.OperatorID != nil && owner == nil:
		// The binding proves which aircraft this is, not that its owner
		// is in good standing: as for a broadcast.
		id.Status, id.Reason = core.IdentUnknownOperator, core.ReasonOwnerUnknown
	default:
		id.Status, id.Reason = core.IdentRegistered, core.ReasonSessionBinding
	}
	return id
}

// SerialConflict is the verdict on a broadcast of one of our serials heard
// where our authenticated telemetry says the aircraft is not (S-10, I-08;
// JudgeFleet's VerdictConflict): a separate unverified track,
// unknown_operator / serial_conflict with mismatch set, naming no registry
// aircraft.
func SerialConflict(sn string, operatorReg *string) core.Identification {
	return core.Identification{
		Status:      core.IdentUnknownOperator,
		Reason:      core.ReasonSerialConflict,
		Serial:      nonEmpty(serial.Normalize(sn)),
		OperatorReg: clean(operatorReg),
		Mismatch:    true,
		Basis:       core.BasisAsBroadcast,
	}
}

// Unavailable is a broadcast identity the registry could not be consulted
// for (spec 04 section 3.2): unknown_operator / registry_unavailable on
// the broadcast basis. A broadcast without a serial needs no registry and
// stays unidentified / no_serial.
func Unavailable(sn, operatorReg *string) core.Identification {
	sn, operatorReg = cleanSerial(sn), clean(operatorReg)
	if sn == nil {
		return noSerial(operatorReg)
	}
	return core.Identification{
		Status:      core.IdentUnknownOperator,
		Reason:      core.ReasonRegistryUnavailable,
		Serial:      sn,
		OperatorReg: operatorReg,
		Basis:       core.BasisAsBroadcast,
	}
}

func noSerial(operatorReg *string) core.Identification {
	return core.Identification{
		Status:      core.IdentUnidentified,
		Reason:      core.ReasonNoSerial,
		OperatorReg: operatorReg,
		Basis:       core.BasisAsBroadcast,
	}
}

// notInGoodStanding is the status and reason when the UAS or its owner is
// not in good standing: the UAS's own status first, then its owner's. A
// revoked or suspended registration is suspended; an unrecognised one is
// unknown_operator, so that it raises an identification incident where
// an unknown aircraft would (G-03), with not_in_registry for the UAS and
// owner_unknown for the owner.
func notInGoodStanding(uas UASFacts, owner *OperatorFacts) (core.IdentStatus, core.IdentReason, bool) {
	switch classify(uas.RegistrationStatus) {
	case standingActive:
	case standingRevoked:
		return core.IdentSuspended, core.ReasonUASRevoked, true
	case standingSuspended:
		return core.IdentSuspended, core.ReasonUASSuspended, true
	case standingUnrecognised:
		return core.IdentUnknownOperator, core.ReasonNotInRegistry, true
	}
	if owner == nil {
		return "", "", false
	}
	switch classify(owner.Status) {
	case standingActive:
	case standingRevoked:
		return core.IdentSuspended, core.ReasonOperatorRevoked, true
	case standingSuspended:
		return core.IdentSuspended, core.ReasonOperatorSuspended, true
	case standingUnrecognised:
		return core.IdentUnknownOperator, core.ReasonOwnerUnknown, true
	}
	return "", "", false
}
