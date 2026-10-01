package ed318

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/geodesy"
	"github.com/rootxkit/uspace-core/zones"
)

// MaxEventDays bounds how many days of a schedule that uses daylight
// events ToZones resolves into fixed windows (E-10): a TimePeriod with
// events must give startDateTime and endDateTime no further apart.
// zones.Zone holds applicability as ed269 periods, which have no events,
// so each day becomes one period; a year of them costs a judgement about
// 366 period checks for that zone.
const MaxEventDays = 366

// ToZones builds the judgement's view of every zone of fc, as
// zones.FromED269 does for ED-269: one *zones.Zone per geometry part,
// limits in metres (feet converted with core.FeetToMetres
// exactly) in their reference, the published polygon or circle (the
// circle's radius in metres), and the applicability as ed269 periods. A
// zone without limitedApplicability applies always (no periods).
//
// Type is the ED-318 type; Restriction is its ED-269 spelling, empty for
// USPACE (which has none).
//
// A GeometryCollection gives one zone per layer, and each gets its own
// Identifier, the feature's identifier followed by "/L" and the layer's
// index in the collection's geometries: "TSC001/L0", "TSC001/L1". Alerts
// are keyed by country and identifier, so two layers of one zone are two
// keys, and a key does not depend on where the feature sits in the
// collection. A zone with one geometry keeps the plain identifier. Two
// zones with one key (a collection built in code with a feature
// identified "C1/L0" beside layer 0 of "C1", or a repeated identifier)
// are refused, naming both; Parse refuses a "/" in an identifier and a
// repeated identifier, so a parsed collection never collides.
//
// Daylight events are resolved through dl, at the centre of the part's
// bounding box, for every day from startDateTime to endDateTime into
// fixed windows (sunrise moves by about 4 minutes per degree of longitude,
// so across a zone 10 km wide at 42 degrees north by under 30 s). A
// period with events and no end dates, or more than MaxEventDays apart,
// an event the place cannot resolve on one of its days (polar day or
// night), or no dl, is refused with an error naming the feature: never a
// zone that applies always or never. Use Applies for open-ended
// schedules.
//
// A *core.FieldError names the feature for a zone it cannot judge safely:
// a polygon ring geodesy.ValidRing refuses, a circle without a valid
// centre or positive radius, a limit that is not finite or not referenced
// AGL, AMSL or WGS84, a lower limit not below an upper one in the same
// reference.
func ToZones(fc *FeatureCollection, dl Daylight) ([]*zones.Zone, error) {
	if fc == nil {
		return nil, core.Fieldf("$", "no feature collection")
	}
	var out []*zones.Zone
	keys := map[string]string{} // country/identifier -> the path that made it
	for i := range fc.Features {
		f := &fc.Features[i]
		path := index("features", i)
		parts := f.Geometry.parts()
		for k := range parts {
			g := parts[k]
			gpath := path + ".geometry"
			if f.Geometry.Type == GeometryCollection {
				gpath = index(gpath+".geometries", k)
			}
			z, err := partZone(&f.Properties, g, gpath)
			if err != nil {
				return nil, err
			}
			if f.Geometry.Type == GeometryCollection {
				z.Identifier = PartIdentifier(f.Properties.Identifier, k)
			}
			key := z.Country + "/" + z.Identifier
			if first, dup := keys[key]; dup {
				return nil, core.Fieldf(gpath, "makes the zone key %q, which %s makes too; alerts are keyed by country and identifier, so the zones would be one", key, first)
			}
			keys[key] = gpath
			periods, err := zonePeriods(f.Properties.LimitedApplicability, center(z.BBox), dl, path+".properties.limitedApplicability")
			if err != nil {
				return nil, err
			}
			z.Periods = periods
			out = append(out, z)
		}
	}
	return out, nil
}

// PartIdentifier is the identifier ToZones gives layer k of a zone
// published as a GeometryCollection: "<identifier>/L<k>".
func PartIdentifier(identifier string, k int) string {
	return identifier + "/L" + strconv.Itoa(k)
}

