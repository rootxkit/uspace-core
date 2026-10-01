package ed318

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// periods reads the optional limitedApplicability list; nil when absent.
func (p *parser) periods(v *value, where string) []TimePeriod {
	if !present(v) {
		return nil
	}
	if v.kind != kindArray {
		p.ps.add(where, "must be a list of time periods, not "+describe(v))
		return nil
	}
	out := make([]TimePeriod, 0, len(v.arr))
	for i, raw := range v.arr {
		if tp, ok := p.period(raw, index(where, i)); ok {
			out = append(out, tp)
		}
	}
	return out
}

func (p *parser) period(v *value, where string) (TimePeriod, bool) {
	if v.kind != kindObject {
		p.ps.add(where, "a time period must be an object")
		return TimePeriod{}, false
	}
	before := p.ps.count()
	p.unknown(v, where, periodFields, "unknown member; not part of an ED-318 TimePeriod")
	tp := TimePeriod{
		StartDateTime: p.dateTime(v.get("startDateTime"), join(where, "startDateTime")),
		EndDateTime:   p.dateTime(v.get("endDateTime"), join(where, "endDateTime")),
	}
	if s := v.get("schedule"); present(s) {
		if s.kind != kindArray || len(s.arr) == 0 {
			p.ps.add(join(where, "schedule"), "must be a list of at least one daily period")
		} else {
			tp.Schedule = make([]DailyPeriod, 0, len(s.arr))
			for i, raw := range s.arr {
				if d, ok := p.daily(raw, index(join(where, "schedule"), i)); ok {
					tp.Schedule = append(tp.Schedule, d)
				}
			}
		}
	}
	if tp.StartDateTime != nil && tp.EndDateTime != nil && !tp.StartDateTime.Time.Before(tp.EndDateTime.Time) {
		p.ps.add(join(where, "endDateTime"), "is not after startDateTime")
	}
	return tp, p.ps.count() == before
}

func (p *parser) daily(v *value, where string) (DailyPeriod, bool) {
	if v.kind != kindObject {
		p.ps.add(where, "a daily period must be an object")
		return DailyPeriod{}, false
	}
	before := p.ps.count()
	p.unknown(v, where, dailyFields, "unknown member; not part of an ED-318 DailyPeriod")
	d := DailyPeriod{Day: p.days(v.get("day"), join(where, "day"))}
	d.StartTime, d.StartEvent = p.bound(v, where, "startTime", "startEvent")
	d.EndTime, d.EndEvent = p.bound(v, where, "endTime", "endEvent")
	if p.ps.count() > before {
		return DailyPeriod{}, false
	}
	if d.StartTime != nil && d.EndTime != nil {
		s, _ := parseClock(*d.StartTime)
		e, _ := parseClock(*d.EndTime)
		if s.offsetS != e.offsetS {
			p.ps.add(join(where, "endTime"), "has a different offset from startTime; a daily period needs one")
			return DailyPeriod{}, false
		}
		if s.ns == e.ns {
			p.ps.add(join(where, "endTime"), "is the same as startTime")
			return DailyPeriod{}, false
		}
	}
	return d, true
}

// bound reads one end of a daily window: exactly one of a clock time and
// a daylight event.
func (p *parser) bound(v *value, where, timeKey, eventKey string) (*string, *string) {
	t, e := v.get(timeKey), v.get(eventKey)
	switch {
	case present(t) && present(e):
		p.ps.add(join(where, eventKey), "is given with "+timeKey+"; give one of them")
		return nil, nil
	case !present(t) && !present(e):
		p.ps.add(join(where, timeKey), "missing: give "+timeKey+" or "+eventKey)
		return nil, nil
	case present(e):
		s, ok := p.requiredEnum(e, join(where, eventKey), eventValues)
		if !ok {
			return nil, nil
		}
		return nil, &s
	}
	if t.kind == kindString {
		if _, ok := parseClock(t.s); ok {
			s := t.s
			return &s, nil
		}
	}
	p.ps.add(join(where, timeKey), show(t)+" is not an RFC 3339 time with an offset, e.g. 16:00:00Z or 08:30:00+04:00")
	return nil, nil
}

