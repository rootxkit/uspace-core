package identify_test

import (
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/identify"
)

func uas(id, serial string) identify.UASFacts {
	return identify.UASFacts{DroneID: id, Serial: serial, RegistrationStatus: identify.StatusActive, InRegistry: true}
}

func TestSnapshotSerialLookup(t *testing.T) {
	s := identify.NewSnapshot(nil, []identify.UASFacts{
		uas("lower", "ab-1"), uas("upper", "AB-1"),
		uas("unique", " Xy-9 "),
		uas("dup1", "DUP"), uas("dup2", "DUP"),
		uas("no-serial", ""), uas("blank-serial", "  "),
	})
	cases := []struct {
		query string
		want  identify.Match
		id    string
	}{
		{"ab-1", identify.MatchExact, "lower"},   // exact beats fold
		{"AB-1", identify.MatchExact, "upper"},   // exact beats fold
		{"Ab-1", identify.MatchAmbiguous, ""},    // two folds: neither
		{" Xy-9", identify.MatchExact, "unique"}, // stored and query normalised
		{"xy-9", identify.MatchFolded, "unique"}, // one fold: it
		{"XY-9 ", identify.MatchFolded, "unique"},
		{"DUP", identify.MatchAmbiguous, ""}, // two aircraft, one serial
		{"dup", identify.MatchAmbiguous, ""},
		{"nobody", identify.MatchNone, ""},
		{"", identify.MatchNone, ""}, // an empty serial is never indexed
		{"  ", identify.MatchNone, ""},
	}
	for _, c := range cases {
		got, m := s.UASBySerial(c.query)
		if m != c.want || got.DroneID != c.id {
			t.Errorf("%q: %v %q, want %v %q", c.query, m, got.DroneID, c.want, c.id)
		}
	}
}

func TestSnapshotByIDAndOperator(t *testing.T) {
	s := identify.NewSnapshot(
		[]identify.OperatorFacts{{OperatorID: "o1", RegistrationNumber: "GEO1", Status: "active"}, {OperatorID: "o1", RegistrationNumber: "GEO2"}},
		[]identify.UASFacts{uas("d1", "S1"), uas("d1", "S2")},
	)
	if u, ok := s.UASByID("d1"); !ok || u.Serial != "S2" {
		t.Errorf("by id: %v %v, want the last row", u, ok)
	}
	// The replaced row is dropped whole: its serial finds nothing, the
	// kept row's serial finds the kept row.
	if _, m := s.UASBySerial("S1"); m != identify.MatchNone {
		t.Errorf("the replaced row's serial matched %v", m)
	}
	if u, m := s.UASBySerial("S2"); m != identify.MatchExact || u.DroneID != "d1" {
		t.Errorf("the kept row's serial: %v %q", m, u.DroneID)
	}
	if _, ok := s.UASByID("d2"); ok {
		t.Error("an unknown id was found")
	}
	if o, ok := s.Operator("o1"); !ok || o.RegistrationNumber != "GEO2" {
		t.Errorf("operator: %v %v, want the last row", o, ok)
	}
	if _, ok := s.Operator("o2"); ok {
		t.Error("an unknown operator was found")
	}
}

// TestSnapshotEmptyKnowsNobody: an empty registry, and a nil snapshot,
// know nobody; a non-empty one knows its aircraft (E-01 pair).
func TestSnapshotEmptyKnowsNobody(t *testing.T) {
	var nilSnap *identify.Snapshot
	for name, s := range map[string]*identify.Snapshot{"empty": identify.NewSnapshot(nil, nil), "nil": nilSnap} {
		if !s.Empty() {
			t.Errorf("%s: not empty", name)
		}
		if _, m := s.UASBySerial("S1"); m != identify.MatchNone {
			t.Errorf("%s: serial matched %v", name, m)
		}
		if _, ok := s.UASByID("d1"); ok {
			t.Errorf("%s: id found", name)
		}
		if _, ok := s.Operator("o1"); ok {
			t.Errorf("%s: operator found", name)
		}
	}
	full := identify.NewSnapshot(nil, []identify.UASFacts{uas("d1", "S1")})
	if full.Empty() {
		t.Error("a snapshot with an aircraft is empty")
	}
	if _, m := full.UASBySerial("S1"); m != identify.MatchExact {
		t.Errorf("serial: %v", m)
	}
	if identify.NewSnapshot([]identify.OperatorFacts{{OperatorID: "o1"}}, nil).Empty() {
		t.Error("a snapshot with an operator is empty")
	}
}

// TestSnapshotCopiesInputs: a snapshot is immutable once built.
func TestSnapshotCopiesInputs(t *testing.T) {
	owner := "o1"
	in := []identify.UASFacts{{DroneID: "d1", Serial: "S1", OperatorID: &owner, InRegistry: true}}
	s := identify.NewSnapshot(nil, in)
	in[0].Serial, in[0].InRegistry = "S2", false
	owner = "o2"
	u, ok := s.UASByID("d1")
	if !ok || u.Serial != "S1" || !u.InRegistry || u.OperatorID == nil || *u.OperatorID != "o1" {
		t.Fatalf("the snapshot changed with its input: %+v", u)
	}
	// Nor through what it returns.
	*u.OperatorID = "changed"
	byID, _ := s.UASByID("d1")
	bySerial, _ := s.UASBySerial("S1")
	folded, _ := s.UASBySerial("s1")
	for _, got := range []identify.UASFacts{byID, bySerial, folded} {
		if got.OperatorID == nil || *got.OperatorID != "o1" {
			t.Fatalf("the snapshot changed through a returned row: %+v", got)
		}
	}
}

func TestMatchString(t *testing.T) {
	want := map[identify.Match]string{
		identify.MatchNone: "none", identify.MatchExact: "exact", identify.MatchFolded: "folded",
		identify.MatchAmbiguous: "ambiguous", identify.Match(-3): "invalid",
	}
	for m, s := range want {
		if m.String() != s {
			t.Errorf("%d: %q, want %q", int(m), m.String(), s)
		}
	}
}

// caseVariants returns n distinct spellings of one serial that differ
// only by case: the worst case for a fold index.
func caseVariants(n int) []string {
	const base = "abcdefghijklmnopq" // 17 letters: 131 072 spellings
	out := make([]string, n)
	for i := range out {
		b := []byte(base)
		for bit := range b {
			if i>>bit&1 == 1 {
				b[bit] = strings.ToUpper(string(b[bit]))[0]
			}
		}
		out[i] = string(b)
	}
	return out
}

// TestSnapshotLargeFoldIndex: 100 000 aircraft folding to one key build an
// index that is ambiguous on the fold and exact on each spelling (E-10).
func TestSnapshotLargeFoldIndex(t *testing.T) {
	const n = 100_000
	serials := caseVariants(n)
	rows := make([]identify.UASFacts, n)
	for i, sn := range serials {
		rows[i] = uas(sn, sn)
	}
	s := identify.NewSnapshot(nil, rows)
	if _, m := s.UASBySerial("ABCDEFGHIJKLMNOPQ"); m != identify.MatchAmbiguous {
		t.Errorf("fold: %v, want ambiguous", m)
	}
	if u, m := s.UASBySerial(serials[n-1]); m != identify.MatchExact || u.DroneID != serials[n-1] {
		t.Errorf("exact: %v %q", m, u.DroneID)
	}
}
