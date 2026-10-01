// Command genvectors writes vectors/testdata/ed318_roundtrip.json, the
// ED-318 round-trip knowledge vector (plan section 11 gap 7, milestone
// G-M2), from ed318/testdata/authority_collection.json (the base) and
// source.json beside it.
//
// Both are hand-written: the base is one authority-authored collection
// using every ED-318 property; source.json holds a daylight table and the
// cases. A case selects features of the base
// (all by default) and applies a JSON patch (add, replace, remove by JSON
// pointer) to make its one change, so that every refusal is the accepted
// base with exactly one difference (LESSONS E-01). Expected values are
// written by hand in source.json, never computed by the package under
// test; "$document" stands for the case's own input document. After a
// run, regenerate vectors/testdata/SHA256SUMS.
//
//	go run ./ed318/internal/genvectors [-base ed318/testdata/authority_collection.json] [-src ed318/internal/genvectors/source.json] [-out vectors/testdata/ed318_roundtrip.json]
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// utm is the utm commit every vector file of the pinned lab records.
const utm = "484cd228035f2f424530e233ce72100d0fc1842a"

var owners = []string{"authority", "cisp", "ussp", "ansp"}

type source struct {
	Daylight json.RawMessage `json:"daylight"`
	Cases    []sourceCase    `json:"cases"`
}

type patchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value"`
}

type sourceCase struct {
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Features    []int           `json:"features"`
	Patch       []patchOp       `json:"patch"`
	ED269       json.RawMessage `json:"ed269"`
	Lang        string          `json:"lang"`
	At          string          `json:"at"`
	Where       json.RawMessage `json:"where"`
	UseDaylight bool            `json:"use_daylight"`
	Expected    json.RawMessage `json:"expected"`
	Why         string          `json:"why"`
}

type caseInput struct {
	Kind          string          `json:"kind"`
	Document      any             `json:"document,omitempty"`
	ED269Document json.RawMessage `json:"ed269_document,omitempty"`
	Lang          string          `json:"lang,omitempty"`
	At            string          `json:"at,omitempty"`
	Where         json.RawMessage `json:"where,omitempty"`
	Daylight      json.RawMessage `json:"daylight,omitempty"`
}

type vectorCase struct {
	Name     string    `json:"name"`
	Owner    []string  `json:"owner"`
	Input    caseInput `json:"input"`
	Expected any       `json:"expected"`
	Why      string    `json:"why"`
}

type vectorFile struct {
	Description string            `json:"description"`
	Source      []string          `json:"source"`
	Units       map[string]string `json:"units"`
	Tolerance   map[string]string `json:"tolerance"`
	Owners      []string          `json:"owners"`
	Generated   string            `json:"generated"`
	UtmCommit   string            `json:"utm_commit"`
	Cases       []vectorCase      `json:"cases"`
}

func main() {
	base := flag.String("base", "ed318/testdata/authority_collection.json", "the accepted base collection")
	src := flag.String("src", "ed318/internal/genvectors/source.json", "the cases")
	out := flag.String("out", "vectors/testdata/ed318_roundtrip.json", "output file")
	flag.Parse()
	if err := run(*base, *src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "genvectors:", err)
		os.Exit(1) //nolint:forbidigo // a command reports failure by its exit status
	}
}

func run(basePath, srcPath, out string) error {
	baseRaw, err := os.ReadFile(basePath) //nolint:gosec // G304: the path is this command's own flag, given by the developer running it
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(srcPath) //nolint:gosec // G304: as above
	if err != nil {
		return err
	}
	f, err := build(baseRaw, raw)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(f); err != nil {
		return err
	}
	return os.WriteFile(out, buf.Bytes(), 0o600)
}

// build turns the base and the cases into the vector file.
func build(baseRaw, raw []byte) (*vectorFile, error) {
	var src source
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&src); err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	f := &vectorFile{
		Description: "ED-318 UASZone feature collections (EUROCAE ED-318, spec 02 F1-F3, 04 sections 3.4 and 4): parse and validate on receipt, never repair (06 T9); export equal by value; the ED-269 mapping both ways with what each side cannot hold refused by name; applicability with daylight events (BMCT, SR, SS, EECT) resolved per day, an unresolvable event reported as not evaluated.",
		Source: []string{
			"EUROCAE ED-318 JSON schema, github.com/UASGeoZones/ED-318 schema/ at e98b292c5665a04989d62e32fd93829f161a89a9",
			"InterUSS uas_standards src/uas_standards/eurocae_ed318.py at 6e182f43ec960b3bccf131c9006ff9979dd8a56e",
			"Regulation (EU) 2021/664 Art. 3(4) (the U-space airspace requirements block)",
			"spec 02 F1, F2; 03 geo_zones; 04 sections 3.4 and 4; 09 section 1.6",
			"LESSONS Z-01, Z-04, Z-05, Z-07, D-01, T-09",
		},
		Units: map[string]string{
			"layer upper, lower":    "the layer's uom: m (default) or ft",
			"extent radius":         "metres (unverified against the ED-318 text; the schema gives no unit)",
			"where lat_deg lon_deg": "WGS84 degrees",
			"at, daylight":          "RFC 3339 instants",
		},
		Tolerance: map[string]string{
			"accepted":      "exact",
			"export":        "equal by value (JSON values; member order free)",
			"ed269":         "equal by value after ed269.Export",
			"ed318":         "equal by value after ed318.Export",
			"must_include":  "field ends with field_endswith and the reason contains reason_contains",
			"applies":       "exact",
			"not_evaluated": "exact",
		},
		Owners:    owners,
		Generated: "hand-written in uspace-core WP-12 (ed318/testdata/authority_collection.json and ed318/internal/genvectors/source.json, built by ed318/internal/genvectors); not from utm. Each refusal is the accepted base collection with one JSON-patch change. Regenerate rather than edit.",
		UtmCommit: utm,
	}
	var base map[string]any
	if err := json.Unmarshal(baseRaw, &base); err != nil {
		return nil, fmt.Errorf("base: %w", err)
	}
	for i := range src.Cases {
		c := &src.Cases[i]
		vc, err := buildCase(base, src.Daylight, c)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		f.Cases = append(f.Cases, vc)
	}
	return f, nil
}

