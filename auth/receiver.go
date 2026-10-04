package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rootxkit/uspace-core/core"
)

// SignatureMarker separates the report from its signature in a receiver
// datagram: <report bytes>\nsig=<64 lower-case hex characters>.
const SignatureMarker = "\nsig="

// MinReceiverKeyBytes is the shortest receiver key NewReceiverVerifier
// accepts (32 random bytes per receiver, LESSONS R-06).
const MinReceiverKeyBytes = 32

// DefaultNonceMemory is the default bound on the nonces remembered per
// receiver (WithNonceMemory).
const DefaultNonceMemory = 100_000

// MaxNonceBytes bounds the length of one nonce, so that the nonce memory
// is bounded in bytes as well as in entries (E-10). A longer nonce is
// refused as malformed.
const MaxNonceBytes = 256

// DefaultMaxDatagramBytes is the default bound on a receiver datagram
// (WithMaxDatagramBytes). The report is parsed as JSON before its HMAC can
// be checked (the receiver id selects the key), so the size is bounded
// first: an unauthenticated sender cannot make the ingest parse more. A
// signed report with an ODID message pack is well under 1 KiB.
const DefaultMaxDatagramBytes = 4096

// Receiver counter names (E-09).
const (
	CounterAccepted                = "accepted"
	CounterRejectedUnsigned        = "rejected_unsigned"
	CounterRejectedUnknownReceiver = "rejected_unknown_receiver"
	CounterRejectedBadSignature    = "rejected_bad_signature"
	CounterRejectedSkew            = "rejected_skew"
	CounterRejectedReplay          = "rejected_replay"
	CounterRejectedMalformed       = "rejected_malformed"
	CounterNoncesEvicted           = "nonces_evicted"
)

// shortIDBytes bounds how much of an untrusted identifier an error quotes.
const shortIDBytes = 64

// SignReport returns the hex HMAC-SHA256 of the exact report bytes under
// key: what a receiver appends after SignatureMarker.
func SignReport(key, report []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(report)
	return hex.EncodeToString(m.Sum(nil))
}

// Datagram returns report + "\nsig=" + sigHex, the bytes a receiver
// sends.
func Datagram(report []byte, sigHex string) []byte {
	out := make([]byte, 0, len(report)+len(SignatureMarker)+len(sigHex))
	out = append(out, report...)
	out = append(out, SignatureMarker...)
	return append(out, sigHex...)
}

// ReceiverError is a refused receiver datagram. Error returns the reason
// (the phrases of rid_receiver_auth.json); Counter is the counter the
// refusal incremented.
type ReceiverError struct {
	Counter string
	Reason  string
}

func (e *ReceiverError) Error() string { return e.Reason }

// ReceiverOption configures a ReceiverVerifier.
type ReceiverOption func(*receiverOptions)

type receiverOptions struct {
	nonceMemory int
	maxDatagram int
	now         func() time.Time
}

// WithNonceMemory bounds the nonces remembered per receiver (default
// DefaultNonceMemory). When the bound is reached the oldest nonce is
// forgotten and nonces_evicted counts it: that nonce could then be
// replayed inside the window, so the bound should exceed the receiver's
// datagram rate times twice the window.
func WithNonceMemory(maxNonces int) ReceiverOption {
	return func(o *receiverOptions) { o.nonceMemory = maxNonces }
}

// WithMaxDatagramBytes bounds the datagram size (default
// DefaultMaxDatagramBytes). A longer datagram is refused as malformed
// before it is parsed.
func WithMaxDatagramBytes(maxBytes int) ReceiverOption {
	return func(o *receiverOptions) { o.maxDatagram = maxBytes }
}

// WithNow sets the clock Verify uses when it is called with a zero time
// (default time.Now).
func WithNow(now func() time.Time) ReceiverOption {
	return func(o *receiverOptions) { o.now = now }
}

// Report is an authenticated receiver report.
type Report struct {
	ReceiverID  string
	Transmitter string
	PayloadHex  string
	SentAtMS    int64
	Nonce       string
	// Raw is a copy of the exact signed report bytes.
	Raw json.RawMessage
}

// ReceiverVerifier authenticates Remote ID receiver datagrams (LESSONS
// R-06): a known receiver, its HMAC-SHA256 over the exact report bytes,
// sent_at_ms within maxSkew of the ingest's clock, and a nonce not seen
// from that receiver within twice the window. It is safe for concurrent
// use.
//
// An ingest configured without receiver keys cannot authenticate anyone:
// it must refuse every report and report itself not ready until a key
// arrives (R-06); this type refuses to be built without keys.
type ReceiverVerifier struct {
	keys     map[string][]byte
	maxSkew  time.Duration
	opts     receiverOptions
	counters core.Counters

	mu     sync.Mutex
	nonces map[string]*nonceMemory
}

