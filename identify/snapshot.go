//nolint:misspell // serial.Normalize is the API name fixed in docs/PLAN.md §3.6 (Go spelling)
package identify

import (
	"strings"

	"github.com/rootxkit/uspace-core/serial"
)

// Registration statuses as the registry projection writes them. An empty
// status reads as active, as the predecessor read a NULL column. Any other
// value is not recognised and fails safe: it is handled as suspended
// (uas_suspended or operator_suspended), never as active, because core
// has no reason code for an unrecognised status.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusRevoked   = "revoked"
)

// OperatorFacts is one UAS operator as the registry projection holds it.
// Status is StatusActive, StatusSuspended or StatusRevoked.
type OperatorFacts struct {
	OperatorID         string
	RegistrationNumber string
	Status             string
}

// UASFacts is one aircraft as the registry projection holds it.
type UASFacts struct {
	DroneID string
	Label   string
	// Serial as registered: trimmed, case kept (G-05). Empty is no serial.
	Serial string
	// RegistrationStatus is StatusActive, StatusSuspended or StatusRevoked.
	RegistrationStatus string
	// OperatorID is the owning UAS operator; nil for our own fleet.
	OperatorID *string
	// InRegistry is false for a projection row the registry has no
	// aircraft for ("unregistered", G-08): never registered.
	InRegistry bool
}

// Match says how a serial lookup matched (G-05).
type Match int

const (
	// MatchNone is no aircraft with that serial in any case.
	MatchNone Match = iota
	// MatchExact is exactly one aircraft with the serial as given.
	MatchExact
	// MatchFolded is no exact match, and exactly one aircraft whose serial
	// equals it ignoring case.
	MatchFolded
	// MatchAmbiguous is more than one candidate: two aircraft that differ
	// only by case, or two registered with the same serial. It matches
	// neither.
	MatchAmbiguous
)

// String returns the match name, for messages.
func (m Match) String() string {
	switch m {
	case MatchNone:
		return "none"
	case MatchExact:
		return "exact"
	case MatchFolded:
		return "folded"
	case MatchAmbiguous:
		return "ambiguous"
	}
	return "invalid"
}

// Lookup is the registry as one read of the projection saw it. Snapshot
// implements it; a system may implement it over its own store.
type Lookup interface {
	// UASBySerial finds an aircraft by serial: an exact match wins, else a
	// case-folded match when exactly one aircraft has it (G-05). The
	// facts are meaningful only for MatchExact and MatchFolded.
	UASBySerial(serial string) (UASFacts, Match)
	// UASByID finds an aircraft by its registry id.
	UASByID(droneID string) (UASFacts, bool)
	// Operator finds a UAS operator by its id.
	Operator(operatorID string) (OperatorFacts, bool)
}

// Snapshot is an immutable, in-memory Lookup built from one read of the
// registry projection. It is safe for concurrent use. A nil *Snapshot
// knows nobody.
type Snapshot struct {
	uas       []UASFacts
	byID      map[string]int
	byExact   map[string]int // serial -> index, or -1 when repeated
	byFolded  map[string]int // fold key -> index, or -1 when repeated
	operators map[string]OperatorFacts
}

// ambiguous marks a key that more than one aircraft has.
const ambiguous = -1

// NewSnapshot indexes a projection read in time linear in its size. The
// slices are copied; later changes to them do not reach the snapshot.
// Serials are normalised (serial.Normalize) before indexing, and an empty
// serial is not indexed. A repeated drone or operator id keeps the last
// row, as a projection read keyed by id would; the rows it replaces are
// dropped whole, so their serials find nothing.
func NewSnapshot(ops []OperatorFacts, uas []UASFacts) *Snapshot {
	s := &Snapshot{
		uas:       make([]UASFacts, len(uas)),
		byID:      make(map[string]int, len(uas)),
		byExact:   make(map[string]int, len(uas)),
		byFolded:  make(map[string]int, len(uas)),
		operators: make(map[string]OperatorFacts, len(ops)),
	}
	for _, o := range ops {
		s.operators[o.OperatorID] = o
	}
	for i, u := range uas {
		if u.OperatorID != nil {
			id := *u.OperatorID
			u.OperatorID = &id
		}
		u.Serial = serial.Normalize(u.Serial)
		s.uas[i] = u
		s.byID[u.DroneID] = i
	}
	for i, u := range s.uas {
		if u.Serial == "" || s.byID[u.DroneID] != i {
			continue
		}
		index(s.byExact, u.Serial, i)
		index(s.byFolded, serial.FoldKey(u.Serial), i)
	}
	return s
}

func index(m map[string]int, key string, i int) {
	if _, seen := m[key]; seen {
		m[key] = ambiguous
		return
	}
	m[key] = i
}

// Empty reports whether the snapshot holds no aircraft and no operator.
func (s *Snapshot) Empty() bool {
	return s == nil || len(s.uas) == 0 && len(s.operators) == 0
}

// UASBySerial implements Lookup.
func (s *Snapshot) UASBySerial(sn string) (UASFacts, Match) {
	sn = serial.Normalize(sn)
	if s == nil || sn == "" {
		return UASFacts{}, MatchNone
	}
	if i, ok := s.byExact[sn]; ok {
		if i == ambiguous {
			return UASFacts{}, MatchAmbiguous
		}
		return s.row(i), MatchExact
	}
	i, ok := s.byFolded[serial.FoldKey(sn)]
	switch {
	case !ok:
		return UASFacts{}, MatchNone
	case i == ambiguous:
		return UASFacts{}, MatchAmbiguous
	}
	return s.row(i), MatchFolded
}

// UASByID implements Lookup.
func (s *Snapshot) UASByID(droneID string) (UASFacts, bool) {
	if s == nil {
		return UASFacts{}, false
	}
	i, ok := s.byID[droneID]
	if !ok {
		return UASFacts{}, false
	}
	return s.row(i), true
}

// row returns a copy of row i whose OperatorID points at a fresh string,
// so a caller cannot change the snapshot through it.
func (s *Snapshot) row(i int) UASFacts {
	u := s.uas[i]
	if u.OperatorID != nil {
		id := *u.OperatorID
		u.OperatorID = &id
	}
	return u
}

// Operator implements Lookup.
func (s *Snapshot) Operator(operatorID string) (OperatorFacts, bool) {
	if s == nil {
		return OperatorFacts{}, false
	}
	o, ok := s.operators[operatorID]
	return o, ok
}

// inactive classifies a registration status: revoked, suspended, or
// neither. Case and surrounding space are ignored. Only active and the
// empty status are neither; an unrecognised status is suspended (fail
// safe).
func inactive(status string) (suspended, revoked bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusActive, "":
		return false, false
	case StatusRevoked:
		return false, true
	}
	return true, false
}
