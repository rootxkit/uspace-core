package rid

import (
	"fmt"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/odid"
	"github.com/rootxkit/uspace-core/vectors"
)

// Tolerances mirrored from the vector headers (asserted below).
const (
	tolLatDeg   = 1e-9
	tolAltAMSLM = 1e-9
)

type identitySettings struct {
	IdentityTTLS    *float64 `json:"identity_ttl_s"`
	MaxGapS         *float64 `json:"max_gap_s"`
	IdentifyWithinS *float64 `json:"identify_within_s"`
}

type identityMessage struct {
	Type       string   `json:"type"`
	IDType     *uint8   `json:"id_type"`
	UAID       *string  `json:"ua_id"`
	LatDeg     *float64 `json:"lat_deg"`
	OperatorID *string  `json:"operator_id"`
}

type identityStep struct {
	NowS        float64           `json:"now_s"`
	Messages    []identityMessage `json:"messages"`
	Receiver    *string           `json:"receiver"`
	Transmitter *string           `json:"transmitter"`
}

type identityInput struct {
	Settings identitySettings `json:"settings"`
	Steps    []identityStep   `json:"steps"`
}

type identityObservation struct {
	DroneID    string    `json:"drone_id"`
	Label      string    `json:"label"`
	Identified bool      `json:"identified"`
	UAID       string    `json:"ua_id"`
	IDType     uint8     `json:"id_type"`
	OperatorID *string   `json:"operator_id"`
	LatDeg     float64   `json:"lat_deg"`
	RxTS       time.Time `json:"rx_ts"`
}

type identityExpected struct {
	PerStep  []*identityObservation `json:"per_step"`
	Counters map[string]uint64      `json:"counters"`
}

type identityDefaults struct {
	Receiver    string `json:"receiver"`
	Transmitter string `json:"transmitter"`
}

func (m identityMessage) odid(t *testing.T) odid.Message {
	t.Helper()
	switch m.Type {
	case "basic_id":
		if m.IDType == nil || m.UAID == nil {
			t.Fatalf("basic_id without id_type or ua_id")
		}
		return odid.BasicID{IDType: odid.IDType(*m.IDType), UAID: *m.UAID}
	case "location":
		lat, lon := defaultLatDeg, defaultLonDeg
		if m.LatDeg != nil {
			lat = *m.LatDeg
		}
		return odid.Location{Status: odid.StatusAirborne, LatDeg: &lat, LonDeg: &lon}
	case "operator_id":
		if m.OperatorID == nil {
			t.Fatalf("operator_id without operator_id")
		}
		return odid.OperatorID{OperatorID: *m.OperatorID}
	}
	t.Fatalf("unknown message type %q", m.Type)
	return nil
}

func TestVectorsRidIdentity(t *testing.T) {
	f := vectors.Load(t, "rid_identity.json")
	if tol, ok := f.FloatTolerance("lat_deg"); !ok || tol != tolLatDeg {
		t.Fatalf("rid_identity lat_deg tolerance changed: %v", f.Tolerance["lat_deg"])
	}
	var defaults identityDefaults
	f.Header(t, "defaults", &defaults)
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in identityInput
		var exp identityExpected
		c.Decode(t, &in, &exp)
		if len(exp.PerStep) != len(in.Steps) {
			t.Fatalf("%d steps, %d expected observations", len(in.Steps), len(exp.PerStep))
		}
		s := DefaultSettings()
		if v := in.Settings.IdentityTTLS; v != nil {
			s.IdentityTTLS = *v
		}
		if v := in.Settings.MaxGapS; v != nil {
			s.MaxGapS = *v
		}
		if v := in.Settings.IdentifyWithinS; v != nil {
			s.IdentifyWithinS = *v
		}
		tr := NewTracker(s)
		for i, step := range in.Steps {
			fr := Frame{Receiver: defaults.Receiver, Transmitter: defaults.Transmitter, NowS: step.NowS, RxTS: rxAt(step.NowS)}
			if step.Receiver != nil {
				fr.Receiver = *step.Receiver
			}
			if step.Transmitter != nil {
				fr.Transmitter = *step.Transmitter
			}
			for _, m := range step.Messages {
				fr.Messages = append(fr.Messages, m.odid(t))
			}
			compareObservation(t, fmt.Sprintf("per_step[%d]", i), tr.Take(fr), exp.PerStep[i])
		}
		for _, name := range []string{CounterIdentityChanges, CounterSilences, CounterUnidentified, CounterAddressConflicts} {
			want, ok := exp.Counters[name]
			if !ok {
				t.Errorf("counters: %s missing from the vector", name)
			}
			if got := tr.Counters().Get(name); got != want {
				t.Errorf("counters.%s: got %d, want %d", name, got, want)
			}
		}
		if len(exp.Counters) != 4 {
			t.Errorf("counters: the vector names %d counters, want 4: %v", len(exp.Counters), exp.Counters)
		}
	})
}

