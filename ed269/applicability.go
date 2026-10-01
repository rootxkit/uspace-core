package ed269

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// DailyPeriod is one entry of a period's `schedule`: the days it applies
// on and a clock window in one offset. Start and End carry the clock
// time of day in Offset (their date is meaningless). An End before its
// Start runs past midnight and belongs to the day it starts on (Z-07).
type DailyPeriod struct {
	// Days the window starts on; ANY in the file is all seven.
	Days   []time.Weekday
	Start  time.Time
	End    time.Time
	Offset *time.Location

	// As published, for export.
	dayText   []string
	startText string
	endText   string
}

// Period is one `applicability` entry. A permanent period applies at all
// times. Otherwise Start and End (both included) bound it and Schedule,
// when given, narrows it to daily windows.
//
// The zero value never applies: a period that is not permanent and has
// no Start, no End and no Schedule says nothing about when it applies.
// Parse refuses one ("permanent NO needs a startDateTime, an endDateTime
// or a schedule"), and Contains guards against one built in code rather
// than reading it as "always". Set Permanent for a period that always
// applies.
type Period struct {
	Permanent bool
	Start     *time.Time
	End       *time.Time
	Schedule  []DailyPeriod

	// As published, for export.
	startText string
	endText   string
}

// nsPerDay is the length of a clock day.
const nsPerDay = int64(24 * time.Hour)

// clockNS is the time of day of t in its own location, in nanoseconds.
func clockNS(t time.Time) int64 {
	h, m, s := t.Clock()
	return int64(h)*int64(time.Hour) + int64(m)*int64(time.Minute) +
		int64(s)*int64(time.Second) + int64(t.Nanosecond())
}

// Contains reports whether at falls in the daily window. The weekday is
// judged in the period's own offset; both ends are included.
func (d DailyPeriod) Contains(at time.Time) bool {
	loc := d.Offset
	if loc == nil {
		loc = time.UTC
	}
	local := at.In(loc)
	clock := clockNS(local)
	start, end := clockNS(d.Start), clockNS(d.End)
	today := slices.Contains(d.Days, local.Weekday())
	if start <= end {
		return today && start <= clock && clock <= end
	}
	if today && clock >= start {
		return true
	}
	yesterday := (local.Weekday() + 6) % 7
	return slices.Contains(d.Days, yesterday) && clock <= end
}

// Contains reports whether the period applies at at.
func (p Period) Contains(at time.Time) bool {
	if p.Permanent {
		return true
	}
	if p.Start == nil && p.End == nil && len(p.Schedule) == 0 {
		return false
	}
	if p.Start != nil && at.Before(*p.Start) {
		return false
	}
	if p.End != nil && at.After(*p.End) {
		return false
	}
	if len(p.Schedule) == 0 {
		return true
	}
	for i := range p.Schedule {
		if p.Schedule[i].Contains(at) {
			return true
		}
	}
	return false
}

// Applies reports whether a zone with these periods applies at at: when
// any period does (Z-07). at is an instant; its location only says how it
// was written, and the comparison is in UTC (T-09). time.Local is never
// consulted. Evaluate at the aircraft's placed time, not arrival time.
func Applies(periods []Period, at time.Time) bool {
	at = at.UTC()
	for i := range periods {
		if periods[i].Contains(at) {
			return true
		}
	}
	return false
}

// ParseApplicability reads a zone's `applicability` list on its own, with
// the same rules as Parse. Problems name paths under `applicability`.
func ParseApplicability(raw json.RawMessage) ([]Period, *Problems) {
	lim := DefaultLimits
	ps := &collector{max: lim.MaxProblems}
	if len(raw) > lim.MaxBytes {
		ps.add("applicability", fmt.Sprintf("is %d bytes; at most %d", len(raw), lim.MaxBytes))
		return nil, ps.result()
	}
	v := decodeTree(raw, lim.MaxDepth, "applicability", ps)
	if v == nil {
		return nil, ps.result()
	}
	p := &parser{lim: lim, ps: ps}
	periods := p.applicability(v, "applicability")
	if r := ps.result(); r != nil {
		return nil, r
	}
	return periods, nil
}

// applicability reads a zone's period list; nil when refused.
func (p *parser) applicability(v *value, where string) []Period {
	if !present(v) {
		p.ps.add(where, "missing: required, at least one period")
		return nil
	}
	if v.kind != kindArray || len(v.arr) == 0 {
		p.ps.add(where, "must be a list of at least one period")
		return nil
	}
	periods := make([]Period, 0, len(v.arr))
	ok := true
	for i, raw := range v.arr {
		period, good := p.period(raw, index(where, i))
		if !good {
			ok = false
			continue
		}
		periods = append(periods, period)
	}
	if !ok {
		return nil
	}
	return periods
}

var periodFields = []string{"permanent", "startDateTime", "endDateTime", "schedule"}

