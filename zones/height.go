package zones

import "github.com/rootxkit/uspace-core/core"

// JudgeHeightLimit judges the height limit over the ground (the 120 m
// rule, LESSONS D-04; at the authority only, spec 09 §2): the height is
// the AMSL altitude less the DEM ground, and only a height strictly
// greater than pol.MaxHeightAGLM raises (exactly at the limit is
// allowed). The raise is kind "height", a warning, with height_agl_m and
// max_height_agl_m. A pressure altitude is judged on the indicated
// height and flagged vertical_known false (R-09); so is an altitude
// source this package does not know.
//
// With pol.MaxHeightAGLM nil the rule is not in force here and the
// result is empty. Unknown ground or no terrain is NotEvaluated, never
// judged against 0 (D-04), and so is a missing or non-finite altitude or
// a limit that is negative or not finite. These count under
// CounterHeightNotEvaluated, not under the zone counters.
func JudgeHeightLimit(ac Aircraft, env Env, pol Policy) Result {
	if pol.MaxHeightAGLM == nil {
		return Result{height: true}
	}
	maxM := *pol.MaxHeightAGLM
	if !core.IsFinite(maxM) || maxM < 0 {
		return Result{NotEvaluated: true, Reasons: ReasonsOf(ReasonInvalidPolicy), height: true}
	}
	alt, widened, ok := altitude(ac)
	if !ok {
		return Result{NotEvaluated: true, Reasons: ReasonsOf(ReasonNoAltitude), height: true}
	}
	heightAGLM, why := heightIn(core.RefAGL, alt, env)
	if why != "" {
		return Result{NotEvaluated: true, Reasons: ReasonsOf(why), height: true}
	}
	if heightAGLM <= maxM {
		return Result{height: true}
	}
	d := Detail{HeightAGLM: ptr(heightAGLM), MaxHeightAGLM: ptr(maxM)}
	if widened {
		d.VerticalKnown = ptr(false)
	}
	return Result{Raise: &Raise{Kind: KindHeight, Severity: core.SeverityWarning, Detail: d}, height: true}
}
