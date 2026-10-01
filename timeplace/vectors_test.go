package timeplace

import (
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/vectors"
)

// ridTimeInput is the union of the broadcast and network case inputs of
// rid_time.json; TimestampTenths is set only on broadcast cases.
type ridTimeInput struct {
	TimestampTenths   *uint16    `json:"timestamp_tenths"`
	TSAccuracyCode    *uint8     `json:"ts_accuracy_code"`
	StateTimestamp    *time.Time `json:"state_timestamp"`
	ResponseTimestamp *time.Time `json:"response_timestamp"`
	ReceivedAt        time.Time  `json:"received_at"`
	MaxAgeS           *float64   `json:"max_age_s"`
	TimeToleranceS    float64    `json:"time_tolerance_s"`
	MaxLatencyS       float64    `json:"max_latency_s"`
}

type ridTimeExpected struct {
	TS         time.Time `json:"ts"`
	CapturedAt time.Time `json:"captured_at"`
	TimeSource string    `json:"time_source"`
	Fallback   *string   `json:"fallback"`
	Note       *string   `json:"note"`
}

// vectorSource maps the vector's time_source onto core (doc.go, "Source
// mapping").
var vectorSource = map[string]core.TimeSource{
	"broadcast": core.TimeBroadcast,
	"receiver":  core.TimeReceiver,
}

func TestVectorsRidTime(t *testing.T) {
	f := vectors.Load(t, "rid_time.json")
	if tol, ok := f.Tolerance["times"].(string); !ok || tol != "exact to the microsecond" {
		t.Fatalf("rid_time tolerance changed: %v", f.Tolerance["times"])
	}
	var rule string
	f.Header(t, "rule", &rule)
	if rule == "" {
		t.Fatal("rid_time header rule is empty")
	}
	broadcast, network := 0, 0
	f.Run(t, func(t *testing.T, c vectors.Case) {
		var in ridTimeInput
		c.Decode(t, &in, nil)
		if in.TimestampTenths != nil {
			broadcast++
			runBroadcast(t, c, in)
			return
		}
		network++
		runNetwork(t, c, in)
	})
	if broadcast != 18 || network != 7 {
		t.Errorf("ran %d broadcast and %d network cases, want 18 and 7", broadcast, network)
	}
}

func runBroadcast(t *testing.T, c vectors.Case, in ridTimeInput) {
	t.Helper()
	if in.TSAccuracyCode == nil || in.StateTimestamp != nil {
		t.Fatal("broadcast case without ts_accuracy_code or with a state_timestamp")
	}
	var exp ridTimeExpected
	c.Decode(t, nil, &exp)
	if exp.Note != nil {
		t.Fatal("broadcast case with a note")
	}
	pol := BroadcastPolicy{ToleranceS: in.TimeToleranceS, MaxLatencyS: in.MaxLatencyS}
	p := PlaceBroadcast(*in.TimestampTenths, *in.TSAccuracyCode, in.ReceivedAt, pol)
	vectors.EqualTime(t, "ts", p.TS, exp.TS)
	vectors.EqualTime(t, "captured_at", p.CapturedAt, exp.CapturedAt)
	if want := vectorSource[exp.TimeSource]; p.Source != want || want == "" {
		t.Errorf("time_source: got %q, want %q", p.Source, exp.TimeSource)
	}
	wantFB := FallbackNone
	if exp.Fallback != nil {
		wantFB = Fallback(*exp.Fallback)
	}
	if p.Fallback != wantFB {
		t.Errorf("fallback: got %q, want %q", p.Fallback, wantFB)
	}
}

func runNetwork(t *testing.T, c vectors.Case, in ridTimeInput) {
	t.Helper()
	if in.StateTimestamp == nil || in.MaxAgeS == nil {
		t.Fatal("network case without state_timestamp or max_age_s")
	}
	pol := NetworkPolicy{MaxAgeS: *in.MaxAgeS, ToleranceS: in.TimeToleranceS, MaxLatencyS: in.MaxLatencyS}
	p, note, shown := PlaceNetwork(*in.StateTimestamp, in.ResponseTimestamp, in.ReceivedAt, pol)
	if c.ExpectedIsNull() {
		if shown {
			t.Errorf("shown: got true with %+v, want not shown (expected null)", p)
		}
		return
	}
	var exp ridTimeExpected
	c.Decode(t, nil, &exp)
	if exp.Fallback != nil {
		t.Fatal("network case with a fallback")
	}
	if !shown {
		t.Fatal("shown: got false, want shown")
	}
	vectors.EqualTime(t, "ts", p.TS, exp.TS)
	vectors.EqualTime(t, "captured_at", p.CapturedAt, exp.CapturedAt)
	if want := vectorSource[exp.TimeSource]; p.Source != want || want == "" {
		t.Errorf("time_source: got %q, want %q", p.Source, exp.TimeSource)
	}
	wantNote := NoteNone
	if exp.Note != nil {
		wantNote = NetworkNote(*exp.Note)
	}
	if note != wantNote {
		t.Errorf("note: got %q, want %q", note, wantNote)
	}
}
