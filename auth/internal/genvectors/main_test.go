package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/vectors"
)

// Given the key the output is fixed: two builds are identical.
func TestBuildIsDeterministicGivenTheKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	a, err := build(key)
	if err != nil {
		t.Fatal(err)
	}
	b, err := build(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two builds with one key differ")
	}
	if len(a.Cases) != 16 {
		t.Fatalf("%d cases", len(a.Cases))
	}
}

// The written file parses as a vector file and holds no private key part.
func TestRunWritesAPublicOnlyVectorFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "jwt_verify.json")
	if err := run(out); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	f, err := vectors.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) != 16 || len(f.Owners) != 4 {
		t.Fatalf("%d cases, owners %v", len(f.Cases), f.Owners)
	}
	var fx struct {
		JWKS struct {
			Keys []map[string]any `json:"keys"`
		} `json:"jwks"`
	}
	if err := json.Unmarshal(f.Fixtures, &fx); err != nil {
		t.Fatal(err)
	}
	for _, k := range fx.JWKS.Keys {
		for _, private := range []string{"d", "p", "q", "dp", "dq", "qi"} {
			if _, ok := k[private]; ok {
				t.Errorf("the JWKS carries the private member %q", private)
			}
		}
	}
	if strings.Contains(string(raw), "PRIVATE KEY") {
		t.Error("a private key PEM is in the file")
	}
	if err := run(filepath.Join(t.TempDir(), "missing-dir", "x.json")); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
}
