package geoid

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/mmapfile"
	"github.com/rootxkit/uspace-core/internal/pgm"
)

func writeGrid(t testing.TB, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grid.pgm")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// drainMappings waits until the mappings of earlier tests are released.
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

// lattice calls fn at every sample position of a w x h grid, at the
// midpoints between them, and at a stepDeg lattice over the globe with
// longitudes past +-180 and both poles.
func lattice(w, h int, stepDeg float64, fn func(core.LatLon)) {
	for row := 0; row < h; row++ {
		lat := 90 - 180*float64(row)/float64(h-1)
		for col := 0; col <= w; col++ {
			lon := 360 * float64(col) / float64(w)
			fn(core.LatLon{LatDeg: lat, LonDeg: lon})
			if row < h-1 {
				fn(core.LatLon{LatDeg: lat - 90/float64(h-1), LonDeg: lon + 180/float64(w)})
			}
		}
	}
	for lat := -90.0; lat <= 90; lat += stepDeg {
		for lon := -540.0; lon <= 540; lon += stepDeg * 1.7 {
			fn(core.LatLon{LatDeg: lat, LonDeg: lon})
		}
	}
	fn(core.LatLon{LatDeg: 90, LonDeg: 12})
	fn(core.LatLon{LatDeg: -90, LonDeg: -12})
}

// sameUndulations fails unless a and b give bit-identical N at every
// position of lattice, and returns how many positions it compared.
func sameUndulations(t *testing.T, a, b *Grid, w, h int, stepDeg float64) int {
	t.Helper()
	n := 0
	lattice(w, h, stepDeg, func(p core.LatLon) {
		na, errA := a.UndulationM(p)
		nb, errB := b.UndulationM(p)
		if errA != nil || errB != nil {
			t.Fatalf("%+v: %v / %v", p, errA, errB)
		}
		if math.Float64bits(na) != math.Float64bits(nb) {
			t.Fatalf("%+v: N %v loaded, %v mapped", p, na, nb)
		}
		n++
	})
	return n
}

func TestLoadMappedAgreesWithLoad(t *testing.T) {
	path := writeGrid(t, synthetic())
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := LoadMapped(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := sameUndulations(t, loaded, mapped, 36, 19, 0.37); n < 100000 {
		t.Fatalf("only %d positions compared", n)
	}
	if mapped.Description() != loaded.Description() || mapped.Description() == "" {
		t.Errorf("Description %q, Load's %q", mapped.Description(), loaded.Description())
	}
	if mapped.Mapped() != mmapfile.Supported {
		t.Errorf("Mapped() = %v, mmapfile.Supported = %v", mapped.Mapped(), mmapfile.Supported)
	}
	if loaded.Mapped() {
		t.Error("Load reports a mapping")
	}
	if mustParse(t, synthetic()).Mapped() {
		t.Error("Parse reports a mapping")
	}
	// A refused position is refused the same way.
	_, errL := loaded.UndulationM(core.LatLon{LatDeg: 91})
	_, errM := mapped.UndulationM(core.LatLon{LatDeg: 91})
	if errL == nil || errM == nil || errL.Error() != errM.Error() {
		t.Errorf("lat 91: %v loaded, %v mapped", errL, errM)
	}
}

// TestLoadMappedRefusals: every file Load refuses, LoadMapped refuses with
// the same *core.FieldError, and no refused file leaves a mapping behind
// once collected.
func TestLoadMappedRefusals(t *testing.T) {
	good := synthetic()
	ok := []pgm.HeaderLine{{Key: "Offset", Value: "0"}, {Key: "Scale", Value: "1"}}
	hdr := func(lines ...pgm.HeaderLine) []byte { return pgm.Encode(2, 3, lines, nil) }
	cases := map[string][]byte{
		"truncated":      good[:len(good)-1],
		"trailing":       append(append([]byte{}, good...), 0, 0),
		"header only":    []byte("P5\n"),
		"empty":          {},
		"no Scale":       hdr(pgm.HeaderLine{Key: "Offset", Value: "0"}),
		"zero Scale":     hdr(pgm.HeaderLine{Key: "Offset", Value: "0"}, pgm.HeaderLine{Key: "Scale", Value: "0"}),
		"no Offset":      hdr(pgm.HeaderLine{Key: "Scale", Value: "1"}),
		"odd columns":    pgm.Encode(3, 3, ok, nil),
		"even rows":      pgm.Encode(2, 4, ok, nil),
		"negative size":  []byte("P5\n# Offset 0\n# Scale 1\n-2 3\n65535\n"),
		"8-bit samples":  []byte("P5\n# Offset 0\n# Scale 1\n2 3\n255\n\x00\x00\x00\x00\x00\x00"),
		"size past file": []byte("P5\n# Offset 0\n# Scale 1\n200000 100001\n65535\n\x00\x00"),
	}
	drainMappings(t)
	for name, data := range cases {
		path := writeGrid(t, data)
		_, loadErr := Load(path)
		if loadErr == nil {
			t.Fatalf("%s: Load accepted it", name)
		}
		g, err := LoadMapped(path)
		if err == nil || g != nil {
			t.Fatalf("%s: LoadMapped accepted it", name)
		}
		if err.Error() != loadErr.Error() {
			t.Errorf("%s: %v, Load says %v", name, err, loadErr)
		}
		var fe *core.FieldError
		if !errors.As(err, &fe) {
			t.Errorf("%s: %T is not a *core.FieldError", name, err)
		}
	}
	drainMappings(t)
	dir := t.TempDir()
	if _, err := LoadMapped(filepath.Join(dir, "absent.pgm")); err == nil {
		t.Error("a missing file loaded")
	}
	if _, err := LoadMapped(dir); err == nil {
		t.Error("a directory loaded")
	}
	// The accepted twin of the refusals above (E-01).
	if _, err := LoadMapped(writeGrid(t, good)); err != nil {
		t.Errorf("the synthetic grid was refused: %v", err)
	}
}

// heapGrowth returns how much the live heap grew while load ran, with the
// loaded grid kept reachable until the measurement.
func heapGrowth(t *testing.T, load func() (*Grid, error)) int64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	g, err := load()
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	runtime.KeepAlive(g)
	return int64(after.HeapAlloc) - int64(before.HeapAlloc)
}

// TestLoadMappedKeepsTheGridOutOfTheHeap: loading a 4 MB grid through Load
// grows the heap by about its size (the presence twin, which proves the
// measurement), through LoadMapped by less than 1 MiB.
func TestLoadMappedKeepsTheGridOutOfTheHeap(t *testing.T) {
	const w, h = 2000, 1001
	samples := make([]uint16, w*h)
	for i := range samples {
		samples[i] = uint16(i * 31)
	}
	path := writeGrid(t, pgm.Encode(w, h, []pgm.HeaderLine{{Key: "Offset", Value: "-108"}, {Key: "Scale", Value: "0.003"}}, samples))
	const sampleBytes = 2 * w * h
	// Three quarters, not all: garbage of earlier tests collected during
	// the measurement lowers the difference a little.
	if got := heapGrowth(t, func() (*Grid, error) { return Load(path) }); got < sampleBytes*3/4 {
		t.Fatalf("Load grew the heap by %d bytes, less than 3/4 of the %d bytes of samples: the measurement is broken", got, sampleBytes)
	}
	if !mmapfile.Supported {
		t.Skip("no memory maps on this platform: LoadMapped reads into the heap as Load does")
	}
	if got := heapGrowth(t, func() (*Grid, error) { return LoadMapped(path) }); got >= 1<<20 {
		t.Fatalf("LoadMapped grew the heap by %d bytes, want under 1 MiB for %d bytes of samples", got, sampleBytes)
	}
}

// TestRealGridsMapped compares LoadMapped with Load on GeographicLib's
// grids, bit for bit, at every sample, every cell centre and a lattice of
// the globe, and checks that the mapped grid stays out of the heap.
func TestRealGridsMapped(t *testing.T) {
	dir := os.Getenv("USPACE_GEOID_DIR")
	if dir == "" {
		t.Skip("needs egm2008-2_5.pgm and egm96-15.pgm in USPACE_GEOID_DIR")
	}
	for _, c := range []struct {
		name string
		w, h int
	}{{"egm2008-2_5", 8640, 4321}, {"egm96-15", 1440, 721}} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name+".pgm")
			loaded, err := Load(path)
			if err != nil {
				t.Skipf("needs %s.pgm: %v", c.name, err)
			}
			mapped, err := LoadMapped(path)
			if err != nil {
				t.Fatal(err)
			}
			n := sameUndulations(t, loaded, mapped, c.w, c.h, 0.5)
			t.Logf("%s: %d positions bit-identical, mapped %v", c.name, n, mapped.Mapped())
			if !mmapfile.Supported {
				return
			}
			loaded = nil
			if got := heapGrowth(t, func() (*Grid, error) { return LoadMapped(path) }); got >= 1<<20 {
				t.Errorf("LoadMapped grew the heap by %d bytes", got)
			}
			runtime.KeepAlive(loaded)
		})
	}
}

