package identify_test

import (
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/identify"
	"github.com/rootxkit/uspace-core/odid"
)

func ptr(s string) *string { return &s }

func str(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

const (
	opActive    = "op-active"
	opSuspended = "op-suspended"
	opRevoked   = "op-revoked"
	opMissing   = "op-missing"
	opOther     = "op-other-status"
	regActive   = "GEOabcd1234efgh"
)

// registry is a small projection with one aircraft per row of the table.
func registry() *identify.Snapshot {
	return identify.NewSnapshot(
		[]identify.OperatorFacts{
			{OperatorID: opActive, RegistrationNumber: regActive, Status: identify.StatusActive},
			{OperatorID: opSuspended, RegistrationNumber: "GEOSUSP00000001", Status: identify.StatusSuspended},
			{OperatorID: opRevoked, RegistrationNumber: "GEOREVK00000001", Status: identify.StatusRevoked},
			{OperatorID: opOther, RegistrationNumber: "GEOOTHR00000001", Status: "under_review"},
		},
		[]identify.UASFacts{
			{DroneID: "d-active", Serial: "SN-A", RegistrationStatus: identify.StatusActive, OperatorID: ptr(opActive), InRegistry: true},
			{DroneID: "d-uas-susp", Serial: "SN-US", RegistrationStatus: identify.StatusSuspended, OperatorID: ptr(opActive), InRegistry: true},
			{DroneID: "d-uas-rev", Serial: "SN-UR", RegistrationStatus: identify.StatusRevoked, OperatorID: ptr(opActive), InRegistry: true},
			{DroneID: "d-op-susp", Serial: "SN-OS", RegistrationStatus: identify.StatusActive, OperatorID: ptr(opSuspended), InRegistry: true},
			{DroneID: "d-op-rev", Serial: "SN-OR", RegistrationStatus: identify.StatusActive, OperatorID: ptr(opRevoked), InRegistry: true},
			{DroneID: "d-owner-missing", Serial: "SN-OM", RegistrationStatus: identify.StatusActive, OperatorID: ptr(opMissing), InRegistry: true},
			{DroneID: "d-fleet", Serial: "SN-F", RegistrationStatus: "", InRegistry: true},
			{DroneID: "d-orphan", Serial: "SN-ORPH", RegistrationStatus: identify.StatusActive, InRegistry: false},
			{DroneID: "d-case", Serial: "SN-CASE", RegistrationStatus: " Suspended ", OperatorID: ptr(opActive), InRegistry: true},
			{DroneID: "d-other-status", Serial: "SN-PENDING", RegistrationStatus: "pending", OperatorID: ptr(opActive), InRegistry: true},
			{DroneID: "d-active-case", Serial: "SN-ACTIVE-CASE", RegistrationStatus: " Active ", OperatorID: ptr(opActive), InRegistry: true},
			{DroneID: "d-op-other", Serial: "SN-OP-OTHER", RegistrationStatus: identify.StatusActive, OperatorID: ptr(opOther), InRegistry: true},
		},
	)
}

type wantIdent struct {
	status   core.IdentStatus
	reason   core.IdentReason
	serial   *string
	opReg    *string
	regOpReg *string
	mismatch bool
	uasID    *string
	basis    core.IdentBasis
}

func check(t *testing.T, got core.Identification, w wantIdent) {
	t.Helper()
	if got.Status != w.status || got.Reason != w.reason {
		t.Errorf("%s / %s, want %s / %s", got.Status, got.Reason, w.status, w.reason)
	}
	if str(got.Serial) != str(w.serial) {
		t.Errorf("serial %s, want %s", str(got.Serial), str(w.serial))
	}
	if str(got.OperatorReg) != str(w.opReg) {
		t.Errorf("operator_reg %s, want %s", str(got.OperatorReg), str(w.opReg))
	}
	if str(got.RegisteredOperatorReg) != str(w.regOpReg) {
		t.Errorf("registered_operator_reg %s, want %s", str(got.RegisteredOperatorReg), str(w.regOpReg))
	}
	if got.Mismatch != w.mismatch {
		t.Errorf("mismatch %v, want %v", got.Mismatch, w.mismatch)
	}
	if str(got.RegistryUASID) != str(w.uasID) {
		t.Errorf("registry_uas_id %s, want %s", str(got.RegistryUASID), str(w.uasID))
	}
	if got.Basis != w.basis {
		t.Errorf("basis %q, want %q", got.Basis, w.basis)
	}
}

// TestResolveBroadcastTable runs every row of the table, each reason next
// to the registered case it departs from (E-01).
func TestResolveBroadcastTable(t *testing.T) {
	reg := registry()
	bc := core.BasisAsBroadcast
	cases := []struct {
		name      string
		serial    *string
		opReg     *string
		want      wantIdent
		incidents bool
	}{
		{"registered", ptr("SN-A"), ptr(regActive),
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-A"), ptr(regActive), nil, false, ptr("d-active"), bc}, false},
		{"registered-folded-trimmed-secret", ptr("  sn-a "), ptr(" GEOABCD1234efgh-x9z "),
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("sn-a"), ptr("GEOABCD1234efgh-x9z"), nil, false, ptr("d-active"), bc}, false},
		// The secret is stripped after a lower-case prefix too (regnum
		// matches the head ignoring its case), so this is the owner.
		{"lower-case-prefix-with-secret-matches", ptr("SN-A"), ptr("geoabcd1234efgh-x9z"),
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-A"), ptr("geoabcd1234efgh-x9z"), nil, false, ptr("d-active"), bc}, false},
		// Its twin: a lower-case number with the secret that is not the
		// owner's is still a mismatch.
		{"lower-case-other-number-with-secret-mismatch", ptr("SN-A"), ptr("geoother0000001-x9z"),
			wantIdent{core.IdentUnknownOperator, core.ReasonOperatorMismatch, ptr("SN-A"), ptr("geoother0000001-x9z"), ptr(regActive), true, ptr("d-active"), bc}, true},
		{"lower-case-prefix-without-secret-matches", ptr("SN-A"), ptr("geoabcd1234efgh"),
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-A"), ptr("geoabcd1234efgh"), nil, false, ptr("d-active"), bc}, false},
		{"no-serial", nil, ptr(regActive),
			wantIdent{core.IdentUnidentified, core.ReasonNoSerial, nil, ptr(regActive), nil, false, nil, bc}, true},
		{"blank-serial", ptr(" \t "), ptr("  "),
			wantIdent{core.IdentUnidentified, core.ReasonNoSerial, nil, nil, nil, false, nil, bc}, true},
		{"serial-unknown", ptr("SN-NOBODY"), ptr(regActive),
			wantIdent{core.IdentUnknownOperator, core.ReasonSerialUnknown, ptr("SN-NOBODY"), ptr(regActive), nil, false, nil, bc}, true},
		{"not-in-registry", ptr("SN-ORPH"), nil,
			wantIdent{core.IdentUnknownOperator, core.ReasonNotInRegistry, ptr("SN-ORPH"), nil, nil, false, ptr("d-orphan"), bc}, true},
		{"uas-suspended", ptr("SN-US"), ptr(regActive),
			wantIdent{core.IdentSuspended, core.ReasonUASSuspended, ptr("SN-US"), ptr(regActive), nil, false, ptr("d-uas-susp"), bc}, false},
		{"uas-suspended-status-case-and-space", ptr("SN-CASE"), ptr(regActive),
			wantIdent{core.IdentSuspended, core.ReasonUASSuspended, ptr("SN-CASE"), ptr(regActive), nil, false, ptr("d-case"), bc}, false},
		{"uas-suspended-mismatch-flagged", ptr("SN-US"), ptr("GEOOTHER0000001"),
			wantIdent{core.IdentSuspended, core.ReasonUASSuspended, ptr("SN-US"), ptr("GEOOTHER0000001"), ptr(regActive), true, ptr("d-uas-susp"), bc}, false},
		{"uas-revoked", ptr("SN-UR"), ptr(regActive),
			wantIdent{core.IdentSuspended, core.ReasonUASRevoked, ptr("SN-UR"), ptr(regActive), nil, false, ptr("d-uas-rev"), bc}, false},
		{"operator-suspended", ptr("SN-OS"), ptr("GEOSUSP00000001"),
			wantIdent{core.IdentSuspended, core.ReasonOperatorSuspended, ptr("SN-OS"), ptr("GEOSUSP00000001"), nil, false, ptr("d-op-susp"), bc}, false},
		{"operator-revoked", ptr("SN-OR"), nil,
			wantIdent{core.IdentSuspended, core.ReasonOperatorRevoked, ptr("SN-OR"), nil, nil, false, ptr("d-op-rev"), bc}, false},
		{"fleet-alone", ptr("SN-F"), nil,
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-F"), nil, nil, false, ptr("d-fleet"), bc}, false},
		{"fleet-ignores-operator", ptr("SN-F"), ptr("GEOANYTHING00001"),
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-F"), ptr("GEOANYTHING00001"), nil, false, ptr("d-fleet"), bc}, false},
		{"owner-unknown", ptr("SN-OM"), ptr(regActive),
			wantIdent{core.IdentUnknownOperator, core.ReasonOwnerUnknown, ptr("SN-OM"), ptr(regActive), nil, false, ptr("d-owner-missing"), bc}, true},
		{"operator-absent", ptr("SN-A"), nil,
			wantIdent{core.IdentUnknownOperator, core.ReasonOperatorAbsent, ptr("SN-A"), nil, nil, false, ptr("d-active"), bc}, true},
		{"operator-mismatch", ptr("SN-A"), ptr("GEOSUSP00000001"),
			wantIdent{core.IdentUnknownOperator, core.ReasonOperatorMismatch, ptr("SN-A"), ptr("GEOSUSP00000001"), ptr(regActive), true, ptr("d-active"), bc}, true},
		// An unrecognised status fails safe: never active, and in a status
		// that raises an identification incident (G-03), unlike suspended.
		// Its twin: "active" in any case and spacing registers.
		{"unrecognised-uas-status-raises", ptr("SN-PENDING"), ptr(regActive),
			wantIdent{core.IdentUnknownOperator, core.ReasonNotInRegistry, ptr("SN-PENDING"), ptr(regActive), nil, false, ptr("d-other-status"), bc}, true},
		{"active-status-case-and-space-registers", ptr("SN-ACTIVE-CASE"), ptr(regActive),
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-ACTIVE-CASE"), ptr(regActive), nil, false, ptr("d-active-case"), bc}, false},
		{"unrecognised-operator-status-raises", ptr("SN-OP-OTHER"), ptr("GEOOTHR00000001"),
			wantIdent{core.IdentUnknownOperator, core.ReasonOwnerUnknown, ptr("SN-OP-OTHER"), ptr("GEOOTHR00000001"), nil, false, ptr("d-op-other"), bc}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := identify.ResolveBroadcast(reg, c.serial, c.opReg)
			check(t, got, c.want)
			if got.Status.IncidentStatus() != c.incidents {
				t.Errorf("IncidentStatus %v, want %v", got.Status.IncidentStatus(), c.incidents)
			}
			if got.Mismatch && got.Status == core.IdentRegistered {
				t.Error("a mismatch is never registered (G-02)")
			}
		})
	}
}

// TestResolveBroadcastDoesNotAliasInputs: the echoed values are copies.
func TestResolveBroadcastDoesNotAliasInputs(t *testing.T) {
	sn, op := "SN-A", regActive
	got := identify.ResolveBroadcast(registry(), &sn, &op)
	if got.Serial == &sn || got.OperatorReg == &op {
		t.Fatal("the identification points into the caller's strings")
	}
}

// TestDetailsUnavailable: an F3411 network flight whose details were not
// fetched is unidentified / details_unavailable and not judged; its twins
// are the details once fetched: without a serial they are a judged
// no_serial (the source was asked), with a registered serial they resolve.
func TestDetailsUnavailable(t *testing.T) {
	bc := core.BasisAsBroadcast
	pending := identify.DetailsUnavailable(nil)
	check(t, pending, wantIdent{core.IdentUnidentified, core.ReasonDetailsUnavailable, nil, nil, nil, false, nil, bc})
	if pending.Judged() {
		t.Fatal("details_unavailable is judged")
	}
	check(t, identify.DetailsUnavailable(ptr(" GEOX ")),
		wantIdent{core.IdentUnidentified, core.ReasonDetailsUnavailable, nil, ptr("GEOX"), nil, false, nil, bc})
	check(t, identify.DetailsUnavailable(ptr("   ")),
		wantIdent{core.IdentUnidentified, core.ReasonDetailsUnavailable, nil, nil, nil, false, nil, bc})
	asked := identify.ResolveBroadcast(registry(), nil, nil)
	check(t, asked, wantIdent{core.IdentUnidentified, core.ReasonNoSerial, nil, nil, nil, false, nil, bc})
	if !asked.Judged() {
		t.Fatal("no_serial is not judged")
	}
	check(t, identify.ResolveBroadcast(registry(), ptr("SN-A"), ptr(regActive)),
		wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-A"), ptr(regActive), nil, false, ptr("d-active"), bc})
}

// TestRegistryUnavailable: no registry consulted, as opposed to a registry
// that knows nobody (E-02, PLAN section 11 gap 5: no vector).
func TestRegistryUnavailable(t *testing.T) {
	bc := core.BasisAsBroadcast
	check(t, identify.Unavailable(ptr(" SN-A "), ptr(" GEOX ")),
		wantIdent{core.IdentUnknownOperator, core.ReasonRegistryUnavailable, ptr("SN-A"), ptr("GEOX"), nil, false, nil, bc})
	check(t, identify.Unavailable(ptr("  "), ptr("GEOX")),
		wantIdent{core.IdentUnidentified, core.ReasonNoSerial, nil, ptr("GEOX"), nil, false, nil, bc})
	// A nil Lookup is the registry unavailable.
	check(t, identify.ResolveBroadcast(nil, ptr("SN-A"), ptr(regActive)),
		wantIdent{core.IdentUnknownOperator, core.ReasonRegistryUnavailable, ptr("SN-A"), ptr(regActive), nil, false, nil, bc})
	check(t, identify.ResolveRemoteID(nil, identify.RemoteIDIdentity{Identified: true, UAID: "SN-A", IDType: odid.IDTypeSerial}),
		wantIdent{core.IdentUnknownOperator, core.ReasonRegistryUnavailable, ptr("SN-A"), nil, nil, false, nil, bc})
	// Its twin: the same broadcast with a registry resolves.
	check(t, identify.ResolveBroadcast(registry(), ptr("SN-A"), ptr(regActive)),
		wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-A"), ptr(regActive), nil, false, ptr("d-active"), bc})
	// An empty registry is consulted and knows nobody: serial_unknown.
	check(t, identify.ResolveBroadcast(identify.NewSnapshot(nil, nil), ptr("SN-A"), ptr(regActive)),
		wantIdent{core.IdentUnknownOperator, core.ReasonSerialUnknown, ptr("SN-A"), ptr(regActive), nil, false, nil, bc})
}

func TestResolveRemoteID(t *testing.T) {
	reg := registry()
	bc := core.BasisAsBroadcast
	cases := []struct {
		name string
		id   identify.RemoteIDIdentity
		want wantIdent
	}{
		{"serial-registered", identify.RemoteIDIdentity{Identified: true, UAID: "SN-A", IDType: odid.IDTypeSerial, OperatorID: ptr(regActive)},
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-A"), ptr(regActive), nil, false, ptr("d-active"), bc}},
		{"serial-mismatch", identify.RemoteIDIdentity{Identified: true, UAID: "SN-A", IDType: odid.IDTypeSerial, OperatorID: ptr("GEOOTHER0000001")},
			wantIdent{core.IdentUnknownOperator, core.ReasonOperatorMismatch, ptr("SN-A"), ptr("GEOOTHER0000001"), ptr(regActive), true, ptr("d-active"), bc}},
		{"not-identified", identify.RemoteIDIdentity{Identified: false, UAID: "SN-A", IDType: odid.IDTypeSerial, OperatorID: ptr(" " + regActive)},
			wantIdent{core.IdentUnidentified, core.ReasonNoSerial, nil, ptr(regActive), nil, false, nil, bc}},
		{"blank-serial", identify.RemoteIDIdentity{Identified: true, UAID: " ", IDType: odid.IDTypeSerial},
			wantIdent{core.IdentUnidentified, core.ReasonNoSerial, nil, nil, nil, false, nil, bc}},
		{"caa-registration", identify.RemoteIDIdentity{Identified: true, UAID: "SN-A", IDType: odid.IDTypeCAARegistration, OperatorID: ptr(" " + regActive + " ")},
			wantIdent{core.IdentUnknownOperator, core.ReasonNotASerial, nil, ptr(regActive), nil, false, nil, bc}},
		{"utm-assigned", identify.RemoteIDIdentity{Identified: true, UAID: "SN-A", IDType: odid.IDTypeUTMAssigned},
			wantIdent{core.IdentUnknownOperator, core.ReasonNotASerial, nil, nil, nil, false, nil, bc}},
		{"type-none-is-not-a-serial", identify.RemoteIDIdentity{Identified: true, UAID: "SN-A", IDType: odid.IDTypeNone},
			wantIdent{core.IdentUnknownOperator, core.ReasonNotASerial, nil, nil, nil, false, nil, bc}},
		{"blank-session-id", identify.RemoteIDIdentity{Identified: true, UAID: "\t", IDType: odid.IDTypeSpecificSession, OperatorID: ptr(regActive)},
			wantIdent{core.IdentUnidentified, core.ReasonNoSerial, nil, ptr(regActive), nil, false, nil, bc}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { check(t, identify.ResolveRemoteID(reg, c.id), c.want) })
	}
}

func TestResolveBound(t *testing.T) {
	reg := registry()
	au := core.BasisAuthenticated
	cases := []struct {
		name    string
		reg     identify.Lookup
		droneID string
		want    wantIdent
	}{
		{"registered", reg, "d-active",
			wantIdent{core.IdentRegistered, core.ReasonSessionBinding, ptr("SN-A"), ptr(regActive), nil, false, ptr("d-active"), au}},
		{"fleet", reg, "d-fleet",
			wantIdent{core.IdentRegistered, core.ReasonSessionBinding, ptr("SN-F"), nil, nil, false, ptr("d-fleet"), au}},
		// The binding proves identity, not the owner's standing. Its twin
		// is "registered" above: an owner the projection holds.
		{"owner-unknown", reg, "d-owner-missing",
			wantIdent{core.IdentUnknownOperator, core.ReasonOwnerUnknown, ptr("SN-OM"), nil, nil, false, ptr("d-owner-missing"), au}},
		{"not-yet-projected", reg, "d-new",
			wantIdent{core.IdentRegistered, core.ReasonSessionBinding, nil, nil, nil, false, ptr("d-new"), au}},
		{"not-in-registry", reg, "d-orphan",
			wantIdent{core.IdentUnknownOperator, core.ReasonNotInRegistry, ptr("SN-ORPH"), nil, nil, false, ptr("d-orphan"), au}},
		{"uas-suspended", reg, "d-uas-susp",
			wantIdent{core.IdentSuspended, core.ReasonUASSuspended, ptr("SN-US"), ptr(regActive), nil, false, ptr("d-uas-susp"), au}},
		{"uas-revoked", reg, "d-uas-rev",
			wantIdent{core.IdentSuspended, core.ReasonUASRevoked, ptr("SN-UR"), ptr(regActive), nil, false, ptr("d-uas-rev"), au}},
		{"operator-suspended", reg, "d-op-susp",
			wantIdent{core.IdentSuspended, core.ReasonOperatorSuspended, ptr("SN-OS"), ptr("GEOSUSP00000001"), nil, false, ptr("d-op-susp"), au}},
		{"operator-revoked", reg, "d-op-rev",
			wantIdent{core.IdentSuspended, core.ReasonOperatorRevoked, ptr("SN-OR"), ptr("GEOREVK00000001"), nil, false, ptr("d-op-rev"), au}},
		{"unrecognised-status-raises", reg, "d-other-status",
			wantIdent{core.IdentUnknownOperator, core.ReasonNotInRegistry, ptr("SN-PENDING"), ptr(regActive), nil, false, ptr("d-other-status"), au}},
		{"unrecognised-operator-status-raises", reg, "d-op-other",
			wantIdent{core.IdentUnknownOperator, core.ReasonOwnerUnknown, ptr("SN-OP-OTHER"), ptr("GEOOTHR00000001"), nil, false, ptr("d-op-other"), au}},
		{"empty-drone-id-names-nothing", reg, "  ",
			wantIdent{core.IdentUnknownOperator, core.ReasonNotInRegistry, nil, nil, nil, false, nil, au}},
		// The binding proves identity, not a valid registration.
		{"registry-unavailable-is-not-registered", nil, "d-active",
			wantIdent{core.IdentUnknownOperator, core.ReasonRegistryUnavailable, nil, nil, nil, false, ptr("d-active"), au}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { check(t, identify.ResolveBound(c.reg, c.droneID), c.want) })
	}
}

// TestResolveBoundWith pairs every mismatch with its no-mismatch twin
// (E-01): the same aircraft with registrations that agree, or with one
// of the two absent (04 §3.2 errata 2026-10-22).
func TestResolveBoundWith(t *testing.T) {
	reg := registry()
	declared, intended := "GEO-UA-000999", "GEO-UA-000123"
	cases := []struct {
		name               string
		reg                identify.Lookup
		droneID            string
		in                 identify.BoundInputs
		status             core.IdentStatus
		reason             core.IdentReason
		mismatch           bool
		registration       *string
		intentRegistration *string
	}{
		{"mismatch", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr(declared), IntentRegistration: ptr(intended)},
			core.IdentRegistered, core.ReasonRegistrationMismatch, true, ptr(declared), ptr(intended)},
		{"agrees", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr(intended), IntentRegistration: ptr(intended)},
			core.IdentRegistered, core.ReasonSessionBinding, false, ptr(intended), ptr(intended)},
		{"agrees-trimmed-ascii-case", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr(" geo-ua-000123\t"), IntentRegistration: ptr(" " + intended + " ")},
			core.IdentRegistered, core.ReasonSessionBinding, false, ptr("geo-ua-000123"), ptr(intended)},
		// Only ASCII folds: U+212A (Kelvin sign) is not K, U+017F (long s)
		// is not s.
		{"kelvin-sign-is-another-registration", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr("GEO-UA-\u212a1"), IntentRegistration: ptr("GEO-UA-k1")},
			core.IdentRegistered, core.ReasonRegistrationMismatch, true, ptr("GEO-UA-\u212a1"), ptr("GEO-UA-k1")},
		{"long-s-is-another-registration", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr("GEO-\u017f1"), IntentRegistration: ptr("GEO-S1")},
			core.IdentRegistered, core.ReasonRegistrationMismatch, true, ptr("GEO-\u017f1"), ptr("GEO-S1")},
		{"declared-without-intent", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr(declared)},
			core.IdentRegistered, core.ReasonSessionBinding, false, ptr(declared), nil},
		{"intent-without-declared", reg, "d-active", identify.BoundInputs{IntentRegistration: ptr(intended)},
			core.IdentRegistered, core.ReasonSessionBinding, false, nil, nil},
		{"blank-declared-is-absent", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr(" \t "), IntentRegistration: ptr(intended)},
			core.IdentRegistered, core.ReasonSessionBinding, false, nil, nil},
		{"blank-intent-is-absent", reg, "d-active", identify.BoundInputs{DeclaredRegistration: ptr(declared), IntentRegistration: ptr("  ")},
			core.IdentRegistered, core.ReasonSessionBinding, false, ptr(declared), nil},
		{"not-yet-projected-mismatch", reg, "d-new", identify.BoundInputs{DeclaredRegistration: ptr(declared), IntentRegistration: ptr(intended)},
			core.IdentRegistered, core.ReasonRegistrationMismatch, true, ptr(declared), ptr(intended)},
		// A standing problem keeps its reason; the mismatch is still flagged.
		{"suspended-keeps-reason", reg, "d-uas-susp", identify.BoundInputs{DeclaredRegistration: ptr(declared), IntentRegistration: ptr(intended)},
			core.IdentSuspended, core.ReasonUASSuspended, true, ptr(declared), ptr(intended)},
		{"suspended-agrees", reg, "d-uas-susp", identify.BoundInputs{DeclaredRegistration: ptr(intended), IntentRegistration: ptr(intended)},
			core.IdentSuspended, core.ReasonUASSuspended, false, ptr(intended), ptr(intended)},
		{"owner-unknown-keeps-reason", reg, "d-owner-missing", identify.BoundInputs{DeclaredRegistration: ptr(declared), IntentRegistration: ptr(intended)},
			core.IdentUnknownOperator, core.ReasonOwnerUnknown, true, ptr(declared), ptr(intended)},
		// Not judged: the reason stays registry_unavailable, so nothing
		// rests on the flag (core.Identification.Judged).
		{"registry-unavailable-keeps-reason", nil, "d-active", identify.BoundInputs{DeclaredRegistration: ptr(declared), IntentRegistration: ptr(intended)},
			core.IdentUnknownOperator, core.ReasonRegistryUnavailable, true, ptr(declared), ptr(intended)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := identify.ResolveBoundWith(c.reg, c.droneID, c.in)
			if got.Status != c.status || got.Reason != c.reason || got.Mismatch != c.mismatch {
				t.Errorf("%s / %s mismatch %v, want %s / %s mismatch %v", got.Status, got.Reason, got.Mismatch, c.status, c.reason, c.mismatch)
			}
			if str(got.Registration) != str(c.registration) {
				t.Errorf("registration %s, want %s", str(got.Registration), str(c.registration))
			}
			if str(got.IntentRegistration) != str(c.intentRegistration) {
				t.Errorf("intent_registration %s, want %s", str(got.IntentRegistration), str(c.intentRegistration))
			}
			if got.Basis != core.BasisAuthenticated {
				t.Errorf("basis %q", got.Basis)
			}
			// Everything else is ResolveBound's.
			plain := identify.ResolveBound(c.reg, c.droneID)
			if str(got.Serial) != str(plain.Serial) || str(got.OperatorReg) != str(plain.OperatorReg) || str(got.RegistryUASID) != str(plain.RegistryUASID) || got.Status != plain.Status {
				t.Errorf("%+v differs from ResolveBound %+v beyond the registration", got, plain)
			}
			if !c.mismatch && got.Reason != plain.Reason {
				t.Errorf("reason %s without a mismatch, ResolveBound says %s", got.Reason, plain.Reason)
			}
		})
	}
}

