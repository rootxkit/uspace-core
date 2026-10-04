package pgm

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/internal/mmapfile"
)

func openBytes(t testing.TB, data []byte) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grid.pgm")
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

// drainMappings waits until every mapping made by earlier tests is
// released (their grids are unreachable), so that a test can count its
// own from zero.
func drainMappings(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for mmapfile.Live() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d mappings still live ten seconds after their grids became unreachable", mmapfile.Live())
		}
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
}

// sameGrid fails unless a and b have the same size, header and samples.
func sameGrid(t testing.TB, a, b *Grid) {
	t.Helper()
	if a.Width != b.Width || a.Height != b.Height || a.MaxVal != b.MaxVal || len(a.Header) != len(b.Header) {
		t.Fatalf("size or header differ: %dx%d/%d %v vs %dx%d/%d %v", a.Width, a.Height, a.MaxVal, a.Header, b.Width, b.Height, b.MaxVal, b.Header)
	}
	for k, v := range a.Header {
		if b.Header[k] != v {
			t.Fatalf("header %q: %q vs %q", k, v, b.Header[k])
		}
	}
	if !bytes.Equal(a.data, b.data) {
		t.Fatal("samples differ")
	}
	for iy := -1; iy <= a.Height; iy++ {
		for ix := -1; ix <= a.Width; ix++ {
			if a.Raw(ix, iy) != b.Raw(ix, iy) {
				t.Fatalf("Raw(%d, %d): %d vs %d", ix, iy, a.Raw(ix, iy), b.Raw(ix, iy))
			}
		}
	}
}

