package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/rootxkit/uspace-core/auth"
	"github.com/rootxkit/uspace-core/vectors"
)

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func fixtureSet(t *testing.T, f *jwsFile) jwk.Set {
	t.Helper()
	raw, err := json.Marshal(f.Fixtures.JWKS)
	if err != nil {
		t.Fatal(err)
	}
	set, err := jwk.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func checkOutcome(t *testing.T, name string, want jwsExpected, err error) bool {
	t.Helper()
	if !want.Accepted {
		var te *auth.TokenError
		if !errors.As(err, &te) || te.Counter != want.Reason || te.Claim != want.Claim {
			t.Errorf("%s: got %v, want %s on %s", name, err, want.Reason, want.Claim)
		}
		return false
	}
	if err != nil {
		t.Errorf("%s: refused: %v", name, err)
		return false
	}
	return true
}

// Every generated detached case gets the outcome it states from
// auth.DetachedVerifier, so the file the lab receives is what core does.
func TestDetachedCasesMatchTheVerifier(t *testing.T) {
	f, err := buildDetached(genKey(t))
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth.NewDetachedVerifier(context.Background(), auth.DetachedConfig{
		Publishers: map[string]auth.IssuerConfig{f.Fixtures.Publishers[0]: {Keys: fixtureSet(t, f)}},
		MaxAge:     time.Duration(f.Fixtures.MaxAgeS) * time.Second,
		MaxSkew:    time.Duration(f.Fixtures.MaxSkewS) * time.Second,
		Now:        func() time.Time { return time.Unix(nowS, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Cases {
		in := c.Input.(detachedInput)
		s, err := v.Verify(context.Background(), in.Publisher, in.Header, []byte(in.Payload))
		if checkOutcome(t, c.Name, c.Expected, err) && (s.KID != c.Expected.KID || s.IssuedAt.Unix() != c.Expected.IssuedAtS) {
			t.Errorf("%s: got %+v, want %+v", c.Name, s, c.Expected)
		}
	}
}

func TestCompactCasesMatchTheVerifier(t *testing.T) {
	f, err := buildCompact(genKey(t))
	if err != nil {
		t.Fatal(err)
	}
	v, err := auth.NewCompactVerifier(context.Background(), auth.CompactConfig{
		Issuers:   map[string]auth.IssuerConfig{f.Fixtures.Issuer: {Keys: fixtureSet(t, f)}},
		Audiences: f.Fixtures.Audiences,
		MaxAge:    time.Duration(f.Fixtures.MaxAgeS) * time.Second,
		MaxSkew:   time.Duration(f.Fixtures.MaxSkewS) * time.Second,
		Now:       func() time.Time { return time.Unix(nowS, 0) },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Cases {
		in := c.Input.(compactInput)
		cl, body, err := v.Verify(context.Background(), in.Token)
		if !checkOutcome(t, c.Name, c.Expected, err) {
			continue
		}
		e := c.Expected
		if cl.Audience != e.Audience || cl.Subject != e.Subject || cl.JTI != e.JTI || cl.IssuedAt.Unix() != e.IssuedAtS ||
			string(body) != string(e.Body) {
			t.Errorf("%s: got %+v %s, want %+v", c.Name, cl, body, e)
		}
	}
}

// Given the key the output is fixed, and the written files parse as
// vector files with no private key part.
func TestRunJWSWritesPublicOnlyVectorFiles(t *testing.T) {
	key := genKey(t)
	for kind, n := range map[string]int{"jws_detached": 20, "jws_compact": 16} {
		build := buildDetached
		if kind == "jws_compact" {
			build = buildCompact
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
			t.Fatalf("%s: two builds with one key differ", kind)
		}
		out := filepath.Join(t.TempDir(), kind+".json")
		if err := runJWS(kind, out); err != nil {
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
		if len(f.Cases) != n || len(f.Owners) != 4 {
			t.Errorf("%s: %d cases, owners %v", kind, len(f.Cases), f.Owners)
		}
		for _, private := range []string{`"d"`, `"p"`, `"q"`, `"dp"`, `"dq"`, `"qi"`, "PRIVATE KEY"} {
			if strings.Contains(string(raw), private) {
				t.Errorf("%s carries %s", kind, private)
			}
		}
	}
	if err := runJWS("jws_other", filepath.Join(t.TempDir(), "x.json")); err == nil {
		t.Error("an unknown kind was written")
	}
	if err := runJWS("jws_compact", filepath.Join(t.TempDir(), "missing-dir", "x.json")); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
}
