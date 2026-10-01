package terrain

import (
	"container/list"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/rootxkit/uspace-core/core"
)

// Ground gives the ground elevation at a position: (nil, nil) when it is
// unknown. *Store implements it; zones takes it.
type Ground interface {
	Elevation(p core.LatLon) (*Elevation, error)
}

// Elevation is a known ground elevation, with the dataset it came from
// and the sample spacing, which a caller shows beside the number
// (LESSONS D-05). At sea Dataset is SeaDataset and SpacingM is 0.
type Elevation struct {
	ElevationM float64
	Dataset    string
	SpacingM   float64
}

// Index maps a cell name ("N41E044") to the dataset its tile holds, or to
// SeaDataset for a cell with no tile, where the elevation is 0 m. A cell
// absent from the index was never fetched: its elevation is unknown.
type Index map[string]string

// Bounds of ParseIndex: index.json is a few kilobytes for a country and at
// most one entry per 1 x 1 degree cell of the globe.
const (
	MaxIndexBytes = 16 << 20
	MaxIndexCells = 360 * 180
	maxNameBytes  = 128
)

// ParseIndex reads index.json. Both shapes are accepted: the flat map
// {"N41E044": "COP-DEM GLO-30", "N40E044": "sea"} and the lab tool's file
// {"bbox": [...], "fetched_at": "...", "cells": {<flat map>}}, where only
// "cells" is read. Every key must be a cell name as CellName writes it
// and every value a non-empty string. Errors are *core.FieldError naming
// the entry ("cells.N41E044").
func ParseIndex(data []byte) (Index, error) {
	if len(data) > MaxIndexBytes {
		return nil, core.Fieldf("index", "%d bytes, more than %d", len(data), MaxIndexBytes)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, core.Fieldf("index", "not a JSON object: %v", err)
	}
	prefix := ""
	entries := top
	if cells, ok := top["cells"]; ok {
		prefix = "cells."
		entries = nil
		if err := json.Unmarshal(cells, &entries); err != nil || entries == nil {
			return nil, core.Fieldf("cells", "not a JSON object of cell names")
		}
	}
	if len(entries) > MaxIndexCells {
		return nil, core.Fieldf(prefix+"index", "%d cells, more than %d", len(entries), MaxIndexCells)
	}
	idx := make(Index, len(entries))
	for name, raw := range entries {
		f := prefix + name
		if !validCellName(name) {
			return nil, core.Fieldf(truncate(f), "not a cell name like N41E044")
		}
		var ds string
		if err := json.Unmarshal(raw, &ds); err != nil {
			return nil, core.Fieldf(f, "not a string")
		}
		if ds == "" || len(ds) > maxNameBytes {
			return nil, core.Fieldf(f, "dataset name empty or longer than %d bytes", maxNameBytes)
		}
		idx[name] = ds
	}
	return idx, nil
}

// validCellName reports whether s is [NS]dd[EW]ddd within the globe.
func validCellName(s string) bool {
	if len(s) != 7 || (s[0] != 'N' && s[0] != 'S') || (s[3] != 'E' && s[3] != 'W') {
		return false
	}
	for _, i := range []int{1, 2, 4, 5, 6} {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	lat, _ := strconv.Atoi(s[1:3])
	lon, _ := strconv.Atoi(s[4:7])
	return lat <= 90 && lon <= 180
}

func truncate(s string) string {
	const n = 40
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Counter names of Store.Counters.
const (
	CounterTilesLoaded     = "tiles_loaded"      // a tile read, parsed and cached
	CounterTilesEvicted    = "tiles_evicted"     // a tile dropped past MaxTiles
	CounterTileReadFailed  = "tile_read_failed"  // Open or ParseTile failed
	CounterTileReadRetried = "tile_read_retried" // a failed tile read again after RetryAfter
	CounterTileReadDup     = "tile_read_duplicate"
	CounterTileUnavailable = "tile_unavailable" // an answer left unknown because its tile could not be read
	CounterUnknownCell     = "unknown_cell"     // a position whose cell is not in the index
	CounterNoData          = "nodata"           // a NoData sample among the four corners
	CounterInvalidPosition = "invalid_position" // a position refused before lookup
)

// StoreOptions configures a Store. Zero values take the defaults.
type StoreOptions struct {
	// MaxTiles bounds the tiles held in memory (least recently used out
	// first; a GLO-30 tile is about 26 MB). Default 16.
	MaxTiles int
	// RetryAfter is how long a tile that could not be read is answered as
	// unknown before it is read again (LESSONS D-04: once a minute, not
	// once per message). Default 1 minute.
	RetryAfter time.Duration
	// Open returns the bytes of the tile for a cell name ("N41E044").
	// DirOpener reads <dir>/<cell>.pgm. With no Open every tile read
	// fails, counted.
	Open func(cell string) ([]byte, error)
	// Now is the clock for RetryAfter. Default time.Now.
	Now func() time.Time
}

// Defaults of StoreOptions.
const (
	DefaultMaxTiles   = 16
	DefaultRetryAfter = time.Minute
)

// DirOpener returns an Open function reading <dir>/<cell>.pgm through an
// os.Root, so a cell name cannot lead the read out of dir, and refusing a
// file larger than maxBytes (zero or less: 64 MiB).
func DirOpener(dir string, maxBytes int64) func(cell string) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	return func(cell string) ([]byte, error) {
		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}
		defer root.Close() //nolint:errcheck // read-only; a close error changes nothing read
		f, err := root.Open(cell + ".pgm")
		if err != nil {
			return nil, err
		}
		defer f.Close() //nolint:errcheck // read-only; a close error changes nothing read
		data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) > maxBytes {
			return nil, core.Fieldf(cell+".pgm", "larger than %d bytes", maxBytes)
		}
		return data, nil
	}
}

