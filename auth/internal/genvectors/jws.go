package main

import (
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"strings"
)

// The jws_detached and jws_compact generators write vector files for the
// detached (X-JWS-Signature) and compact delivery JWS of WP-14. They are
// proposed to uspace-lab and are never written under vectors/testdata
// here: a new vector file is a major (docs/RELEASING.md section 3.2).
// Like jwt_verify.json, every value is signed by hand from the RFC text
// with an RSA key generated at run time; only its public JWKS is kept.

const (
	publisher      = "ansp-1.uspace.example"
	otherPublisher = "ansp-2.uspace.example"
	cisp           = "https://cisp.uspace.example/"
	hostA          = "ussp-1.uspace.example"
	hostB          = "ussp-1.internal.uspace.example"
	maxAgeS        = 300
)

type jwsExpected struct {
	Accepted  bool            `json:"accepted"`
	Reason    string          `json:"reason,omitempty"`
	Claim     string          `json:"claim,omitempty"`
	KID       string          `json:"kid,omitempty"`
	IssuedAtS int64           `json:"issued_at_s,omitempty"`
	Audience  string          `json:"audience,omitempty"`
	Subject   string          `json:"subject,omitempty"`
	JTI       string          `json:"jti,omitempty"`
	Body      json.RawMessage `json:"body,omitempty"`
}

type detachedInput struct {
	Publisher string `json:"publisher"`
	Header    string `json:"header"`
	Payload   string `json:"payload"`
	NowS      int64  `json:"now_s"`
}

type compactInput struct {
	Token string `json:"token"`
	NowS  int64  `json:"now_s"`
}

type jwsCase struct {
	Name     string      `json:"name"`
	Owner    []string    `json:"owner"`
	Input    any         `json:"input"`
	Expected jwsExpected `json:"expected"`
	Why      string      `json:"why"`
}

type jwsFixtures struct {
	Publishers []string `json:"publishers,omitempty"`
	Issuer     string   `json:"issuer,omitempty"`
	Audiences  []string `json:"audiences,omitempty"`
	MaxAgeS    int      `json:"max_age_s"`
	MaxSkewS   int      `json:"max_skew_s"`
	JWKS       struct {
		Keys []jwkJSON `json:"keys"`
	} `json:"jwks"`
}

type jwsFile struct {
	Description string            `json:"description"`
	Source      []string          `json:"source"`
	Units       map[string]string `json:"units"`
	Tolerance   map[string]string `json:"tolerance"`
	Owners      []string          `json:"owners"`
	Generated   string            `json:"generated"`
	UtmCommit   string            `json:"utm_commit"`
	Fixtures    jwsFixtures       `json:"fixtures"`
	Cases       []jwsCase         `json:"cases"`
}

