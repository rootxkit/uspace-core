package ed318

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/ed269"
	"github.com/rootxkit/uspace-core/geodesy"
	"github.com/rootxkit/uspace-core/zones"
)

func baseCollection(t *testing.T) *FeatureCollection {
	t.Helper()
	fc, probs := Parse(baseBytes(t), Limits{})
	if probs != nil {
		t.Fatal(probs)
	}
	return fc
}

func TestToZones(t *testing.T) {
	fc := baseCollection(t)
	zs, err := ToZones(fc, NOAADaylight{})
	if err != nil {
		t.Fatal(err)
	}
	if len(zs) != 6 {
		t.Fatalf("%d zones, want 6 (TSC001 has two layers)", len(zs))
	}
	byID := func(i int, id string) *zones.Zone {
		if zs[i].Identifier != id || zs[i].Country != "GEO" {
			t.Fatalf("zone %d is %s", i, zs[i].Identifier)
		}
		return zs[i]
	}
	u := byID(0, "TSU001")
	if u.Type != core.ZoneUSpace || u.Restriction != "" || u.Upper.Ref != core.RefWGS84 || u.Upper.ValueM != 300 || u.Polygon == nil {
		t.Errorf("TSU001 %+v", u)
	}
	d := byID(1, "TSD001")
	if d.Type != core.ZoneProhibited || d.Restriction != ed269.RestrictionProhibited || d.Circle == nil || d.Circle.RadiusM != 1500 {
		t.Errorf("TSD001 %+v", d)
	}
	if d.Upper.ValueM != 2500*core.FeetToMetres || d.Upper.Ref != core.RefAMSL || d.Lower.ValueM != 0 {
		t.Errorf("TSD001 limits %+v %+v", d.Lower, d.Upper)
	}
	if in, err := d.ContainsHorizontally(tbilisi); !in || err != nil {
		t.Errorf("the circle's centre: %v %v", in, err)
	}
	r := byID(2, "TSR001")
	if r.Restriction != ed269.RestrictionReqAuthorisation || len(r.Polygon.Rings) != 2 {
		t.Errorf("TSR001 %+v", r)
	}
	// Inside the hole is outside the zone.
	if in, _ := r.ContainsHorizontally(core.LatLon{LatDeg: 41.71, LonDeg: 44.81}); in {
		t.Error("a point in the hole is inside")
	}
	c1, c2 := byID(3, "TSC001"), byID(4, "TSC001")
	if c1.Upper.ValueM != 50 || c2.Lower.ValueM != 50 || c2.Upper.ValueM != 150 || c1.Type != core.ZoneConditional {
		t.Errorf("TSC001 layers %+v / %+v", c1, c2)
	}
	n := byID(5, "TSN001")
	if n.Lower != nil || n.Upper != nil || n.Periods != nil || !n.AppliesAt(utc(t, "2030-01-01T00:00:00Z")) {
		t.Errorf("TSN001 %+v", n)
	}
}

// zones.Zone.AppliesAt on the converted periods agrees with Applies on the
// ED-318 periods at every quarter hour of the zones' dates (TSD001's
// daylight window resolved at the circle's centre, which is where Applies
// is asked here).
func TestToZonesAgreesWithApplies(t *testing.T) {
	fc := baseCollection(t)
	zs, err := ToZones(fc, NOAADaylight{})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []struct{ feature, zone int }{{1, 1}, {2, 2}, {3, 3}} {
		f := fc.Features[k.feature]
		z := zs[k.zone]
		where := center(z.BBox)
		yes, no := 0, 0
		for at := utc(t, "2026-09-30T00:00:00Z"); at.Before(utc(t, "2026-10-12T00:00:00Z")); at = at.Add(15 * time.Minute) {
			want, err := Applies(f.Properties.LimitedApplicability, at, where, NOAADaylight{})
			if err != nil {
				t.Fatal(err)
			}
			if got := z.AppliesAt(at); got != want {
				t.Errorf("%s at %s: zone applies %v, Applies %v", z.Identifier, at.Format(time.RFC3339), got, want)
			}
			if want {
				yes++
			} else {
				no++
			}
		}
		// Both answers occur, so the agreement is not vacuous.
		if yes == 0 || no == 0 {
			t.Errorf("%s: applies at %d instants and not at %d", z.Identifier, yes, no)
		}
		t.Logf("%s: applies at %d of %d instants", z.Identifier, yes, yes+no)
	}
}

func TestToZonesDaylightRefusals(t *testing.T) {
	cases := map[string]struct {
		mut    func(fc *FeatureCollection)
		dl     Daylight
		reason string
	}{
		"no end date": {func(fc *FeatureCollection) { fc.Features[1].Properties.LimitedApplicability[0].EndDateTime = nil }, NOAADaylight{}, "without startDateTime and endDateTime"},
		"too long": {func(fc *FeatureCollection) {
			fc.Features[1].Properties.LimitedApplicability[0].EndDateTime = &DateTime{Time: time.Date(2027, 10, 9, 0, 0, 0, 0, time.UTC)}
		}, NOAADaylight{}, "at most 366"},
		"no daylight":  {func(*FeatureCollection) {}, nil, "no Daylight source"},
		"not in table": {func(*FeatureCollection) {}, FixedDaylight{}, "not evaluated"},
		"polar": {func(fc *FeatureCollection) {
			c := core.LatLon{LatDeg: 78, LonDeg: 15}
			fc.Features[1].Geometry.Center = &c
			fc.Features[1].Properties.LimitedApplicability[0].StartDateTime = &DateTime{Time: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)}
			fc.Features[1].Properties.LimitedApplicability[0].EndDateTime = &DateTime{Time: time.Date(2026, 6, 8, 0, 0, 0, 0, time.UTC)}
		}, NOAADaylight{}, "does not occur"},
	}
	for name, c := range cases {
		fc := baseCollection(t)
		c.mut(fc)
		_, err := ToZones(fc, c.dl)
		var fe *core.FieldError
		if !errors.As(err, &fe) || !strings.HasPrefix(fe.Field, "features[1].properties.limitedApplicability[0]") || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// E-01: at MaxEventDays the period is resolved.
	fc := baseCollection(t)
	fc.Features[1].Properties.LimitedApplicability[0].EndDateTime = &DateTime{Time: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, MaxEventDays)}
	zs, err := ToZones(fc, NOAADaylight{})
	if err != nil {
		t.Fatalf("%d days: %v", MaxEventDays, err)
	}
	if n := len(zs[1].Periods); n < MaxEventDays || n > MaxEventDays+2 {
		t.Errorf("%d periods for %d days", n, MaxEventDays)
	}
}

