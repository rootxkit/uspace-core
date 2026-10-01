package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/vectors"
)

const basePath = "../../testdata/authority_collection.json"

// The committed vector is what the generator writes from source.json:
// nobody edited it by hand, and a change to source.json is regenerated.
func TestCommittedVectorIsGenerated(t *testing.T) {
	out := filepath.Join(t.TempDir(), "ed318_roundtrip.json")
	if err := run(basePath, "source.json", out); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(vectors.Dir(), "ed318_roundtrip.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("vectors/testdata/ed318_roundtrip.json differs from the generator's output: run go run ./ed318/internal/genvectors and regenerate SHA256SUMS")
	}
	f, err := vectors.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) != 21 || len(f.Owners) != 4 || f.UtmCommit != utm {
		t.Errorf("%d cases, owners %v, utm %s", len(f.Cases), f.Owners, f.UtmCommit)
	}
}

// Every refusal of kind parse is the accepted base with exactly the
// change its patch makes: the input documents differ from the accepted
// one, and only there.
func TestRefusalsDifferFromTheBaseOnce(t *testing.T) {
	baseRaw, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("source.json")
	if err != nil {
		t.Fatal(err)
	}
	f, err := build(baseRaw, raw)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := json.Marshal(f.Cases[0].Input.Document)
	for _, c := range f.Cases[1:] {
		if c.Input.Kind != "parse" {
			continue
		}
		doc, _ := json.Marshal(c.Input.Document)
		if bytes.Equal(doc, base) {
			t.Errorf("%s: the input is the base", c.Name)
		}
	}
}

func TestPatchErrors(t *testing.T) {
	doc := map[string]any{"a": map[string]any{"b": 1}, "l": []any{1, 2}}
	for name, p := range map[string]patchOp{
		"not a pointer":    {Op: "add", Path: "a", Value: json.RawMessage("1")},
		"bad value":        {Op: "add", Path: "/c", Value: json.RawMessage("{")},
		"add existing":     {Op: "add", Path: "/a", Value: json.RawMessage("1")},
		"replace missing":  {Op: "replace", Path: "/z", Value: json.RawMessage("1")},
		"no parent member": {Op: "add", Path: "/z/y", Value: json.RawMessage("1")},
		"no parent index":  {Op: "add", Path: "/l/7/y", Value: json.RawMessage("1")},
		"scalar parent":    {Op: "add", Path: "/a/b/c", Value: json.RawMessage("1")},
		"list add":         {Op: "add", Path: "/l/0", Value: json.RawMessage("1")},
		"under a scalar":   {Op: "add", Path: "/a/b/c/d", Value: json.RawMessage("1")},
	} {
		if _, err := apply(doc, p); err == nil {
			t.Errorf("%s: applied", name)
		}
	}
	got, err := apply(doc, patchOp{Op: "remove", Path: "/a/b"})
	if err != nil || len(got.(map[string]any)["a"].(map[string]any)) != 0 {
		t.Errorf("remove: %v %v", got, err)
	}
	if _, err := apply(doc, patchOp{Op: "replace", Path: "/l/1", Value: json.RawMessage(`"x"`)}); err != nil {
		t.Errorf("list replace: %v", err)
	}
}

func TestBuildErrors(t *testing.T) {
	for name, c := range map[string]struct{ base, src string }{
		"not json":       {`{}`, `{`},
		"unknown member": {`{}`, `{"x":1}`},
		"bad base":       {`[]`, `{"cases":[]}`},
		"unknown kind":   {`{"features":[]}`, `{"cases":[{"name":"x","kind":"guess","expected":{}}]}`},
		"bad feature":    {`{"features":[]}`, `{"cases":[{"name":"x","kind":"parse","features":[3],"expected":{}}]}`},
		"bad patch":      {`{"features":[]}`, `{"cases":[{"name":"x","kind":"parse","patch":[{"op":"replace","path":"/q","value":1}],"expected":{}}]}`},
		"bad expected":   {`{"features":[]}`, `{"cases":[{"name":"x","kind":"from_ed269","expected":[]}]}`},
	} {
		if _, err := build([]byte(c.base), []byte(c.src)); err == nil {
			t.Errorf("%s: built", name)
		}
	}
	if err := run("missing.json", "source.json", filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Error("a missing base was read")
	}
	if err := run(basePath, "missing.json", filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Error("a missing source was read")
	}
	if err := run(basePath, "source.json", filepath.Join(t.TempDir(), "missing-dir", "x.json")); err == nil || !strings.Contains(err.Error(), "x.json") {
		t.Errorf("writing into a missing directory: %v", err)
	}
}
