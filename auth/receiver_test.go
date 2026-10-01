package auth

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/vectors"
)

var (
	rxKey   = bytes.Repeat([]byte{0x11}, 32)
	rxOther = bytes.Repeat([]byte{0x22}, 32)
	rxNow   = time.Unix(1_790_000_000, 0).UTC()
)

func rxReport(id string, sentAtMS any, nonce any) []byte {
	return []byte(fmt.Sprintf(`{"receiver_id": %q, "transmitter": "AA:BB", "payload_hex": "00", "sent_at_ms": %v, "nonce": %v}`,
		id, sentAtMS, nonce))
}

func rxSigned(key, report []byte) []byte { return Datagram(report, SignReport(key, report)) }

func newRX(t testing.TB, opts ...ReceiverOption) *ReceiverVerifier {
	t.Helper()
	v, err := NewReceiverVerifier(map[string][]byte{"rx-1": rxKey}, 30*time.Second, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func wantRefused(t *testing.T, err error, counter, reason string) {
	t.Helper()
	var re *ReceiverError
	if !errors.As(err, &re) {
		t.Fatalf("got %v, want a *ReceiverError %q", err, reason)
	}
	if re.Counter != counter || re.Reason != reason {
		t.Errorf("got %s %q, want %s %q", re.Counter, re.Reason, counter, reason)
	}
}

// Each refusal beside the accepted datagram that differs in one thing
// (E-01).
func TestReceiverRefusalsBesideAcceptance(t *testing.T) {
	good := rxReport("rx-1", 1_790_000_000_000, `"n-1"`)
	cases := []struct {
		name     string
		datagram []byte
		counter  string
		reason   string
	}{
		{"accepted", rxSigned(rxKey, good), CounterAccepted, ""},
		{"unsigned", good, CounterRejectedUnsigned, "not signed"},
		{"not JSON", rxSigned(rxKey, []byte(`{"receiver_id": `)), CounterRejectedMalformed, "the signed report is not JSON"},
		{"array", rxSigned(rxKey, []byte(`[1]`)), CounterRejectedMalformed, "the signed report is not a JSON object"},
		{"null", rxSigned(rxKey, []byte(`null`)), CounterRejectedMalformed, "the signed report is not a JSON object"},
		{"unknown receiver", rxSigned(rxKey, rxReport("rx-2", 1_790_000_000_000, `"n-1"`)), CounterRejectedUnknownReceiver, "unknown receiver 'rx-2'"},
		{"receiver id not a string", rxSigned(rxKey, []byte(`{"receiver_id": 7}`)), CounterRejectedUnknownReceiver, "unknown receiver (receiver_id is not a string)"},
		{"receiver id quoted safely", rxSigned(rxKey, rxReport("rx-\n"+strings.Repeat("x", 80), 1, `"n"`)), CounterRejectedUnknownReceiver,
			"unknown receiver 'rx-\\n" + strings.Repeat("x", 60) + "'..."},
		{"wrong key", rxSigned(rxOther, good), CounterRejectedBadSignature, "bad signature from rx-1"},
		{"short signature", Datagram(good, "abcd"), CounterRejectedBadSignature, "bad signature from rx-1"},
		{"signature not hex", Datagram(good, strings.Repeat("zz", 32)), CounterRejectedBadSignature, "bad signature from rx-1"},
		{"sent_at_ms float", rxSigned(rxKey, rxReport("rx-1", "1790000000000.0", `"n-1"`)), CounterRejectedMalformed, "sent_at_ms is not an integer"},
		{"sent_at_ms exponent", rxSigned(rxKey, rxReport("rx-1", "1.79e12", `"n-1"`)), CounterRejectedMalformed, "sent_at_ms is not an integer"},
		{"sent_at_ms bool", rxSigned(rxKey, rxReport("rx-1", "true", `"n-1"`)), CounterRejectedMalformed, "sent_at_ms is not an integer"},
		{"sent_at_ms past int64", rxSigned(rxKey, rxReport("rx-1", "99999999999999999999", `"n-1"`)), CounterRejectedMalformed, "sent_at_ms is not an integer"},
		{"sent_at_ms minus in the middle", rxSigned(rxKey, rxReport("rx-1", "1-2", `"n-1"`)), CounterRejectedMalformed, "the signed report is not JSON"},
		{"nonce empty", rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `""`)), CounterRejectedMalformed, "no nonce"},
		{"nonce a number", rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `5`)), CounterRejectedMalformed, "no nonce"},
		{"nonce too long", rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `"`+strings.Repeat("n", MaxNonceBytes+1)+`"`)), CounterRejectedMalformed, "nonce longer than 256 bytes"},
		{"nonce at the bound", rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `"`+strings.Repeat("n", MaxNonceBytes)+`"`)), CounterAccepted, ""},
		{"skew far past", rxSigned(rxKey, rxReport("rx-1", "-9223372036854775808", `"n-1"`)), CounterRejectedSkew, "sent 9223372037 s from now, more than 30 s"},
		{"skew exactly 30 s", rxSigned(rxKey, rxReport("rx-1", 1_789_999_970_000, `"n-1"`)), CounterAccepted, ""},
		{"skew 30.001 s", rxSigned(rxKey, rxReport("rx-1", 1_789_999_969_999, `"n-1"`)), CounterRejectedSkew, "sent 30 s from now, more than 30 s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := newRX(t)
			r, err := v.Verify(tc.datagram, rxNow)
			if tc.counter == CounterAccepted {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				if r.ReceiverID != "rx-1" || r.Transmitter != "AA:BB" || r.PayloadHex != "00" {
					t.Errorf("report %+v", r)
				}
			} else {
				wantRefused(t, err, tc.counter, tc.reason)
			}
			if got := v.Counters().Get(tc.counter); got != 1 {
				t.Errorf("counter %s = %d, want 1 (%v)", tc.counter, got, v.Counters().Snapshot())
			}
		})
	}
}