// TestRegistrationCompared: both registrations present is a comparison
// (04 §3.2 errata 2026-10-23), whether they agree or not; either absent,
// or a nil block, is none.
func TestRegistrationCompared(t *testing.T) {
	reg := registry()
	for _, c := range []struct {
		name string
		in   identify.BoundInputs
		want bool
	}{
		{"agrees", identify.BoundInputs{DeclaredRegistration: ptr("R1"), IntentRegistration: ptr("r1")}, true},
		{"differs", identify.BoundInputs{DeclaredRegistration: ptr("R2"), IntentRegistration: ptr("R1")}, true},
		{"none_declared", identify.BoundInputs{IntentRegistration: ptr("R1")}, false},
		{"no_intent_registration", identify.BoundInputs{DeclaredRegistration: ptr("R1")}, false},
		{"blank_declared", identify.BoundInputs{DeclaredRegistration: ptr(" "), IntentRegistration: ptr("R1")}, false},
	} {
		id := identify.ResolveBoundWith(reg, "d-active", c.in)
		if got := id.RegistrationCompared(); got != c.want {
			t.Errorf("%s: RegistrationCompared %v, want %v", c.name, got, c.want)
		}
	}
	var none *core.Identification
	if none.RegistrationCompared() {
		t.Error("a nil block compared a registration")
	}
}

