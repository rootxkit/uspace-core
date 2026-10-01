package odid

import (
	"encoding/hex"
	"testing"
)

func BenchmarkDecodeMessage(b *testing.B) {
	raw, _ := hex.DecodeString("12205a33fdda128718134dc21a0000410df707000000000000")
	frame := [MessageSize]byte(raw)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := DecodeMessage(frame, DecodeOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDecodePack(b *testing.B) {
	frame, err := EncodePack([]Message{BasicID{IDType: IDTypeSerial, UAID: "1581F4XFC233L00B00A9"}, baseLocation(), baseSystem(), OperatorID{OperatorID: "GEO-OP-1"}})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if ms, err := Decode(frame, DecodeOptions{}); err != nil || len(ms) != 4 {
			b.Fatal(err, len(ms))
		}
	}
}

func BenchmarkEncodeMessage(b *testing.B) {
	l := baseLocation()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Encode(l); err != nil {
			b.Fatal(err)
		}
	}
}