// NewReceiverVerifier builds a verifier for the given receiver keys. It
// refuses no keys, an empty receiver id, a key shorter than
// MinReceiverKeyBytes, a non-positive maxSkew or nonce bound (B-14: a
// configuration error stops start-up). The keys are copied.
func NewReceiverVerifier(keys map[string][]byte, maxSkew time.Duration, opts ...ReceiverOption) (*ReceiverVerifier, error) {
	o := receiverOptions{nonceMemory: DefaultNonceMemory, maxDatagram: DefaultMaxDatagramBytes, now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}
	if len(keys) == 0 {
		return nil, core.Fieldf("keys", "no receiver keys: an ingest without keys refuses every report")
	}
	if maxSkew <= 0 {
		return nil, core.Fieldf("max_skew", "must be positive, got %s", maxSkew)
	}
	if o.nonceMemory < 1 {
		return nil, core.Fieldf("nonce_memory", "must be at least 1, got %d", o.nonceMemory)
	}
	if o.maxDatagram < 1 {
		return nil, core.Fieldf("max_datagram_bytes", "must be at least 1, got %d", o.maxDatagram)
	}
	if o.now == nil {
		return nil, core.Fieldf("now", "the clock is nil")
	}
	v := &ReceiverVerifier{
		keys:    make(map[string][]byte, len(keys)),
		maxSkew: maxSkew,
		opts:    o,
		nonces:  make(map[string]*nonceMemory, len(keys)),
	}
	for id, key := range keys {
		if id == "" {
			return nil, core.Fieldf("keys", "a receiver id is empty")
		}
		if len(key) < MinReceiverKeyBytes {
			return nil, core.Fieldf("keys."+id, "the key is %d bytes; at least %d", len(key), MinReceiverKeyBytes)
		}
		v.keys[id] = bytes.Clone(key)
	}
	return v, nil
}

// AddReceiverKey is the startup-time form of NewReceiverVerifier for key
// files read line by line: it refuses a receiver id that appears twice
// (B-14), which a Go map cannot express.
func AddReceiverKey(keys map[string][]byte, id string, key []byte) error {
	if id == "" {
		return core.Fieldf("keys", "a receiver id is empty")
	}
	if _, dup := keys[id]; dup {
		return core.Fieldf("keys."+id, "%s appears twice", quoteShort(id))
	}
	if len(key) < MinReceiverKeyBytes {
		return core.Fieldf("keys."+id, "the key is %d bytes; at least %d", len(key), MinReceiverKeyBytes)
	}
	keys[id] = bytes.Clone(key)
	return nil
}

// Counters returns the verifier's counters: accepted, rejected_unsigned,
// rejected_unknown_receiver, rejected_bad_signature, rejected_skew,
// rejected_replay, rejected_malformed, nonces_evicted.
func (v *ReceiverVerifier) Counters() *core.Counters { return &v.counters }

// Verify authenticates one datagram at now (the ingest's clock; a zero
// now reads the WithNow clock). The checks run in this order, each
// refusal counted: signature marker present, report is a JSON object,
// receiver known, signature valid, sent_at_ms an integer, nonce present,
// sent_at_ms within maxSkew of now, nonce new for that receiver.
func (v *ReceiverVerifier) Verify(datagram []byte, now time.Time) (Report, error) {
	if now.IsZero() {
		now = v.opts.now()
	}
	r, err := v.verify(datagram, now)
	if err != nil {
		var re *ReceiverError
		if errors.As(err, &re) {
			v.counters.Inc(re.Counter)
		}
		return Report{}, err
	}
	v.counters.Inc(CounterAccepted)
	return r, nil
}

func refuse(counter, format string, args ...any) error {
	return &ReceiverError{Counter: counter, Reason: fmt.Sprintf(format, args...)}
}