// partZone builds the zone of one geometry part, without periods.
func partZone(props *UASZone, g Geometry, path string) (*zones.Zone, error) {
	if !props.Type.Valid() {
		return nil, core.Fieldf(path, "type %q is not an ED-318 zone type", string(props.Type))
	}
	z := &zones.Zone{Identifier: props.Identifier, Country: props.Country, Type: props.Type}
	if props.Type != core.ZoneUSpace {
		z.Restriction = ed269.Restriction(props.Type.ED269())
	}
	if l := g.Layer; l != nil {
		var err error
		if z.Lower, err = limitOf(l.LowerM(), l.LowerReference, path+".layer.lower"); err != nil {
			return nil, err
		}
		if z.Upper, err = limitOf(l.UpperM(), l.UpperReference, path+".layer.upper"); err != nil {
			return nil, err
		}
	}
	if z.Lower != nil && z.Upper != nil && z.Lower.Ref == z.Upper.Ref && z.Lower.ValueM >= z.Upper.ValueM {
		return nil, core.Fieldf(path+".layer.upper", "is not above lower")
	}
	switch g.Type {
	case GeometryPolygon:
		if len(g.Rings) == 0 {
			return nil, core.Fieldf(path+".coordinates", "has no ring")
		}
		poly := &geodesy.Polygon{Rings: make([]geodesy.Ring, len(g.Rings))}
		for i, r := range g.Rings {
			ring := geodesy.Ring(slices.Clone(r))
			if err := geodesy.ValidRing(ring, maxRingVertices); err != nil {
				return nil, core.Fieldf(index(path+".coordinates", i), "%v", err)
			}
			poly.Rings[i] = ring
		}
		z.Polygon = poly
		z.BBox = poly.BBox()
	case GeometryPoint:
		if g.Center == nil || !g.Center.Valid() {
			return nil, core.Fieldf(path+".coordinates", "is missing or not a valid WGS84 position")
		}
		if g.RadiusM == nil || !core.IsFinite(*g.RadiusM) || *g.RadiusM <= 0 || *g.RadiusM > MaxCircleRadiusM {
			return nil, core.Fieldf(path+".extent.radius", "must be finite, positive and at most %v m", float64(MaxCircleRadiusM))
		}
		z.Circle = &geodesy.Circle{Center: *g.Center, RadiusM: *g.RadiusM}
		z.BBox = z.Circle.BBox()
	default:
		return nil, core.Fieldf(path+".type", "%q is neither a Polygon nor a Point with a Circle extent", g.Type)
	}
	return z, nil
}

// maxRingVertices bounds one ring, as Parse does by default.
var maxRingVertices = ed269.DefaultLimits.MaxRingVertices

// limitOf checks one converted limit.
func limitOf(valueM *float64, ref core.VerticalRef, field string) (*zones.Limit, error) {
	if valueM == nil {
		return nil, nil
	}
	if !core.IsFinite(*valueM) {
		return nil, core.Fieldf(field, "is not finite (%v)", *valueM)
	}
	if !ref.Valid() {
		return nil, core.Fieldf(field, "reference %q is not AGL, AMSL or WGS84", string(ref))
	}
	return &zones.Limit{ValueM: *valueM, Ref: ref}, nil
}

// center is the middle of a box, across the antimeridian when the box
// crosses it.
func center(b geodesy.BBox) core.LatLon {
	lon := (b.MinLon + b.MaxLon) / 2
	if b.MinLon > b.MaxLon {
		lon = core.WrapLonDeg((b.MinLon + b.MaxLon + 360) / 2)
	}
	return core.LatLon{LatDeg: (b.MinLat + b.MaxLat) / 2, LonDeg: lon}
}