// FuzzLoadMapped: a file is accepted or refused by LoadMapped exactly as
// its bytes are by Parse, with the same error, and an accepted grid
// answers bit for bit what Parse's answers.
func FuzzLoadMapped(f *testing.F) {
	f.Add(synthetic())
	f.Add(synthetic()[:40])
	f.Add(pgm.Encode(2, 3, []pgm.HeaderLine{{Key: "Offset", Value: "-108"}, {Key: "Scale", Value: "0.003"}}, []uint16{0, 1, 2, 3, 0xFFFF, 5}))
	f.Add([]byte("P5\n# Offset 0\n# Scale 1\n2 3\n65535\n"))
	f.Add([]byte("P5\n# Offset 0\n# Scale 1\n2 3\n65535\n\x00\x00\x00\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		want, wantErr := Parse(data)
		g, err := LoadMapped(writeGrid(t, data))
		if (err == nil) != (wantErr == nil) {
			t.Fatalf("LoadMapped error %v, Parse error %v", err, wantErr)
		}
		if err != nil {
			if err.Error() != wantErr.Error() {
				t.Fatalf("%v, Parse says %v", err, wantErr)
			}
			return
		}
		for _, p := range []core.LatLon{
			{LatDeg: 90}, {LatDeg: -90}, {LatDeg: 0, LonDeg: 359.999}, {LatDeg: 12.3, LonDeg: -45.6},
			{LatDeg: -89.99, LonDeg: 1e10}, {LatDeg: 45, LonDeg: -1e-300}, {LatDeg: 1e-9, LonDeg: 180},
		} {
			a, errA := want.UndulationM(p)
			b, errB := g.UndulationM(p)
			if errA != nil || errB != nil || math.Float64bits(a) != math.Float64bits(b) {
				t.Fatalf("%+v: %v (%v) parsed, %v (%v) mapped", p, a, errA, b, errB)
			}
		}
		if g.Description() != want.Description() {
			t.Fatalf("Description %q, Parse's %q", g.Description(), want.Description())
		}
	})
}