// period reads one applicability entry.
func (p *parser) period(v *value, where string) (Period, bool) {
	if v.kind != kindObject {
		p.ps.add(where, "a period must be an object")
		return Period{}, false
	}
	ok := p.unknown(v, where, periodFields, "unknown field; not part of ED-269")
	permanent, okPerm := p.enum(v.get("permanent"), join(where, "permanent"), yesNoValues)
	start, okStart := p.instant(v.get("startDateTime"), join(where, "startDateTime"))
	end, okEnd := p.instant(v.get("endDateTime"), join(where, "endDateTime"))
	schedule, okSched := p.schedule(v.get("schedule"), join(where, "schedule"))
	if !okPerm || !okStart || !okEnd || !okSched {
		return Period{}, false
	}
	out := Period{Permanent: permanent == string(Yes), Schedule: schedule}
	if permanent == string(Yes) {
		for _, name := range []string{"startDateTime", "endDateTime", "schedule"} {
			if present(v.get(name)) {
				p.ps.add(join(where, name),
					"a permanent period (permanent YES) applies at all times and has no "+name)
				ok = false
			}
		}
	} else if start == nil && end == nil && schedule == nil {
		p.ps.add(where, "permanent NO needs a startDateTime, an endDateTime or a schedule to say when it applies")
		ok = false
	}
	if start != nil && end != nil && !start.t.Before(end.t) {
		p.ps.add(join(where, "endDateTime"), "is not after startDateTime")
		ok = false
	}
	if !ok {
		return Period{}, false
	}
	if start != nil {
		out.Start, out.startText = &start.t, start.text
	}
	if end != nil {
		out.End, out.endText = &end.t, end.text
	}
	return out, true
}

// instantValue is a parsed date-time with its published text.
type instantValue struct {
	t    time.Time
	text string
}

// instantLayouts are the date-time forms accepted, each with an offset
// written Z, +hh:mm or +hhmm: the same offset rule as a clock time.
// time.Parse also accepts a fractional second after the seconds.
var instantLayouts = []string{
	time.RFC3339, "2006-01-02T15:04Z07:00",
	"2006-01-02T15:04:05Z0700", "2006-01-02T15:04Z0700",
}

// naiveLayouts are date-times without an offset: refused by name.
var naiveLayouts = []string{"2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"}

// instant reads an optional date-time. It returns nil when absent and
// false when refused.
func (p *parser) instant(v *value, where string) (*instantValue, bool) {
	if !present(v) {
		return nil, true
	}
	if v.kind != kindString {
		p.ps.add(where, "must be an ISO 8601 date-time, not "+describe(v))
		return nil, false
	}
	for _, layout := range instantLayouts {
		if t, err := time.Parse(layout, v.s); err == nil {
			return &instantValue{t: t.UTC(), text: v.s}, true
		}
	}
	for _, layout := range naiveLayouts {
		if _, err := time.Parse(layout, v.s); err == nil {
			p.ps.add(where, quote(v.s)+" has no offset; give Z or +hh:mm")
			return nil, false
		}
	}
	p.ps.add(where, quote(v.s)+" is not an ISO 8601 date-time")
	return nil, false
}

// schedule reads an optional list of daily periods. It returns nil when
// absent and false when refused.
func (p *parser) schedule(v *value, where string) ([]DailyPeriod, bool) {
	if !present(v) {
		return nil, true
	}
	if v.kind != kindArray || len(v.arr) == 0 {
		p.ps.add(where, "must be a list of at least one daily period")
		return nil, false
	}
	out := make([]DailyPeriod, 0, len(v.arr))
	ok := true
	for i, raw := range v.arr {
		d, good := p.daily(raw, index(where, i))
		if !good {
			ok = false
			continue
		}
		out = append(out, d)
	}
	return out, ok
}

var dailyFields = []string{"day", "startTime", "endTime"}

// dayNames are ED-269's weekdays, Monday first; ANY is every day.
var dayNames = []string{"MON", "TUE", "WED", "THU", "FRI", "SAT", "SUN"}

const anyDay = "ANY"

var dayValues = append(slices.Clone(dayNames), anyDay)

// weekdayOf maps an ED-269 day name onto time.Weekday.
var weekdayOf = map[string]time.Weekday{
	"MON": time.Monday, "TUE": time.Tuesday, "WED": time.Wednesday,
	"THU": time.Thursday, "FRI": time.Friday, "SAT": time.Saturday,
	"SUN": time.Sunday,
}

// daily reads one schedule entry.
func (p *parser) daily(v *value, where string) (DailyPeriod, bool) {
	if v.kind != kindObject {
		p.ps.add(where, "a daily period must be an object")
		return DailyPeriod{}, false
	}
	ok := p.unknown(v, where, dailyFields, "unknown field; not part of ED-269")
	days, dayText, okDays := p.days(v.get("day"), join(where, "day"))
	start, okStart := p.clock(v.get("startTime"), join(where, "startTime"))
	end, okEnd := p.clock(v.get("endTime"), join(where, "endTime"))
	if !ok || !okDays || !okStart || !okEnd {
		return DailyPeriod{}, false
	}
	if start.offsetS != end.offsetS {
		p.ps.add(join(where, "endTime"), "has a different offset from startTime; a daily period needs one")
		return DailyPeriod{}, false
	}
	if start.ns == end.ns {
		p.ps.add(join(where, "endTime"), "is the same as startTime")
		return DailyPeriod{}, false
	}
	loc := offsetLocation(start.offsetS)
	return DailyPeriod{
		Days:      days,
		Start:     clockTime(start.ns, loc),
		End:       clockTime(end.ns, loc),
		Offset:    loc,
		dayText:   dayText,
		startText: start.text,
		endText:   end.text,
	}, true
}