// zonePeriods converts limitedApplicability into ed269 periods.
func zonePeriods(tps []TimePeriod, where core.LatLon, dl Daylight, path string) ([]ed269.Period, error) {
	if len(tps) == 0 {
		return nil, nil
	}
	var out []ed269.Period
	for i, tp := range tps {
		here := index(path, i)
		base := ed269.Period{}
		if tp.StartDateTime != nil {
			t := tp.StartDateTime.Time.UTC()
			base.Start = &t
		}
		if tp.EndDateTime != nil {
			t := tp.EndDateTime.Time.UTC()
			base.End = &t
		}
		if base.Start == nil && base.End == nil && len(tp.Schedule) == 0 {
			out = append(out, ed269.Period{Permanent: true})
			continue
		}
		var clock []ed269.DailyPeriod
		var events []DailyPeriod
		for _, d := range tp.Schedule {
			if d.usesEvents() {
				events = append(events, d)
				continue
			}
			dp, err := clockDaily(d)
			if err != nil {
				return nil, core.Fieldf(here+".schedule", "%v", err)
			}
			clock = append(clock, dp)
		}
		if len(tp.Schedule) == 0 || len(clock) > 0 {
			p := base
			p.Schedule = clock
			out = append(out, p)
		}
		if len(events) > 0 {
			ps, err := eventPeriods(tp, events, where, dl, here)
			if err != nil {
				return nil, err
			}
			out = append(out, ps...)
		}
	}
	if len(out) == 0 {
		// Every window was clipped away. zones.Zone reads no periods as
		// "applies always", so the zone gets one period that never
		// applies (its end is before its start).
		start := time.Unix(0, 0).UTC()
		end := start.Add(-time.Nanosecond)
		out = append(out, ed269.Period{Start: &start, End: &end})
	}
	return out, nil
}

// clockDaily converts a daily period with clock times at both ends.
func clockDaily(d DailyPeriod) (ed269.DailyPeriod, error) {
	if d.StartTime == nil || d.EndTime == nil {
		return ed269.DailyPeriod{}, fmt.Errorf("a daily period needs a start and an end")
	}
	s, okS := parseClock(*d.StartTime)
	e, okE := parseClock(*d.EndTime)
	if !okS || !okE {
		return ed269.DailyPeriod{}, fmt.Errorf("%q to %q is not two RFC 3339 times", *d.StartTime, *d.EndTime)
	}
	if s.offsetS != e.offsetS {
		return ed269.DailyPeriod{}, fmt.Errorf("start and end have different offsets")
	}
	loc := offsetLocation(s.offsetS)
	ref := time.Date(2000, 1, 1, 0, 0, 0, 0, loc)
	days := make([]time.Weekday, 0, 7)
	for wd := time.Sunday; wd <= time.Saturday; wd++ {
		if slices.Contains(d.Day, anyDay) || slices.Contains(d.Day, weekdayNames[wd]) {
			days = append(days, wd)
		}
	}
	return ed269.DailyPeriod{
		Days:   days,
		Start:  ref.Add(time.Duration(s.ns)),
		End:    ref.Add(time.Duration(e.ns)),
		Offset: loc,
	}, nil
}

// eventPeriods resolves the daily periods that use daylight events into
// one fixed window per day, clipped to the period's dates.
func eventPeriods(tp TimePeriod, events []DailyPeriod, where core.LatLon, dl Daylight, path string) ([]ed269.Period, error) {
	if tp.StartDateTime == nil || tp.EndDateTime == nil {
		return nil, core.Fieldf(path, "uses daylight events without startDateTime and endDateTime; judge it with ed318.Applies")
	}
	start, end := tp.StartDateTime.Time, tp.EndDateTime.Time
	if days := end.Sub(start).Hours() / 24; days > MaxEventDays {
		return nil, core.Fieldf(path, "uses daylight events over %.0f days; at most %d are resolved", math.Ceil(days), MaxEventDays)
	}
	if dl == nil {
		return nil, core.Fieldf(path, "uses daylight events and no Daylight source was given: not evaluated")
	}
	var out []ed269.Period
	for _, d := range events {
		loc := d.frame()
		first := start.In(loc).AddDate(0, 0, -1)
		date := time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc)
		for !date.After(end) {
			if d.onDay(date) {
				w, err := d.windowOn(date, where, dl)
				if err != nil {
					return nil, core.Fieldf(path, "not evaluated: %v", err)
				}
				s, e := maxTime(w.start, start), minTime(w.end, end)
				if !e.Before(s) {
					s, e = s.UTC(), e.UTC()
					out = append(out, ed269.Period{Start: &s, End: &e})
				}
			}
			date = date.AddDate(0, 0, 1)
		}
	}
	return out, nil
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