func buildCase(base map[string]any, daylight json.RawMessage, c *sourceCase) (vectorCase, error) {
	vc := vectorCase{Name: c.Name, Owner: owners, Why: c.Why, Input: caseInput{Kind: c.Kind}}
	switch c.Kind {
	case "parse", "to_ed269", "applies":
		doc, err := selectFeatures(base, c.Features)
		if err != nil {
			return vc, err
		}
		for _, p := range c.Patch {
			if doc, err = apply(doc, p); err != nil {
				return vc, err
			}
		}
		vc.Input.Document = doc
		if c.Kind == "to_ed269" {
			vc.Input.Lang = c.Lang
		}
		if c.Kind == "applies" {
			vc.Input.At, vc.Input.Where = c.At, c.Where
			if c.UseDaylight {
				vc.Input.Daylight = daylight
			}
		}
	case "from_ed269":
		vc.Input.ED269Document, vc.Input.Lang = c.ED269, c.Lang
	default:
		return vc, fmt.Errorf("unknown kind %q", c.Kind)
	}
	var exp map[string]any
	if err := json.Unmarshal(c.Expected, &exp); err != nil {
		return vc, fmt.Errorf("expected: %w", err)
	}
	if exp["export"] == "$document" {
		exp["export"] = vc.Input.Document
	}
	vc.Expected = exp
	return vc, nil
}

// selectFeatures is a deep copy of base holding only the features named
// (all when none are).
func selectFeatures(base map[string]any, idx []int) (any, error) {
	b, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if len(idx) == 0 {
		return doc, nil
	}
	all, _ := doc["features"].([]any)
	picked := make([]any, 0, len(idx))
	for _, i := range idx {
		if i < 0 || i >= len(all) {
			return nil, fmt.Errorf("feature %d of %d", i, len(all))
		}
		picked = append(picked, all[i])
	}
	doc["features"] = picked
	return doc, nil
}

// apply performs one add, replace or remove at a JSON pointer.
func apply(doc any, p patchOp) (any, error) {
	if !strings.HasPrefix(p.Path, "/") {
		return nil, fmt.Errorf("path %q is not a JSON pointer", p.Path)
	}
	parts := strings.Split(p.Path[1:], "/")
	var value any
	if p.Op != "remove" {
		if err := json.Unmarshal(p.Value, &value); err != nil {
			return nil, fmt.Errorf("%s value: %w", p.Path, err)
		}
	}
	parent := doc
	for _, part := range parts[:len(parts)-1] {
		next, err := child(parent, part)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Path, err)
		}
		parent = next
	}
	last := parts[len(parts)-1]
	switch t := parent.(type) {
	case map[string]any:
		_, exists := t[last]
		switch {
		case p.Op == "add" && exists:
			return nil, fmt.Errorf("%s: add of a member that exists", p.Path)
		case p.Op != "add" && !exists:
			return nil, fmt.Errorf("%s: %s of a member that does not exist", p.Path, p.Op)
		case p.Op == "remove":
			delete(t, last)
		default:
			t[last] = value
		}
	case []any:
		i, err := strconv.Atoi(last)
		if err != nil || i < 0 || i >= len(t) || p.Op != "replace" {
			return nil, fmt.Errorf("%s: only replace of an existing list element is supported", p.Path)
		}
		t[i] = value
	default:
		return nil, fmt.Errorf("%s: the parent is not an object or a list", p.Path)
	}
	return doc, nil
}

func child(v any, part string) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		c, ok := t[part]
		if !ok {
			return nil, fmt.Errorf("no member %q", part)
		}
		return c, nil
	case []any:
		i, err := strconv.Atoi(part)
		if err != nil || i < 0 || i >= len(t) {
			return nil, fmt.Errorf("no element %q", part)
		}
		return t[i], nil
	}
	return nil, errors.New("not an object or a list")
}