func runJWS(kind, out string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	var f *jwsFile
	switch kind {
	case "jws_detached":
		f, err = buildDetached(key)
	case "jws_compact":
		f, err = buildCompact(key)
	default:
		return fmt.Errorf("unknown kind %q", kind)
	}
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

func rsSigner(key *rsa.PrivateKey, h crypto.Hash) signer {
	return func(in []byte) ([]byte, error) {
		d := h.New()
		d.Write(in)
		return rsa.SignPKCS1v15(nil, key, h, d.Sum(nil))
	}
}

func publicJWKS(key *rsa.PrivateKey) []jwkJSON {
	return []jwkJSON{{
		Kty: "RSA", Kid: kid, Use: "sig", Alg: "RS256",
		N: b64(key.N.Bytes()), E: b64(big.NewInt(int64(key.E)).Bytes()),
	}}
}

// detached builds <protected>..<signature> over the unencoded payload
// (RFC 7797 section 3).
func detached(header kv, payload string, sign signer) (string, error) {
	h, err := b64JSON(header)
	if err != nil {
		return "", err
	}
	sig, err := sign([]byte(h + "." + payload))
	if err != nil {
		return "", err
	}
	return h + ".." + b64(sig), nil
}

func newJWSFile(description string, source []string, units map[string]string) *jwsFile {
	return &jwsFile{
		Description: description,
		Source:      source,
		Units:       units,
		Tolerance:   map[string]string{"accepted": "exact", "reason": "exact", "claim": "exact", "*": "exact"},
		Owners:      owners,
		Generated: "hand-written in uspace-core WP-14 (C1) from RFC 7515, RFC 7797 and reconciliation M19/M26; not from utm. " +
			"Values are signed by auth/internal/genvectors with an RSA key generated at run time and discarded; only its public JWKS is kept (06 §4). Regenerate rather than edit.",
		UtmCommit: utm,
	}
}

func buildDetached(key *rsa.PrivateKey) (*jwsFile, error) {
	rs := rsSigner(key, crypto.SHA256)
	payload := `{"type":"cis/change/v1","seq":7,"note":"a.b"}`
	changed := `{"type":"cis/change/v1","seq":8,"note":"a.b"}`
	h := kv{"alg", "RS256", "kid", kid, "iat", nowS - 60, "b64", false, "crit", []string{"b64"}}
	hs, none, err := confusionSigners(key)
	if err != nil {
		return nil, err
	}
	accepted := func(iat int64) jwsExpected {
		return jwsExpected{Accepted: true, KID: kid, IssuedAtS: iat}
	}
	refused := func(reason, claim string) jwsExpected { return jwsExpected{Reason: reason, Claim: claim} }
	type spec struct {
		name, pub   string
		hdr         kv
		sign        signer
		payload     string
		signPayload string // when the signature is made over other bytes
		attached    bool
		expected    jwsExpected
		why         string
	}
	specs := []spec{
		{"accept-valid", publisher, h, rs, payload, "", false, accepted(nowS - 60),
			"RS256 over ASCII(BASE64URL(protected)) || '.' || payload with b64 false declared in crit, by a key of the named publisher, iat inside five minutes."},
		{"accept-unknown-header-member", publisher, h.set("x-trace", "t-1"), rs, payload, "", false, accepted(nowS - 60),
			"An unknown header member is tolerated; only unknown crit entries are refused (RFC 7515 section 4.1.11)."},
		{"refuse-payload-one-byte-changed", publisher, h, rs, changed, payload, false, refused("rejected_signature", "signature"),
			"The body differs in one byte from the signed one."},
		{"refuse-signature-over-encoded-payload", publisher, h, rs, payload, b64([]byte(payload)), false, refused("rejected_signature", "signature"),
			"A signature over the base64url payload (b64 true) is not a signature over the raw bytes."},
		{"refuse-attached-payload", publisher, h, rs, payload, "", true, refused("rejected_malformed", "header"),
			"The middle part is not empty: X-JWS-Signature carries no payload (RFC 7515 Appendix F)."},
		{"refuse-alg-none", publisher, h.set("alg", "none"), none, payload, "", false, refused("rejected_algorithm", "alg"),
			"alg none carries no signature (RFC 8725 section 2.1)."},
		{"refuse-hs256-public-key-as-secret", publisher, h.set("alg", "HS256"), hs, payload, "", false, refused("rejected_algorithm", "alg"),
			"HS256 keyed with the publisher's public key PEM: only RS256 is accepted."},
		{"refuse-alg-rs512-same-key", publisher, h.set("alg", "RS512"), rsSigner(key, crypto.SHA512), payload, "", false, refused("rejected_algorithm", "alg"),
			"The algorithm is pinned to RS256, not negotiated."},
		{"refuse-b64-absent", publisher, h.set("b64", nil).set("crit", nil), rs, payload, "", false, refused("rejected_b64", "b64"),
			"The header must declare the unencoded payload (RFC 7797 section 3)."},
		{"refuse-b64-true", publisher, h.set("b64", true), rs, payload, "", false, refused("rejected_b64", "b64"),
			"b64 true is the encoded form; a detached publication signs the raw bytes."},
		{"refuse-crit-without-b64", publisher, h.set("crit", nil), rs, payload, "", false, refused("rejected_b64", "crit"),
			"b64 false must be listed in crit (RFC 7797 section 3)."},
		{"refuse-crit-unknown", publisher, h.set("crit", []string{"b64", "x-ext"}).set("x-ext", 1), rs, payload, "", false, refused("rejected_crit", "crit"),
			"A critical extension the recipient does not understand is refused (RFC 7515 section 4.1.11)."},
		{"refuse-kid-missing", publisher, h.set("kid", nil), rs, payload, "", false, refused("rejected_kid", "kid"),
			"Keys are selected by kid, never by trying each key."},
		{"refuse-kid-unknown", publisher, h.set("kid", "uspace-vector-unknown"), rs, payload, "", false, refused("rejected_kid", "kid"),
			"The kid is not in the publisher's JWKS (after the rate-limited refresh)."},
		{"refuse-publisher-unknown", otherPublisher, h, rs, payload, "", false, refused("rejected_publisher", "publisher"),
			"The caller names the publisher from the bearer token it verified; one not on the allow-list is refused before any JWKS fetch."},
		{"refuse-iat-missing", publisher, h.set("iat", nil), rs, payload, "", false, refused("rejected_iat", "iat"),
			"iat is required: it bounds the replay window (M26)."},
		{"accept-iat-at-max-age", publisher, h.set("iat", nowS-maxAgeS), rs, payload, "", false, accepted(nowS - maxAgeS),
			"Exactly five minutes old: inside the bound."},
		{"refuse-iat-past-max-age", publisher, h.set("iat", nowS-maxAgeS-1), rs, payload, "", false, refused("rejected_iat", "iat"),
			"One second older than five minutes."},
		{"accept-iat-ahead-by-max-skew", publisher, h.set("iat", nowS+maxSkewS), rs, payload, "", false, accepted(nowS + maxSkewS),
			"30 s ahead: inside the clock skew."},
		{"refuse-iat-ahead-past-max-skew", publisher, h.set("iat", nowS+maxSkewS+1), rs, payload, "", false, refused("rejected_iat", "iat"),
			"31 s ahead: beyond the clock skew."},
	}
	f := newJWSFile(
		"Verifying a detached JWS (X-JWS-Signature, RFC 7515 Appendix F, RFC 7797 b64 false): RS256 only, b64 false declared in crit, kid from the named publisher's JWKS, iat at most max_age_s old and max_skew_s ahead. Each refusal names its reason (the verifier's counter) and the part.",
		[]string{"RFC 7515 Appendix F", "RFC 7797 sections 3 and 5", "RFC 8725", "reconciliation M26"},
		map[string]string{"now_s": "epoch seconds on the verifier's clock", "issued_at_s": "epoch seconds", "max_age_s": "seconds", "max_skew_s": "seconds", "payload": "the exact body bytes, UTF-8"},
	)
	f.Fixtures.Publishers = []string{publisher}
	f.Fixtures.MaxAgeS, f.Fixtures.MaxSkewS = maxAgeS, maxSkewS
	f.Fixtures.JWKS.Keys = publicJWKS(key)
	for i := range specs {
		s := &specs[i]
		signed := s.payload
		if s.signPayload != "" {
			signed = s.signPayload
		}
		hdr, err := detached(s.hdr, signed, s.sign)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.name, err)
		}
		if s.attached {
			p, sig, _ := strings.Cut(hdr, "..")
			hdr = p + "." + b64([]byte(s.payload)) + "." + sig
		}
		f.Cases = append(f.Cases, jwsCase{
			Name: s.name, Owner: owners,
			Input:    detachedInput{Publisher: s.pub, Header: hdr, Payload: s.payload, NowS: nowS},
			Expected: s.expected, Why: s.why,
		})
	}
	return f, nil
}

