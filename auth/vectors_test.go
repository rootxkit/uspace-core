package auth

import (
	"encoding/hex"
	"errors"
	"math"
	"testing"
	"time"

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