// Store answers ground elevation from the tiles of an Index, read on
// first use through StoreOptions.Open and kept in a bounded LRU cache. The
// cache lock is never held across Open or ParseTile (LESSONS B-06): a
// cached lookup never waits behind another tile's read, and two goroutines
// missing the same tile may both read it (counted tile_read_duplicate).
// A Store is safe for concurrent use.
type Store struct {
	index    Index
	opts     StoreOptions
	counters core.Counters

	mu     sync.Mutex
	lru    *list.List               // of *cached, most recent at the front
	tiles  map[string]*list.Element // cell -> element of lru
	failed map[string]time.Time     // cell -> when its read failed
}

type cached struct {
	cell string
	tile *Tile
}

// NewStore returns a Store over index. The index is not copied; the caller
// must not modify it afterwards.
func NewStore(index Index, opts StoreOptions) *Store {
	if opts.MaxTiles <= 0 {
		opts.MaxTiles = DefaultMaxTiles
	}
	if opts.RetryAfter <= 0 {
		opts.RetryAfter = DefaultRetryAfter
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Store{
		index:  index,
		opts:   opts,
		lru:    list.New(),
		tiles:  make(map[string]*list.Element),
		failed: make(map[string]time.Time),
	}
}

// Counters returns the store's counters: tiles_loaded, tiles_evicted,
// tile_read_failed, tile_read_retried, tile_read_duplicate,
// tile_unavailable, unknown_cell, nodata, invalid_position.
func (s *Store) Counters() *core.Counters { return &s.counters }

// Elevation returns the ground elevation at p, or (nil, nil) when it is
// unknown: the cell is not in the index (unknown_cell), a sample around p
// is NoData (nodata), or the cell's tile cannot be read (tile_unavailable;
// the read is not tried again before RetryAfter). A sea cell is 0 m with
// dataset "sea" and reads no tile. An invalid position is an error naming
// lat_deg or lon_deg.
func (s *Store) Elevation(p core.LatLon) (*Elevation, error) {
	if !p.Valid() {
		s.counters.Inc(CounterInvalidPosition)
		if !core.IsFinite(p.LatDeg) || p.LatDeg < -90 || p.LatDeg > 90 {
			return nil, core.Fieldf("lat_deg", "%v is outside [-90, 90]", p.LatDeg)
		}
		return nil, core.Fieldf("lon_deg", "%v is outside [-180, 180]", p.LonDeg)
	}
	cell := CellName(p)
	dataset, ok := s.index[cell]
	if !ok {
		s.counters.Inc(CounterUnknownCell)
		return nil, nil
	}
	if dataset == SeaDataset {
		return &Elevation{ElevationM: 0, Dataset: SeaDataset, SpacingM: 0}, nil
	}
	t := s.tile(cell)
	if t == nil {
		s.counters.Inc(CounterTileUnavailable)
		return nil, nil
	}
	e := t.ElevationM(p)
	if e == nil {
		s.counters.Inc(CounterNoData)
		return nil, nil
	}
	return &Elevation{ElevationM: *e, Dataset: t.Dataset(), SpacingM: t.SpacingM()}, nil
}

// tile returns the cell's tile, reading it when it is not cached and has
// not failed within RetryAfter; nil when it cannot be had.
func (s *Store) tile(cell string) *Tile {
	s.mu.Lock()
	if el, ok := s.tiles[cell]; ok {
		s.lru.MoveToFront(el)
		t := el.Value.(*cached).tile
		s.mu.Unlock()
		return t
	}
	retry := false
	if at, ok := s.failed[cell]; ok {
		if s.opts.Now().Sub(at) < s.opts.RetryAfter {
			s.mu.Unlock()
			return nil
		}
		delete(s.failed, cell)
		retry = true
	}
	s.mu.Unlock()

	// No lock from here to the insert: the read may take a while.
	if retry {
		s.counters.Inc(CounterTileReadRetried)
	}
	t, err := s.read(cell)
	if err != nil {
		s.counters.Inc(CounterTileReadFailed)
		s.mu.Lock()
		s.failed[cell] = s.opts.Now()
		s.mu.Unlock()
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failed, cell)
	if el, ok := s.tiles[cell]; ok {
		s.counters.Inc(CounterTileReadDup)
		s.lru.MoveToFront(el)
		return el.Value.(*cached).tile
	}
	s.tiles[cell] = s.lru.PushFront(&cached{cell: cell, tile: t})
	s.counters.Inc(CounterTilesLoaded)
	for s.lru.Len() > s.opts.MaxTiles {
		old := s.lru.Back()
		s.lru.Remove(old)
		delete(s.tiles, old.Value.(*cached).cell)
		s.counters.Inc(CounterTilesEvicted)
	}
	return t
}

func (s *Store) read(cell string) (*Tile, error) {
	if s.opts.Open == nil {
		return nil, core.Fieldf("open", "no Open function configured")
	}
	data, err := s.opts.Open(cell)
	if err != nil {
		return nil, err
	}
	return ParseTile(data)
}

// Cached returns the cell names held in memory, most recently used first.
func (s *Store) Cached() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, s.lru.Len())
	for el := s.lru.Front(); el != nil; el = el.Next() {
		out = append(out, el.Value.(*cached).cell)
	}
	return out
}
