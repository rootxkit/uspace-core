package ed318

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

func strp(s string) *string { return &s }

func utc(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func dt(t *testing.T, s string) *DateTime {
	t.Helper()
	return &DateTime{Time: utc(t, s), Text: s}
}

var tbilisi = core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}

type appliesCase struct {
	at   string
	want bool
}

func checkApplies(t *testing.T, tp []TimePeriod, where core.LatLon, dl Daylight, cases []appliesCase) {
	t.Helper()
	for _, c := range cases {
		got, err := Applies(tp, utc(t, c.at), where, dl)
		if err != nil {
			t.Errorf("%s: %v", c.at, err)
		}
		if got != c.want {
			t.Errorf("%s: applies %v, want %v", c.at, got, c.want)
		}
	}
}

// A clock window that runs past midnight belongs to the day it starts on
// (Z-07); both ends are included.
func TestAppliesClockWindowPastMidnight(t *testing.T) {
	tp := []TimePeriod{{Schedule: []DailyPeriod{{Day: []string{"SAT", "SUN"}, StartTime: strp("22:00:00Z"), EndTime: strp("02:00:00Z")}}}}
	checkApplies(t, tp, core.LatLon{}, nil, []appliesCase{
		{"2026-10-03T22:00:00Z", true},  // Saturday, the start
		{"2026-10-03T23:00:00Z", true},  // Saturday night
		{"2026-10-04T01:00:00Z", true},  // Sunday morning, Saturday's window
		{"2026-10-05T02:00:00Z", true},  // Monday 02:00, Sunday's window, its end
		{"2026-10-05T02:00:01Z", false}, // after it
		{"2026-10-02T23:00:00Z", false}, // Friday night
		{"2026-10-03T21:59:59Z", false}, // before Saturday's start
	})
}

// The weekday is judged in the schedule's offset, and the dates bound it.
func TestAppliesOffsetAndDates(t *testing.T) {
	tp := []TimePeriod{{
		StartDateTime: dt(t, "2026-10-01T00:00:00+04:00"),
		EndDateTime:   dt(t, "2027-01-01T00:00:00+04:00"),
		Schedule:      []DailyPeriod{{Day: []string{"MON", "TUE", "WED", "THU", "FRI"}, StartTime: strp("08:00:00+04:00"), EndTime: strp("18:00:00+04:00")}},
	}}
	checkApplies(t, tp, core.LatLon{}, nil, []appliesCase{
		{"2026-10-05T04:00:00Z", true},  // Monday 08:00 local
		{"2026-10-05T14:00:00Z", true},  // Monday 18:00 local
		{"2026-10-05T14:00:01Z", false}, // after
		{"2026-10-05T03:59:59Z", false}, // before
		{"2026-10-03T08:00:00Z", false}, // Saturday
		{"2026-09-29T08:00:00Z", false}, // Tuesday before the dates
		{"2027-01-04T08:00:00Z", false}, // Monday after the dates
	})
}

func TestAppliesWithoutPeriodsOrLimits(t *testing.T) {
	checkApplies(t, nil, core.LatLon{}, nil, []appliesCase{{"2026-10-03T10:00:00Z", true}})
	checkApplies(t, []TimePeriod{{}}, core.LatLon{}, nil, []appliesCase{{"2026-10-03T10:00:00Z", true}})
	checkApplies(t, []TimePeriod{{StartDateTime: dt(t, "2026-10-01T00:00:00Z")}}, core.LatLon{}, nil, []appliesCase{
		{"2026-10-03T10:00:00Z", true}, {"2026-09-30T10:00:00Z", false}})
}

// Sunrise to sunset at Tbilisi with NOAA: a day window in UTC.
func TestAppliesDaylightWindow(t *testing.T) {
	tp := []TimePeriod{{Schedule: []DailyPeriod{{Day: []string{"ANY"}, StartEvent: strp(EventSR), EndEvent: strp(EventSS)}}}}
	// 2026-06-21: SR 01:26:11Z, SS 16:38:46Z (astropy).
	checkApplies(t, tp, tbilisi, NOAADaylight{}, []appliesCase{
		{"2026-06-21T01:27:00Z", true},
		{"2026-06-21T16:38:00Z", true},
		{"2026-06-21T01:25:00Z", false},
		{"2026-06-21T16:40:00Z", false},
		{"2026-06-21T23:00:00Z", false},
	})
}

// Sunset to sunrise runs past midnight into the next day's sunrise.
func TestAppliesNightBetweenEvents(t *testing.T) {
	tp := []TimePeriod{{Schedule: []DailyPeriod{{Day: []string{"ANY"}, StartEvent: strp(EventSS), EndEvent: strp(EventSR)}}}}
	checkApplies(t, tp, tbilisi, NOAADaylight{}, []appliesCase{
		{"2026-06-21T23:00:00Z", true},
		{"2026-06-22T01:00:00Z", true},
		{"2026-06-21T12:00:00Z", false},
	})
}

