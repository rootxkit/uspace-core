package ed318

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// Reference times computed when this test was written with astropy 6.1.3
// (ERFA): the instant the apparent sun's centre, without refraction, is
// 0.833 degrees (SR, SS) or 6 degrees (BMCT, EECT) below the horizon at
// height 0, found by bisection to well under a second. They are an
// independent check of the NOAA algorithm, not its output.
var sunReferences = []struct {
	place  string
	at     core.LatLon
	date   string
	events map[string]string
}{
	{"Tbilisi", core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}, "2026-03-20", map[string]string{
		EventSR: "2026-03-20T03:04:28Z", EventSS: "2026-03-20T15:12:33Z", EventBMCT: "2026-03-20T02:36:46Z", EventEECT: "2026-03-20T15:40:19Z"}},
	{"Tbilisi", core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}, "2026-06-21", map[string]string{
		EventSR: "2026-06-21T01:26:11Z", EventSS: "2026-06-21T16:38:46Z", EventBMCT: "2026-06-21T00:51:56Z", EventEECT: "2026-06-21T17:13:01Z"}},
	{"Tbilisi", core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}, "2026-12-21", map[string]string{
		EventSR: "2026-12-21T04:24:17Z", EventSS: "2026-12-21T13:33:05Z", EventBMCT: "2026-12-21T03:52:41Z", EventEECT: "2026-12-21T14:04:41Z"}},
	{"Oslo", core.LatLon{LatDeg: 59.9139, LonDeg: 10.7522}, "2026-03-20", map[string]string{
		EventSR: "2026-03-20T05:18:57Z", EventSS: "2026-03-20T17:31:18Z", EventBMCT: "2026-03-20T04:37:34Z", EventEECT: "2026-03-20T18:12:52Z"}},
	{"Oslo", core.LatLon{LatDeg: 59.9139, LonDeg: 10.7522}, "2026-06-21", map[string]string{
		EventSR: "2026-06-21T01:53:44Z", EventSS: "2026-06-21T20:43:50Z", EventBMCT: "2026-06-21T00:09:35Z", EventEECT: "2026-06-21T22:27:58Z"}},
	{"Oslo", core.LatLon{LatDeg: 59.9139, LonDeg: 10.7522}, "2026-12-21", map[string]string{
		EventSR: "2026-12-21T08:18:06Z", EventSS: "2026-12-21T14:11:57Z", EventBMCT: "2026-12-21T07:20:34Z", EventEECT: "2026-12-21T15:09:29Z"}},
	// Sydney's morning of a date falls on the UTC day before: the event
	// is the one of the local date.
	{"Sydney", core.LatLon{LatDeg: -33.8688, LonDeg: 151.2093}, "2026-03-20", map[string]string{
		EventSR: "2026-03-19T19:57:57Z", EventSS: "2026-03-20T08:06:56Z", EventBMCT: "2026-03-19T19:33:01Z", EventEECT: "2026-03-20T08:31:50Z"}},
	{"Sydney", core.LatLon{LatDeg: -33.8688, LonDeg: 151.2093}, "2026-06-21", map[string]string{
		EventSR: "2026-06-20T20:59:57Z", EventSS: "2026-06-21T06:53:48Z", EventBMCT: "2026-06-20T20:32:13Z", EventEECT: "2026-06-21T07:21:32Z"}},
	{"Sydney", core.LatLon{LatDeg: -33.8688, LonDeg: 151.2093}, "2026-12-21", map[string]string{
		EventSR: "2026-12-20T18:40:39Z", EventSS: "2026-12-21T09:05:23Z", EventBMCT: "2026-12-20T18:11:29Z", EventEECT: "2026-12-21T09:34:34Z"}},
}

// noaaToleranceS is NOAA's stated accuracy between 72 degrees north and
// south: one minute.
const noaaToleranceS = 60

func TestNOAADaylightWithinAMinute(t *testing.T) {
	worst := 0.0
	for _, r := range sunReferences {
		day, err := time.Parse("2006-01-02", r.date)
		if err != nil {
			t.Fatal(err)
		}
		for ev, ref := range r.events {
			want, err := time.Parse(time.RFC3339, ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := NOAADaylight{}.Event(ev, day, r.at)
			if err != nil {
				t.Errorf("%s %s %s: %v", r.place, r.date, ev, err)
				continue
			}
			d := math.Abs(got.Sub(want).Seconds())
			worst = math.Max(worst, d)
			if d > noaaToleranceS {
				t.Errorf("%s %s %s: %s, reference %s (%.0f s off)", r.place, r.date, ev, got.Format(time.RFC3339), ref, d)
			}
		}
	}
	t.Logf("largest difference from the astropy references: %.0f s", worst)
}

// The date is read in day's own location: midnight +04:00 on 21 June is
// still 21 June, not 20 June in UTC.
func TestNOAADaylightReadsTheDateInItsLocation(t *testing.T) {
	tbilisi := core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}
	utc, err := NOAADaylight{}.Event(EventSR, time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), tbilisi)
	if err != nil {
		t.Fatal(err)
	}
	local, err := NOAADaylight{}.Event(EventSR, time.Date(2026, 6, 21, 0, 0, 0, 0, time.FixedZone("+04:00", 4*3600)), tbilisi)
	if err != nil || !local.Equal(utc) {
		t.Errorf("local date: %v (%v), UTC date: %v", local, err, utc)
	}
}

// Where the sun does not rise or set that day the event is not resolved:
// an error wrapping ErrNoEvent, never a time. The same place resolves the
// event on an equinox (E-01).
func TestNOAADaylightPolar(t *testing.T) {
	tromso := core.LatLon{LatDeg: 69.6496, LonDeg: 18.956}
	for _, c := range []struct {
		date string
		ev   string
	}{{"2026-06-21", EventSS}, {"2026-06-21", EventSR}, {"2026-12-21", EventSR}, {"2026-06-21", EventEECT}} {
		day, _ := time.Parse("2006-01-02", c.date)
		if _, err := (NOAADaylight{}).Event(c.ev, day, tromso); !errors.Is(err, ErrNoEvent) {
			t.Errorf("Tromsø %s %s: %v, want ErrNoEvent", c.date, c.ev, err)
		}
	}
	day, _ := time.Parse("2006-01-02", "2026-03-20")
	if _, err := (NOAADaylight{}).Event(EventSR, day, tromso); err != nil {
		t.Errorf("Tromsø equinox sunrise: %v", err)
	}
}

func TestNOAADaylightRefusals(t *testing.T) {
	day := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	var fe *core.FieldError
	if _, err := (NOAADaylight{}).Event("NOON", day, core.LatLon{LatDeg: 41, LonDeg: 44}); !errors.As(err, &fe) || fe.Field != "event" {
		t.Errorf("unknown event: %v", err)
	}
	if _, err := (NOAADaylight{}).Event(EventSR, day, core.LatLon{LatDeg: math.NaN()}); !errors.As(err, &fe) || fe.Field != "at" {
		t.Errorf("invalid place: %v", err)
	}
}

func TestFixedDaylight(t *testing.T) {
	sr := time.Date(2026, 6, 21, 1, 26, 0, 0, time.UTC)
	dl := FixedDaylight{"2026-06-21": {EventSR: sr}}
	got, err := dl.Event(EventSR, time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), core.LatLon{})
	if err != nil || !got.Equal(sr) {
		t.Errorf("listed event: %v, %v", got, err)
	}
	if _, err := dl.Event(EventSS, time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC), core.LatLon{}); !errors.Is(err, ErrNoEvent) {
		t.Errorf("missing event: %v", err)
	}
}
