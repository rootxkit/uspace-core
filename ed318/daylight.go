package ed318

import (
	"fmt"
	"math"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// Zenith angles of the events: the sun's centre 0.833 degrees below the
// horizon for sunrise and sunset (refraction and the sun's radius), 6
// degrees for civil twilight (NOAA Solar Calculator).
const (
	zenithSunDeg   = 90.833
	zenithCivilDeg = 96
)

// NOAADaylight resolves the four ED-318 daylight events with the NOAA
// Solar Calculator algorithm (Meeus, "Astronomical Algorithms"):
// sunrise and sunset at a solar zenith of 90.833 degrees, the beginning
// of morning and end of evening civil twilight (BMCT, EECT) at 96
// degrees, for the date of day at the place at, in pure Go.
//
// Accuracy: NOAA states the algorithm is within one minute between 72
// degrees north and south for dates from 1800 to 2100 (and within ten
// minutes nearer the poles); the tests check it to within 60 s against
// times computed with astropy at Tbilisi, Oslo and Sydney. Refraction is
// the standard 0.833 degrees, terrain and height above sea level are not
// taken into account, so an observer on a ridge sees the sun earlier.
// Where the sun does not reach the event's altitude that day (polar day
// or night) Event returns an error wrapping ErrNoEvent: never a guess.
type NOAADaylight struct{}

// Event implements Daylight.
func (NOAADaylight) Event(name string, day time.Time, at core.LatLon) (time.Time, error) {
	var zenith float64
	var rising bool
	switch name {
	case EventSR:
		zenith, rising = zenithSunDeg, true
	case EventSS:
		zenith, rising = zenithSunDeg, false
	case EventBMCT:
		zenith, rising = zenithCivilDeg, true
	case EventEECT:
		zenith, rising = zenithCivilDeg, false
	default:
		return time.Time{}, core.Fieldf("event", "%q is not one of BMCT, SR, SS, EECT", name)
	}
	if !at.Valid() {
		return time.Time{}, core.Fieldf("at", "not a valid WGS84 position (lat_deg %v, lon_deg %v)", at.LatDeg, at.LonDeg)
	}
	y, m, d := day.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	jd0 := julianDay(y, int(m), d)
	minutes := 720 - 4*at.LonDeg // solar noon, the first guess
	for range 3 {
		next, ok := eventMinutes(jd0+minutes/1440, at, zenith, rising)
		if !ok {
			return time.Time{}, fmt.Errorf("%s at %.4f, %.4f: %w", name, at.LatDeg, at.LonDeg, ErrNoEvent)
		}
		minutes = next
	}
	return midnight.Add(time.Duration(minutes * float64(time.Minute))).Round(time.Second), nil
}

// julianDay is the Julian day at 0h UTC of a Gregorian date.
func julianDay(y, m, d int) float64 {
	if m <= 2 {
		y--
		m += 12
	}
	a := y / 100
	b := 2 - a + a/4
	return math.Floor(365.25*float64(y+4716)) + math.Floor(30.6001*float64(m+1)) + float64(d) + float64(b) - 1524.5
}

const degToRad = math.Pi / 180

// eventMinutes is the event's time in minutes after 0h UTC of the date,
// with the sun's position taken at the Julian day jd; false when the sun
// does not reach the zenith angle that day.
func eventMinutes(jd float64, at core.LatLon, zenithDeg float64, rising bool) (float64, bool) {
	jc := (jd - 2451545) / 36525
	eqTimeMin, declRad := sunPosition(jc)
	latRad := at.LatDeg * degToRad
	cosHA := math.Cos(zenithDeg*degToRad)/(math.Cos(latRad)*math.Cos(declRad)) - math.Tan(latRad)*math.Tan(declRad)
	if cosHA < -1 || cosHA > 1 || math.IsNaN(cosHA) {
		return 0, false
	}
	haDeg := math.Acos(cosHA) / degToRad
	if !rising {
		haDeg = -haDeg
	}
	// Minutes after 0h UTC of the date: the event around that date's
	// solar noon at this longitude (it may fall on the UTC day before or
	// after for a place far from Greenwich).
	return 720 - 4*(at.LonDeg+haDeg) - eqTimeMin, true
}

// sunPosition is the equation of time in minutes and the sun's
// declination in radians at jc Julian centuries from J2000 (NOAA).
func sunPosition(jc float64) (eqTimeMin, declRad float64) {
	l0 := math.Mod(280.46646+jc*(36000.76983+jc*0.0003032), 360)
	mDeg := 357.52911 + jc*(35999.05029-0.0001537*jc)
	e := 0.016708634 - jc*(0.000042037+0.0000001267*jc)
	mRad := mDeg * degToRad
	c := math.Sin(mRad)*(1.914602-jc*(0.004817+0.000014*jc)) +
		math.Sin(2*mRad)*(0.019993-0.000101*jc) +
		math.Sin(3*mRad)*0.000289
	trueLong := l0 + c
	omega := (125.04 - 1934.136*jc) * degToRad
	appLong := (trueLong - 0.00569 - 0.00478*math.Sin(omega)) * degToRad
	meanObliq := 23 + (26+(21.448-jc*(46.815+jc*(0.00059-jc*0.001813)))/60)/60
	obliq := (meanObliq + 0.00256*math.Cos(omega)) * degToRad
	declRad = math.Asin(math.Sin(obliq) * math.Sin(appLong))
	y := math.Tan(obliq / 2)
	y *= y
	l0Rad := l0 * degToRad
	eq := y*math.Sin(2*l0Rad) - 2*e*math.Sin(mRad) + 4*e*y*math.Sin(mRad)*math.Cos(2*l0Rad) -
		0.5*y*y*math.Sin(4*l0Rad) - 1.25*e*e*math.Sin(2*mRad)
	return 4 * eq / degToRad, declRad
}

// FixedDaylight resolves events from a table, for tests and for a caller
// that holds published times: FixedDaylight[date][event] with date as
// "2006-01-02" (the date of day in its own location). The place is not
// used. A missing entry is an error wrapping ErrNoEvent.
type FixedDaylight map[string]map[string]time.Time

// Event implements Daylight.
func (f FixedDaylight) Event(name string, day time.Time, _ core.LatLon) (time.Time, error) {
	date := day.Format("2006-01-02")
	if t, ok := f[date][name]; ok {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%s on %s is not in the table: %w", name, date, ErrNoEvent)
}
