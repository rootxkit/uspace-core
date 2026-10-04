package terrain

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/mmapfile"
)

// drainMappings waits until the mappings of earlier tests are released.
func drainMappings(t testing.TB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for mmapfile.Live() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("%d mappings still live ten seconds after their tiles became unreachable", mmapfile.Live())
		}
		runtime.GC()
		time.Sleep(time.Millisecond)
	}
}

func tileDir(t testing.TB, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for cell, data := range files {
		if err := os.WriteFile(filepath.Join(dir, cell+".pgm"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// sameElevation fails unless a and b give the same answer, bit for bit,
// at p (both unknown, or both the same float).
func sameElevation(t *testing.T, p core.LatLon, a, b *float64) {
	t.Helper()
	if (a == nil) != (b == nil) || (a != nil && math.Float64bits(*a) != math.Float64bits(*b)) {
		t.Fatalf("%+v: %v parsed, %v mapped", p, deref(a), deref(b))
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestMappedDirOpenerAgreesWithParseTile(t *testing.T) {
	want := syntheticTile(t)
	dir := tileDir(t, map[string][]byte{"N41E044": syntheticTileBytes()})
	got, err := MappedDirOpener(dir, 0)("N41E044")
	if err != nil {
		t.Fatal(err)
	}
	n, unknown := 0, 0
	for lat := 40.9; lat <= 42.1; lat += 0.0137 {
		for lon := 43.9; lon <= 45.1; lon += 0.0119 {
			p := core.LatLon{LatDeg: lat, LonDeg: lon}
			a, b := want.ElevationM(p), got.ElevationM(p)
			sameElevation(t, p, a, b)
			n++
			if a == nil {
				unknown++
			}
		}
	}
	// Both branches ran: known elevations and the nodata corner (E-01).
	if unknown == 0 || unknown == n {
		t.Fatalf("%d of %d positions unknown: the comparison missed a branch", unknown, n)
	}
	sameElevation(t, core.LatLon{LatDeg: math.NaN()}, want.ElevationM(core.LatLon{LatDeg: math.NaN()}), got.ElevationM(core.LatLon{LatDeg: math.NaN()}))
	if got.Dataset() != want.Dataset() || got.SpacingM() != want.SpacingM() {
		t.Errorf("dataset %q spacing %v, ParseTile's %q %v", got.Dataset(), got.SpacingM(), want.Dataset(), want.SpacingM())
	}
	if got.Mapped() != mmapfile.Supported {
		t.Errorf("Mapped() = %v, mmapfile.Supported = %v", got.Mapped(), mmapfile.Supported)
	}
	if want.Mapped() {
		t.Error("ParseTile reports a mapping")
	}
}

// TestMappedDirOpenerRefusals: what ParseTile refuses is refused with the
// same error; the file bound, a missing file or directory, a directory as
// a tile and a cell name leading out of dir are refused as DirOpener
// refuses them; none leaves a mapping behind once collected.
func TestMappedDirOpenerRefusals(t *testing.T) {
	good := syntheticTileBytes()
	badOffset := []byte(strings.Replace(string(good), "# Offset -500.0", "# Offset -400.0", 1))
	noDataset := []byte(strings.Replace(string(good), "# Dataset COP-DEM GLO-30", "# Other x", 1))
	parsed := map[string][]byte{
		"N01E001": good[:len(good)-1],                   // truncated
		"N02E002": append(append([]byte{}, good...), 0), // trailing byte
		"N03E003": badOffset,
		"N04E004": noDataset,
		"N05E005": {}, // empty
	}
	dir := tileDir(t, parsed)
	open := MappedDirOpener(dir, 0)
	drainMappings(t)
	for cell, data := range parsed {
		_, want := ParseTile(data)
		if want == nil {
			t.Fatalf("%s: ParseTile accepted it", cell)
		}
		tile, err := open(cell)
		if err == nil || tile != nil {
			t.Fatalf("%s: accepted", cell)
		}
		if err.Error() != want.Error() {
			t.Errorf("%s: %v, ParseTile says %v", cell, err, want)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "N41E044.pgm"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := MappedDirOpener(dir, int64(len(good)-1))("N41E044")
	var fe *core.FieldError
	if !errors.As(err, &fe) || fe.Field != "N41E044.pgm" || !strings.Contains(fe.Reason, "larger than") {
		t.Errorf("past the bound: %v", err)
	}
	if _, err := MappedDirOpener(dir, int64(len(good)))("N41E044"); err != nil {
		t.Errorf("at the bound: %v", err)
	}
	if _, err := open("N00E000"); err == nil {
		t.Error("a missing file was read")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.pgm"), good, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := open("../outside"); err == nil {
		t.Error("a path outside the directory was read")
	}
	if _, err := MappedDirOpener(filepath.Join(dir, "absent"), 0)("N41E044"); err == nil {
		t.Error("a missing directory was read")
	}
	if err := os.Mkdir(filepath.Join(dir, "N42E044.pgm"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err = open("N42E044")
	if !errors.As(err, &fe) || fe.Field != "N42E044.pgm" || !strings.Contains(fe.Reason, "not a regular file") {
		t.Errorf("a directory as a tile: %v", err)
	}
	drainMappings(t)
}

// TestStoreOpenTile: a Store reading mapped tiles answers what a Store
// reading bytes answers, counts the same, and OpenTile wins over Open.
func TestStoreOpenTile(t *testing.T) {
	idx := Index{}
	files := map[string][]byte{"N41E044": syntheticTileBytes()}
	idx["N41E044"] = "COP-DEM GLO-30"
	for lat := 42; lat < 45; lat++ {
		cell := CellName(core.LatLon{LatDeg: float64(lat), LonDeg: 44})
		idx[cell] = "COP-DEM GLO-90"
		files[cell] = cellTile(lat, 44, float64(100*lat))
	}
	idx["N40E044"] = SeaDataset
	idx["N39E044"] = "COP-DEM GLO-90" // indexed, no file
	dir := tileDir(t, files)
	refuse := func(string) ([]byte, error) { return nil, errors.New("the store called Open with OpenTile set") }
	read := NewStore(idx, StoreOptions{Open: DirOpener(dir, 0)})
	mapped := NewStore(idx, StoreOptions{Open: refuse, OpenTile: MappedDirOpener(dir, 0)})
	for _, p := range []core.LatLon{
		{LatDeg: 41.125, LonDeg: 44.875}, {LatDeg: 41.75, LonDeg: 44.25}, // known, nodata corner
		{LatDeg: 42.5, LonDeg: 44.5}, {LatDeg: 43.01, LonDeg: 44.99}, {LatDeg: 44.5, LonDeg: 44.1},
		seaPos, nowhere, {LatDeg: 39.5, LonDeg: 44.5}, // sea, not indexed, unreadable
	} {
		a, errA := read.Elevation(p)
		b, errB := mapped.Elevation(p)
		if errA != nil || errB != nil || (a == nil) != (b == nil) || (a != nil && *a != *b) {
			t.Fatalf("%+v: %v %v read, %v %v mapped", p, a, errA, b, errB)
		}
	}
	if got, want := mapped.Counters().Snapshot(), read.Counters().Snapshot(); len(got) == 0 || !mapsEqual(got, want) {
		t.Errorf("counters %v mapped, %v read", got, want)
	}
	wantCounters(t, mapped, map[string]uint64{
		CounterTilesLoaded: 4, CounterNoData: 1, CounterUnknownCell: 1,
		CounterTileReadFailed: 1, CounterTileUnavailable: 1,
	})
}

func mapsEqual(a, b map[string]uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// TestStoreOpenTileFailures: an OpenTile error, and a nil tile without an
// error, are tile_read_failed and leave the answer unknown, retried after
// RetryAfter like an Open failure; the retry that succeeds answers.
func TestStoreOpenTileFailures(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	var mu sync.Mutex
	mode := "error"
	openTile := func(string) (*Tile, error) {
		mu.Lock()
		defer mu.Unlock()
		switch mode {
		case "error":
			return nil, os.ErrPermission
		case "nil":
			return nil, nil
		default:
			return ParseTile(cellTile(41, 44, 250))
		}
	}
	s := NewStore(Index{"N41E044": "COP-DEM GLO-90"}, StoreOptions{OpenTile: openTile, Now: clock.Now})
	if e, err := s.Elevation(tbilisi); e != nil || err != nil {
		t.Fatalf("error: %v %v", e, err)
	}
	clock.advance(DefaultRetryAfter)
	mu.Lock()
	mode = "nil"
	mu.Unlock()
	if e, err := s.Elevation(tbilisi); e != nil || err != nil {
		t.Fatalf("nil tile: %v %v", e, err)
	}
	wantCounters(t, s, map[string]uint64{
		CounterTileReadFailed: 2, CounterTileUnavailable: 2, CounterTileReadRetried: 1,
	})
	clock.advance(DefaultRetryAfter)
	mu.Lock()
	mode = "ok"
	mu.Unlock()
	if e, err := s.Elevation(tbilisi); err != nil || e == nil || e.ElevationM != 250 {
		t.Fatalf("after the retry: %v %v", e, err)
	}
	wantCounters(t, s, map[string]uint64{
		CounterTileReadFailed: 2, CounterTileUnavailable: 2, CounterTileReadRetried: 2, CounterTilesLoaded: 1,
	})
}

// TestStoreEvictsMappedTiles: past MaxTiles mapped tiles are evicted while
// goroutines read them (-race), every answer stays right, and every
// mapping is released once the Store is gone (E-10).
func TestStoreEvictsMappedTiles(t *testing.T) {
	idx := Index{}
	files := map[string][]byte{}
	for lat := 30; lat < 42; lat++ {
		cell := CellName(core.LatLon{LatDeg: float64(lat), LonDeg: 44})
		idx[cell] = "COP-DEM GLO-90"
		files[cell] = cellTile(lat, 44, float64(10*lat))
	}
	dir := tileDir(t, files)
	drainMappings(t)
	s := NewStore(idx, StoreOptions{OpenTile: MappedDirOpener(dir, 0), MaxTiles: 2})
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				lat := 30 + (i*7+g)%12
				e, err := s.Elevation(core.LatLon{LatDeg: float64(lat) + 0.5, LonDeg: 44.5})
				if err != nil || e == nil || e.ElevationM != float64(10*lat) {
					errs <- "wrong answer"
					return
				}
				if i%50 == 0 {
					runtime.GC()
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	c := s.Counters().Snapshot()
	if c[CounterTilesEvicted] == 0 || len(s.Cached()) > 2 {
		t.Fatalf("no eviction past MaxTiles 2: %v, cached %v", c, s.Cached())
	}
	if mmapfile.Supported && mmapfile.Live() < 1 {
		t.Errorf("no live mapping while the store holds tiles")
	}
	s = nil
	_ = s
	drainMappings(t)
}
