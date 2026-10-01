// Command genvectors writes vectors/testdata/jwt_verify.json, the
// knowledge vector for the ecosystem JWT verifier (spec 00 section 6.2,
// 06 section 3; plan section 11 gap 7).
//
// It generates an RSA key at run time, signs every case's token with it
// and keeps only the public JWKS: no private key is ever written (06
// section 4). Given the key, the output is deterministic (RS256 and HS256
// signatures are deterministic and every claim is fixed), so a new run
// produces a new file only because the key is new. After a run,
// regenerate vectors/testdata/SHA256SUMS.
//
//	go run ./auth/internal/genvectors [-out vectors/testdata/jwt_verify.json]
package main

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"math/big"
	"os"
	"strings"
)

const (
	issuer   = "https://authority.uspace.example/"
	audience = "ussp-1"
	kid      = "uspace-vector-1"
	nowS     = 1790000000
	maxSkewS = 30
	utm      = "484cd228035f2f424530e233ce72100d0fc1842a"
)

var owners = []string{"authority", "cisp", "ussp", "ansp"}

type caseInput struct {
	Token        string `json:"token"`
	NowS         int64  `json:"now_s"`
	RequireScope string `json:"require_scope,omitempty"`
}

type caseExpected struct {
	Accepted       bool     `json:"accepted"`
	Reason         string   `json:"reason,omitempty"`
	Claim          string   `json:"claim,omitempty"`
	Subject        string   `json:"subject,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	RequireScopeOK *bool    `json:"require_scope_ok,omitempty"`
}

type vectorCase struct {
	Name     string       `json:"name"`
	Owner    []string     `json:"owner"`
	Input    caseInput    `json:"input"`
	Expected caseExpected `json:"expected"`
	Why      string       `json:"why"`
}

type jwkJSON struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type fixtures struct {
	Issuer   string `json:"issuer"`
	Audience string `json:"audience"`
	MaxSkewS int    `json:"max_skew_s"`
	JWKS     struct {
		Keys []jwkJSON `json:"keys"`
	} `json:"jwks"`
}

type file struct {
	Description string            `json:"description"`
	Source      []string          `json:"source"`
	Units       map[string]string `json:"units"`
	Tolerance   map[string]string `json:"tolerance"`
	Owners      []string          `json:"owners"`
	Generated   string            `json:"generated"`
	UtmCommit   string            `json:"utm_commit"`
	Fixtures    fixtures          `json:"fixtures"`
	Cases       []vectorCase      `json:"cases"`
}

// claims and headers are ordered key/value lists so the JSON is fixed.
type kv []any

func (o kv) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i := 0; i+1 < len(o); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(o[i])
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(o[i+1])
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// set returns o with key set to value (appended when absent); a nil
// value removes the key.
func (o kv) set(key string, value any) kv {
	out := kv{}
	found := false
	for i := 0; i+1 < len(o); i += 2 {
		if o[i] == key {
			found = true
			if value != nil {
				out = append(out, key, value)
			}
			continue
		}
		out = append(out, o[i], o[i+1])
	}
	if !found && value != nil {
		out = append(out, key, value)
	}
	return out
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func b64JSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return b64(raw), nil
}

type signer func(input []byte) ([]byte, error)

func compact(header, claims kv, sign signer) (string, error) {
	h, err := b64JSON(header)
	if err != nil {
		return "", err
	}
	p, err := b64JSON(claims)
	if err != nil {
		return "", err
	}
	input := h + "." + p
	sig, err := sign([]byte(input))
	if err != nil {
		return "", err
	}
	return input + "." + b64(sig), nil
}

func main() {
	out := flag.String("out", "vectors/testdata/jwt_verify.json", "output file")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "genvectors:", err)
		os.Exit(1) //nolint:forbidigo // a command reports failure by its exit status
	}
}

func run(out string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	f, err := build(key)
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

func build(key *rsa.PrivateKey) (*file, error) {
	rs := func(h crypto.Hash) signer {
		return func(in []byte) ([]byte, error) {
			d := h.New()
			d.Write(in)
			return rsa.SignPKCS1v15(nil, key, h, d.Sum(nil))
		}
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	hs := func(in []byte) ([]byte, error) {
		m := hmac.New(sha256.New, pubPEM)
		m.Write(in)
		return m.Sum(nil), nil
	}
	none := func([]byte) ([]byte, error) { return nil, nil }

	header := kv{"alg", "RS256", "typ", "JWT", "kid", kid}
	claims := kv{
		"iss", issuer, "sub", "operator-GEO-1", "aud", audience,
		"iat", nowS - 60, "exp", nowS + 600, "jti", "jti-0001",
		"scope", "rid.read flights.write",
	}
	good, err := compact(header, claims, rs(crypto.SHA256))
	if err != nil {
		return nil, err
	}
	tampered := func() (string, error) {
		p, err := b64JSON(claims.set("sub", "operator-GEO-2"))
		if err != nil {
			return "", err
		}
		parts := strings.Split(good, ".")
		return parts[0] + "." + p + "." + parts[2], nil
	}
	yes, no := true, false
	accepted := func(scopes []string, requireOK *bool) caseExpected {
		return caseExpected{Accepted: true, Subject: "operator-GEO-1", Scopes: scopes, RequireScopeOK: requireOK}
	}
	refused := func(reason, claim string) caseExpected {
		return caseExpected{Reason: reason, Claim: claim}
	}
	scopes := []string{"rid.read", "flights.write"}

	type spec struct {
		name     string
		token    func() (string, error)
		require  string
		expected caseExpected
		why      string
	}
	sign := func(h, c kv, s signer) func() (string, error) {
		return func() (string, error) { return compact(h, c, s) }
	}
	specs := []spec{
		{"accept-valid", sign(header, claims, rs(crypto.SHA256)), "rid.read", accepted(scopes, &yes),
			"RS256 by the issuer's key under its kid, allow-listed iss, our aud, inside exp, sub, jti and a scope that RequireScope finds."},
		{"refuse-wrong-audience", sign(header, claims.set("aud", "cisp-1"), rs(crypto.SHA256)), "", refused("rejected_audience", "aud"),
			"A token for another system is not a token for this one (00 section 6.2: aud equals the own system id)."},
		{"refuse-expired-beyond-skew", sign(header, claims.set("exp", nowS-31), rs(crypto.SHA256)), "", refused("rejected_expired", "exp"),
			"Expired 31 s ago: beyond the 30 s skew."},
		{"accept-expired-within-skew", sign(header, claims.set("exp", nowS-29), rs(crypto.SHA256)), "", accepted(scopes, nil),
			"Expired 29 s ago: inside the 30 s skew allowed for clock differences."},
		{"refuse-unknown-kid", sign(header.set("kid", "uspace-vector-unknown"), claims, rs(crypto.SHA256)), "", refused("rejected_kid", "kid"),
			"The kid is not in the issuer's JWKS (after the rate-limited refresh)."},
		{"refuse-missing-kid", sign(header.set("kid", nil), claims, rs(crypto.SHA256)), "", refused("rejected_kid", "kid"),
			"kid is required: keys are selected by kid, never by trying each key."},
		{"refuse-unknown-issuer", sign(header, claims.set("iss", "https://issuer.attacker.example/"), rs(crypto.SHA256)), "", refused("rejected_issuer", "iss"),
			"iss is not on the allow-list; refused before any JWKS fetch, even though the signature is good."},
		{"refuse-alg-none", sign(header.set("alg", "none"), claims, none), "", refused("rejected_algorithm", "alg"),
			"alg none carries no signature (RFC 8725 section 2.1)."},
		{"refuse-hs256-public-key-as-secret", sign(header.set("alg", "HS256"), claims, hs), "", refused("rejected_algorithm", "alg"),
			"HS256 keyed with the issuer's public key PEM: the key-confusion attack. Only RS256 is accepted, so the public key is never used as an HMAC secret."},
		{"refuse-alg-rs512-same-key", sign(header.set("alg", "RS512"), claims, rs(crypto.SHA512)), "", refused("rejected_algorithm", "alg"),
			"A valid RS512 signature by the right key is still not RS256: the algorithm is pinned, not negotiated."},
		{"refuse-tampered-payload", tampered, "", refused("rejected_signature", "signature"),
			"sub changed after signing: the signature no longer verifies."},
		{"refuse-missing-jti", sign(header, claims.set("jti", nil), rs(crypto.SHA256)), "", refused("rejected_claims", "jti"),
			"jti is required (00 section 6.2)."},
		{"refuse-missing-exp", sign(header, claims.set("exp", nil), rs(crypto.SHA256)), "", refused("rejected_claims", "exp"),
			"exp is required: a token without expiry is never accepted."},
		{"accept-missing-scope-then-require-scope-refused", sign(header, claims.set("scope", nil), rs(crypto.SHA256)), "rid.read", accepted(nil, &no),
			"A token without scope verifies but grants nothing: RequireScope refuses the endpoint (06 section 3)."},
		{"refuse-nbf-beyond-skew", sign(header, claims.set("nbf", nowS+31), rs(crypto.SHA256)), "", refused("rejected_not_yet_valid", "nbf"),
			"nbf 31 s in the future: beyond the skew."},
		{"accept-nbf-within-skew", sign(header, claims.set("nbf", nowS+29), rs(crypto.SHA256)), "", accepted(scopes, nil),
			"nbf 29 s in the future: inside the skew."},
	}

	f := &file{
		Description: "Verifying ecosystem JWTs (spec 00 section 6.2, 06 section 3): RS256 only, kid from the allow-listed issuer's JWKS, aud, exp/nbf with 30 s skew, jti, and the space-separated scope claim checked per endpoint. Each refusal names its reason (the verifier's counter) and the claim.",
		Source:      []string{"spec 00 section 6.2", "spec 06 section 3", "RFC 7519", "RFC 8725"},
		Units: map[string]string{
			"now_s":      "epoch seconds on the verifier's clock",
			"max_skew_s": "seconds",
		},
		Tolerance: map[string]string{"accepted": "exact", "reason": "exact", "claim": "exact", "scopes": "exact"},
		Owners:    owners,
		Generated: "hand-written in uspace-core WP-11 from spec 00 §6.2 and 06 §3; not from utm. Tokens are signed by auth/internal/genvectors with an RSA key generated at run time and discarded; only its public JWKS is kept (06 §4). Regenerate rather than edit.",
		UtmCommit: utm,
	}
	f.Fixtures.Issuer = issuer
	f.Fixtures.Audience = audience
	f.Fixtures.MaxSkewS = maxSkewS
	f.Fixtures.JWKS.Keys = []jwkJSON{{
		Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
		N: b64(key.N.Bytes()), E: b64(big.NewInt(int64(key.E)).Bytes()),
	}}
	for i := range specs {
		s := &specs[i]
		tok, err := s.token()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.name, err)
		}
		f.Cases = append(f.Cases, vectorCase{
			Name: s.name, Owner: owners,
			Input:    caseInput{Token: tok, NowS: nowS, RequireScope: s.require},
			Expected: s.expected, Why: s.why,
		})
	}
	return f, nil
}
