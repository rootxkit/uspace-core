package identify_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/identify"
	"github.com/rootxkit/uspace-core/odid"
	"github.com/rootxkit/uspace-core/vectors"
)

type vecOperator struct {
	OperatorID         string `json:"operator_id"`
	RegistrationNumber string `json:"registration_number"`
	Status             string `json:"status"`
}

type vecUAS struct {
	DroneID            string  `json:"drone_id"`
	Label              string  `json:"label"`
	Serial             string  `json:"serial"`
	RegistrationStatus string  `json:"registration_status"`
	UASOperatorID      *string `json:"uas_operator_id"`
	InRegistry         bool    `json:"in_registry"`
}

type vecRegistry struct {
	Operators []vecOperator `json:"operators"`
	UAS       []vecUAS      `json:"uas"`
	Notes     []string      `json:"notes"`
}

func (r vecRegistry) snapshot() *identify.Snapshot {
	ops := make([]identify.OperatorFacts, 0, len(r.Operators))
	for _, o := range r.Operators {
		ops = append(ops, identify.OperatorFacts{OperatorID: o.OperatorID, RegistrationNumber: o.RegistrationNumber, Status: o.Status})
	}
	uas := make([]identify.UASFacts, 0, len(r.UAS))
	for _, u := range r.UAS {
		uas = append(uas, identify.UASFacts{
			DroneID: u.DroneID, Label: u.Label, Serial: u.Serial,
			RegistrationStatus: u.RegistrationStatus, OperatorID: u.UASOperatorID, InRegistry: u.InRegistry,
		})
	}
	return identify.NewSnapshot(ops, uas)
}

type vecRemoteID struct {
	Identified bool    `json:"identified"`
	UAID       string  `json:"ua_id"`
	IDType     uint8   `json:"id_type"`
	OperatorID *string `json:"operator_id"`
}

type vecIdentInput struct {
	Kind             string       `json:"kind"`
	Serial           *string      `json:"serial"`
	OperatorReg      *string      `json:"operator_reg"`
	RegistryOverride *vecRegistry `json:"registry_override"`
	RemoteID         *vecRemoteID `json:"remote_id"`
	DroneID          *string      `json:"drone_id"`
}

type vecIdentExpected struct {
	Status                string  `json:"status"`
	Reason                string  `json:"reason"`
	Serial                *string `json:"serial"`
	OperatorReg           *string `json:"operator_reg"`
	Mismatch              bool    `json:"mismatch"`
	RegisteredOperatorReg *string `json:"registered_operator_reg"`
	DroneID               *string `json:"drone_id"`
}

// reasonRenames are the predecessor's reason codes that spec 04 section
// 3.2 renamed. The vector file is generated from the predecessor and is
// never edited; the test maps the old code onto the spec's and counts
// every mapping, so a regenerated file with the new codes shows up here.
var reasonRenames = map[string]core.IdentReason{
	// Our own fleet: the spec's reason list has no "fleet"; the aircraft
	// is registered on its serial alone, reason matched (WP-7 brief).
	"fleet": core.ReasonMatched,
	// The predecessor's relay binding is the spec's authenticated session
	// binding (04 section 3.2, PLAN section 11 gap 5).
	"relay_binding": core.ReasonSessionBinding,
}

func TestVectorsIdentificationStatus(t *testing.T) {
	f := vectors.Load(t, "identification_status.json")
	var fx struct {
		Registry vecRegistry `json:"registry"`
	}
	vectors.Unmarshal(t, f.Fixtures, &fx)
	if len(fx.Registry.Operators) != 3 || len(fx.Registry.UAS) != 8 {
		t.Fatalf("fixture registry changed: %d operators, %d uas", len(fx.Registry.Operators), len(fx.Registry.UAS))
	}
	fixture := fx.Registry.snapshot()

	var incident []string
	f.Header(t, "incident_statuses", &incident)
	for _, s := range []core.IdentStatus{core.IdentRegistered, core.IdentSuspended, core.IdentUnknownOperator, core.IdentUnidentified} {
		if s.IncidentStatus() != slices.Contains(incident, string(s)) {
			t.Errorf("incident_statuses %v disagrees with %s.IncidentStatus()", incident, s)
		}
	}

	ran := map[string]int{}
	renamed := map[string]int{}
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in vecIdentInput
		var exp vecIdentExpected
		c.Decode(t, &in, &exp)
		reg := fixture
		if in.RegistryOverride != nil {
			reg = in.RegistryOverride.snapshot()
		}
		var got core.Identification
		wantBasis := core.BasisAsBroadcast
		switch in.Kind {
		case "broadcast":
			got = identify.ResolveBroadcast(reg, in.Serial, in.OperatorReg)
		case "remote_id_block":
			if in.RemoteID == nil {
				t.Fatal("remote_id_block case without remote_id")
			}
			got = identify.ResolveRemoteID(reg, identify.RemoteIDIdentity{
				Identified: in.RemoteID.Identified, UAID: in.RemoteID.UAID,
				IDType: odid.IDType(in.RemoteID.IDType), OperatorID: in.RemoteID.OperatorID,
			})
		case "bound":
			if in.DroneID == nil {
				t.Fatal("bound case without drone_id")
			}
			got = identify.ResolveBound(reg, *in.DroneID)
			wantBasis = core.BasisAuthenticated
		case "serial_conflict":
			if in.Serial == nil {
				t.Fatal("serial_conflict case without serial")
			}
			got = identify.SerialConflict(*in.Serial, in.OperatorReg)
		default:
			t.Fatalf("unknown kind %q", in.Kind)
		}
		ran[in.Kind]++

		wantReason := core.IdentReason(exp.Reason)
		if to, ok := reasonRenames[exp.Reason]; ok {
			renamed[exp.Reason]++
			t.Logf("reason %q is the spec's %q (04 section 3.2)", exp.Reason, to)
			wantReason = to
		}
		if got.Status != core.IdentStatus(exp.Status) {
			t.Errorf("status %q, want %q", got.Status, exp.Status)
		}
		if got.Reason != wantReason {
			t.Errorf("reason %q, want %q", got.Reason, wantReason)
		}
		vectors.EqualStrPtr(t, "serial", got.Serial, exp.Serial)
		vectors.EqualStrPtr(t, "operator_reg", got.OperatorReg, exp.OperatorReg)
		if got.Mismatch != exp.Mismatch {
			t.Errorf("mismatch %v, want %v", got.Mismatch, exp.Mismatch)
		}
		vectors.EqualStrPtr(t, "registered_operator_reg", got.RegisteredOperatorReg, exp.RegisteredOperatorReg)
		vectors.EqualStrPtr(t, "drone_id", got.RegistryUASID, exp.DroneID)
		if got.Basis != wantBasis {
			t.Errorf("basis %q, want %q", got.Basis, wantBasis)
		}
	})
	want := map[string]int{"broadcast": 24, "remote_id_block": 6, "bound": 6, "serial_conflict": 1}
	for k, n := range want {
		if ran[k] != n {
			t.Errorf("kind %s: ran %d cases, want %d", k, ran[k], n)
		}
	}
	for code := range reasonRenames {
		if renamed[code] == 0 {
			t.Errorf("rename of %q declared but no case uses it", code)
		}
	}
	t.Logf("ran %v; reasons renamed %v", ran, renamed)
}