// days reads a daily period's day list.
func (p *parser) days(v *value, where string) ([]time.Weekday, []string, bool) {
	allowed := strings.Join(dayValues, ", ")
	if !present(v) || v.kind != kindArray || len(v.arr) < 1 || len(v.arr) > 7 {
		p.ps.add(where, "must be a list of 1 to 7 of "+allowed)
		return nil, nil, false
	}
	var set [7]bool
	text := make([]string, 0, len(v.arr))
	for i, d := range v.arr {
		here := index(where, i)
		if d.kind == kindString && d.s == anyDay {
			// ANY is every day, so with any other entry some day is
			// listed twice, whichever comes first.
			if i > 0 {
				p.ps.add(here, "ANY is listed twice or with a day it already covers")
				return nil, nil, false
			}
			for k := range set {
				set[k] = true
			}
			text = append(text, d.s)
			continue
		}
		wd, known := weekdayOf[d.s]
		if d.kind != kindString || !known {
			p.ps.add(here, show(d)+" is not one of "+allowed)
			return nil, nil, false
		}
		if set[wd] {
			if text[0] == anyDay {
				p.ps.add(here, d.s+" is listed twice: ANY already covers it")
				return nil, nil, false
			}
			p.ps.add(here, d.s+" is listed twice")
			return nil, nil, false
		}
		set[wd] = true
		text = append(text, d.s)
	}
	days := make([]time.Weekday, 0, 7)
	for _, name := range dayNames {
		if wd := weekdayOf[name]; set[wd] {
			days = append(days, wd)
		}
	}
	return days, text, true
}

// clockValue is a parsed time of day with its offset.
type clockValue struct {
	ns      int64
	offsetS int
	text    string
}

// clock reads a time of day with an offset: hh:mm, hh:mm:ss or
// hh:mm:ss.f (1 to 6 digits), then Z or +hh:mm, -hh:mm, +hhmm.
func (p *parser) clock(v *value, where string) (clockValue, bool) {
	if v != nil && v.kind == kindString {
		if c, ok := parseClock(v.s); ok {
			return c, true
		}
	}
	p.ps.add(where, show(v)+" is not a time of day with an offset, e.g. 17:00:00.00Z or 08:30+04:00")
	return clockValue{}, false
}

func parseClock(s string) (clockValue, bool) {
	c := clockValue{text: s}
	h, ok1 := twoDigits(s, 23)
	m, ok2 := twoDigits(s[min(3, len(s)):], 59)
	if !ok1 || !ok2 || len(s) < 5 || s[2] != ':' {
		return c, false
	}
	rest := s[5:]
	sec, frac := 0, int64(0)
	if strings.HasPrefix(rest, ":") {
		var ok bool
		sec, ok = twoDigits(rest[1:], 59)
		if !ok {
			return c, false
		}
		rest = rest[3:]
		if strings.HasPrefix(rest, ".") {
			n := 0
			for n < len(rest)-1 && n < 7 && isDigit(rest[1+n]) {
				n++
			}
			if n < 1 || n > 6 {
				return c, false
			}
			for i := range 6 {
				frac *= 10
				if i < n {
					frac += int64(rest[1+i] - '0')
				}
			}
			rest = rest[1+n:]
		}
	}
	off, ok := parseOffset(rest)
	if !ok {
		return c, false
	}
	c.ns = int64(h)*int64(time.Hour) + int64(m)*int64(time.Minute) +
		int64(sec)*int64(time.Second) + frac*int64(time.Microsecond)
	c.offsetS = off
	return c, true
}

// parseOffset reads Z, +hh:mm, +hhmm (or -).
func parseOffset(s string) (int, bool) {
	if s == "Z" {
		return 0, true
	}
	if len(s) < 5 || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	h, ok := twoDigits(s[1:], 23)
	if !ok {
		return 0, false
	}
	mm := s[3:]
	if mm[0] == ':' {
		mm = mm[1:]
	}
	m, ok := twoDigits(mm, 59)
	if !ok || len(mm) != 2 {
		return 0, false
	}
	off := h*3600 + m*60
	if s[0] == '-' {
		off = -off
	}
	return off, true
}

// twoDigits reads two ASCII digits at the start of s, at most hi.
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

// clockTime places a time of day on a fixed reference date in loc.
func clockTime(ns int64, loc *time.Location) time.Time {
	return time.Date(2000, 1, 1, 0, 0, 0, 0, loc).Add(time.Duration(ns % nsPerDay))
}