func TestReceiverReportFields(t *testing.T) {
	v := newRX(t)
	rep := rxReport("rx-1", 1_790_000_000_500, `"n-9"`)
	d := rxSigned(rxKey, rep)
	r, err := v.Verify(d, rxNow)
	if err != nil {
		t.Fatal(err)
	}
	want := Report{ReceiverID: "rx-1", Transmitter: "AA:BB", PayloadHex: "00", SentAtMS: 1_790_000_000_500, Nonce: "n-9"}
	if r.ReceiverID != want.ReceiverID || r.Transmitter != want.Transmitter || r.PayloadHex != want.PayloadHex ||
		r.SentAtMS != want.SentAtMS || r.Nonce != want.Nonce {
		t.Errorf("got %+v, want %+v", r, want)
	}
	if !bytes.Equal(r.Raw, rep) {
		t.Errorf("raw %q", r.Raw)
	}
	d[0] = 'X' // the caller reuses its buffer: Raw is a copy
	if r.Raw[0] != '{' {
		t.Error("Raw aliases the datagram")
	}
}

// A trailing line end after the MAC is tolerated; the MAC itself is
// lower-case hex only, as the vector writes it.
func TestReceiverMACCaseAndTrailingLineEnd(t *testing.T) {
	rep := rxReport("rx-1", 1_790_000_000_000, `"n-1"`)
	sig := SignReport(rxKey, rep)
	if _, err := newRX(t).Verify(Datagram(rep, sig+"\r\n"), rxNow); err != nil {
		t.Fatalf("lower-case MAC with a line end refused: %v", err)
	}
	_, err := newRX(t).Verify(Datagram(rep, strings.ToUpper(sig)), rxNow)
	wantRefused(t, err, CounterRejectedBadSignature, "bad signature from rx-1")
}

// E-10: the datagram bound, at it (accepted) and one byte past it.
func TestReceiverDatagramBound(t *testing.T) {
	pad := func(n int) []byte {
		base := rxReport("rx-1", 1_790_000_000_000, `"n-1"`)
		sigLen := len(SignatureMarker) + 64
		rep := append([]byte(nil), base[:len(base)-1]...)
		for len(rep)+1+sigLen < n {
			rep = append(rep, ' ')
		}
		return rxSigned(rxKey, append(rep, '}'))
	}
	const bound = 512
	at := pad(bound)
	if len(at) != bound {
		t.Fatalf("built %d bytes", len(at))
	}
	if _, err := newRX(t, WithMaxDatagramBytes(bound)).Verify(at, rxNow); err != nil {
		t.Fatalf("a datagram at the bound was refused: %v", err)
	}
	v := newRX(t, WithMaxDatagramBytes(bound))
	_, err := v.Verify(pad(bound+1), rxNow)
	wantRefused(t, err, CounterRejectedMalformed, "datagram longer than 512 bytes")
	_, err = newRX(t).Verify(pad(DefaultMaxDatagramBytes+1), rxNow)
	wantRefused(t, err, CounterRejectedMalformed, "datagram longer than 4096 bytes")
}

