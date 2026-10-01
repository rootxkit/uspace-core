package core

import (
	"errors"
	"math"
	"sync"
	"testing"
)

func TestLatLonValid(t *testing.T) {
	// E-01: the accepting case beside every refusing one.
	ok := []LatLon{{0, 0}, {90, 180}, {-90, -180}, {41.7151, 44.8271}}
	for _, p := range ok {
		if !p.Valid() {
			t.Errorf("%+v should be valid", p)
		}
	}
	bad := []LatLon{
		{math.NaN(), 0}, {0, math.NaN()},
		{math.Inf(1), 0}, {0, math.Inf(-1)},
		{90.0001, 0}, {-90.0001, 0}, {0, 180.0001}, {0, -180.0001},
	}
	for _, p := range bad {
		if p.Valid() {
			t.Errorf("%+v should be invalid", p)
		}
	}
}

func TestWrapLonDeg(t *testing.T) {
	cases := []struct{ in, want float64 }{
		{0, 0}, {179, 179}, {180, 180}, {-180, 180}, {181, -179},
		{360, 0}, {-181, 179}, {540, 180}, {-540, 180}, {44.8271, 44.8271},
	}
	for _, c := range cases {
		if got := WrapLonDeg(c.in); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("WrapLonDeg(%v) = %v, want %v", c.in, got, c.want)
		}
	}
	if !math.IsNaN(WrapLonDeg(math.NaN())) {
		t.Error("NaN must pass through, not be wrapped into a number")
	}
}

func TestZoneTypeSpellings(t *testing.T) {
	if ZoneReqAuthorization.ED269() != "REQ_AUTHORISATION" {
		t.Fatalf("ED-269 spelling: %s", ZoneReqAuthorization.ED269())
	}
	if ZoneProhibited.ED269() != "PROHIBITED" {
		t.Fatal("PROHIBITED must be unchanged")
	}
	z, ok := ZoneTypeFromED269("REQ_AUTHORISATION")
	if !ok || z != ZoneReqAuthorization {
		t.Fatal("REQ_AUTHORISATION must map to the ED-318 value")
	}
	// Z-04: the Z spelling is not an ED-269 restriction.
	if _, ok := ZoneTypeFromED269("REQ_AUTHORIZATION"); ok {
		t.Fatal("the American spelling must not be accepted as ED-269")
	}
	if _, ok := ZoneTypeFromED269("USPACE"); ok {
		t.Fatal("USPACE is not an ED-269 restriction")
	}
	for _, z := range []ZoneType{ZoneProhibited, ZoneReqAuthorization, ZoneConditional, ZoneNoRestriction, ZoneUSpace} {
		if !z.Valid() {
			t.Errorf("%s should be valid", z)
		}
	}
	if ZoneType("FORBIDDEN").Valid() {
		t.Error("FORBIDDEN is not a zone type")
	}
	if !ZoneProhibited.IncidentZone() || !ZoneReqAuthorization.IncidentZone() {
		t.Error("G-03: PROHIBITED and REQ_AUTHORISATION are incident zones")
	}
	if ZoneConditional.IncidentZone() || ZoneNoRestriction.IncidentZone() {
		t.Error("G-03: CONDITIONAL and NO_RESTRICTION raise no identification alert")
	}
}

func TestIdentStatusIncident(t *testing.T) {
	if !IdentUnidentified.IncidentStatus() || !IdentUnknownOperator.IncidentStatus() {
		t.Error("unidentified and unknown_operator are incident statuses")
	}
	if IdentRegistered.IncidentStatus() || IdentSuspended.IncidentStatus() {
		t.Error("registered and suspended are not")
	}
}

func TestVerticalRefValid(t *testing.T) {
	for _, r := range []VerticalRef{RefAGL, RefAMSL, RefWGS84} {
		if !r.Valid() {
			t.Errorf("%s should be valid", r)
		}
	}
	if VerticalRef("FL").Valid() {
		t.Error("FL is not a vertical reference here")
	}
}

func TestCounters(t *testing.T) {
	var c Counters
	if c.Get("never") != 0 {
		t.Fatal("an unknown counter reads 0")
	}
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				c.Inc("rejected_backlog")
			}
		}()
	}
	wg.Wait()
	c.Add("zone_checks_not_evaluated", 3)
	if got := c.Get("rejected_backlog"); got != 5000 {
		t.Fatalf("rejected_backlog = %d, want 5000", got)
	}
	snap := c.Snapshot()
	if snap["zone_checks_not_evaluated"] != 3 || len(snap) != 2 {
		t.Fatalf("snapshot = %v", snap)
	}
	snap["rejected_backlog"] = 0
	if c.Get("rejected_backlog") != 5000 {
		t.Fatal("snapshot must be a copy")
	}
	names := c.Names()
	if len(names) != 2 || names[0] != "rejected_backlog" || names[1] != "zone_checks_not_evaluated" {
		t.Fatalf("names = %v", names)
	}
}

func TestFieldError(t *testing.T) {
	err := Fieldf("features[3].geometry[0].upperLimit", "missing: %s", "required")
	var fe *FieldError
	if !errors.As(err, &fe) {
		t.Fatal("Fieldf must return a *FieldError")
	}
	if err.Error() != "features[3].geometry[0].upperLimit: missing: required" {
		t.Fatalf("Error() = %q", err.Error())
	}
	if (&FieldError{Reason: "empty"}).Error() != "empty" {
		t.Fatal("an error without a field is just the reason")
	}
}

func TestTimesLag(t *testing.T) {
	var tm Times
	tm.CapturedAt = tm.RxTS.Add(-2_500_000_000)
	if lag := tm.LagS(); math.Abs(lag-2.5) > 1e-9 {
		t.Fatalf("LagS = %v, want 2.5", lag)
	}
}
