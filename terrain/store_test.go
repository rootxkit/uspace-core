package terrain

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// fakeDisk is an Open function over in-memory tiles that counts reads.
type fakeDisk struct {
	mu    sync.Mutex
	files map[string][]byte
	reads map[string]int
	gate  chan struct{} // when set, every read waits on it
}

func newDisk() *fakeDisk {
	return &fakeDisk{files: map[string][]byte{}, reads: map[string]int{}}
}

func (d *fakeDisk) open(cell string) ([]byte, error) {
	d.mu.Lock()
	d.reads[cell]++
	data, ok := d.files[cell]
	gate := d.gate
	d.mu.Unlock()
	if gate != nil {
		<-gate
	}
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (d *fakeDisk) put(cell string, data []byte) {
	d.mu.Lock()
	d.files[cell] = data
	d.mu.Unlock()
}

func (d *fakeDisk) readCount(cell string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.reads[cell]
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func wantCounters(t *testing.T, s *Store, want map[string]uint64) {
	t.Helper()
	got := s.Counters().Snapshot()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("counters %v, want %v", got, want)
	}
}

var (
	tbilisi = core.LatLon{LatDeg: 41.7151, LonDeg: 44.8271}
	seaPos  = core.LatLon{LatDeg: 40.5, LonDeg: 44.5}
	nowhere = core.LatLon{LatDeg: 10.5, LonDeg: 10.5}
)

// TestStoreKnown reads what a tile says, with its dataset and spacing,
// and a second lookup comes from the cache (E-02: the success branch).
func TestStoreKnown(t *testing.T) {
	disk := newDisk()
	disk.put("N41E044", syntheticTileBytes())
	s := NewStore(Index{"N41E044": "COP-DEM GLO-30"}, StoreOptions{Open: disk.open})
	for i := 0; i < 3; i++ {
		e, err := s.Elevation(core.LatLon{LatDeg: 41.125, LonDeg: 44.875})
		if err != nil || e == nil {
			t.Fatalf("Elevation: %v %v", e, err)
		}
		if math.Abs(e.ElevationM-438.5) > 1e-9 || e.Dataset != "COP-DEM GLO-30" || math.Abs(e.SpacingM-27830) > 1e-9 {
			t.Errorf("got %+v", *e)
		}
	}
	if n := disk.readCount("N41E044"); n != 1 {
		t.Errorf("%d reads, want 1 (cached)", n)
	}
	wantCounters(t, s, map[string]uint64{CounterTilesLoaded: 1})
	if got := s.Cached(); !reflect.DeepEqual(got, []string{"N41E044"}) {
		t.Errorf("Cached %v", got)
	}
}

func TestStoreSea(t *testing.T) {
	disk := newDisk()
	s := NewStore(Index{"N40E044": SeaDataset}, StoreOptions{Open: disk.open})
	e, err := s.Elevation(seaPos)
	if err != nil || e == nil {
		t.Fatalf("sea: %v %v", e, err)
	}
	if *e != (Elevation{ElevationM: 0, Dataset: "sea", SpacingM: 0}) {
		t.Errorf("sea %+v", *e)
	}
	if disk.readCount("N40E044") != 0 {
		t.Error("a sea cell read a tile")
	}
	wantCounters(t, s, map[string]uint64{})
}

func TestStoreUnknownCell(t *testing.T) {
	disk := newDisk()
	disk.put("N10E010", cellTile(10, 10, 100)) // on disk but not in the index
	s := NewStore(Index{"N41E044": "COP-DEM GLO-30"}, StoreOptions{Open: disk.open})
	e, err := s.Elevation(nowhere)
	if err != nil || e != nil {
		t.Fatalf("unknown cell: %v %v, want nil, nil", e, err)
	}
	if disk.readCount("N10E010") != 0 {
		t.Error("a cell outside the index was read")
	}
	wantCounters(t, s, map[string]uint64{CounterUnknownCell: 1})
}

func TestStoreNoData(t *testing.T) {
	disk := newDisk()
	disk.put("N41E044", syntheticTileBytes())
	s := NewStore(Index{"N41E044": "COP-DEM GLO-30"}, StoreOptions{Open: disk.open})
	e, err := s.Elevation(core.LatLon{LatDeg: 41.875, LonDeg: 44.125})
	if err != nil || e != nil {
		t.Fatalf("nodata: %v %v, want nil, nil (never 0)", e, err)
	}
	wantCounters(t, s, map[string]uint64{CounterTilesLoaded: 1, CounterNoData: 1})
}

func TestStoreInvalidPosition(t *testing.T) {
	s := NewStore(Index{}, StoreOptions{})
	for _, c := range []struct {
		p     core.LatLon
		field string
	}{
		{core.LatLon{LatDeg: math.NaN()}, "lat_deg"},
		{core.LatLon{LatDeg: 91}, "lat_deg"},
		{core.LatLon{LonDeg: 180.5}, "lon_deg"},
		{core.LatLon{LonDeg: math.Inf(1)}, "lon_deg"},
	} {
		e, err := s.Elevation(c.p)
		var fe *core.FieldError
		if e != nil || !errors.As(err, &fe) || fe.Field != c.field {
			t.Errorf("%+v: %v %v, want field %s", c.p, e, err, c.field)
		}
	}
	wantCounters(t, s, map[string]uint64{CounterInvalidPosition: 4})
}

// TestStoreReadFailureRetry: a listed but missing tile is read once,
// answered unknown without reading until RetryAfter has passed, then read
// again; once the file appears it is served (D-04, E-02).
func TestStoreReadFailureRetry(t *testing.T) {
	disk := newDisk()
	clock := &fakeClock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	s := NewStore(Index{"N41E044": "COP-DEM GLO-30"}, StoreOptions{Open: disk.open, Now: clock.Now, RetryAfter: time.Minute})
	ask := func() *Elevation {
		t.Helper()
		e, err := s.Elevation(tbilisi)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	for i := 0; i < 5; i++ {
		if e := ask(); e != nil {
			t.Fatalf("missing tile answered %+v", *e)
		}
		clock.advance(10 * time.Second)
	}
	if n := disk.readCount("N41E044"); n != 1 {
		t.Fatalf("%d reads within RetryAfter, want 1", n)
	}
	wantCounters(t, s, map[string]uint64{CounterTileReadFailed: 1, CounterTileUnavailable: 5})

	clock.advance(10 * time.Second) // 60 s after the failure
	if e := ask(); e != nil {
		t.Fatal("still missing")
	}
	if n := disk.readCount("N41E044"); n != 2 {
		t.Fatalf("%d reads after RetryAfter, want 2", n)
	}
	wantCounters(t, s, map[string]uint64{CounterTileReadFailed: 2, CounterTileReadRetried: 1, CounterTileUnavailable: 6})

	// A malformed tile is a failure too.
	disk.put("N41E044", []byte("P5\nnot a tile\n"))
	clock.advance(time.Minute)
	if e := ask(); e != nil {
		t.Fatal("malformed tile answered")
	}
	wantCounters(t, s, map[string]uint64{CounterTileReadFailed: 3, CounterTileReadRetried: 2, CounterTileUnavailable: 7})

	// The operator installs the tile: served after the next RetryAfter.
	disk.put("N41E044", cellTile(41, 44, 500))
	clock.advance(59 * time.Second)
	if e := ask(); e != nil {
		t.Fatal("served before RetryAfter")
	}
	clock.advance(time.Second)
	e := ask()
	if e == nil || math.Abs(e.ElevationM-500) > 1e-9 || e.Dataset != "COP-DEM GLO-90" {
		t.Fatalf("after install: %+v", e)
	}
	wantCounters(t, s, map[string]uint64{
		CounterTileReadFailed: 3, CounterTileReadRetried: 3, CounterTileUnavailable: 8, CounterTilesLoaded: 1,
	})
}

func TestStoreNoOpen(t *testing.T) {
	s := NewStore(Index{"N41E044": "COP-DEM GLO-30"}, StoreOptions{})
	e, err := s.Elevation(tbilisi)
	if err != nil || e != nil {
		t.Fatalf("%v %v", e, err)
	}
	wantCounters(t, s, map[string]uint64{CounterTileReadFailed: 1, CounterTileUnavailable: 1})
	if s.opts.MaxTiles != DefaultMaxTiles || s.opts.RetryAfter != DefaultRetryAfter {
		t.Errorf("defaults %+v", s.opts)
	}
}

// TestStoreEviction exceeds MaxTiles (E-10): the least recently used
// tile goes, a recently used one stays, and an evicted tile is read again.
func TestStoreEviction(t *testing.T) {
	disk := newDisk()
	idx := Index{}
	for lat := 40; lat < 45; lat++ {
		cell := CellName(core.LatLon{LatDeg: float64(lat), LonDeg: 44})
		idx[cell] = "COP-DEM GLO-90"
		disk.put(cell, cellTile(lat, 44, float64(100*lat)))
	}
	s := NewStore(idx, StoreOptions{Open: disk.open, MaxTiles: 3})
	at := func(lat int) {
		t.Helper()
		e, err := s.Elevation(core.LatLon{LatDeg: float64(lat) + 0.5, LonDeg: 44.5})
		if err != nil || e == nil || e.ElevationM != float64(100*lat) {
			t.Fatalf("lat %d: %v %v", lat, e, err)
		}
	}
	at(40)
	at(41)
	at(42)
	at(40) // 40 is now the most recent; 41 the least
	at(43) // evicts 41
	if got, want := s.Cached(), []string{"N43E044", "N40E044", "N42E044"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Cached %v, want %v", got, want)
	}
	at(44) // evicts 42
	at(41) // read again, evicts 40
	if n := disk.readCount("N41E044"); n != 2 {
		t.Errorf("N41E044 read %d times, want 2", n)
	}
	if len(s.Cached()) != 3 {
		t.Errorf("cache holds %d tiles, bound 3", len(s.Cached()))
	}
	wantCounters(t, s, map[string]uint64{CounterTilesLoaded: 6, CounterTilesEvicted: 3})
}

// TestStoreLockNotHeldAcrossOpen: while one goroutine is blocked inside
// Open for one tile, a cached lookup of another answers (B-06).
func TestStoreLockNotHeldAcrossOpen(t *testing.T) {
	disk := newDisk()
	disk.put("N41E044", cellTile(41, 44, 300))
	disk.put("N42E044", cellTile(42, 44, 400))
	s := NewStore(Index{"N41E044": "a", "N42E044": "b"}, StoreOptions{Open: disk.open})
	if e, _ := s.Elevation(tbilisi); e == nil {
		t.Fatal("warm-up")
	}
	gate := make(chan struct{})
	disk.mu.Lock()
	disk.gate = gate
	disk.mu.Unlock()
	done := make(chan *Elevation)
	go func() {
		e, _ := s.Elevation(core.LatLon{LatDeg: 42.5, LonDeg: 44.5})
		done <- e
	}()
	for disk.readCount("N42E044") == 0 {
		time.Sleep(time.Millisecond)
	}
	// The other goroutine is inside Open now. A cached lookup must answer.
	answered := make(chan *Elevation)
	go func() {
		e, _ := s.Elevation(tbilisi)
		answered <- e
	}()
	select {
	case e := <-answered:
		if e == nil || e.ElevationM != 300 {
			t.Errorf("cached lookup %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a cached lookup waited behind another tile's read")
	}
	close(gate)
	if e := <-done; e == nil || e.ElevationM != 400 {
		t.Errorf("blocked read %+v", e)
	}
}

// TestStoreConcurrent drives Elevation from many goroutines under -race;
// duplicate reads of one tile are allowed and counted.
func TestStoreConcurrent(t *testing.T) {
	disk := newDisk()
	idx := Index{"N40E044": SeaDataset}
	for lat := 41; lat < 45; lat++ {
		cell := CellName(core.LatLon{LatDeg: float64(lat), LonDeg: 44})
		idx[cell] = "COP-DEM GLO-90"
		disk.put(cell, cellTile(lat, 44, float64(10*lat)))
	}
	s := NewStore(idx, StoreOptions{Open: disk.open, MaxTiles: 2})
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				lat := 39 + (g+i)%7 // 39 unknown, 40 sea, 41-44 tiles, 45 unknown
				e, err := s.Elevation(core.LatLon{LatDeg: float64(lat) + 0.3, LonDeg: 44.7})
				if err != nil {
					errs <- err
					return
				}
				var want *float64
				switch {
				case lat == 40:
					want = new(float64)
				case lat >= 41 && lat <= 44:
					v := float64(10 * lat)
					want = &v
				}
				if (e == nil) != (want == nil) || (e != nil && e.ElevationM != *want) {
					errs <- fmt.Errorf("lat %d: got %+v, want %v", lat, e, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	c := s.Counters()
	if c.Get(CounterUnknownCell) == 0 || c.Get(CounterTilesLoaded) < 4 {
		t.Errorf("counters %v", c.Snapshot())
	}
	if l, e := c.Get(CounterTilesLoaded), c.Get(CounterTilesEvicted); l-e != uint64(len(s.Cached())) || len(s.Cached()) > 2 {
		t.Errorf("loaded %d - evicted %d != cached %d (bound 2)", l, e, len(s.Cached()))
	}
}

// TestStoreDuplicateRead: two goroutines missing the same tile at once
// both read it; one copy is kept and the second counted.
func TestStoreDuplicateRead(t *testing.T) {
	disk := newDisk()
	disk.put("N41E044", cellTile(41, 44, 250))
	gate := make(chan struct{})
	disk.gate = gate
	s := NewStore(Index{"N41E044": "a"}, StoreOptions{Open: disk.open})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e, err := s.Elevation(tbilisi); err != nil || e == nil || e.ElevationM != 250 {
				t.Errorf("%v %v", e, err)
			}
		}()
	}
	for disk.readCount("N41E044") < 2 {
		time.Sleep(time.Millisecond)
	}
	close(gate)
	wg.Wait()
	wantCounters(t, s, map[string]uint64{CounterTilesLoaded: 1, CounterTileReadDup: 1})
	if len(s.Cached()) != 1 {
		t.Errorf("cached %v", s.Cached())
	}
}

func TestParseIndex(t *testing.T) {
	flat := `{"N41E044": "COP-DEM GLO-30", "N40E044": "sea", "S01W001": "COP-DEM GLO-90"}`
	wrapped := `{"bbox": [39.9, 41.0, 46.8, 43.6], "fetched_at": "2026-09-01T00:00:00Z", "cells": ` + flat + `}`
	for name, doc := range map[string]string{"flat": flat, "wrapped": wrapped} {
		idx, err := ParseIndex([]byte(doc))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := Index{"N41E044": "COP-DEM GLO-30", "N40E044": "sea", "S01W001": "COP-DEM GLO-90"}
		if !reflect.DeepEqual(idx, want) {
			t.Errorf("%s: %v", name, idx)
		}
	}
	if idx, err := ParseIndex([]byte(`{}`)); err != nil || len(idx) != 0 {
		t.Errorf("empty index: %v %v", idx, err)
	}
	big := strings.Builder{}
	big.WriteString("{")
	n := 0
	for lat := -90; lat <= 90 && n <= MaxIndexCells; lat++ {
		for lon := -180; lon <= 180 && n <= MaxIndexCells; lon++ {
			if n > 0 {
				big.WriteString(",")
			}
			fmt.Fprintf(&big, "%q:\"s\"", CellName(core.LatLon{LatDeg: float64(lat) + 0.5*b2f(lat < 90), LonDeg: float64(lon) + 0.5*b2f(lon < 180)}))
			n++
		}
	}
	big.WriteString("}")
	for _, c := range []struct {
		name, doc, field string
	}{
		{"not json", `[`, "index"},
		{"array", `["N41E044"]`, "index"},
		{"too large", strings.Repeat(" ", MaxIndexBytes+1), "index"},
		{"bad cell name", `{"bbox": "x"}`, "bbox"},
		{"lower case", `{"n41e044": "a"}`, "n41e044"},
		{"lat past 90", `{"N91E044": "a"}`, "N91E044"},
		{"lon past 180", `{"N41E181": "a"}`, "N41E181"},
		{"letter digit", `{"N4xE044": "a"}`, "N4xE044"},
		{"number value", `{"N41E044": 1}`, "N41E044"},
		{"empty value", `{"N41E044": ""}`, "N41E044"},
		{"long value", `{"N41E044": "` + strings.Repeat("d", 200) + `"}`, "N41E044"},
		{"cells not object", `{"cells": [1]}`, "cells"},
		{"cells null", `{"cells": null}`, "cells"},
		{"bad cell in cells", `{"cells": {"X": "a"}}`, "cells.X"},
		{"long bad key", `{"` + strings.Repeat("k", 100) + `": "a"}`, "kkkk"},
		{"too many cells", big.String(), "index"},
	} {
		idx, err := ParseIndex([]byte(c.doc))
		var fe *core.FieldError
		if idx != nil || !errors.As(err, &fe) || !strings.HasPrefix(fe.Field, c.field) {
			t.Errorf("%s: %v %v, want field %s", c.name, idx, err, c.field)
		}
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func TestDirOpener(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "N41E044.pgm"), cellTile(41, 44, 700), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewStore(Index{"N41E044": "COP-DEM GLO-90"}, StoreOptions{Open: DirOpener(dir, 0)})
	e, err := s.Elevation(tbilisi)
	if err != nil || e == nil || e.ElevationM != 700 {
		t.Fatalf("%v %v", e, err)
	}
	if _, err := DirOpener(dir, 10)("N41E044"); err == nil {
		t.Error("a file over the bound was read")
	}
	if _, err := DirOpener(dir, 0)("N00E000"); err == nil {
		t.Error("a missing file was read")
	}
	if _, err := DirOpener(dir, 0)("../outside"); err == nil {
		t.Error("a path outside the directory was read")
	}
	if _, err := DirOpener(filepath.Join(dir, "absent"), 0)("N41E044"); err == nil {
		t.Error("a missing directory was read")
	}
	if err := os.Mkdir(filepath.Join(dir, "N42E044.pgm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := DirOpener(dir, 0)("N42E044"); err == nil {
		t.Error("a directory was read as a tile")
	}
}

func TestAttribution(t *testing.T) {
	for _, part := range []string{"Copernicus WorldDEM-30", "DLR e.V.", "Airbus Defence and Space", "European Union and ESA"} {
		if !strings.Contains(Attribution, part) {
			t.Errorf("Attribution lacks %q", part)
		}
	}
}

// Store and *Store satisfy the interfaces zones takes.
var _ Ground = (*Store)(nil)
