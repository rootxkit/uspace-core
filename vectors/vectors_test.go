package vectors

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestManifest proves the vendored copy is the one the plan pins: every
// manifest file exists, has the recorded case count, matches SHA256SUMS,
// and no stray file sits beside them.
func TestManifest(t *testing.T) {
	sums := readSums(t)
	total := 0
	seen := map[string]bool{}
	for _, e := range Manifest {
		seen[e.File] = true
		f := Load(t, e.File)
		if len(f.Cases) != e.Cases {
			t.Errorf("%s: %d cases, manifest says %d", e.File, len(f.Cases), e.Cases)
		}
		total += len(f.Cases)
		if f.UtmCommit == "" {
			t.Errorf("%s: no utm_commit", e.File)
		}
		if len(f.Owners) == 0 {
			t.Errorf("%s: no owners", e.File)
		}
		raw, err := os.ReadFile(filepath.Join(Dir(), e.File))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		got := hex.EncodeToString(sum[:])
		want, ok := sums[e.File]
		if !ok {
			t.Errorf("%s: not in SHA256SUMS; run scripts/sync-vectors.sh", e.File)
		} else if got != want {
			t.Errorf("%s: sha256 %s, SHA256SUMS says %s: the file was edited by hand", e.File, got, want)
		}
	}
	if total != TotalCases {
		t.Errorf("total cases %d, want %d", total, TotalCases)
	}
	entries, err := os.ReadDir(Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range entries {
		name := de.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if !seen[name] {
			t.Errorf("%s is in testdata but not in the manifest", name)
		}
	}
	for name := range sums {
		if !seen[name] {
			t.Errorf("%s is in SHA256SUMS but not in the manifest", name)
		}
	}
}

func TestVersionFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(Dir(), "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("VERSION line %q is not key=value", line)
		}
		fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	for _, k := range []string{"uspace_lab_commit", "uspace_lab_path", "utm_commit"} {
		if fields[k] == "" {
			t.Errorf("VERSION lacks %s", k)
		}
	}
	if _, ok := fields["local_files"]; !ok {
		t.Error("VERSION lacks local_files (may be empty)")
	}
	if len(fields["uspace_lab_commit"]) != 40 {
		t.Errorf("uspace_lab_commit %q is not a full sha", fields["uspace_lab_commit"])
	}
	local := map[string]bool{}
	for _, name := range strings.Fields(fields["local_files"]) {
		local[name] = true
	}
	// Every lab file must have been generated from the utm commit VERSION
	// says; a local file (written here, proposed upstream) is exempt.
	for _, e := range Manifest {
		if local[e.File] {
			continue
		}
		f := Load(t, e.File)
		if f.UtmCommit != fields["utm_commit"] {
			t.Errorf("%s: utm_commit %s, VERSION says %s", e.File, f.UtmCommit, fields["utm_commit"])
		}
	}
}