// When every window is clipped away the zone gets a period that never
// applies, never no periods (which zones.Zone reads as "always").
func TestToZonesNoWindowNeverApplies(t *testing.T) {
	fc := baseCollection(t)
	// 2026-10-01 to 2026-10-03 is Thursday to Saturday: no Monday.
	tp := &fc.Features[1].Properties.LimitedApplicability[0]
	tp.EndDateTime = &DateTime{Time: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	tp.Schedule[0].Day = []string{"MON"}
	zs, err := ToZones(fc, NOAADaylight{})
	if err != nil {
		t.Fatal(err)
	}
	z := zs[1]
	if len(z.Periods) != 1 {
		t.Fatalf("periods %+v", z.Periods)
	}
	for at := utc(t, "2026-09-28T00:00:00Z"); at.Before(utc(t, "2026-10-10T00:00:00Z")); at = at.Add(time.Hour) {
		if z.AppliesAt(at) {
			t.Fatalf("applies at %s", at)
		}
	}
	if !zs[2].AppliesAt(utc(t, "2026-10-05T10:00:00Z")) {
		t.Error("TSR001 (twin) does not apply on a Monday at 14:00 local")
	}
}

func TestToZonesRefusals(t *testing.T) {
	if _, err := ToZones(nil, nil); err == nil {
		t.Error("nil collection")
	}
	bad := 1.0
	cases := map[string]struct {
		mut   func(fc *FeatureCollection)
		field string
	}{
		"type":            {func(fc *FeatureCollection) { fc.Features[0].Properties.Type = "FORBIDDEN" }, "features[0].geometry"},
		"lower reference": {func(fc *FeatureCollection) { fc.Features[0].Geometry.Layer.LowerReference = "SFC" }, "features[0].geometry.layer.lower"},
		"upper reference": {func(fc *FeatureCollection) { fc.Features[0].Geometry.Layer.UpperReference = "" }, "features[0].geometry.layer.upper"},
		"upper not finite": {func(fc *FeatureCollection) {
			v := posInf()
			fc.Features[0].Geometry.Layer.Upper = &v
		}, "features[0].geometry.layer.upper"},
		"lower above upper": {func(fc *FeatureCollection) {
			v := 400.0
			fc.Features[0].Geometry.Layer.Lower = &v
			fc.Features[0].Geometry.Layer.LowerReference = core.RefWGS84
		}, "features[0].geometry.layer.upper"},
		"no rings":    {func(fc *FeatureCollection) { fc.Features[0].Geometry.Rings = nil }, "features[0].geometry.coordinates"},
		"bad ring":    {func(fc *FeatureCollection) { fc.Features[0].Geometry.Rings[0] = fc.Features[0].Geometry.Rings[0][:3] }, "features[0].geometry.coordinates[0]"},
		"no centre":   {func(fc *FeatureCollection) { fc.Features[1].Geometry.Center = nil }, "features[1].geometry.coordinates"},
		"bad radius":  {func(fc *FeatureCollection) { fc.Features[1].Geometry.RadiusM = &bad; bad = -1 }, "features[1].geometry.extent.radius"},
		"member type": {func(fc *FeatureCollection) { fc.Features[3].Geometry.Geometries[1].Type = "LineString" }, "features[3].geometry.geometries[1].type"},
		"clock offsets": {func(fc *FeatureCollection) {
			fc.Features[2].Properties.LimitedApplicability[0].Schedule[0].EndTime = strp("18:00:00Z")
		}, "features[2].properties.limitedApplicability[0].schedule"},
	}
	for name, c := range cases {
		fc := baseCollection(t)
		c.mut(fc)
		_, err := ToZones(fc, NOAADaylight{})
		var fe *core.FieldError
		if !errors.As(err, &fe) || !strings.HasPrefix(fe.Field, c.field) {
			t.Errorf("%s: %v, want a FieldError on %q", name, err, c.field)
		}
	}
}

func TestCenterAcrossTheAntimeridian(t *testing.T) {
	c := center(geodesyBox(170, -170))
	if c.LonDeg != 180 && c.LonDeg != -180 {
		t.Errorf("centre of a box from 170 to -170: %v", c.LonDeg)
	}
	if c := center(geodesyBox(10, 20)); c.LonDeg != 15 {
		t.Errorf("centre of 10..20: %v", c.LonDeg)
	}
}

func posInf() float64 { return math.Inf(1) }

func geodesyBox(minLon, maxLon float64) geodesy.BBox {
	return geodesy.BBox{MinLat: 0, MaxLat: 1, MinLon: minLon, MaxLon: maxLon}
}