func TestReceiverZeroNowUsesClock(t *testing.T) {
	v := newRX(t, WithNow(func() time.Time { return rxNow }))
	d := rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `"n-1"`))
	if _, err := v.Verify(d, time.Time{}); err != nil {
		t.Fatalf("refused with the injected clock: %v", err)
	}
	v2 := newRX(t, WithNow(func() time.Time { return rxNow.Add(time.Hour) }))
	_, err := v2.Verify(d, time.Time{})
	wantRefused(t, err, CounterRejectedSkew, "sent 3600 s from now, more than 30 s")
}

func TestNewReceiverVerifierRefusesBadConfig(t *testing.T) {
	cases := []struct {
		name  string
		keys  map[string][]byte
		skew  time.Duration
		opts  []ReceiverOption
		field string
	}{
		{"no keys", nil, time.Second, nil, "keys"},
		{"empty id", map[string][]byte{"": rxKey}, time.Second, nil, "keys"},
		{"short key", map[string][]byte{"rx-1": rxKey[:31]}, time.Second, nil, "keys.rx-1"},
		{"zero skew", map[string][]byte{"rx-1": rxKey}, 0, nil, "max_skew"},
		{"zero nonce memory", map[string][]byte{"rx-1": rxKey}, time.Second, []ReceiverOption{WithNonceMemory(0)}, "nonce_memory"},
		{"nil clock", map[string][]byte{"rx-1": rxKey}, time.Second, []ReceiverOption{WithNow(nil)}, "now"},
		{"zero datagram bound", map[string][]byte{"rx-1": rxKey}, time.Second, []ReceiverOption{WithMaxDatagramBytes(0)}, "max_datagram_bytes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewReceiverVerifier(tc.keys, tc.skew, tc.opts...)
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != tc.field {
				t.Fatalf("got %v, want a FieldError on %s", err, tc.field)
			}
		})
	}
	// The presence twin: a 32-byte key and a positive window build.
	if _, err := NewReceiverVerifier(map[string][]byte{"rx-1": rxKey[:32]}, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestAddReceiverKey(t *testing.T) {
	keys := map[string][]byte{}
	if err := AddReceiverKey(keys, "rx-1", rxKey); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   string
		key  []byte
		want string
	}{
		{"rx-1", rxOther, "keys.rx-1: 'rx-1' appears twice"},
		{"", rxKey, "keys: a receiver id is empty"},
		{"rx-2", rxKey[:8], "keys.rx-2: the key is 8 bytes; at least 32"},
	} {
		if err := AddReceiverKey(keys, tc.id, tc.key); err == nil || err.Error() != tc.want {
			t.Errorf("got %v, want %q", err, tc.want)
		}
	}
	if !bytes.Equal(keys["rx-1"], rxKey) || len(keys) != 1 {
		t.Errorf("keys changed by a refused add: %v", keys)
	}
}

func TestReceiverKeysAreCopied(t *testing.T) {
	key := bytes.Clone(rxKey)
	v, err := NewReceiverVerifier(map[string][]byte{"rx-1": key}, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	key[0] ^= 0xff
	if _, err := v.Verify(rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `"n"`)), rxNow); err != nil {
		t.Fatalf("a caller's later change to the key reached the verifier: %v", err)
	}
}

// A nonce is refused while it is remembered (twice the window) and
// accepted again once forgotten; nonces are per receiver.
func TestReceiverNonceWindow(t *testing.T) {
	v, err := NewReceiverVerifier(map[string][]byte{"rx-1": rxKey, "rx-2": rxOther}, 30*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	at := func(now time.Time, id string, key []byte) error {
		ms := now.UnixMilli()
		_, err := v.Verify(rxSigned(key, rxReport(id, ms, `"same"`)), now)
		return err
	}
	if err := at(rxNow, "rx-1", rxKey); err != nil {
		t.Fatal(err)
	}
	if err := at(rxNow, "rx-2", rxOther); err != nil {
		t.Fatalf("a nonce of rx-1 refused rx-2: %v", err)
	}
	wantRefused(t, at(rxNow.Add(60*time.Second), "rx-1", rxKey), CounterRejectedReplay, "nonce repeated by rx-1")
	if err := at(rxNow.Add(60*time.Second+time.Millisecond), "rx-1", rxKey); err != nil {
		t.Fatalf("a nonce older than twice the window is still remembered: %v", err)
	}
}

// E-10: the nonce memory past its bound evicts the oldest and counts it.
func TestReceiverNonceMemoryBound(t *testing.T) {
	const bound = 3
	v := newRX(t, WithNonceMemory(bound))
	send := func(nonce string) error {
		_, err := v.Verify(rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `"`+nonce+`"`)), rxNow)
		return err
	}
	for i := range bound {
		if err := send(fmt.Sprint("n", i)); err != nil {
			t.Fatal(err)
		}
	}
	if got := v.Counters().Get(CounterNoncesEvicted); got != 0 {
		t.Fatalf("evicted %d at the bound", got)
	}
	wantRefused(t, send("n0"), CounterRejectedReplay, "nonce repeated by rx-1")
	if err := send("n3"); err != nil { // past the bound: n0 evicted
		t.Fatal(err)
	}
	if got := v.Counters().Get(CounterNoncesEvicted); got != 1 {
		t.Fatalf("nonces_evicted = %d, want 1", got)
	}
	if err := send("n0"); err != nil {
		t.Fatalf("the evicted nonce is still remembered: %v", err)
	}
	wantRefused(t, send("n3"), CounterRejectedReplay, "nonce repeated by rx-1")
}

