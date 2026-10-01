package vectors

// Entry says which package of this module owns a vector file and how
// many cases it holds at the pinned uspace-lab commit (testdata/VERSION).
type Entry struct {
	File    string
	Package string // import path relative to the module root
	Cases   int
}

// Manifest is the authoritative list. TestManifest checks it against the
// files on disk; each package's vectors_test.go must load the file named
// here. A new file in uspace-lab is added here with its owning package,
// never run "somewhere".
var Manifest = []Entry{
	{"alert_lifecycle.json", "alerting", 28},
	{"cpa.json", "cpa", 27},
	{"ed269_parse.json", "ed269", 52},
	{"ed318_roundtrip.json", "ed318", 21},
	{"fleet_match.json", "identify", 10},
	{"geodesy.json", "geodesy", 17},
	{"identification_status.json", "identify", 37},
	{"jwt_verify.json", "auth", 16},
	{"odid_decode.json", "odid", 187},
	{"pressure_altitude.json", "rid", 16},
	{"rid_identity.json", "rid", 24},
	{"rid_receiver_auth.json", "auth", 14},
	{"rid_time.json", "timeplace", 25},
	{"serials_and_registration.json", "serial", 33},
	{"source_control.json", "sources", 8},
	{"terrain_geoid.json", "terrain", 48},
	{"zones_applicability.json", "ed269", 32},
	{"zones_vertical.json", "zones", 38},
}

// TotalCases is the number of cases across every file: 596 at the pinned
// lab commit (knowledge/README.md) plus the 16 of the local
// jwt_verify.json (WP-11) and the 21 of the local ed318_roundtrip.json
// (WP-12).
const TotalCases = 633
