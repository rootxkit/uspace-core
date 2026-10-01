package f3548

import (
	"fmt"

	"github.com/rootxkit/uspace-core/core"
)

// What UnmarshalOperationalIntent checks after decoding, and nothing more
// (a member not listed is passed through as decoded). Required members are
// those the OpenAPI file requires; ranges are its minimum and maximum:
//
//   - reference: id, manager, uss_base_url and subscription_id present and
//     not empty; state one of the four OperationalIntentState values;
//     uss_availability one of UssAvailabilityState; time_start and time_end
//     present, format RFC3339, non-zero, and the end not before the start.
//     version is an int32 by decoding.
//   - details.volumes and details.off_nominal_volumes: each what
//     Volume4DToZonesEnvelope requires (one outline, valid vertices or
//     centre, a positive radius in M, RFC3339 times in order), each
//     altitude W84, M and -8000 to 100000 m (OpenAPI Altitude), and the
//     lower altitude not above the upper.
//
// A refusal is a *core.FieldError naming the member by its JSON path.

// The OpenAPI Altitude.value range in metres.
const (
	altitudeMinM = -8000
	altitudeMaxM = 100000
)

type checker struct{ err error }

func (c *checker) fail(path, format string, args ...any) {
	if c.err == nil {
		c.err = core.Fieldf(path, format, args...)
	}
}

func (c *checker) time(path string, t Time) {
	if t.Format != RFC3339 {
		c.fail(path+".format", "%q is not RFC3339", string(t.Format))
	}
	if t.Value.IsZero() {
		c.fail(path+".value", "missing: required")
	}
}

func (c *checker) text(path, s string) {
	if s == "" {
		c.fail(path, "missing: required")
	}
}

func (c *checker) altitude(path string, a *Altitude) (float64, bool) {
	if a == nil {
		return 0, false
	}
	v, err := a.HAEM()
	if err != nil {
		c.fail(path, "%v", err)
		return 0, false
	}
	if v < altitudeMinM || v > altitudeMaxM {
		c.fail(path+".value", "%v is outside %d to %d m", v, altitudeMinM, altitudeMaxM)
		return 0, false
	}
	return v, true
}

func (c *checker) volumes(path string, vs *[]Volume4D) {
	if vs == nil {
		return
	}
	for i := range *vs {
		v := &(*vs)[i]
		here := fmt.Sprintf("%s[%d]", path, i)
		if _, _, _, err := Volume4DToZonesEnvelope(*v); err != nil {
			c.fail(here, "%v", err)
		}
		lo, okLo := c.altitude(here+".volume.altitude_lower", v.Volume.AltitudeLower)
		hi, okHi := c.altitude(here+".volume.altitude_upper", v.Volume.AltitudeUpper)
		if okLo && okHi && lo > hi {
			c.fail(here+".volume.altitude_upper", "%v is below altitude_lower %v", hi, lo)
		}
	}
}

func (c *checker) intent(path string, oi *OperationalIntent) {
	r := &oi.Reference
	here := path + ".reference"
	c.text(here+".id", r.Id)
	c.text(here+".manager", r.Manager)
	c.text(here+".uss_base_url", r.UssBaseUrl)
	c.text(here+".subscription_id", r.SubscriptionId)
	if !r.State.Valid() {
		c.fail(here+".state", "%q is not one of the four DSS states", string(r.State))
	}
	if !r.UssAvailability.Valid() {
		c.fail(here+".uss_availability", "%q is not a UssAvailabilityState", string(r.UssAvailability))
	}
	c.time(here+".time_start", r.TimeStart)
	c.time(here+".time_end", r.TimeEnd)
	if !r.TimeStart.Value.IsZero() && r.TimeEnd.Value.Before(r.TimeStart.Value) {
		c.fail(here+".time_end", "is before time_start")
	}
	c.volumes(path+".details.volumes", oi.Details.Volumes)
	c.volumes(path+".details.off_nominal_volumes", oi.Details.OffNominalVolumes)
}
