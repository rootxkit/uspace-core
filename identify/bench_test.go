package identify_test

import (
	"strconv"
	"testing"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/identify"
	"github.com/rootxkit/uspace-core/odid"
)

// benchRegistry is a national-sized projection: n aircraft owned by n/10
// operators, every serial distinct.
func benchRegistry(n int) *identify.Snapshot {
	ops := make([]identify.OperatorFacts, n/10)
	for i := range ops {
		ops[i] = identify.OperatorFacts{OperatorID: "op-" + strconv.Itoa(i), RegistrationNumber: "GEO" + strconv.Itoa(10_000_000+i), Status: identify.StatusActive}
	}
	uas := make([]identify.UASFacts, n)
	for i := range uas {
		owner := "op-" + strconv.Itoa(i/10)
		uas[i] = identify.UASFacts{
			DroneID: "d-" + strconv.Itoa(i), Serial: "1581F5FKD" + strconv.Itoa(1_000_000+i),
			RegistrationStatus: identify.StatusActive, OperatorID: &owner, InRegistry: true,
		}
	}
	return identify.NewSnapshot(ops, uas)
}

// BenchmarkResolveBroadcast is a registered serial with its owner's number
// and the EU secret suffix: the longest path (PLAN 8.6 target 2000 ns).
func BenchmarkResolveBroadcast(b *testing.B) {
	reg := benchRegistry(100_000)
	sn, op := "1581f5fkd1012345", "GEO10001234-x9z"
	if id := identify.ResolveBroadcast(reg, &sn, &op); id.Status != core.IdentRegistered {
		b.Fatalf("%+v", id)
	}
	b.ReportAllocs()
	for b.Loop() {
		identify.ResolveBroadcast(reg, &sn, &op)
	}
}

// BenchmarkResolveRemoteID is a direct Remote ID serial (target 2000 ns).
func BenchmarkResolveRemoteID(b *testing.B) {
	reg := benchRegistry(100_000)
	op := "GEO10001234"
	id := identify.RemoteIDIdentity{Identified: true, UAID: "1581F5FKD1012345", IDType: odid.IDTypeSerial, OperatorID: &op}
	if got := identify.ResolveRemoteID(reg, id); got.Status != core.IdentRegistered {
		b.Fatalf("%+v", got)
	}
	b.ReportAllocs()
	for b.Loop() {
		identify.ResolveRemoteID(reg, id)
	}
}

// BenchmarkJudgeFleet is one second of 10 Hz telemetry judged against a
// broadcast (target 2000 ns).
func BenchmarkJudgeFleet(b *testing.B) {
	rows := make([]identify.AuthRow, 10)
	for i := range rows {
		rows[i] = identify.AuthRow{HeardAtS: float64(i) / 10, Pos: pos(home), BehindS: 0.2}
	}
	in := identify.FleetInput{SerialIsOurs: true, Rows: rows, Broadcast: far, NowS: 1, LiveForS: 5, SpoofDistanceM: 300}
	if r := identify.JudgeFleet(in); r.Verdict != identify.VerdictConflict {
		b.Fatalf("%+v", r)
	}
	b.ReportAllocs()
	for b.Loop() {
		identify.JudgeFleet(in)
	}
}

// BenchmarkNewSnapshot100k builds the indexes of 100 000 aircraft whose
// serials all fold to one key: linear, not quadratic, in the rows.
func BenchmarkNewSnapshot100k(b *testing.B) {
	serials := caseVariants(100_000)
	rows := make([]identify.UASFacts, len(serials))
	for i, sn := range serials {
		rows[i] = uas(sn, sn)
	}
	b.ReportAllocs()
	for b.Loop() {
		identify.NewSnapshot(nil, rows)
	}
}
