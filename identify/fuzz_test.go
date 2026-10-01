package identify_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/identify"
	"github.com/rootxkit/uspace-core/odid"
)

var validReasons = map[core.IdentStatus]map[core.IdentReason]bool{
	core.IdentRegistered: {core.ReasonMatched: true, core.ReasonSessionBinding: true},
	core.IdentSuspended: {
		core.ReasonUASSuspended: true, core.ReasonUASRevoked: true,
		core.ReasonOperatorSuspended: true, core.ReasonOperatorRevoked: true,
	},
	core.IdentUnknownOperator: {
		core.ReasonSerialUnknown: true, core.ReasonNotASerial: true, core.ReasonOperatorAbsent: true,
		core.ReasonOperatorMismatch: true, core.ReasonOwnerUnknown: true, core.ReasonNotInRegistry: true,
		core.ReasonSerialConflict: true, core.ReasonRegistryUnavailable: true,
	},
	core.IdentUnidentified: {core.ReasonNoSerial: true},
}

// invariants every identification holds, whatever the input.
func invariants(t *testing.T, id core.Identification) {
	t.Helper()
	if !validReasons[id.Status][id.Reason] {
		t.Fatalf("status %q with reason %q", id.Status, id.Reason)
	}
	if id.Mismatch && id.Status == core.IdentRegistered {
		t.Fatal("a mismatch is registered (G-02)")
	}
	if id.Mismatch != (id.RegisteredOperatorReg != nil) && id.Reason != core.ReasonSerialConflict {
		t.Fatalf("mismatch %v with registered_operator_reg %v", id.Mismatch, id.RegisteredOperatorReg)
	}
	if id.Status == core.IdentUnidentified && id.Serial != nil {
		t.Fatalf("unidentified with serial %q", *id.Serial)
	}
	for _, p := range []*string{id.Serial, id.OperatorReg} {
		if p != nil && *p == "" {
			t.Fatal("an empty string where nil is meant")
		}
	}
	if id.Basis != core.BasisAsBroadcast {
		t.Fatalf("basis %q", id.Basis)
	}
}

func optional(s string, present bool) *string {
	if !present {
		return nil
	}
	return &s
}

// FuzzResolveBroadcast: any broadcast serial and operator number, seeded
// from the vectors, resolves without a panic to a consistent block.
func FuzzResolveBroadcast(f *testing.F) {
	reg := registry()
	for _, raw := range vectorInputs(f, "identification_status.json") {
		var in struct {
			Serial      *string         `json:"serial"`
			OperatorReg *string         `json:"operator_reg"`
			RemoteID    json.RawMessage `json:"remote_id"`
		}
		_ = json.Unmarshal(raw, &in) // seeds only; the vector test decodes strictly
		var sn, op string
		if in.Serial != nil {
			sn = *in.Serial
		}
		if in.OperatorReg != nil {
			op = *in.OperatorReg
		}
		f.Add(sn, in.Serial != nil, op, in.OperatorReg != nil)
	}
	f.Add("SN-A", true, "GEOabcd1234efgh-x9z", true)
	f.Add("\xff\x00", true, "-\xff-abc", true)
	f.Fuzz(func(t *testing.T, sn string, hasSerial bool, op string, hasOp bool) {
		got := identify.ResolveBroadcast(reg, optional(sn, hasSerial), optional(op, hasOp))
		invariants(t, got)
		invariants(t, identify.Unavailable(optional(sn, hasSerial), optional(op, hasOp)))
		invariants(t, identify.SerialConflict(sn, optional(op, hasOp)))
	})
}

// FuzzResolveRemoteID: any decoded Remote ID identity, seeded from the
// vectors.
func FuzzResolveRemoteID(f *testing.F) {
	reg := registry()
	for _, raw := range vectorInputs(f, "identification_status.json") {
		var in struct {
			RemoteID *struct {
				Identified bool    `json:"identified"`
				UAID       string  `json:"ua_id"`
				IDType     uint8   `json:"id_type"`
				OperatorID *string `json:"operator_id"`
			} `json:"remote_id"`
		}
		if json.Unmarshal(raw, &in) != nil || in.RemoteID == nil {
			continue
		}
		r := in.RemoteID
		var op string
		if r.OperatorID != nil {
			op = *r.OperatorID
		}
		f.Add(r.Identified, r.UAID, r.IDType, op, r.OperatorID != nil)
	}
	f.Fuzz(func(t *testing.T, identified bool, uaid string, idType uint8, op string, hasOp bool) {
		got := identify.ResolveRemoteID(reg, identify.RemoteIDIdentity{
			Identified: identified, UAID: uaid, IDType: odid.IDType(idType), OperatorID: optional(op, hasOp),
		})
		invariants(t, got)
		if odid.IDType(idType) != odid.IDTypeSerial && got.RegistryUASID != nil {
			t.Fatalf("ID type %d named registry aircraft %q (I-05)", idType, *got.RegistryUASID)
		}
	})
}

// FuzzJudgeFleet: any times, positions and thresholds, seeded from the
// vectors, give a verdict without a panic, and never a conflict without a
// finite distance beyond the threshold.
func FuzzJudgeFleet(f *testing.F) {
	for _, raw := range vectorInputs(f, "fleet_match.json") {
		var in vecFleetInput
		if json.Unmarshal(raw, &in) != nil {
			continue
		}
		var row vecRelayRow
		if len(in.RelayRows) > 0 {
			row = in.RelayRows[0]
		}
		var lat, lon float64
		hasPos := row.LatDeg != nil && row.LonDeg != nil
		if hasPos {
			lat, lon = *row.LatDeg, *row.LonDeg
		}
		f.Add(in.SerialIsOurs, row.HeardAtS, lat, lon, hasPos, row.Backlog, row.BehindS, row.Source != "",
			in.BroadcastPosition[0], in.BroadcastPosition[1], in.NowS, in.LiveForS, in.SpoofDistanceM)
	}
	f.Fuzz(func(t *testing.T, ours bool, heardS, lat, lon float64, hasPos, backlog bool, behindS float64, broadcastRow bool,
		bLat, bLon, nowS, liveForS, spoofM float64,
	) {
		row := identify.AuthRow{HeardAtS: heardS, Backlog: backlog, BehindS: behindS}
		if hasPos {
			row.Pos = &core.LatLon{LatDeg: lat, LonDeg: lon}
		}
		if broadcastRow {
			row.Source = "remote_id"
		}
		in := identify.FleetInput{
			SerialIsOurs: ours, Rows: []identify.AuthRow{row, row},
			Broadcast: core.LatLon{LatDeg: bLat, LonDeg: bLon}, NowS: nowS, LiveForS: liveForS, SpoofDistanceM: spoofM,
		}
		got := identify.JudgeFleet(in)
		switch got.Verdict {
		case identify.VerdictStranger, identify.VerdictAsOurs, identify.VerdictWithhold:
		case identify.VerdictConflict:
			if got.ApartM == nil || math.IsNaN(*got.ApartM) || !(*got.ApartM > spoofM) {
				t.Fatalf("conflict with apart_m %v at threshold %v", got.ApartM, spoofM)
			}
		default:
			t.Fatalf("verdict %q", got.Verdict)
		}
		if got.IgnoredHistoryRows < 0 || got.IgnoredHistoryRows > len(in.Rows) {
			t.Fatalf("ignored %d of %d rows", got.IgnoredHistoryRows, len(in.Rows))
		}
		if !ours && got.Verdict != identify.VerdictStranger {
			t.Fatalf("not ours but %q", got.Verdict)
		}
	})
}
