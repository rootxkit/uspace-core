package rid

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func TestDefaultAltPolicy(t *testing.T) {
	p := DefaultAltPolicy()
	if p.MinVerticalAccuracy != 2 || !p.HoldPressure || p.PressureHoldS != 10 {
		t.Errorf("DefaultAltPolicy = %+v", p)
	}
}

func TestSelectAltitudeDegenerate(t *testing.T) {
	pol := AltPolicy{MinVerticalAccuracy: 2}
	r := SelectAltitude(AltInput{AltHAEM: f64(math.NaN()), AltPressureM: f64(500), VertAccuracyCode: 4, UndulationM: f64(20)}, pol)
	if r.Source != core.AltPressure || *r.AltAMSLM != 500 {
		t.Errorf("NaN HAE: %+v, want pressure", r)
	}
	r = SelectAltitude(AltInput{AltHAEM: f64(520), VertAccuracyCode: 4, UndulationM: f64(math.Inf(1))}, pol)
	if r.Source != core.AltNone || r.AltAMSLM != nil {
		t.Errorf("infinite N: %+v, want none", r)
	}
	r = SelectAltitude(AltInput{AltPressureM: f64(math.NaN())}, pol)
	if r.Source != core.AltNone {
		t.Errorf("NaN pressure: %+v, want none", r)
	}
	// The result does not alias the input.
	in := AltInput{AltPressureM: f64(500)}
	r = SelectAltitude(in, pol)
	*in.AltPressureM = 1
	if *r.AltAMSLM != 500 {
		t.Error("result aliases the input")
	}
}

// With HoldPressure off the selector never holds (E-01 pair of the hold
// vectors).
func TestAltitudeSelectorHoldOff(t *testing.T) {
	pol := DefaultAltPolicy()
	pol.HoldPressure = false
	s := NewAltitudeSelector(pol)
	poor := AltInput{AltHAEM: f64(520), AltPressureM: f64(507.5), VertAccuracyCode: 1, UndulationM: f64(20)}
	good := poor
	good.VertAccuracyCode = 4
	if r := s.Select(poor, 0); r.Source != core.AltPressure {
		t.Errorf("poor: %+v", r)
	}
	if r := s.Select(good, 1); r.Source != core.AltGeodetic || *r.AltAMSLM != 500 {
		t.Errorf("good after poor without hold: %+v", r)
	}
	// A fresh selector with the hold on and no poor fix yet is geodetic.
	if r := NewAltitudeSelector(DefaultAltPolicy()).Select(good, 0); r.Source != core.AltGeodetic {
		t.Errorf("first good fix: %+v", r)
	}
}

func BenchmarkSelectAltitude(b *testing.B) {
	in := AltInput{AltHAEM: f64(520), AltPressureM: f64(507.5), VertAccuracyCode: 4, UndulationM: f64(20)}
	pol := DefaultAltPolicy()
	for b.Loop() {
		_ = SelectAltitude(in, pol)
	}
}

// The default policy enables the selector's hold but never makes the
// stateless path hold: a good geodetic altitude is used (absence), and
// pressure is used only when the caller says the hold is in force
// (presence).
func TestSelectAltitudeDefaultPolicyHoldPair(t *testing.T) {
	pol := DefaultAltPolicy()
	in := AltInput{AltHAEM: f64(520), AltPressureM: f64(507.5), VertAccuracyCode: 4, UndulationM: f64(20)}
	r := SelectAltitude(in, pol)
	if r.Source != core.AltGeodetic || r.AltAMSLM == nil || *r.AltAMSLM != 500 {
		t.Errorf("default policy, hold not active: %+v, want geodetic 500", r)
	}
	in.PressureHoldActive = true
	r = SelectAltitude(in, pol)
	if r.Source != core.AltPressure || r.AltAMSLM == nil || *r.AltAMSLM != 507.5 {
		t.Errorf("default policy, hold active: %+v, want pressure 507.5", r)
	}
}

// The selector decides the hold itself: a caller's PressureHoldActive is
// ignored.
func TestAltitudeSelectorOwnsHoldActive(t *testing.T) {
	s := NewAltitudeSelector(DefaultAltPolicy())
	in := AltInput{AltHAEM: f64(520), AltPressureM: f64(507.5), VertAccuracyCode: 4, UndulationM: f64(20), PressureHoldActive: true}
	if r := s.Select(in, 0); r.Source != core.AltGeodetic {
		t.Errorf("no poor fix yet: %+v, want geodetic", r)
	}
}
