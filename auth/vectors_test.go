package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/rootxkit/uspace-core/vectors"
)

type receiverSignInput struct {
	KeyHex     string `json:"key_hex"`
	ReportUTF8 string `json:"report_utf8"`
}

type receiverSignExpected struct {
	DatagramUTF8 string `json:"datagram_utf8"`
	SignatureHex string `json:"signature_hex"`
}

type receiverVerifyInput struct {
	KeysHex   map[string]string `json:"keys_hex"`
	MaxSkewS  float64           `json:"max_skew_s"`
	Datagrams []struct {
		UTF8 string  `json:"utf8"`
		NowS float64 `json:"now_s"`
	} `json:"datagrams"`
}

type receiverVerifyExpected struct {
	PerDatagram []struct {
		Accepted bool    `json:"accepted"`
		Error    *string `json:"error"`
	} `json:"per_datagram"`
}

func epochSeconds(s float64) time.Time {
	sec, frac := math.Modf(s)
	return time.Unix(int64(sec), int64(math.Round(frac*1e9))).UTC()
}

func TestVectorsRIDReceiverAuth(t *testing.T) {
	f := vectors.Load(t, "rid_receiver_auth.json")
	f.Run(t, func(t *testing.T, c vectors.Case) {
		if c.Name == "signature-is-hmac-sha256-hex-of-report-bytes" {
			var in receiverSignInput
			var exp receiverSignExpected
			c.Decode(t, &in, &exp)
			key, err := hex.DecodeString(in.KeyHex)
			if err != nil {
				t.Fatal(err)
			}
			sig := SignReport(key, []byte(in.ReportUTF8))
			if sig != exp.SignatureHex {
				t.Errorf("signature_hex: got %s, want %s", sig, exp.SignatureHex)
			}
			if got := string(Datagram([]byte(in.ReportUTF8), sig)); got != exp.DatagramUTF8 {
				t.Errorf("datagram_utf8: got %q, want %q", got, exp.DatagramUTF8)
			}
			return
		}
		var in receiverVerifyInput
		var exp receiverVerifyExpected
		c.Decode(t, &in, &exp)
		keys := map[string][]byte{}
		for id, h := range in.KeysHex {
			k, err := hex.DecodeString(h)
			if err != nil {
				t.Fatal(err)
			}
			keys[id] = k
		}
		v, err := NewReceiverVerifier(keys, time.Duration(in.MaxSkewS*float64(time.Second)))
		if err != nil {
			t.Fatal(err)
		}
		if len(in.Datagrams) != len(exp.PerDatagram) {
			t.Fatalf("%d datagrams, %d expectations", len(in.Datagrams), len(exp.PerDatagram))
		}
		for i, d := range in.Datagrams {
			want := exp.PerDatagram[i]
			_, err := v.Verify([]byte(d.UTF8), epochSeconds(d.NowS))
			if got := err == nil; got != want.Accepted {
				t.Errorf("datagram %d: accepted %v, want %v (err %v)", i, got, want.Accepted, err)
				continue
			}
			if want.Error != nil {
				var re *ReceiverError
				if !errors.As(err, &re) {
					t.Fatalf("datagram %d: error %T is not a *ReceiverError", i, err)
				}
				if re.Reason != *want.Error {
					t.Errorf("datagram %d: error %q, want %q", i, re.Reason, *want.Error)
				}
			}
		}
	})
}

type jwtFixtures struct {
	Issuer   string          `json:"issuer"`
	Audience string          `json:"audience"`
	MaxSkewS float64         `json:"max_skew_s"`
	JWKS     json.RawMessage `json:"jwks"`
}

type jwtInput struct {
	Token        string `json:"token"`
	NowS         int64  `json:"now_s"`
	RequireScope string `json:"require_scope"`
}

type jwtExpected struct {
	Accepted       bool     `json:"accepted"`
	Reason         string   `json:"reason"`
	Claim          string   `json:"claim"`
	Subject        string   `json:"subject"`
	Scopes         []string `json:"scopes"`
	RequireScopeOK *bool    `json:"require_scope_ok"`
}

func TestVectorsJWTVerify(t *testing.T) {
	f := vectors.Load(t, "jwt_verify.json")
	var fx jwtFixtures
	vectors.Unmarshal(t, f.Fixtures, &fx)
	set, err := jwk.Parse(fx.JWKS)
	if err != nil {
		t.Fatal(err)
	}
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in jwtInput
		var exp jwtExpected
		c.Decode(t, &in, &exp)
		now := time.Unix(in.NowS, 0).UTC()
		v, err := NewVerifier(context.Background(), Config{
			Issuers:  map[string]IssuerConfig{fx.Issuer: {Keys: set}},
			Audience: fx.Audience,
			MaxSkew:  time.Duration(fx.MaxSkewS * float64(time.Second)),
			Now:      func() time.Time { return now },
		})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := v.Verify(context.Background(), in.Token)
		if got := err == nil; got != exp.Accepted {
			t.Fatalf("accepted %v, want %v (err %v)", got, exp.Accepted, err)
		}
		if !exp.Accepted {
			var te *TokenError
			if !errors.As(err, &te) {
				t.Fatalf("error %T is not a *TokenError", err)
			}
			if te.Counter != exp.Reason || te.Claim != exp.Claim {
				t.Errorf("refused %s on %s (%v), want %s on %s", te.Counter, te.Claim, err, exp.Reason, exp.Claim)
			}
			if v.Counters().Get(exp.Reason) != 1 {
				t.Errorf("counter %s not incremented: %v", exp.Reason, v.Counters().Snapshot())
			}
			return
		}
		if claims.Subject != exp.Subject || !slices.Equal(claims.Scopes, exp.Scopes) {
			t.Errorf("subject %q scopes %q, want %q %q", claims.Subject, claims.Scopes, exp.Subject, exp.Scopes)
		}
		if (in.RequireScope == "") != (exp.RequireScopeOK == nil) {
			t.Fatal("require_scope and require_scope_ok must come together")
		}
		if exp.RequireScopeOK != nil {
			if got := RequireScope(claims, in.RequireScope) == nil; got != *exp.RequireScopeOK {
				t.Errorf("RequireScope(%q) ok %v, want %v", in.RequireScope, got, *exp.RequireScopeOK)
			}
		}
	})
}

// jwtVectorTokens returns the tokens of jwt_verify.json, as fuzz seeds.
func jwtVectorTokens(t vectors.TB) []string {
	f := vectors.Load(t, "jwt_verify.json")
	out := make([]string, 0, len(f.Cases))
	for _, c := range f.Cases {
		var in jwtInput
		if err := vectors.StrictUnmarshal(c.Input, &in); err == nil {
			out = append(out, in.Token)
		}
	}
	return out
}