func confusionSigners(key *rsa.PrivateKey) (hs, none signer, err error) {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	hs = func(in []byte) ([]byte, error) {
		m := hmac.New(sha256.New, pubPEM)
		m.Write(in)
		return m.Sum(nil), nil
	}
	return hs, func([]byte) ([]byte, error) { return nil, nil }, nil
}

func buildCompact(key *rsa.PrivateKey) (*jwsFile, error) {
	rs := rsSigner(key, crypto.SHA256)
	hs, none, err := confusionSigners(key)
	if err != nil {
		return nil, err
	}
	body := json.RawMessage(`{"type":"cis/change/v1","seq":7}`)
	header := kv{"alg", "RS256", "kid", kid, "typ", "JWT"}
	claims := kv{"iss", cisp, "aud", hostA, "sub", "cisp", "iat", nowS - 60, "jti", "delivery-0001", "body", body}
	accepted := func(aud string, iat int64) jwsExpected {
		return jwsExpected{Accepted: true, Audience: aud, Subject: "cisp", JTI: "delivery-0001", IssuedAtS: iat, Body: body}
	}
	refused := func(reason, claim string) jwsExpected { return jwsExpected{Reason: reason, Claim: claim} }
	good, err := compact(header, claims, rs)
	if err != nil {
		return nil, err
	}
	tampered := func() (string, error) {
		p, err := b64JSON(claims.set("body", json.RawMessage(`{"type":"cis/change/v1","seq":8}`)))
		if err != nil {
			return "", err
		}
		parts := strings.Split(good, ".")
		return parts[0] + "." + p + "." + parts[2], nil
	}
	sign := func(h, c kv, s signer) func() (string, error) {
		return func() (string, error) { return compact(h, c, s) }
	}
	type spec struct {
		name     string
		token    func() (string, error)
		expected jwsExpected
		why      string
	}
	specs := []spec{
		{"accept-valid", sign(header, claims, rs), accepted(hostA, nowS-60),
			"RS256 by the issuer's key, allow-listed iss, aud one of this receiver's hosts, iat inside five minutes, sub, jti and an object body."},
		{"accept-second-audience-in-array", sign(header, claims.set("aud", []string{"other.example", hostB}), rs), accepted(hostB, nowS-60),
			"aud is an array holding the receiver's second host (M18: audiences are hosts)."},
		{"refuse-other-audience", sign(header, claims.set("aud", "ussp-2.uspace.example"), rs), refused("rejected_audience", "aud"),
			"A delivery for another receiver."},
		{"refuse-alg-none", sign(header.set("alg", "none"), claims, none), refused("rejected_algorithm", "alg"),
			"alg none carries no signature (RFC 8725 section 2.1)."},
		{"refuse-hs256-public-key-as-secret", sign(header.set("alg", "HS256"), claims, hs), refused("rejected_algorithm", "alg"),
			"HS256 keyed with the issuer's public key PEM: only RS256 is accepted."},
		{"refuse-crit", sign(header.set("crit", []string{"x-ext"}).set("x-ext", 1), claims, rs), refused("rejected_crit", "crit"),
			"No critical extension is understood in the compact form."},
		{"refuse-b64", sign(header.set("b64", false).set("crit", nil), claims, rs), refused("rejected_b64", "b64"),
			"b64 belongs to the detached form; a delivery's payload is base64url."},
		{"refuse-kid-unknown", sign(header.set("kid", "uspace-vector-unknown"), claims, rs), refused("rejected_kid", "kid"),
			"The kid is not in the issuer's JWKS."},
		{"refuse-unknown-issuer", sign(header, claims.set("iss", "https://issuer.attacker.example/"), rs), refused("rejected_issuer", "iss"),
			"iss is not on the allow-list; refused before any JWKS fetch."},
		{"refuse-tampered-body", tampered, refused("rejected_signature", "signature"),
			"The body changed after signing."},
		{"accept-iat-at-max-age", sign(header, claims.set("iat", nowS-maxAgeS), rs), accepted(hostA, nowS-maxAgeS),
			"Exactly five minutes old: inside the bound."},
		{"refuse-iat-past-max-age", sign(header, claims.set("iat", nowS-maxAgeS-1), rs), refused("rejected_iat", "iat"),
			"One second older than five minutes."},
		{"refuse-iat-ahead-past-max-skew", sign(header, claims.set("iat", nowS+maxSkewS+1), rs), refused("rejected_iat", "iat"),
			"31 s ahead: beyond the clock skew."},
		{"refuse-missing-jti", sign(header, claims.set("jti", nil), rs), refused("rejected_claims", "jti"),
			"jti is the delivery id the receiver's idempotent store keys on."},
		{"refuse-missing-sub", sign(header, claims.set("sub", nil), rs), refused("rejected_claims", "sub"),
			"sub is required."},
		{"refuse-missing-body", sign(header, claims.set("body", nil), rs), refused("rejected_claims", "body"),
			"A delivery without a body object delivers nothing."},
	}
	f := newJWSFile(
		"Verifying a compact delivery JWS (M19: a JWT whose payload carries iss, aud, sub, iat, jti and the message as body): RS256 only, iss allow-listed, kid from its JWKS, aud containing one of the receiver's hosts, iat at most max_age_s old and max_skew_s ahead, sub, jti and an object body. No exp: jti is the delivery id and the receiver's store is the replay guard.",
		[]string{"RFC 7515", "RFC 7519 section 4", "RFC 8725", "reconciliation M18, M19"},
		map[string]string{"now_s": "epoch seconds on the verifier's clock", "issued_at_s": "epoch seconds", "max_age_s": "seconds", "max_skew_s": "seconds"},
	)
	f.Fixtures.Issuer = cisp
	f.Fixtures.Audiences = []string{hostA, hostB}
	f.Fixtures.MaxAgeS, f.Fixtures.MaxSkewS = maxAgeS, maxSkewS
	f.Fixtures.JWKS.Keys = publicJWKS(key)
	for i := range specs {
		s := &specs[i]
		tok, err := s.token()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.name, err)
		}
		f.Cases = append(f.Cases, jwsCase{
			Name: s.name, Owner: owners,
			Input: compactInput{Token: tok, NowS: nowS}, Expected: s.expected, Why: s.why,
		})
	}
	return f, nil
}
