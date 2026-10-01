package rid

import (
	"math"

	"github.com/rootxkit/uspace-core/odid"
)

// VelocityNED converts a Remote ID ground speed, track over the ground
// (degrees true) and climb rate (positive up) to north, east and down in
// m/s (R-10): vn = v cos(track), ve = v sin(track), vd = -climb.
//
// A speed without a direction is not a velocity: unless both the speed
// and the track are known all three are nil, never zero, because zero
// would claim the aircraft is hovering. With speed and track but no climb
// rate, vn and ve are returned and vd is nil. (utm returned vd alone when
// the horizontal velocity was unknown; R-10 and the plan leave all three
// nil.) Remote ID gives no heading, only the track.
func VelocityNED(speedMS, trackDeg, climbMS *float64) (vn, ve, vd *float64) {
	if speedMS == nil || trackDeg == nil {
		return nil, nil, nil
	}
	rad := *trackDeg * math.Pi / 180
	n := *speedMS * math.Cos(rad)
	e := *speedMS * math.Sin(rad)
	vn, ve = &n, &e
	if climbMS != nil {
		d := -*climbMS
		vd = &d
	}
	return vn, ve, vd
}

// Airborne reports whether a Remote ID status counts as flying (R-11):
// only GROUND is not airborne; undeclared, emergency and Remote ID
// system failure err towards flying. Remote ID has no arming state.
func Airborne(st odid.Status) bool {
	return st.Airborne()
}
