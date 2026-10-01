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

// maxFuzzRows bounds the rows one fuzz input builds.
const maxFuzzRows = 16

// fuzzRows builds a mix of rows from one byte each: the low two bits pick
// live, backlog, captured too long before receipt, or broadcast; bit 2
// drops the position; the high bits move the heard time back in 0.5 s
// steps, so some rows fall out of the live window.
func fuzzRows(kinds []byte, pos core.LatLon, liveForS, nowS float64) []identify.AuthRow {
	if len(kinds) > maxFuzzRows {
		kinds = kinds[:maxFuzzRows]
	}
	rows := make([]identify.AuthRow, 0, len(kinds))
	for _, k := range kinds {
		p := pos
		r := identify.AuthRow{HeardAtS: nowS - float64(k>>3)/2, Pos: &p}
		switch k & 3 {
		case 1:
			r.Backlog = true
		case 2:
			r.BehindS = liveForS + 1
		case 3:
			r.Source = "network_remote_id"
		}
		if k&4 != 0 {
			r.Pos = nil
		}
		rows = append(rows, r)
	}
	return rows
}

// FuzzJudgeFleet: any mix of live, history and broadcast rows, times,
// positions and thresholds, seeded from the vectors, gives a verdict
// without a panic. History is counted exactly, a quiet link is as_ours,
// and a conflict always has a finite distance beyond the threshold.
func FuzzJudgeFleet(f *testing.F) {
	for _, raw := range vectorInputs(f, "fleet_match.json") {
		var in vecFleetInput
		if json.Unmarshal(raw, &in) != nil {
			continue
		}
		var lat, lon float64
		kinds := make([]byte, 0, len(in.RelayRows))
		for _, r := range in.RelayRows {
			var k byte
			switch {
			case r.Source != "":
				k = 3
			case r.Backlog:
				k = 1
			case r.BehindS > in.LiveForS:
				k = 2
			}
			if r.LatDeg == nil || r.LonDeg == nil {
				k |= 4
			} else {
				lat, lon = *r.LatDeg, *r.LonDeg
			}
			kinds = append(kinds, k)
		}
		f.Add(in.SerialIsOurs, kinds, lat, lon, in.BroadcastPosition[0], in.BroadcastPosition[1],
			in.NowS, in.LiveForS, in.SpoofDistanceM)
	}
	f.Add(true, []byte{0, 1, 2, 3, 4, 5, 6, 7, 0x50, 0x08}, 41.7151, 44.8271, 41.7196, 44.8271, 1.0, 5.0, 300.0)
	f.Fuzz(func(t *testing.T, ours bool, kinds []byte, lat, lon, bLat, bLon, nowS, liveForS, spoofM float64) {
		in := identify.FleetInput{
			SerialIsOurs: ours, Rows: fuzzRows(kinds, core.LatLon{LatDeg: lat, LonDeg: lon}, liveForS, nowS),
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
		if !ours {
			if got.Verdict != identify.VerdictStranger {
				t.Fatalf("not ours but %q", got.Verdict)
			}
			return
		}
		if got.Problem != nil {
			if got.Verdict != identify.VerdictWithhold {
				t.Fatalf("a problem with verdict %q", got.Verdict)
			}
			return
		}
		history, live := 0, false
		for _, r := range in.Rows {
			switch {
			case r.Source != "":
			case r.Backlog || !(r.BehindS <= liveForS):
				history++
			case nowS-r.HeardAtS <= liveForS:
				live = true
			}
		}
		if got.IgnoredHistoryRows != history {
			t.Fatalf("ignored %d history rows, counted %d", got.IgnoredHistoryRows, history)
		}
		if live == (got.Verdict == identify.VerdictAsOurs) {
			t.Fatalf("live %v but verdict %q", live, got.Verdict)
		}
	})
}