// A clock start and an event end are framed in the clock's offset.
func TestAppliesClockToEvent(t *testing.T) {
	dl := FixedDaylight{
		"2026-10-02": {EventSS: utc(t, "2026-10-02T14:30:00Z")},
		"2026-10-03": {EventSS: utc(t, "2026-10-03T14:28:00Z")},
		"2026-10-04": {EventSS: utc(t, "2026-10-04T14:26:00Z")},
	}
	tp := []TimePeriod{{Schedule: []DailyPeriod{{Day: []string{"SAT"}, StartTime: strp("08:00:00+04:00"), EndEvent: strp(EventSS)}}}}
	checkApplies(t, tp, tbilisi, dl, []appliesCase{
		{"2026-10-03T04:00:00Z", true}, // Saturday 08:00 local
		{"2026-10-03T14:28:00Z", true}, // Saturday's sunset
		{"2026-10-03T14:29:00Z", false},
	})
}

// Sydney's sunrise of 21 June is on 20 June in UTC: the window of the next
// UTC date is tried too.
func TestAppliesEventOnANeighbouringUTCDate(t *testing.T) {
	sydney := core.LatLon{LatDeg: -33.8688, LonDeg: 151.2093}
	tp := []TimePeriod{{Schedule: []DailyPeriod{{Day: []string{"ANY"}, StartEvent: strp(EventSR), EndEvent: strp(EventSS)}}}}
	checkApplies(t, tp, sydney, NOAADaylight{}, []appliesCase{
		{"2026-06-20T22:00:00Z", true},  // 08:00 AEST on 21 June
		{"2026-06-20T19:00:00Z", false}, // 05:00 AEST, before sunrise
	})
}

// An event the place cannot resolve is not evaluated, never applies; a
// period that does apply still decides (E-02: the branch that says nothing
// is wrong runs too).
func TestAppliesNotEvaluated(t *testing.T) {
	events := []TimePeriod{{Schedule: []DailyPeriod{{Day: []string{"ANY"}, StartEvent: strp(EventSR), EndEvent: strp(EventSS)}}}}
	at := utc(t, "2026-06-21T12:00:00Z")
	for name, c := range map[string]struct {
		where  core.LatLon
		dl     Daylight
		reason string
	}{
		"no daylight":      {tbilisi, nil, "no Daylight source"},
		"invalid place":    {core.LatLon{LatDeg: 100}, NOAADaylight{}, "not a valid WGS84 position"},
		"polar day":        {core.LatLon{LatDeg: 78, LonDeg: 15}, NOAADaylight{}, "does not occur"},
		"missing in table": {tbilisi, FixedDaylight{}, "not in the table"},
	} {
		got, err := Applies(events, at, c.where, c.dl)
		if got || err == nil || !strings.Contains(err.Error(), c.reason) || !strings.Contains(err.Error(), "not evaluated") {
			t.Errorf("%s: applies %v, error %v", name, got, err)
		}
	}
	// Polar day makes the error wrap ErrNoEvent.
	if _, err := Applies(events, at, core.LatLon{LatDeg: 78, LonDeg: 15}, NOAADaylight{}); !errors.Is(err, ErrNoEvent) {
		t.Errorf("polar: %v", err)
	}
	// Another period that applies decides.
	both := append([]TimePeriod{{StartDateTime: dt(t, "2026-01-01T00:00:00Z")}}, events...)
	if got, err := Applies(both, at, tbilisi, nil); !got || err != nil {
		t.Errorf("an applying period beside an unresolvable one: %v, %v", got, err)
	}
	// Another schedule of the same period that applies decides too.
	mixed := []TimePeriod{{Schedule: []DailyPeriod{events[0].Schedule[0], {Day: []string{"ANY"}, StartTime: strp("11:00:00Z"), EndTime: strp("13:00:00Z")}}}}
	if got, err := Applies(mixed, at, tbilisi, nil); !got || err != nil {
		t.Errorf("an applying clock window beside an event window: %v, %v", got, err)
	}
}

func TestParseClock(t *testing.T) {
	for s, ok := range map[string]bool{
		"16:00:00Z": true, "16:00:00z": true, "08:30:00+04:00": true, "08:30:00.123456789-03:30": true,
		"23:59:59Z": true, "24:00:00Z": false, "16:00Z": false, "16:00:60Z": false, "16:00:00": false,
		"16:00:00+0400": false, "16:00:00.Z": false, "16:00:00.1234567890Z": false, "16-00-00Z": false,
		"16:00:00+24:00": false, "1:00:00Z": false,
	} {
		if _, got := parseClock(s); got != ok {
			t.Errorf("%q: %v, want %v", s, got, ok)
		}
	}
	c, _ := parseClock("08:30:15.5-03:30")
	if c.ns != int64(8*time.Hour+30*time.Minute+15*time.Second+500*time.Millisecond) || c.offsetS != -(3*3600+30*60) {
		t.Errorf("08:30:15.5-03:30 read as %+v", c)
	}
	if loc := offsetLocation(-(3*3600 + 30*60)); loc.String() != "-03:30" {
		t.Errorf("offset name %q", loc)
	}
}

// A daily period built in code with neither a time nor an event at an
// end, or a time that is not RFC 3339, is not evaluated.
func TestAppliesBuiltInCodeRefusals(t *testing.T) {
	at := utc(t, "2026-06-21T12:00:00Z")
	for name, d := range map[string]DailyPeriod{
		"no start": {Day: []string{"ANY"}, EndTime: strp("13:00:00Z")},
		"bad time": {Day: []string{"ANY"}, StartTime: strp("noon"), EndTime: strp("13:00:00Z")},
	} {
		if got, err := Applies([]TimePeriod{{Schedule: []DailyPeriod{d}}}, at, tbilisi, nil); got || err == nil {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}
}
