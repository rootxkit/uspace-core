package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"strings"
	"time"
	"unicode"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"

	"github.com/rootxkit/uspace-core/core"
)

// Issuer signs ecosystem JWTs for a token service: RS256, kid in the
// header, iss, aud, sub, scope, exp, iat and a random jti. It is safe for
// concurrent use.
type Issuer struct {
	iss  string
	priv jwk.Key
	jwks jwk.Set
}

// NewIssuer builds an issuer for iss signing with key under kid. It
// refuses an empty iss or kid, a nil key, a key shorter than MinRSABits
// or one that fails rsa.PrivateKey.Validate.
func NewIssuer(iss string, key *rsa.PrivateKey, kid string) (*Issuer, error) {
	switch {
	case iss == "":
		return nil, core.Fieldf("iss", "empty")
	case kid == "":
		return nil, core.Fieldf("kid", "empty")
	case key == nil || key.N == nil:
		return nil, core.Fieldf("key", "nil")
	case key.N.BitLen() < MinRSABits:
		return nil, core.Fieldf("key", "%d bits, shorter than %d", key.N.BitLen(), MinRSABits)
	}
	if err := key.Validate(); err != nil {
		return nil, core.Fieldf("key", "invalid RSA key: %v", err)
	}
	priv, err := jwk.Import(key)
	if err != nil {
		return nil, core.Fieldf("key", "%v", err)
	}
	pub, err := jwk.Import(&key.PublicKey)
	if err != nil {
		return nil, core.Fieldf("key", "%v", err)
	}
	for _, k := range []jwk.Key{priv, pub} {
		if err := k.Set(jwk.KeyIDKey, kid); err != nil {
			return nil, core.Fieldf("kid", "%v", err)
		}
		if err := k.Set(jwk.AlgorithmKey, jwa.RS256()); err != nil {
			return nil, core.Fieldf("key", "%v", err)
		}
	}
	if err := pub.Set(jwk.KeyUsageKey, "sig"); err != nil {
		return nil, core.Fieldf("key", "%v", err)
	}
	set := jwk.NewSet()
	if err := set.AddKey(pub); err != nil {
		return nil, core.Fieldf("key", "%v", err)
	}
	return &Issuer{iss: iss, priv: priv, jwks: set}, nil
}

// Issue signs a token for sub and aud granting scopes (sent as the
// space-separated scope claim; omitted when scopes is empty), valid from
// now for ttl. Times are whole seconds (RFC 7519 NumericDate).
func (i *Issuer) Issue(sub, aud string, scopes []string, ttl time.Duration, now time.Time) (string, error) {
	switch {
	case sub == "":
		return "", core.Fieldf("sub", "empty")
	case aud == "":
		return "", core.Fieldf("aud", "empty")
	case ttl < time.Second:
		return "", core.Fieldf("ttl", "%s is shorter than one second", ttl)
	}
	for _, s := range scopes {
		// The verifier splits scope with strings.Fields (unicode.IsSpace),
		// so any rune it splits on would turn one scope into two.
		if s == "" || strings.IndexFunc(s, unicode.IsSpace) >= 0 {
			return "", core.Fieldf("scope", "%s is empty or contains white space", quoteShort(s))
		}
	}
	var jti [16]byte
	if _, err := rand.Read(jti[:]); err != nil {
		return "", core.Fieldf("jti", "no randomness: %v", err)
	}
	now = now.Truncate(time.Second)
	b := jwt.NewBuilder().
		Issuer(i.iss).
		Subject(sub).
		Audience([]string{aud}).
		IssuedAt(now).
		Expiration(now.Add(ttl)).
		JwtID(hex.EncodeToString(jti[:]))
	if len(scopes) > 0 {
		b = b.Claim("scope", strings.Join(scopes, " "))
	}
	tok, err := b.Build()
	if err != nil {
		return "", core.Fieldf("token", "%v", err)
	}
	signed, err := jwt.Sign(tok, jwt.WithKey(jwa.RS256(), i.priv))
	if err != nil {
		return "", core.Fieldf("token", "%v", err)
	}
	return string(signed), nil
}

// JWKS returns the public key set to publish at the issuer's JWKS URL:
// one RSA key with kid, alg RS256 and use sig.
func (i *Issuer) JWKS() jwk.Set { return i.jwks }