// TestResolveBoundWithCopiesInputs: the block never aliases the caller's
// strings, so a caller reusing its buffer cannot rewrite a published
// identification.
func TestResolveBoundWithCopiesInputs(t *testing.T) {
	declared, intended := "GEO-UA-000999", "GEO-UA-000123"
	got := identify.ResolveBoundWith(registry(), "d-active", identify.BoundInputs{DeclaredRegistration: &declared, IntentRegistration: &intended})
	if got.Registration == &declared || got.IntentRegistration == &intended {
		t.Fatal("the identification aliases an input")
	}
	declared, intended = "changed", "changed"
	if str(got.Registration) != "GEO-UA-000999" || str(got.IntentRegistration) != "GEO-UA-000123" {
		t.Fatalf("registration %s, intent_registration %s after the inputs changed", str(got.Registration), str(got.IntentRegistration))
	}
}

// TestRegistrationMismatchIsJudged: registration_mismatch rests on inputs
// that answered, so it is judged, and registered is no incident status
// (only identification_mismatch is raised, G-02).
func TestRegistrationMismatchIsJudged(t *testing.T) {
	if !core.ReasonRegistrationMismatch.Judged() {
		t.Fatal("registration_mismatch is not judged")
	}
	if core.ReasonRegistrationMismatch != "registration_mismatch" {
		t.Fatalf("wire value %q", core.ReasonRegistrationMismatch)
	}
	if core.IdentRegistered.IncidentStatus() {
		t.Fatal("registered raises an identification incident")
	}
}