func TestParseFileAgreesWithParse(t *testing.T) {
	for name, data := range map[string][]byte{"minimal": minimal(), "geoid": syntheticGeoid(), "tile": syntheticTile()} {
		want, err := Parse(data, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, mapped := range []bool{false, true} {
			g, err := ParseFile(openBytes(t, data), int64(len(data)), 0, mapped, nil)
			if err != nil {
				t.Fatalf("%s mapped=%v: %v", name, mapped, err)
			}
			sameGrid(t, g, want)
			if got := g.Mapped(); got != (mapped && mmapfile.Supported) {
				t.Errorf("%s mapped=%v: Mapped() = %v (Supported %v)", name, mapped, got, mmapfile.Supported)
			}
		}
	}
	if g, err := Parse(minimal(), 0); err != nil || g.Mapped() {
		t.Errorf("a grid parsed from memory reports a mapping (%v)", err)
	}
	var nilGrid *Grid
	if nilGrid.Mapped() {
		t.Error("nil grid")
	}
}

// TestParseFileRefusals: what Parse refuses from memory ParseFile refuses
// from a file with the same error, mapped or read, and no mapping is left
// behind; a file past maxFileBytes is refused before it is mapped.
func TestParseFileRefusals(t *testing.T) {
	good := minimal()
	cases := map[string][]byte{
		"truncated by one byte": good[:len(good)-1],
		"header only":           good[:len(good)-12],
		"trailing byte":         append(append([]byte{}, good...), 0),
		"empty":                 {},
		"bad magic":             append([]byte("P6"), good[2:]...),
		"maxval 255":            []byte("P5\n1 1\n255\n\x00"),
	}
	drainMappings(t)
	for name, data := range cases {
		_, parseErr := Parse(data, 0)
		if parseErr == nil {
			t.Fatalf("%s: Parse accepted it", name)
		}
		for _, mapped := range []bool{false, true} {
			g, err := ParseFile(openBytes(t, data), 1<<20, 0, mapped, nil)
			if err == nil || g != nil {
				t.Fatalf("%s mapped=%v: accepted", name, mapped)
			}
			if err.Error() != parseErr.Error() {
				t.Errorf("%s mapped=%v: %v, Parse says %v", name, mapped, err, parseErr)
			}
			if mmapfile.Live() != 0 {
				t.Errorf("%s mapped=%v: a refused file left a live mapping", name, mapped)
			}
		}
	}
	// Past the file bound: refused before mapping (E-10); at it, accepted.
	for _, mapped := range []bool{false, true} {
		if _, err := ParseFile(openBytes(t, good), int64(len(good)-1), 0, mapped, nil); err == nil {
			t.Errorf("mapped=%v: a file past maxFileBytes was accepted", mapped)
		}
		if _, err := ParseFile(openBytes(t, good), int64(len(good)), 0, mapped, nil); err != nil {
			t.Errorf("mapped=%v: a file at maxFileBytes was refused: %v", mapped, err)
		}
	}
	// The caller's check refuses a grid Parse accepts, with its own error,
	// and the mapping goes with it; a check that passes keeps the grid.
	errCheck := errors.New("refused by the caller")
	drainMappings(t) // the grids accepted at the bound above
	for _, mapped := range []bool{false, true} {
		g, err := ParseFile(openBytes(t, good), 1<<20, 0, mapped, func(*Grid) error { return errCheck })
		if g != nil || !errors.Is(err, errCheck) {
			t.Errorf("mapped=%v: check refusal gave %v, %v", mapped, g, err)
		}
		if mmapfile.Live() != 0 {
			t.Errorf("mapped=%v: a grid the check refused left a live mapping", mapped)
		}
		g, err = ParseFile(openBytes(t, good), 1<<20, 0, mapped, func(g *Grid) error {
			if g.Width != 2 {
				return errCheck
			}
			return nil
		})
		if err != nil || g == nil || g.Raw(1, 0) != 0xFFFF {
			t.Errorf("mapped=%v: a grid the check accepts: %v", mapped, err)
		}
	}
	// The sample bound applies as in Parse.
	if _, err := ParseFile(openBytes(t, good), 1<<20, 11, true, nil); err == nil {
		t.Error("samples past maxBytes accepted")
	}
}

// TestParseFileReleasesMapping: an accepted grid holds its mapping while
// reachable and releases it once unreachable.
func TestParseFileReleasesMapping(t *testing.T) {
	if !mmapfile.Supported {
		t.Skip("no memory maps on this platform: ParseFile reads, nothing to release")
	}
	drainMappings(t)
	g, err := ParseFile(openBytes(t, syntheticGeoid()), 1<<20, 0, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if mmapfile.Live() != 1 {
		t.Fatalf("live mappings %d, want 1", mmapfile.Live())
	}
	for range 3 {
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
	if g.Raw(35, 18) != 1000+100*18+35 || mmapfile.Live() != 1 {
		t.Fatal("the mapping went while the grid was reachable")
	}
	g = nil
	_ = g
	drainMappings(t)
}

// FuzzParseFile: a file is accepted or refused by ParseFile, mapped or
// read, exactly as its bytes are by Parse, with the same error, and an
// accepted file gives the same grid.
func FuzzParseFile(f *testing.F) {
	f.Add(syntheticGeoid())
	f.Add(syntheticTile())
	f.Add(minimal())
	f.Add(minimal()[:20])
	f.Add([]byte("P5\n# Offset 0\n2 3\n65535\n"))
	f.Add([]byte{})
	const bound = 1 << 16
	f.Fuzz(func(t *testing.T, data []byte) {
		want, wantErr := Parse(data, bound)
		for _, mapped := range []bool{true, false} {
			g, err := ParseFile(openBytes(t, data), bound+1<<10, bound, mapped, nil)
			if (err == nil) != (wantErr == nil) {
				t.Fatalf("mapped=%v: ParseFile error %v, Parse error %v", mapped, err, wantErr)
			}
			if err != nil {
				if len(data) <= bound+1<<10 && err.Error() != wantErr.Error() {
					t.Fatalf("mapped=%v: %v, Parse says %v", mapped, err, wantErr)
				}
				continue
			}
			sameGrid(t, g, want)
		}
	})
}