// The queue compacts without losing order across many arrivals.
func TestReceiverNonceQueueCompacts(t *testing.T) {
	v := newRX(t, WithNonceMemory(10))
	for i := range 5000 {
		_, err := v.Verify(rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, fmt.Sprintf(`"n%d"`, i))), rxNow)
		if err != nil {
			t.Fatal(err)
		}
	}
	m := v.nonces["rx-1"]
	if len(m.seen) != 10 || len(m.order)-m.head != 10 || len(m.order) > 2048 {
		t.Fatalf("seen %d, queue %d (head %d)", len(m.seen), len(m.order), m.head)
	}
	if _, err := v.Verify(rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, `"n4999"`)), rxNow); err == nil {
		t.Fatal("the newest nonce was lost by compaction")
	}
	m.popOldest()
	(&nonceMemory{seen: map[string]struct{}{}}).popOldest() // empty queue: no-op
}

func TestReceiverConcurrentVerify(t *testing.T) {
	v := newRX(t)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 50 {
				d := rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, fmt.Sprintf(`"g%d-%d"`, g, i)))
				if _, err := v.Verify(d, rxNow); err != nil {
					t.Error(err)
				}
				if _, err := v.Verify(d, rxNow); err == nil {
					t.Error("replay accepted")
				}
			}
		})
	}
	wg.Wait()
	if a, r := v.Counters().Get(CounterAccepted), v.Counters().Get(CounterRejectedReplay); a != 400 || r != 400 {
		t.Errorf("accepted %d, replays %d", a, r)
	}
}

func receiverSeeds(f *testing.F) {
	vf := vectors.Load(f, "rid_receiver_auth.json")
	for _, c := range vf.Cases {
		var in receiverVerifyInput
		if err := vectors.StrictUnmarshal(c.Input, &in); err != nil {
			continue
		}
		for _, d := range in.Datagrams {
			f.Add([]byte(d.UTF8))
		}
	}
	f.Add([]byte(""))
	f.Add([]byte(SignatureMarker))
	f.Add([]byte("null\nsig=" + strings.Repeat("0", 64)))
}

func FuzzVerifyReceiver(f *testing.F) {
	receiverSeeds(f)
	keys := map[string][]byte{"rx-1": {
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f,
	}}
	v, err := NewReceiverVerifier(keys, 30*time.Second, WithNonceMemory(64))
	if err != nil {
		f.Fatal(err)
	}
	now := time.Unix(1_790_000_000, 0)
	f.Fuzz(func(t *testing.T, datagram []byte) {
		r, err := v.Verify(datagram, now)
		if err != nil {
			var re *ReceiverError
			if !errors.As(err, &re) || re.Counter == "" {
				t.Fatalf("refusal %v is not a counted ReceiverError", err)
			}
			return
		}
		if r.ReceiverID != "rx-1" || r.Nonce == "" || !bytes.HasPrefix(datagram, r.Raw) {
			t.Fatalf("accepted an inconsistent report %+v", r)
		}
	})
}

func BenchmarkVerifyReceiver(b *testing.B) {
	v := newRX(b, WithNonceMemory(1024))
	datagrams := make([][]byte, 4096)
	for i := range datagrams {
		datagrams[i] = rxSigned(rxKey, rxReport("rx-1", 1_790_000_000_000, fmt.Sprintf(`"n%d"`, i)))
	}
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		// Nonces repeat after 4096 datagrams, by then evicted (bound 1024).
		if _, err := v.Verify(datagrams[i%len(datagrams)], rxNow); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