func readSums(t *testing.T) map[string]string {
	t.Helper()
	fh, err := os.Open(filepath.Join(Dir(), "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	sums := map[string]string{}
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			t.Fatalf("SHA256SUMS line %q", line)
		}
		sums[strings.TrimPrefix(parts[1], "*")] = parts[0]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return sums
}

func TestParseRejectsMalformedDocuments(t *testing.T) {
	// E-01: the refusals, each beside the acceptance below.
	bad := map[string]string{
		"not json":      `{`,
		"unnamed case":  `{"cases":[{"owner":["authority"],"input":{},"expected":{}}]}`,
		"repeated name": `{"cases":[{"name":"a","owner":["x"]},{"name":"a","owner":["x"]}]}`,
		"no owner":      `{"cases":[{"name":"a","input":{},"expected":{}}]}`,
	}
	for name, doc := range bad {
		if _, err := Parse([]byte(doc)); err == nil {
			t.Errorf("%s: Parse accepted %s", name, doc)
		}
	}
	good := `{"description":"d","owners":["authority"],"utm_commit":"abc","policy":{"x":1},
	  "tolerance":{"t":0.01,"s":"exact","d":"0.1 (rounded)"},
	  "cases":[{"name":"a","owner":["authority"],"input":{"v":1},"expected":null,"why":"w"}]}`
	f, err := Parse([]byte(good))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.Extra["policy"]; !ok {
		t.Fatal("extra header key policy must be kept")
	}
	if _, ok := f.Extra["tolerance"]; ok {
		t.Fatal("known header keys do not land in Extra")
	}
	if !f.Cases[0].ExpectedIsNull() {
		t.Fatal("expected null must be reported as null")
	}
	if tol, ok := f.FloatTolerance("t"); !ok || tol != 0.01 {
		t.Fatalf("numeric tolerance: %v %v", tol, ok)
	}
	if _, ok := f.FloatTolerance("s"); ok {
		t.Fatal("\"exact\" is not a numeric tolerance")
	}
	if tol, ok := f.FloatTolerance("d"); !ok || tol != 0.1 {
		t.Fatalf("prose tolerance: %v %v", tol, ok)
	}
	if _, ok := f.FloatTolerance("absent"); ok {
		t.Fatal("absent key")
	}
	var pol struct {
		X int `json:"x"`
	}
	f.Header(t, "policy", &pol)
	if pol.X != 1 {
		t.Fatal("Header decode")
	}
}

func TestStrictUnmarshal(t *testing.T) {
	type in struct {
		A int `json:"a"`
	}
	var v in
	if err := StrictUnmarshal([]byte(`{"a":1}`), &v); err != nil || v.A != 1 {
		t.Fatalf("accept: %v", err)
	}
	// A renamed field must fail, not read as zero (knowledge/README.md).
	if err := StrictUnmarshal([]byte(`{"b":1}`), &v); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err := StrictUnmarshal([]byte(`{"a":1} {"a":2}`), &v); err == nil {
		t.Fatal("trailing data accepted")
	}
}

func TestRunAndRunOwned(t *testing.T) {
	doc := `{"cases":[
	  {"name":"a","owner":["authority"],"input":{},"expected":{}},
	  {"name":"b","owner":["cisp"],"input":{},"expected":{}}]}`
	f, err := Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	all := 0
	f.Run(t, func(_ *testing.T, _ Case) { all++ })
	if all != 2 {
		t.Fatalf("Run ran %d cases, want 2", all)
	}
	owned := 0
	f.RunOwned(t, "cisp", func(_ *testing.T, c Case) {
		owned++
		if c.Name != "b" {
			t.Errorf("RunOwned ran %s", c.Name)
		}
	})
	if owned != 1 {
		t.Fatalf("RunOwned ran %d, want 1", owned)
	}
}

// recorder stands in for *testing.T so the helpers' own failures can be
// observed instead of failing this test (E-01: test presence).
type recorder struct {
	errors []string
	fatal  bool
}

func (r *recorder) Helper()                   {}
func (r *recorder) Logf(string, ...any)       {}
func (r *recorder) Errorf(f string, a ...any) { r.errors = append(r.errors, fmt.Sprintf(f, a...)) }
func (r *recorder) Fatalf(f string, a ...any) { r.fatal = true; r.Errorf(f, a...) }
func (r *recorder) failed() bool              { return len(r.errors) > 0 }

func TestComparators(t *testing.T) {
	one, two := 1.0, 2.0
	s1, s2 := "x", "y"
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Microsecond)
	tz := t0.In(time.FixedZone("tbilisi", 4*3600))
	nan := math.NaN()

	cases := []struct {
		name string
		fail bool
		fn   func(r TB)
	}{
		{"near-inside", false, func(r TB) { Near(r, "f", 1.0, 1.005, 0.01) }},
		{"near-outside", true, func(r TB) { Near(r, "f", 1.0, 1.02, 0.01) }},
		{"near-nan", true, func(r TB) { Near(r, "f", 0, nan, 1) }},
		{"near-exact", false, func(r TB) { Near(r, "f", 2.5, 2.5, 0) }},
		{"near-exact-differs", true, func(r TB) { Near(r, "f", 2.5, 2.5000001, 0) }},
		{"nearptr-both-nil", false, func(r TB) { NearPtr(r, "f", nil, nil, 0) }},
		{"nearptr-got-nil", true, func(r TB) { NearPtr(r, "f", nil, &one, 0) }},
		{"nearptr-want-nil", true, func(r TB) { NearPtr(r, "f", &one, nil, 0) }},
		{"nearptr-differ", true, func(r TB) { NearPtr(r, "f", &one, &two, 0.5) }},
		{"nearptr-equal", false, func(r TB) { NearPtr(r, "f", &one, &one, 0) }},
		{"time-equal-across-zones", false, func(r TB) { EqualTime(r, "t", t0, tz) }},
		{"time-one-microsecond", true, func(r TB) { EqualTime(r, "t", t0, t1) }},
		{"timeptr-nil", false, func(r TB) { EqualTimePtr(r, "t", nil, nil) }},
		{"timeptr-mixed", true, func(r TB) { EqualTimePtr(r, "t", &t0, nil) }},
		{"timeptr-mixed-2", true, func(r TB) { EqualTimePtr(r, "t", nil, &t0) }},
		{"timeptr-equal", false, func(r TB) { EqualTimePtr(r, "t", &t0, &tz) }},
		{"strptr-equal", false, func(r TB) { EqualStrPtr(r, "s", &s1, &s1) }},
		{"strptr-differ", true, func(r TB) { EqualStrPtr(r, "s", &s1, &s2) }},
		{"strptr-nil", true, func(r TB) { EqualStrPtr(r, "s", nil, &s1) }},
		{"strptr-nil-2", true, func(r TB) { EqualStrPtr(r, "s", &s1, nil) }},
		{"strptr-both-nil", false, func(r TB) { EqualStrPtr(r, "s", nil, nil) }},
	}
	for _, c := range cases {
		r := &recorder{}
		c.fn(r)
		if r.failed() != c.fail {
			t.Errorf("%s: failed=%v (%v), want %v", c.name, r.failed(), r.errors, c.fail)
		}
	}
}

func TestDecodeIsStrict(t *testing.T) {
	c := Case{Input: json.RawMessage(`{"a":1}`), Expected: json.RawMessage(`{"b":"x"}`)}
	var in struct {
		A int `json:"a"`
	}
	var exp struct {
		B string `json:"b"`
	}
	c.Decode(t, &in, &exp)
	if in.A != 1 || exp.B != "x" {
		t.Fatal("decode")
	}
	var wrong struct {
		Z int `json:"z"`
	}
	r := &recorder{}
	c.Decode(r, &wrong, nil)
	if !r.fatal {
		t.Fatal("a renamed field must fail the test, not read as zero")
	}
}

func TestLoadRefusals(t *testing.T) {
	r := &recorder{}
	Load(r, "does-not-exist.json")
	if !r.fatal {
		t.Fatal("a missing file must fail")
	}
	f := &File{Name: "x", Extra: map[string]json.RawMessage{}}
	r = &recorder{}
	var v any
	f.Header(r, "policy", &v)
	if !r.fatal {
		t.Fatal("a missing header key must fail")
	}
}
