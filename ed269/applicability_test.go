package ed269

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// raw is a JSON fragment kept as written in a test document.
func raw(s string) json.RawMessage { return json.RawMessage(s) }

func hasProblem(probs *Problems, field, reason string) bool {
	if probs == nil {
		return false
	}
	for _, p := range probs.List {
		if p.Field == field && strings.Contains(p.Reason, reason) {
			return true
		}
	}
	return false
}

func TestParseApplicabilityRefusals(t *testing.T) {
	if _, probs := ParseApplicability(raw(`[`)); !hasProblem(probs, "applicability", "not JSON") {
		t.Errorf("not JSON: %v", probs)
	}
	if _, probs := ParseApplicability([]byte("[{\"permanent\":\"YES\xff\"}]")); !hasProblem(probs, "applicability", "not UTF-8: invalid byte 0xff") {
		t.Errorf("not UTF-8: %v", probs)
	}
	if _, probs := ParseApplicability(raw(`[]`)); !hasProblem(probs, "applicability", "at least one period") {
		t.Errorf("empty: %v", probs)
	}
	if _, probs := ParseApplicability(raw(`null`)); !hasProblem(probs, "applicability", "missing") {
		t.Errorf("null: %v", probs)
	}
	if _, probs := ParseApplicability(raw(`[{"permanent":"NO"}]`)); !hasProblem(probs, "applicability[0]", "to say when") {
		t.Errorf("never: %v", probs)
	}
	huge := append([]byte(`["`), bytes.Repeat([]byte("x"), DefaultLimits.MaxBytes)...)
	if _, probs := ParseApplicability(huge); !hasProblem(probs, "applicability", "bytes; at most") {
		t.Errorf("huge: %v", probs)
	}
	if ps, probs := ParseApplicability(raw(`[{"permanent":"YES"}]`)); probs != nil || len(ps) != 1 || !ps[0].Permanent {
		t.Errorf("accepted twin: %v %v", ps, probs)
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func mustPeriods(t *testing.T, s string) []Period {
	t.Helper()
	ps, probs := ParseApplicability(raw(s))
	if probs != nil {
		t.Fatalf("refused: %v", probs)
	}
	return ps
}

// E-01 pairs beyond the vectors (Z-07).
func TestAppliesPairs(t *testing.T) {
	tests := []struct {
		name    string
		periods string
		at      string
		want    bool
	}{
		{"exactly at the end is inside", `[{"permanent":"NO","startDateTime":"2026-10-01T00:00:00Z","endDateTime":"2026-10-02T00:00:00Z"}]`, "2026-10-02T00:00:00Z", true},
		{"a nanosecond after the end is outside", `[{"permanent":"NO","startDateTime":"2026-10-01T00:00:00Z","endDateTime":"2026-10-02T00:00:00Z"}]`, "2026-10-02T00:00:00.000000001Z", false},
		{"exactly at the start is inside", `[{"permanent":"NO","startDateTime":"2026-10-01T00:00:00Z"}]`, "2026-10-01T00:00:00Z", true},
		{"Sunday 01:30+04:00 is Saturday in UTC", `[{"permanent":"NO","schedule":[{"day":["SUN"],"startTime":"01:00+04:00","endTime":"03:00+04:00"}]}]`, "2026-10-03T21:30:00Z", true},
		{"Saturday 01:30+04:00 is not Sunday", `[{"permanent":"NO","schedule":[{"day":["SUN"],"startTime":"01:00+04:00","endTime":"03:00+04:00"}]}]`, "2026-10-02T21:30:00Z", false},
		{"Sunday 23:00+04:00 night into Monday", `[{"permanent":"NO","schedule":[{"day":["SUN"],"startTime":"23:00+04:00","endTime":"01:00+04:00"}]}]`, "2026-10-04T20:30:00Z", true},
		{"ANY applies on a Tuesday", `[{"permanent":"NO","schedule":[{"day":["ANY"],"startTime":"08:00Z","endTime":"09:00Z"}]}]`, "2026-10-06T08:30:00Z", true},
		{"ANY outside the hours", `[{"permanent":"NO","schedule":[{"day":["ANY"],"startTime":"08:00Z","endTime":"09:00Z"}]}]`, "2026-10-06T09:30:00Z", false},
		{"dates bound a schedule: before the start", `[{"permanent":"NO","startDateTime":"2026-10-05T00:00:00Z","endDateTime":"2026-10-07T00:00:00Z","schedule":[{"day":["ANY"],"startTime":"08:00Z","endTime":"18:00Z"}]}]`, "2026-10-04T12:00:00Z", false},
		{"dates bound a schedule: inside", `[{"permanent":"NO","startDateTime":"2026-10-05T00:00:00Z","endDateTime":"2026-10-07T00:00:00Z","schedule":[{"day":["ANY"],"startTime":"08:00Z","endTime":"18:00Z"}]}]`, "2026-10-06T12:00:00Z", true},
		{"dates bound a schedule: after the end", `[{"permanent":"NO","startDateTime":"2026-10-05T00:00:00Z","endDateTime":"2026-10-07T00:00:00Z","schedule":[{"day":["ANY"],"startTime":"08:00Z","endTime":"18:00Z"}]}]`, "2026-10-07T12:00:00Z", false},
		{"negative offset night belongs to its start day", `[{"permanent":"NO","schedule":[{"day":["FRI"],"startTime":"22:00-03:00","endTime":"02:00-03:00"}]}]`, "2026-10-10T04:30:00Z", true},
		{"negative offset night not on Thursday", `[{"permanent":"NO","schedule":[{"day":["FRI"],"startTime":"22:00-03:00","endTime":"02:00-03:00"}]}]`, "2026-10-09T04:30:00Z", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Applies(mustPeriods(t, tc.periods), mustTime(t, tc.at)); got != tc.want {
				t.Errorf("Applies = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("no periods never applies", func(t *testing.T) {
		if Applies(nil, time.Now()) {
			t.Error("applies")
		}
	})
}

// T-09: the instant's location only says how it was written.
func TestAppliesIgnoresTheInstantsLocation(t *testing.T) {
	ps := mustPeriods(t, `[{"permanent":"NO","schedule":[{"day":["MON"],"startTime":"09:00Z","endTime":"17:00Z"}]}]`)
	at := mustTime(t, "2026-10-05T12:00:00Z")
	for _, loc := range []*time.Location{time.UTC, time.FixedZone("x", 13*3600), time.FixedZone("y", -11*3600)} {
		if !Applies(ps, at.In(loc)) {
			t.Errorf("in %v: does not apply", loc)
		}
		if Applies(ps, at.Add(6*time.Hour).In(loc)) {
			t.Errorf("in %v: applies at 18:00Z", loc)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func TestVolumeUnits(t *testing.T) {
	v := Volume{Uom: UomFeet, LowerLimit: ptr(1000.0), Projection: HorizontalProjection{Type: ShapeCircle, Radius: ptr(1640.0)}}
	if got := *v.LowerM(); got != 1000*core.FeetToMetres {
		t.Errorf("LowerM %v", got)
	}
	if v.UpperM() != nil {
		t.Error("UpperM of an absent limit")
	}
	if got := *v.RadiusM(); got != 1640*core.FeetToMetres {
		t.Errorf("RadiusM %v", got)
	}
	v.Projection.Type = ShapePolygon
	if v.RadiusM() != nil {
		t.Error("RadiusM of a polygon")
	}
	if _, ok := (&GeoZone{}).Volume(); ok {
		t.Error("Volume of a zone without geometry")
	}
}

func TestRestrictionZoneType(t *testing.T) {
	want := map[Restriction]core.ZoneType{
		RestrictionProhibited:            core.ZoneProhibited,
		RestrictionReqAuthorisation:      core.ZoneReqAuthorization,
		RestrictionConditional:           core.ZoneConditional,
		RestrictionNoRestriction:         core.ZoneNoRestriction,
		Restriction("REQ_AUTHORIZATION"): "",
	}
	for r, zt := range want {
		if got := r.ZoneType(); got != zt {
			t.Errorf("%s: %q, want %q", r, got, zt)
		}
	}
}

func TestDescribe(t *testing.T) {
	for v, want := range map[*value]string{
		nil:                "null",
		{kind: kindNull}:   "null",
		{kind: kindBool}:   "a boolean",
		{kind: kindNumber}: "a number",
		{kind: kindString}: "a string",
		{kind: kindArray}:  "a list",
		{kind: kindObject}: "an object",
		{kind: kind(99)}:   "unknown",
	} {
		if got := describe(v); got != want {
			t.Errorf("describe(%v) = %q, want %q", v, got, want)
		}
	}
	if got := show(&value{kind: kindBool, b: true}); got != "true" {
		t.Errorf("show(true) = %q", got)
	}
	if got := show(nil); got != "null" {
		t.Errorf("show(nil) = %q", got)
	}
}

// The zero Period never applies; its permanent twin always does.
func TestZeroPeriodNeverApplies(t *testing.T) {
	at := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	if Applies([]Period{{}}, at) || (Period{Schedule: []DailyPeriod{}}).Contains(at) {
		t.Error("a period with nothing that says when it applies applies")
	}
	if !Applies([]Period{{Permanent: true}}, at) {
		t.Error("a permanent period does not apply")
	}
}