// days reads a daily period's day list: 1 to 7 of MON..SUN, or ANY alone.
func (p *parser) days(v *value, where string) []string {
	allowed := strings.Join(dayValues, ", ")
	if !present(v) || v.kind != kindArray || len(v.arr) < 1 || len(v.arr) > 7 {
		p.ps.add(where, "must be a list of 1 to 7 of "+allowed)
		return nil
	}
	out := make([]string, 0, len(v.arr))
	for i, d := range v.arr {
		here := index(where, i)
		if d.kind != kindString || !slices.Contains(dayValues, d.s) {
			p.ps.add(here, show(d)+" is not one of "+allowed)
			return nil
		}
		if d.s == anyDay && len(v.arr) > 1 {
			p.ps.add(here, "ANY is every day; it stands alone")
			return nil
		}
		if slices.Contains(out, d.s) {
			p.ps.add(here, d.s+" is listed twice")
			return nil
		}
		out = append(out, d.s)
	}
	return out
}

// clockValue is a time of day with its offset.
type clockValue struct {
	ns      int64
	offsetS int
}

// parseClock reads an RFC 3339 full-time: hh:mm:ss, an optional fraction
// of 1 to 9 digits, then Z or +hh:mm / -hh:mm.
func parseClock(s string) (clockValue, bool) {
	if len(s) < 9 || s[2] != ':' || s[5] != ':' {
		return clockValue{}, false
	}
	h, ok1 := twoDigits(s[0:], 23)
	m, ok2 := twoDigits(s[3:], 59)
	sec, ok3 := twoDigits(s[6:], 60)
	if !ok1 || !ok2 || !ok3 || sec == 60 {
		return clockValue{}, false
	}
	rest := s[8:]
	frac := int64(0)
	if strings.HasPrefix(rest, ".") {
		n := 0
		for n+1 < len(rest) && isDigit(rest[n+1]) {
			n++
		}
		if n < 1 || n > 9 {
			return clockValue{}, false
		}
		for i := range 9 {
			frac *= 10
			if i < n {
				frac += int64(rest[1+i] - '0')
			}
		}
		rest = rest[1+n:]
	}
	off, ok := parseOffset(rest)
	if !ok {
		return clockValue{}, false
	}
	ns := int64(h)*int64(time.Hour) + int64(m)*int64(time.Minute) + int64(sec)*int64(time.Second) + frac
	return clockValue{ns: ns, offsetS: off}, true
}

// parseOffset reads Z, z or +hh:mm / -hh:mm.
func parseOffset(s string) (int, bool) {
	if s == "Z" || s == "z" {
		return 0, true
	}
	if len(s) != 6 || (s[0] != '+' && s[0] != '-') || s[3] != ':' {
		return 0, false
	}
	h, ok1 := twoDigits(s[1:], 23)
	m, ok2 := twoDigits(s[4:], 59)
	if !ok1 || !ok2 {
		return 0, false
	}
	off := h*3600 + m*60
	if s[0] == '-' {
		off = -off
	}
	return off, true
}

func twoDigits(s string, hi int) (int, bool) {
	if len(s) < 2 || !isDigit(s[0]) || !isDigit(s[1]) {
		return 0, false
	}
	n := int(s[0]-'0')*10 + int(s[1]-'0')
	return n, n <= hi
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// offsetLocation is a fixed zone for an offset in seconds; UTC for 0.
func offsetLocation(offsetS int) *time.Location {
	if offsetS == 0 {
		return time.UTC
	}
	sign, a := '+', offsetS
	if a < 0 {
		sign, a = '-', -a
	}
	return time.FixedZone(fmt.Sprintf("%c%02d:%02d", sign, a/3600, a%3600/60), offsetS)
}

// Daylight resolves a daylight event (BMCT, SR, SS, EECT) on a day at a
// place. day names a calendar date by its year, month and day in day's
// own location; the result is the instant of the event belonging to that
// date at that place (for SR and BMCT the morning of the date, for SS and
// EECT its evening, as local solar time counts the date). An error means
// the event cannot be resolved (the sun does not rise or set that day, an
// unknown event or place), never "no restriction".
type Daylight interface {
	Event(name string, day time.Time, at core.LatLon) (time.Time, error)
}

// ErrNoEvent is returned, wrapped, when the sun does not reach the
// event's altitude on that date at that place (polar day or night).
var ErrNoEvent = errors.New("the event does not occur on that date at that place")

// weekdayNames maps time.Weekday onto ED-318's names.
var weekdayNames = [7]string{"SUN", "MON", "TUE", "WED", "THU", "FRI", "SAT"}

// window is one concrete daily window.
type window struct{ start, end time.Time }

// frame is the location a daily period's dates and weekdays are judged
// in: the offset of its clock times, or UTC when both ends are events.
func (d DailyPeriod) frame() *time.Location {
	for _, t := range []*string{d.StartTime, d.EndTime} {
		if t != nil {
			if c, ok := parseClock(*t); ok {
				return offsetLocation(c.offsetS)
			}
		}
	}
	return time.UTC
}

// usesEvents reports whether either end is a daylight event.
func (d DailyPeriod) usesEvents() bool { return d.StartEvent != nil || d.EndEvent != nil }

// onDay reports whether the period's day list holds the weekday of date.
func (d DailyPeriod) onDay(date time.Time) bool {
	return slices.Contains(d.Day, anyDay) || slices.Contains(d.Day, weekdayNames[date.Weekday()])
}

// at resolves one end on date (midnight of the date in the frame).
func endAt(clock, event *string, date time.Time, where core.LatLon, dl Daylight) (time.Time, error) {
	if clock != nil {
		c, ok := parseClock(*clock)
		if !ok {
			return time.Time{}, core.Fieldf("time", "%q is not an RFC 3339 time with an offset", *clock)
		}
		return date.Add(time.Duration(c.ns)), nil
	}
	if event == nil {
		return time.Time{}, core.Fieldf("time", "neither a time nor an event")
	}
	if dl == nil {
		return time.Time{}, core.Fieldf("daylight", "%s cannot be resolved: no Daylight source", *event)
	}
	if !where.Valid() {
		return time.Time{}, core.Fieldf("where", "%s cannot be resolved: not a valid WGS84 position", *event)
	}
	t, err := dl.Event(*event, date, where)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s on %s: %w", *event, date.Format("2006-01-02"), err)
	}
	return t, nil
}

