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
