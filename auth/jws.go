package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"time"
)

// Counter names of the detached and compact JWS verifiers (E-09), beside
// the JWT ones they share (accepted, rejected_malformed,
// rejected_algorithm, rejected_kid, rejected_signature, rejected_issuer,
// rejected_audience, rejected_claims, jwks_refresh, jwks_refresh_failed,
// jwks_refresh_rate_limited). Stable from v1.1.0.
const (
	CounterRejectedB64       = "rejected_b64"
	CounterRejectedCrit      = "rejected_crit"
	CounterRejectedPublisher = "rejected_publisher"
	CounterRejectedIAT       = "rejected_iat"
	CounterRejectedTooLarge  = "rejected_too_large"
)

// Defaults of DetachedConfig and CompactConfig.
const (
	// DefaultDetachedMaxAge is how old an iat may be (M26: five minutes).
	DefaultDetachedMaxAge = 5 * time.Minute
	// DefaultMaxDetachedPayloadBytes bounds a detached payload (16 MiB).
	DefaultMaxDetachedPayloadBytes = 16 << 20
)

// decodeObject decodes the top-level members of one JSON object. A
// member name that appears twice is refused: a parser that keeps the
// first and one that keeps the last would read two different headers,
// and the library that checks the signature is not this parser.
func decodeObject(raw []byte) (map[string]json.RawMessage, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, false
	}
	m := make(map[string]json.RawMessage)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, false
		}
		name, ok := tok.(string)
		if !ok {
			return nil, false
		}
		if _, dup := m[name]; dup {
			return nil, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false
		}
		m[name] = v
	}
	if tok, err := dec.Token(); err != nil || tok != json.Delim('}') {
		return nil, false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false
	}
	return m, true
}

// judgeIAT checks a NumericDate iat against now: at most maxAge old and
// at most maxSkew ahead. Every refusal is rejected_iat.
func judgeIAT(hdr claimSet, now time.Time, maxAge, maxSkew time.Duration) (time.Time, error) {
	iat, present, err := hdr.numericDate("iat")
	switch {
	case err != nil:
		return time.Time{}, refuseToken(CounterRejectedIAT, "iat", "not a NumericDate")
	case !present:
		return time.Time{}, refuseToken(CounterRejectedIAT, "iat", "missing")
	case now.Sub(iat) > maxAge:
		return time.Time{}, refuseToken(CounterRejectedIAT, "iat", "issued at %s, more than %s ago",
			iat.UTC().Format(time.RFC3339), maxAge)
	case iat.Sub(now) > maxSkew:
		return time.Time{}, refuseToken(CounterRejectedIAT, "iat", "issued at %s, more than %s from now",
			iat.UTC().Format(time.RFC3339), maxSkew)
	}
	return iat, nil
}