func compareObservation(t *testing.T, field string, got *Observation, want *identityObservation) {
	t.Helper()
	switch {
	case got == nil && want == nil:
		return
	case got == nil:
		t.Errorf("%s: got nothing published, want %s", field, want.Label)
		return
	case want == nil:
		t.Errorf("%s: got %s (identified %v) published, want nothing", field, got.Label, got.Identified)
		return
	}
	if got.DroneID != want.DroneID {
		t.Errorf("%s.drone_id: got %s, want %s", field, got.DroneID, want.DroneID)
	}
	if got.Label != want.Label {
		t.Errorf("%s.label: got %q, want %q", field, got.Label, want.Label)
	}
	if got.Identified != want.Identified {
		t.Errorf("%s.identified: got %v, want %v", field, got.Identified, want.Identified)
	}
	if got.UAID != want.UAID {
		t.Errorf("%s.ua_id: got %q, want %q", field, got.UAID, want.UAID)
	}
	if got.IDType != odid.IDType(want.IDType) {
		t.Errorf("%s.id_type: got %d, want %d", field, got.IDType, want.IDType)
	}
	vectors.EqualStrPtr(t, field+".operator_id", got.OperatorID, want.OperatorID)
	vectors.NearPtr(t, field+".lat_deg", got.Location.LatDeg, &want.LatDeg, tolLatDeg)
	vectors.EqualTime(t, field+".rx_ts", got.RxTS, want.RxTS)
}

type altitudeStep struct {
	NowS             float64  `json:"now_s"`
	VertAccuracyCode uint8    `json:"vert_accuracy_code"`
	AltPressureM     *float64 `json:"alt_pressure_m"`
}

// altitudeInput is the union of the stateless and the step cases of
// pressure_altitude.json; Steps is set only on step cases.
type altitudeInput struct {
	AltHAEM             *float64       `json:"alt_hae_m"`
	AltPressureM        *float64       `json:"alt_pressure_m"`
	VertAccuracyCode    *uint8         `json:"vert_accuracy_code"`
	GeoidUndulationM    *float64       `json:"geoid_undulation_m"`
	MinVerticalAccuracy *uint8         `json:"min_vertical_accuracy"`
	HoldPressure        *bool          `json:"hold_pressure"`
	PressureHoldS       *float64       `json:"pressure_hold_s"`
	Steps               []altitudeStep `json:"steps"`
}

type altitudeResult struct {
	AltAMSLM  *float64 `json:"alt_amsl_m"`
	AltSource *string  `json:"alt_source"`
}

type altitudeExpected struct {
	AltAMSLM  *float64         `json:"alt_amsl_m"`
	AltSource *string          `json:"alt_source"`
	PerStep   []altitudeResult `json:"per_step"`
}

func compareAltitude(t *testing.T, field string, got AltResult, want altitudeResult) {
	t.Helper()
	vectors.NearPtr(t, field+".alt_amsl_m", got.AltAMSLM, want.AltAMSLM, tolAltAMSLM)
	wantSource := core.AltNone
	if want.AltSource != nil {
		wantSource = core.AltSource(*want.AltSource)
	}
	if got.Source != wantSource {
		t.Errorf("%s.alt_source: got %q, want %q", field, got.Source, wantSource)
	}
	if (got.AltAMSLM == nil) != (got.Source == core.AltNone) {
		t.Errorf("%s: alt %v with source %q", field, got.AltAMSLM, got.Source)
	}
}

func TestVectorsPressureAltitude(t *testing.T) {
	f := vectors.Load(t, "pressure_altitude.json")
	if tol, ok := f.FloatTolerance("alt_amsl_m"); !ok || tol != tolAltAMSLM {
		t.Fatalf("pressure_altitude alt_amsl_m tolerance changed: %v", f.Tolerance["alt_amsl_m"])
	}
	stateless, stepped := 0, 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in altitudeInput
		var exp altitudeExpected
		c.Decode(t, &in, &exp)
		if in.Steps == nil {
			stateless++
			if in.VertAccuracyCode == nil || in.MinVerticalAccuracy == nil || in.HoldPressure == nil {
				t.Fatal("stateless case without vert_accuracy_code, min_vertical_accuracy or hold_pressure")
			}
			// hold_pressure is "the hold is in force for this message".
			pol := DefaultAltPolicy()
			pol.MinVerticalAccuracy = *in.MinVerticalAccuracy
			got := SelectAltitude(AltInput{
				AltHAEM: in.AltHAEM, AltPressureM: in.AltPressureM,
				VertAccuracyCode: *in.VertAccuracyCode, UndulationM: in.GeoidUndulationM,
				PressureHoldActive: *in.HoldPressure,
			}, pol)
			compareAltitude(t, "result", got, altitudeResult{AltAMSLM: exp.AltAMSLM, AltSource: exp.AltSource})
			return
		}
		stepped++
		if in.PressureHoldS == nil || len(exp.PerStep) != len(in.Steps) {
			t.Fatal("step case without pressure_hold_s or with a per_step of another length")
		}
		pol := DefaultAltPolicy()
		pol.PressureHoldS = *in.PressureHoldS
		sel := NewAltitudeSelector(pol)
		for i, s := range in.Steps {
			got := sel.Select(AltInput{
				AltHAEM: in.AltHAEM, AltPressureM: s.AltPressureM,
				VertAccuracyCode: s.VertAccuracyCode, UndulationM: in.GeoidUndulationM,
			}, s.NowS)
			compareAltitude(t, fmt.Sprintf("per_step[%d]", i), got, exp.PerStep[i])
		}
	})
	if stateless != 12 || stepped != 4 {
		t.Errorf("ran %d stateless and %d step cases, want 12 and 4", stateless, stepped)
	}
}