type vecRelayRow struct {
	HeardAtS float64  `json:"heard_at_s"`
	LatDeg   *float64 `json:"lat_deg"`
	LonDeg   *float64 `json:"lon_deg"`
	Backlog  bool     `json:"backlog"`
	BehindS  float64  `json:"behind_s"`
	Source   string   `json:"source"`
}

type vecFleetInput struct {
	SerialIsOurs      bool          `json:"serial_is_ours"`
	RelayRows         []vecRelayRow `json:"relay_rows"`
	BroadcastPosition [2]float64    `json:"broadcast_position"`
	NowS              float64       `json:"now_s"`
	LiveForS          float64       `json:"live_for_s"`
	SpoofDistanceM    float64       `json:"spoof_distance_m"`
}

func (in vecFleetInput) fleet(t *testing.T) identify.FleetInput {
	t.Helper()
	rows := make([]identify.AuthRow, 0, len(in.RelayRows))
	for i, r := range in.RelayRows {
		row := identify.AuthRow{HeardAtS: r.HeardAtS, Backlog: r.Backlog, BehindS: r.BehindS, Source: r.Source}
		switch {
		case r.LatDeg != nil && r.LonDeg != nil:
			row.Pos = &core.LatLon{LatDeg: *r.LatDeg, LonDeg: *r.LonDeg}
		case r.LatDeg != nil || r.LonDeg != nil:
			t.Fatalf("relay_rows[%d] has only one coordinate", i)
		}
		rows = append(rows, row)
	}
	return identify.FleetInput{
		SerialIsOurs: in.SerialIsOurs, Rows: rows,
		Broadcast: core.LatLon{LatDeg: in.BroadcastPosition[0], LonDeg: in.BroadcastPosition[1]},
		NowS:      in.NowS, LiveForS: in.LiveForS, SpoofDistanceM: in.SpoofDistanceM,
	}
}

type vecFleetExpected struct {
	Verdict            string   `json:"verdict"`
	ApartM             *float64 `json:"apart_m"`
	IgnoredHistoryRows int      `json:"ignored_history_rows"`
}

// tolApartM mirrors the file's tolerance header for apart_m.
const tolApartM = 0.01

func TestVectorsFleetMatch(t *testing.T) {
	f := vectors.Load(t, "fleet_match.json")
	if tol, ok := f.FloatTolerance("apart_m"); !ok || tol != tolApartM {
		t.Fatalf("apart_m tolerance is %v (%v), the test applies %v", tol, ok, tolApartM)
	}
	verdicts := map[string]int{}
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in vecFleetInput
		var exp vecFleetExpected
		c.Decode(t, &in, &exp)
		got := identify.JudgeFleet(in.fleet(t))
		if got.Verdict != identify.Verdict(exp.Verdict) {
			t.Errorf("verdict %q, want %q", got.Verdict, exp.Verdict)
		}
		vectors.NearPtr(t, "apart_m", got.ApartM, exp.ApartM, tolApartM)
		if got.IgnoredHistoryRows != exp.IgnoredHistoryRows {
			t.Errorf("ignored_history_rows %d, want %d", got.IgnoredHistoryRows, exp.IgnoredHistoryRows)
		}
		verdicts[exp.Verdict]++
	})
	// Every verdict is exercised by at least one case (E-01).
	for _, v := range []identify.Verdict{identify.VerdictStranger, identify.VerdictWithhold, identify.VerdictAsOurs, identify.VerdictConflict} {
		if verdicts[string(v)] == 0 {
			t.Errorf("no case expects verdict %q", v)
		}
	}
	t.Logf("verdicts %v", verdicts)
}

// vectorInputs returns the raw inputs of a vector file, for fuzz seeds.
func vectorInputs(f *testing.F, name string) []json.RawMessage {
	f.Helper()
	file := vectors.Load(f, name)
	out := make([]json.RawMessage, 0, len(file.Cases))
	for _, c := range file.Cases {
		out = append(out, c.Input)
	}
	return out
}
