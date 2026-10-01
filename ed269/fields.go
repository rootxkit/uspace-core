package ed269

import (
	"bytes"
	"slices"
	"strings"
)

// parser carries the limits and the problems of one input.
type parser struct {
	lim Limits
	ps  *collector
}

var (
	restrictionValues = []string{"PROHIBITED", "REQ_AUTHORISATION", "CONDITIONAL", "NO_RESTRICTION"}
	reasonValues      = []string{
		"AIR_TRAFFIC", "SENSITIVE", "PRIVACY", "POPULATION", "NATURE",
		"NOISE", "FOREIGN_TERRITORY", "EMERGENCY", "OTHER",
	}
	typeValues    = []string{"COMMON", "CUSTOMIZED"}
	purposeValues = []string{"AUTHORIZATION", "NOTIFICATION", "INFORMATION"}
	yesNoValues   = []string{"YES", "NO"}
	uomValues     = []string{"M", "FT"}
	refValues     = []string{"AGL", "AMSL", "WGS84"}
)

// unknown reports every member of v not in allowed; false when any.
func (p *parser) unknown(v *value, path string, allowed []string, reason string) bool {
	ok := true
	for _, k := range v.keys {
		if !slices.Contains(allowed, k) {
			p.ps.add(join(path, k), reason)
			ok = false
		}
	}
	return ok
}

// enum checks that v is one of allowed.
func (p *parser) enum(v *value, where string, allowed []string) (string, bool) {
	list := strings.Join(allowed, ", ")
	if v == nil || v.kind != kindString {
		p.ps.add(where, "must be one of "+list+", not "+describe(v))
		return "", false
	}
	if !slices.Contains(allowed, v.s) {
		p.ps.add(where, quote(v.s)+" is not one of "+list)
		return "", false
	}
	return v.s, true
}

// quote writes a string as the reasons show it: 'value'.
func quote(s string) string { return "'" + s + "'" }

// show writes a value as the reasons show it: strings quoted, the rest as
// JSON.
func show(v *value) string {
	if v == nil {
		return "null"
	}
	if v.kind == kindString {
		return quote(v.s)
	}
	var b bytes.Buffer
	compact(v, &b)
	return b.String()
}
