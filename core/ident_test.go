package core

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBasisProviderWireForm pins the provider basis and its wire form
// inside an Identification (track/telemetry/v1).
func TestBasisProviderWireForm(t *testing.T) {
	if BasisProvider != "provider" {
		t.Fatalf("BasisProvider = %q", BasisProvider)
	}
	raw, err := json.Marshal(Identification{Status: IdentUnidentified, Reason: ReasonNoSerial, Basis: BasisProvider})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"basis":"provider"`) {
		t.Fatalf("marshalled %s", raw)
	}
	var back Identification
	if err := json.Unmarshal(raw, &back); err != nil || back.Basis != BasisProvider {
		t.Fatalf("round trip: %v, basis %q", err, back.Basis)
	}
}

// TestJudged covers every reason: only the two whose input was not
// consulted are not judged (presence and absence over one table), and a
// nil identification is not judged.
func TestJudged(t *testing.T) {
	for _, c := range []struct {
		reason IdentReason
		want   bool
	}{
		{ReasonMatched, true},
		{ReasonSessionBinding, true},
		{ReasonUASSuspended, true},
		{ReasonUASRevoked, true},
		{ReasonOperatorSuspended, true},
		{ReasonOperatorRevoked, true},
		{ReasonSerialUnknown, true},
		{ReasonNotASerial, true},
		{ReasonOperatorAbsent, true},
		{ReasonOperatorMismatch, true},
		{ReasonOwnerUnknown, true},
		{ReasonNotInRegistry, true},
		{ReasonSerialConflict, true},
		{ReasonNoSerial, true},
		{ReasonRegistryUnavailable, false},
		{ReasonDetailsUnavailable, false},
		{IdentReason("a_reason_from_a_later_version"), true},
	} {
		if got := c.reason.Judged(); got != c.want {
			t.Errorf("%q.Judged() = %v, want %v", c.reason, got, c.want)
		}
		id := &Identification{Status: IdentUnidentified, Reason: c.reason}
		if got := id.Judged(); got != c.want {
			t.Errorf("Identification{%q}.Judged() = %v, want %v", c.reason, got, c.want)
		}
	}
	var none *Identification
	if none.Judged() {
		t.Error("a nil identification is judged")
	}
	if ReasonDetailsUnavailable != "details_unavailable" {
		t.Fatalf("ReasonDetailsUnavailable = %q", ReasonDetailsUnavailable)
	}
}
