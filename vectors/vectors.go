package vectors

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// File is one vectors/*.json document. Input and Expected stay raw so a
// test decodes them into its own strict structs with Case.Decode.
type File struct {
	Description string            `json:"description"`
	Source      []string          `json:"source"`
	Units       map[string]string `json:"units"`
	Tolerance   map[string]any    `json:"tolerance"`
	Owners      []string          `json:"owners"`
	Generated   string            `json:"generated"`
	UtmCommit   string            `json:"utm_commit"`
	Fixtures    json.RawMessage   `json:"fixtures,omitempty"`
	Cases       []Case            `json:"cases"`

	// Extra holds the header keys some files add (policy, rule, limits,
	// defaults, ellipsoid, byte_order, ...). Read one with Header.
	Extra map[string]json.RawMessage `json:"-"`
	// Name is the file name, for messages.
	Name string `json:"-"`
}

// Case is one input/expected pair.
type Case struct {
	Name     string          `json:"name"`
	Owner    []string        `json:"owner"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
	Why      string          `json:"why"`
	Source   string          `json:"source,omitempty"`
}

// knownHeaderKeys are decoded into File's named fields; everything else
// in the header lands in Extra.
var knownHeaderKeys = map[string]bool{
	"description": true, "source": true, "units": true, "tolerance": true,
	"owners": true, "generated": true, "utm_commit": true, "fixtures": true,
	"cases": true,
}

// TB is the subset of testing.TB the helpers use. *testing.T and
// *testing.B satisfy it; the package's own tests supply a recorder.
type TB interface {
	Helper()
	Logf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// Dir returns the directory holding the vendored vector files. It is
// located relative to this source file, so it works from the module cache
// as well as from a checkout.
func Dir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("vectors", "testdata")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

// Load reads testdata/<name> and fails the test if it cannot, or if the
// file has no cases (E-01: an empty table proves nothing).
func Load(t TB, name string) *File {
	t.Helper()
	f, err := Read(filepath.Join(Dir(), name))
	if err != nil {
		t.Fatalf("vectors %s: %v", name, err)
		return nil
	}
	if len(f.Cases) == 0 {
		t.Fatalf("vectors %s: no cases", name)
		return nil
	}
	return f
}

// Read parses a vector file from disk without a testing.TB, for tools.
func Read(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	f, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	f.Name = filepath.Base(path)
	return f, nil
}

// Parse parses a vector document.
func Parse(raw []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	f.Extra = make(map[string]json.RawMessage)
	for k, v := range header {
		if !knownHeaderKeys[k] {
			f.Extra[k] = v
		}
	}
	seen := make(map[string]bool, len(f.Cases))
	for i, c := range f.Cases {
		if c.Name == "" {
			return nil, fmt.Errorf("case %d has no name", i)
		}
		if seen[c.Name] {
			return nil, fmt.Errorf("case name %q repeated", c.Name)
		}
		seen[c.Name] = true
		if len(c.Owner) == 0 {
			return nil, fmt.Errorf("case %q has no owner", c.Name)
		}
	}
	return &f, nil
}

// Header decodes an extra header key (policy, limits, defaults, ...)
// into v strictly, and fails the test if the key is absent.
func (f *File) Header(t TB, key string, v any) {
	t.Helper()
	raw, ok := f.Extra[key]
	if !ok {
		t.Fatalf("vectors %s: no header key %q", f.Name, key)
		return
	}
	Unmarshal(t, raw, v)
}

// FloatTolerance returns the numeric tolerance recorded in the header
// under key, and false when the entry is absent or textual ("exact").
func (f *File) FloatTolerance(key string) (float64, bool) {
	v, ok := f.Tolerance[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case string:
		// Some files write the number inside prose ("0.1 (the old ...").
		first := strings.Fields(n)
		if len(first) == 0 {
			return 0, false
		}
		x, err := strconv.ParseFloat(first[0], 64)
		if err != nil {
			return 0, false
		}
		return x, true
	}
	return 0, false
}

// Run runs every case as a subtest. uspace-core is the shared
// implementation and runs all cases whatever their owner list. The
// case's `why` is logged so a failure explains what it prevents.
func (f *File) Run(t *testing.T, fn func(t *testing.T, c Case)) {
	t.Helper()
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Logf("why: %s", c.Why)
			if c.Source != "" {
				t.Logf("source: %s", c.Source)
			}
			fn(t, c)
		})
	}
}

// RunOwned runs only the cases that name owner (a system repository's
// code: authority, cisp, ussp, ansp, lab) and fails if none does.
func (f *File) RunOwned(t *testing.T, owner string, fn func(t *testing.T, c Case)) {
	t.Helper()
	ran := 0
	for _, c := range f.Cases {
		if !slices.Contains(c.Owner, owner) {
			continue
		}
		ran++
		t.Run(c.Name, func(t *testing.T) {
			t.Logf("why: %s", c.Why)
			fn(t, c)
		})
	}
	if ran == 0 {
		t.Fatalf("vectors %s: no case is owned by %q", f.Name, owner)
	}
}

// ExpectedIsNull reports whether the whole expected value is null (a
// state that is "not shown", for example rid_time network cases).
func (c Case) ExpectedIsNull() bool {
	return isNull(c.Expected)
}

// Decode unmarshals the case's input and expected into in and exp
// strictly: an unknown or renamed field fails the test instead of reading
// as zero. Pass nil to skip either side.
func (c Case) Decode(t TB, in, exp any) {
	t.Helper()
	if in != nil {
		Unmarshal(t, c.Input, in)
	}
	if exp != nil {
		Unmarshal(t, c.Expected, exp)
	}
}

// Unmarshal decodes raw into v with DisallowUnknownFields and fails the
// test on any error.
func Unmarshal(t TB, raw json.RawMessage, v any) {
	t.Helper()
	if err := StrictUnmarshal(raw, v); err != nil {
		t.Fatalf("decode: %v\n%s", err, truncate(raw, 400))
	}
}

// StrictUnmarshal decodes raw into v, refusing unknown fields and
// trailing data.
func StrictUnmarshal(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}

// Near fails the test unless |got - want| <= tol (tol 0 compares
// exactly). NaN never passes.
func Near(t TB, field string, got, want, tol float64) {
	t.Helper()
	if math.IsNaN(got) || math.IsNaN(want) {
		t.Errorf("%s: got %v, want %v (NaN never matches)", field, got, want)
		return
	}
	if math.Abs(got-want) > tol {
		t.Errorf("%s: got %v, want %v (tolerance %v, off by %v)", field, got, want, tol, got-want)
	}
}

// NearPtr compares optional numbers: both nil passes, one nil fails,
// both present compares with Near. A vector's null is a nil pointer,
// never a zero value.
func NearPtr(t TB, field string, got, want *float64, tol float64) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil:
		t.Errorf("%s: got null, want %v", field, *want)
	case want == nil:
		t.Errorf("%s: got %v, want null", field, *got)
	default:
		Near(t, field, *got, *want, tol)
	}
}

// EqualTime compares two instants with Equal (never ==) and reports the
// difference in microseconds on failure.
func EqualTime(t TB, field string, got, want time.Time) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s: got %s, want %s (off by %d us)", field,
			got.UTC().Format(time.RFC3339Nano), want.UTC().Format(time.RFC3339Nano),
			got.Sub(want).Microseconds())
	}
}

// EqualTimePtr compares optional instants.
func EqualTimePtr(t TB, field string, got, want *time.Time) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil:
		t.Errorf("%s: got null, want %s", field, want.UTC().Format(time.RFC3339Nano))
	case want == nil:
		t.Errorf("%s: got %s, want null", field, got.UTC().Format(time.RFC3339Nano))
	default:
		EqualTime(t, field, *got, *want)
	}
}

// EqualStrPtr compares optional strings exactly.
func EqualStrPtr(t TB, field string, got, want *string) {
	t.Helper()
	switch {
	case got == nil && want == nil:
	case got == nil:
		t.Errorf("%s: got null, want %q", field, *want)
	case want == nil:
		t.Errorf("%s: got %q, want null", field, *got)
	case *got != *want:
		t.Errorf("%s: got %q, want %q", field, *got, *want)
	}
}

func isNull(raw json.RawMessage) bool {
	return len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
