package auth

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jws"

	"github.com/rootxkit/uspace-core/core"
)

// SessionScope is the scope claim of every console or portal session
// token (cross-plan Appendix A, M20).
const SessionScope = "session"

// SessionClaims are the caller's part of a console or portal session
// token (cross-plan Appendix A, M20): the issuer adds iss, scope
// "session" and the kid header. Every field is required. Times are
// written as whole seconds (RFC 7519 NumericDate), the fraction dropped.
// Added in v1.2.0.
type SessionClaims struct {
	// Audience is the system's own host; written as a one-string aud.
	Audience string
	// Subject is the account id.
	Subject string
	// Roles are the account's console roles: at least one, none empty,
	// none with surrounding white space, none twice.
	Roles []string
	// Realm is console, police or portal; the issuer does not restrict
	// it, a verifier does with Config.Realms.
	Realm string
	// IssuedAt and ExpiresAt bound the session; ExpiresAt must be after
	// IssuedAt in whole seconds. The TTL limit (Appendix A: <= 12 h) and
	// the idle timeout are the caller's policy; a verifier enforces the
	// TTL with Config.MaxSessionTTL.
	IssuedAt  time.Time
	ExpiresAt time.Time
	// JTI is the session id, kept by the caller for revocation.
	JTI string
}

// sessionPayload is Appendix A's session row, exactly and in this order.
type sessionPayload struct {
	Issuer    string   `json:"iss"`
	Audience  string   `json:"aud"`
	Subject   string   `json:"sub"`
	Scope     string   `json:"scope"`
	Roles     []string `json:"roles"`
	Realm     string   `json:"realm"`
	IssuedAt  int64    `json:"iat"`
	ExpiresAt int64    `json:"exp"`
	JTI       string   `json:"jti"`
}

// IssueSession signs a session token: a compact JWS with header alg
// RS256, kid and typ JWT, whose payload is exactly iss (the issuer's),
// aud, sub, scope "session", roles, realm, iat, exp and jti. It refuses,
// as a *core.FieldError, an issuer without iss or key, an empty aud, sub,
// jti or realm, no roles, an empty role, a role with surrounding white
// space or listed twice, a string that is not valid UTF-8 (JSON would
// rewrite it), iat before 1970, exp not after iat or after
// 9999-12-31T23:59:59Z, and a token longer than DefaultMaxTokenBytes.
// Every token it returns is accepted by a Verifier with
// StrictSessionClaims that allows the issuer with its JWKS and has aud
// as an audience, at any time from iat to exp, and the verified Claims
// carry the given values. Added in v1.2.0.
func (i *Issuer) IssueSession(c SessionClaims) (string, error) {
	switch {
	case i == nil || i.iss == "":
		return "", core.Fieldf("iss", "empty")
	case i.priv == nil:
		return "", core.Fieldf("key", "nil")
	}
	for _, f := range []struct{ name, value string }{
		{"aud", c.Audience}, {"sub", c.Subject}, {"jti", c.JTI}, {"realm", c.Realm},
	} {
		if f.value == "" {
			return "", core.Fieldf(f.name, "empty")
		}
		if !utf8.ValidString(f.value) {
			return "", core.Fieldf(f.name, "not valid UTF-8")
		}
	}
	if len(c.Roles) == 0 {
		return "", core.Fieldf("roles", "none")
	}
	seen := make(map[string]struct{}, len(c.Roles))
	for _, r := range c.Roles {
		_, twice := seen[r]
		seen[r] = struct{}{}
		switch {
		case r == "" || !utf8.ValidString(r):
			return "", core.Fieldf("roles", "a role is empty or not valid UTF-8")
		case strings.TrimSpace(r) != r:
			return "", core.Fieldf("roles", "a role has surrounding white space")
		case twice:
			return "", core.Fieldf("roles", "a role is listed twice")
		}
	}
	iat, exp := c.IssuedAt.Unix(), c.ExpiresAt.Unix()
	switch {
	case iat < 0:
		return "", core.Fieldf("iat", "before 1970")
	case exp <= iat:
		return "", core.Fieldf("exp", "not after iat in whole seconds")
	case exp > maxNumericDate:
		return "", core.Fieldf("exp", "after 9999-12-31T23:59:59Z")
	}
	payload, err := json.Marshal(sessionPayload{
		Issuer: i.iss, Audience: c.Audience, Subject: c.Subject, Scope: SessionScope,
		Roles: c.Roles, Realm: c.Realm, IssuedAt: iat, ExpiresAt: exp, JTI: c.JTI,
	})
	if err != nil {
		return "", core.Fieldf("token", "%v", err)
	}
	kid, ok := i.priv.KeyID()
	if !ok || kid == "" {
		return "", core.Fieldf("kid", "empty")
	}
	hdr := jws.NewHeaders()
	if err := hdr.Set(jws.KeyIDKey, kid); err != nil {
		return "", core.Fieldf("header", "%v", err)
	}
	if err := hdr.Set(jws.TypeKey, "JWT"); err != nil {
		return "", core.Fieldf("header", "%v", err)
	}
	signed, err := jws.Sign(payload, jws.WithKey(jwa.RS256(), i.priv, jws.WithProtectedHeaders(hdr)))
	if err != nil {
		return "", core.Fieldf("signature", "%v", err)
	}
	if len(signed) > DefaultMaxTokenBytes {
		return "", core.Fieldf("token", "%d bytes, longer than %d", len(signed), DefaultMaxTokenBytes)
	}
	return string(signed), nil
}
