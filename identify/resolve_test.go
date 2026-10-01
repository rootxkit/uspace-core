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
		// An unrecognised status fails safe: it is a suspension, never
		// active. Its twin: "active" in any case and spacing registers.
		{"unrecognised-uas-status-is-suspended", ptr("SN-PENDING"), ptr(regActive),
			wantIdent{core.IdentSuspended, core.ReasonUASSuspended, ptr("SN-PENDING"), ptr(regActive), nil, false, ptr("d-other-status"), bc}, false},
		{"active-status-case-and-space-registers", ptr("SN-ACTIVE-CASE"), ptr(regActive),
			wantIdent{core.IdentRegistered, core.ReasonMatched, ptr("SN-ACTIVE-CASE"), ptr(regActive), nil, false, ptr("d-active-case"), bc}, false},
		{"unrecognised-operator-status-is-suspended", ptr("SN-OP-OTHER"), ptr("GEOOTHR00000001"),
			wantIdent{core.IdentSuspended, core.ReasonOperatorSuspended, ptr("SN-OP-OTHER"), ptr("GEOOTHR00000001"), nil, false, ptr("d-op-other"), bc}, false},
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
		{"owner-unknown-binding-is-the-proof", reg, "d-owner-missing",
			wantIdent{core.IdentRegistered, core.ReasonSessionBinding, ptr("SN-OM"), nil, nil, false, ptr("d-owner-missing"), au}},
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
		{"unrecognised-status-is-suspended", reg, "d-other-status",
			wantIdent{core.IdentSuspended, core.ReasonUASSuspended, ptr("SN-PENDING"), ptr(regActive), nil, false, ptr("d-other-status"), au}},
		{"empty-drone-id-names-nothing", reg, "  ",
			wantIdent{core.IdentUnknownOperator, core.ReasonNotInRegistry, nil, nil, nil, false, nil, au}},
		{"registry-unavailable", nil, "d-active",
			wantIdent{core.IdentUnknownOperator, core.ReasonRegistryUnavailable, nil, nil, nil, false, ptr("d-active"), au}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { check(t, identify.ResolveBound(c.reg, c.droneID), c.want) })
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
