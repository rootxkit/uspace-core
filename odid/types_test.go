package odid

import "testing"

func TestStatusAirborne(t *testing.T) {
	// R-11: only GROUND is not airborne; the others err towards flying.
	if StatusGround.Airborne() {
		t.Fatal("GROUND is not airborne")
	}
	for _, s := range []Status{StatusUndeclared, StatusAirborne, StatusEmergency, StatusRemoteIDSystemFailure} {
		if !s.Airborne() {
			t.Errorf("status %d must count as airborne", s)
		}
	}
}

func TestMessageTypes(t *testing.T) {
	cases := []struct {
		m    Message
		want MessageType
	}{
		{BasicID{}, TypeBasicID}, {Location{}, TypeLocation}, {System{}, TypeSystem},
		{OperatorID{}, TypeOperatorID}, {SelfID{}, TypeSelfID}, {Authentication{}, TypeAuthentication},
	}
	for _, c := range cases {
		if c.m.Type() != c.want {
			t.Errorf("%T.Type() = %d, want %d", c.m, c.m.Type(), c.want)
		}
	}
}