// windowOn is the window that starts on date (midnight of the date in the
// frame). An end at or before the start belongs to the next date.
func (d DailyPeriod) windowOn(date time.Time, where core.LatLon, dl Daylight) (window, error) {
	start, err := endAt(d.StartTime, d.StartEvent, date, where, dl)
	if err != nil {
		return window{}, err
	}
	end, err := endAt(d.EndTime, d.EndEvent, date, where, dl)
	if err != nil {
		return window{}, err
	}
	if !end.After(start) {
		end, err = endAt(d.EndTime, d.EndEvent, date.AddDate(0, 0, 1), where, dl)
		if err != nil {
			return window{}, err
		}
	}
	return window{start: start, end: end}, nil
}

// contains reports whether at falls in a window of the daily period; both
// ends included. Windows starting the day before, on, and after at's date
// in the frame are tried (a window may run past midnight, and an event
// far from the frame's meridian may fall on a neighbouring date).
func (d DailyPeriod) contains(at time.Time, where core.LatLon, dl Daylight) (bool, error) {
	loc := d.frame()
	local := at.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	var firstErr error
	for _, offset := range []int{-1, 0, 1} {
		date := today.AddDate(0, 0, offset)
		if !d.onDay(date) {
			continue
		}
		w, err := d.windowOn(date, where, dl)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if !at.Before(w.start) && !at.After(w.end) {
			return true, nil
		}
	}
	return false, firstErr
}

// contains reports whether the period applies at at.
func (tp TimePeriod) contains(at time.Time, where core.LatLon, dl Daylight) (bool, error) {
	if tp.StartDateTime != nil && at.Before(tp.StartDateTime.Time) {
		return false, nil
	}
	if tp.EndDateTime != nil && at.After(tp.EndDateTime.Time) {
		return false, nil
	}
	if len(tp.Schedule) == 0 {
		return true, nil
	}
	var firstErr error
	for _, d := range tp.Schedule {
		in, err := d.contains(at, where, dl)
		if in {
			return true, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return false, firstErr
}

// Applies reports whether a zone with these periods applies at at, at the
// place where (used only to resolve daylight events), with the semantics
// of ed269.Applies (LESSONS Z-07, T-09): any period applies; both ends of
// a window are included; a window whose end is at or before its start
// runs into the next day and belongs to the day it starts on; the weekday
// is judged in the schedule's offset (UTC when both ends are events);
// dates bound a schedule. No periods at all is a zone without limited
// applicability: it applies.
//
// Daylight events are resolved per day through dl at where. When no
// period applies and an event could not be resolved (no dl, an invalid
// where, a day without sunrise), the answer is not known: Applies returns
// false with the error that says why, and the caller counts the zone as
// not evaluated. It never reports such a zone as applying or as not
// applying without that error.
func Applies(tp []TimePeriod, at time.Time, where core.LatLon, dl Daylight) (bool, error) {
	if len(tp) == 0 {
		return true, nil
	}
	var firstErr error
	for _, p := range tp {
		in, err := p.contains(at, where, dl)
		if in {
			return true, nil
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return false, fmt.Errorf("ed318: applicability not evaluated: %w", firstErr)
	}
	return false, nil
}
