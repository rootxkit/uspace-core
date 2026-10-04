package mmapfile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func writeFile(t *testing.T, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.bin")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// forceFallback switches Map to Read for one test and restores it (E-11).
func forceFallback(t *testing.T) {
	t.Helper()
	old := supported
	supported = false
	t.Cleanup(func() { supported = old })
}

func content(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7 + i>>8)
	}
	return b
}

func TestMapReadsTheFile(t *testing.T) {
	want := content(3*4096 + 17)
	before := Live()
	m, err := Map(writeFile(t, want), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.Bytes(), want) {
		t.Fatal("mapped bytes differ from the file")
	}
	if m.Mapped() != Supported {
		t.Errorf("Mapped() = %v, Supported = %v", m.Mapped(), Supported)
	}
	if Supported && Live() != before+1 {
		t.Errorf("live mappings %d, want %d", Live(), before+1)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if Live() != before {
		t.Errorf("live mappings %d after Close, want %d", Live(), before)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if Live() != before {
		t.Errorf("a second Close changed the live count to %d", Live())
	}
	if m.Bytes() != nil {
		t.Error("bytes still reachable after Close")
	}
}

// TestMapSurvivesClosingTheFile: the mapping does not need the descriptor.
func TestMapSurvivesClosingTheFile(t *testing.T) {
	want := content(10000)
	f := writeFile(t, want)
	m, err := Map(f, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.Bytes(), want) {
		t.Fatal("bytes changed after the file was closed")
	}
}

func TestFallbackReadsTheFile(t *testing.T) {
	forceFallback(t)
	want := content(5000)
	before := Live()
	m, err := Map(writeFile(t, want), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if m.Mapped() {
		t.Error("the fallback reports a mapping")
	}
	if !bytes.Equal(m.Bytes(), want) {
		t.Fatal("read bytes differ from the file")
	}
	if Live() != before {
		t.Errorf("the fallback counted a live mapping")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyFile(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "map", true: "read"}[fallback], func(t *testing.T) {
			if fallback {
				forceFallback(t)
			}
			m, err := Map(writeFile(t, nil), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Bytes()) != 0 || m.Mapped() {
				t.Errorf("empty file: %d bytes, mapped %v", len(m.Bytes()), m.Mapped())
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestBound: a file one byte past the bound is refused before anything is
// mapped (E-10); one exactly at the bound is accepted (E-01 twin).
func TestBound(t *testing.T) {
	const bound = 4096
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "map", true: "read"}[fallback], func(t *testing.T) {
			if fallback {
				forceFallback(t)
			}
			before := Live()
			_, err := Map(writeFile(t, content(bound+1)), bound)
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != "file" || !strings.Contains(fe.Reason, "larger than 4096 bytes") {
				t.Fatalf("past the bound: %v", err)
			}
			if Live() != before {
				t.Error("a refused file left a live mapping")
			}
			m, err := Map(writeFile(t, content(bound)), bound)
			if err != nil {
				t.Fatalf("at the bound: %v", err)
			}
			if len(m.Bytes()) != bound {
				t.Errorf("%d bytes", len(m.Bytes()))
			}
			_ = m.Close()
		})
	}
}

// TestReadRefusesGrowth: a file that grows past the bound after the size
// check is refused by the read itself; the read accepts one at the bound.
func TestReadRefusesGrowth(t *testing.T) {
	_, err := readLimited(writeFile(t, content(151)), 150)
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "file" {
		t.Fatalf("grown file: %v", err)
	}
	data, err := readLimited(writeFile(t, content(150)), 150)
	if err != nil || len(data) != 150 {
		t.Fatalf("at the bound: %d bytes, %v", len(data), err)
	}
}

func TestRefusesDirectory(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "map", true: "read"}[fallback], func(t *testing.T) {
			if fallback {
				forceFallback(t)
			}
			d, err := os.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			_, err = Map(d, 1<<20)
			var fe *core.FieldError
			if !errors.As(err, &fe) || fe.Field != "file" || !strings.Contains(fe.Reason, "not a regular file") {
				t.Fatalf("directory: %v", err)
			}
		})
	}
}

func TestRefusesClosedFile(t *testing.T) {
	f := writeFile(t, content(10))
	_ = f.Close()
	if _, err := Map(f, 1<<20); err == nil {
		t.Error("a closed file mapped")
	}
	if _, err := Read(f, 1<<20); err == nil {
		t.Error("a closed file read")
	}
}

// TestMapRefusesAFileTruncatedWhileMapping: a file truncated between the
// size check and the map is refused, not mapped past its end, where the
// first read would fault the process with SIGBUS. The accepted twin is
// the same file left alone.
func TestMapRefusesAFileTruncatedWhileMapping(t *testing.T) {
	if !Supported {
		t.Skip("no memory maps on this platform: Read copies what is there")
	}
	const n = 5 * 4096
	f := writeFile(t, content(n))
	beforeMap = func() {
		if err := os.Truncate(f.Name(), 100); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { beforeMap = nil })
	before := Live()
	m, err := Map(f, 1<<20)
	if err == nil {
		// Without the check this read is past the end of the file.
		t.Fatalf("a truncated file mapped, last byte %d", m.Bytes()[n-1])
	}
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "file" || !strings.Contains(fe.Reason, "changed") {
		t.Fatalf("truncated while mapping: %v", err)
	}
	if Live() != before {
		t.Error("the refused mapping was not released")
	}
	beforeMap = nil
	g := writeFile(t, content(n))
	m, err = Map(g, 1<<20)
	if err != nil || len(m.Bytes()) != n || m.Bytes()[n-1] != content(n)[n-1] {
		t.Fatalf("untouched file: %v", err)
	}
	_ = m.Close()
}