func TestSerialConflict(t *testing.T) {
	bc := core.BasisAsBroadcast
	check(t, identify.SerialConflict(" SN-F ", ptr(" GEOX ")),
		wantIdent{core.IdentUnknownOperator, core.ReasonSerialConflict, ptr("SN-F"), ptr("GEOX"), nil, true, nil, bc})
	check(t, identify.SerialConflict("", ptr("")),
		wantIdent{core.IdentUnknownOperator, core.ReasonSerialConflict, nil, nil, nil, true, nil, bc})
	// Its twin: the same serial, not in conflict, is our fleet aircraft.
	check(t, identify.ResolveBroadcast(registry(), ptr("SN-F"), ptr("GEOX")),
		wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-F"), ptr("GEOX"), nil, false, ptr("d-fleet"), bc})
}

// lookupStub is a Lookup that is not a Snapshot, to show ResolveBroadcast
// relies on the interface alone and treats an unknown Match as no match.
type lookupStub struct{ m identify.Match }

func (l lookupStub) UASBySerial(string) (identify.UASFacts, identify.Match) {
	return identify.UASFacts{DroneID: "d-stub", InRegistry: true}, l.m
}
func (lookupStub) UASByID(string) (identify.UASFacts, bool) { return identify.UASFacts{}, false }
func (lookupStub) Operator(string) (identify.OperatorFacts, bool) {
	return identify.OperatorFacts{}, false
}

func TestResolveBroadcastTrustsOnlyAMatch(t *testing.T) {
	for _, m := range []identify.Match{identify.MatchNone, identify.MatchAmbiguous, identify.Match(99)} {
		got := identify.ResolveBroadcast(lookupStub{m}, ptr("X"), nil)
		if got.Reason != core.ReasonSerialUnknown || got.RegistryUASID != nil {
			t.Errorf("match %v: %s, uas %s", m, got.Reason, str(got.RegistryUASID))
		}
	}
	for _, m := range []identify.Match{identify.MatchExact, identify.MatchFolded} {
		got := identify.ResolveBroadcast(lookupStub{m}, ptr("X"), nil)
		if got.Reason != core.ReasonMatched || str(got.RegistryUASID) != "d-stub" {
			t.Errorf("match %v: %s, uas %s", m, got.Reason, str(got.RegistryUASID))
		}
	}
}