func (v *ReceiverVerifier) verify(datagram []byte, now time.Time) (Report, error) {
	if len(datagram) > v.opts.maxDatagram {
		return Report{}, refuse(CounterRejectedMalformed, "datagram longer than %d bytes", v.opts.maxDatagram)
	}
	i := bytes.LastIndex(datagram, []byte(SignatureMarker))
	if i < 0 {
		return Report{}, refuse(CounterRejectedUnsigned, "not signed")
	}
	report := datagram[:i]
	sig := bytes.TrimSpace(datagram[i+len(SignatureMarker):])

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(report, &fields); err != nil || fields == nil {
		var typeErr *json.UnmarshalTypeError
		if err != nil && !errors.As(err, &typeErr) {
			return Report{}, refuse(CounterRejectedMalformed, "the signed report is not JSON")
		}
		return Report{}, refuse(CounterRejectedMalformed, "the signed report is not a JSON object")
	}

	id, ok := jsonString(fields["receiver_id"])
	if !ok {
		return Report{}, refuse(CounterRejectedUnknownReceiver, "unknown receiver (receiver_id is not a string)")
	}
	key, ok := v.keys[id]
	if !ok {
		return Report{}, refuse(CounterRejectedUnknownReceiver, "unknown receiver %s", quoteShort(id))
	}

	want := hmac.New(sha256.New, key)
	want.Write(report)
	got := make([]byte, sha256.Size)
	// Exactly 64 lower-case hex characters, as receivers send them.
	if len(sig) != hex.EncodedLen(sha256.Size) || !isLowerHex(sig) {
		return Report{}, refuse(CounterRejectedBadSignature, "bad signature from %s", id)
	}
	if _, err := hex.Decode(got, sig); err != nil {
		return Report{}, refuse(CounterRejectedBadSignature, "bad signature from %s", id)
	}
	if !hmac.Equal(want.Sum(nil), got) {
		return Report{}, refuse(CounterRejectedBadSignature, "bad signature from %s", id)
	}

	sentAtMS, ok := jsonInt64(fields["sent_at_ms"])
	if !ok {
		return Report{}, refuse(CounterRejectedMalformed, "sent_at_ms is not an integer")
	}
	nonce, ok := jsonString(fields["nonce"])
	if !ok || nonce == "" {
		return Report{}, refuse(CounterRejectedMalformed, "no nonce")
	}
	if len(nonce) > MaxNonceBytes {
		return Report{}, refuse(CounterRejectedMalformed, "nonce longer than %d bytes", MaxNonceBytes)
	}

	skew := absDuration(now.Sub(time.UnixMilli(sentAtMS)))
	if skew > v.maxSkew {
		return Report{}, refuse(CounterRejectedSkew, "sent %.0f s from now, more than %.0f s",
			skew.Seconds(), v.maxSkew.Seconds())
	}

	if !v.remember(id, nonce, now) {
		return Report{}, refuse(CounterRejectedReplay, "nonce repeated by %s", id)
	}

	r := Report{
		ReceiverID: id,
		SentAtMS:   sentAtMS,
		Nonce:      nonce,
		Raw:        json.RawMessage(bytes.Clone(report)),
	}
	r.Transmitter, _ = jsonString(fields["transmitter"])
	r.PayloadHex, _ = jsonString(fields["payload_hex"])
	return r, nil
}

// remember records nonce for receiver id at now and reports whether it
// was new. Nonces are remembered for twice the window: a nonce is refused
// for as long as its sent_at_ms could still be accepted, whichever side
// of now it was.
func (v *ReceiverVerifier) remember(id, nonce string, now time.Time) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	m := v.nonces[id]
	if m == nil {
		m = &nonceMemory{seen: make(map[string]struct{})}
		v.nonces[id] = m
	}
	m.forget(now, 2*v.maxSkew)
	if _, dup := m.seen[nonce]; dup {
		return false
	}
	for len(m.seen) >= v.opts.nonceMemory {
		m.popOldest()
		v.counters.Inc(CounterNoncesEvicted)
	}
	m.seen[nonce] = struct{}{}
	m.order = append(m.order, nonceEntry{at: now, nonce: nonce})
	return true
}

type nonceEntry struct {
	at    time.Time
	nonce string
}

// nonceMemory is one receiver's nonces in arrival order (a queue over a
// slice, compacted when half of it is consumed).
type nonceMemory struct {
	seen  map[string]struct{}
	order []nonceEntry
	head  int
}

func (m *nonceMemory) forget(now time.Time, keep time.Duration) {
	for m.head < len(m.order) && now.Sub(m.order[m.head].at) > keep {
		m.popOldest()
	}
}

func (m *nonceMemory) popOldest() {
	if m.head >= len(m.order) {
		return
	}
	delete(m.seen, m.order[m.head].nonce)
	m.order[m.head] = nonceEntry{}
	m.head++
	if m.head == len(m.order) {
		m.order = m.order[:0]
		m.head = 0
	} else if m.head >= 1024 && 2*m.head >= len(m.order) {
		n := copy(m.order, m.order[m.head:])
		clear(m.order[n:])
		m.order = m.order[:n]
		m.head = 0
	}
}

func isLowerHex(b []byte) bool {
	for _, c := range b {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) < 2 || raw[0] != '"' {
		return "", false
	}
	// raw comes from a successful parse: without an escape its content
	// is the string itself.
	if inner := raw[1 : len(raw)-1]; bytes.IndexByte(inner, '\\') < 0 && utf8.Valid(inner) {
		return string(inner), true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false
	}
	return s, true
}

// jsonInt64 accepts a JSON integer literal only: a fraction, an exponent,
// a string or a boolean is not an integer (the old implementation's
// isinstance(int) and not bool).
func jsonInt64(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	for j, c := range raw {
		if (c < '0' || c > '9') && (c != '-' || j != 0) {
			return 0, false
		}
	}
	n, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func absDuration(d time.Duration) time.Duration {
	if d >= 0 {
		return d
	}
	if d == time.Duration(-1<<63) {
		return time.Duration(1<<63 - 1)
	}
	return -d
}

// quoteShort quotes an untrusted identifier for an error message: single
// quotes, non-ASCII and control characters escaped, at most shortIDBytes
// bytes of the input.
func quoteShort(s string) string {
	suffix := ""
	if len(s) > shortIDBytes {
		s = s[:shortIDBytes]
		suffix = "..."
	}
	q := strconv.QuoteToASCII(s)
	return "'" + q[1:len(q)-1] + "'" + suffix
}
