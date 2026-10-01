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
	{"alert_lifecycle.json", "alerting", 35},
	{"cpa.json", "cpa", 37},
	{"ed269_parse.json", "ed269", 54},
	{"ed318_roundtrip.json", "ed318", 22},
	{"fleet_match.json", "identify", 14},
	{"geodesy.json", "geodesy", 17},
	{"identification_status.json", "identify", 43},
	{"jwt_verify.json", "auth", 16},
	{"odid_decode.json", "odid", 187},
	{"pressure_altitude.json", "rid", 16},
	{"rid_identity.json", "rid", 24},
	{"rid_receiver_auth.json", "auth", 14},
	{"rid_time.json", "timeplace", 25},
	{"serials_and_registration.json", "serial", 41},
	{"source_control.json", "sources", 8},
	{"terrain_geoid.json", "terrain", 48},
	{"zones_applicability.json", "ed269", 32},
	{"zones_vertical.json", "zones", 49},
}

// TotalCases is the number of cases across every file at the pinned lab
// commit (knowledge/README.md): 682, jwt_verify.json (WP-11) and
// ed318_roundtrip.json (WP-12) included. No file is local.
const TotalCases = 682
