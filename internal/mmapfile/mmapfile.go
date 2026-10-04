package mmapfile

import (
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"

	"github.com/rootxkit/uspace-core/core"
)

// Mapping is the bytes of a file: mapped read-only when Mapped, read into
// memory otherwise. Close releases a mapping; the bytes must not be read
// after it. A Mapping is safe for concurrent reads.
type Mapping struct {
	data   []byte
	mapped bool
	once   sync.Once
	err    error
}

// beforeMap, when set by a test, runs between the size check and the map.
var beforeMap func()

// live counts the mappings not yet closed.
var live atomic.Int64

// Live returns the number of mappings made by Map and not yet closed, so
// that tests can check a mapping is released.
func Live() int64 { return live.Load() }

// Bytes returns the file's bytes. For a mapped file they are read-only.
func (m *Mapping) Bytes() []byte { return m.data }

// Mapped reports whether the bytes are a memory map of the file (true) or
// a copy read into memory (false).
func (m *Mapping) Mapped() bool { return m.mapped }

// Close unmaps a mapped file; for a copy it only drops the bytes. A second
// Close does nothing and returns the first one's result.
func (m *Mapping) Close() error {
	m.once.Do(func() {
		if m.mapped {
			m.err = unmap(m.data)
			live.Add(-1)
		}
		m.data = nil
	})
	return m.err
}

// Map returns the bytes of f, mapped read-only where Supported and read
// otherwise (Read). It refuses a file that is not regular or that is
// larger than maxBytes (a *core.FieldError on "file") before mapping. The
// caller may close f once Map returns; the mapping does not need it.
func Map(f *os.File, maxBytes int64) (*Mapping, error) {
	if !supported {
		return Read(f, maxBytes)
	}
	size, err := regularSize(f, maxBytes)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return &Mapping{data: []byte{}}, nil
	}
	if beforeMap != nil {
		beforeMap()
	}
	data, err := mmap(f, int(size))
	if err != nil {
		return nil, err
	}
	live.Add(1)
	m := &Mapping{data: data, mapped: true}
	// A file truncated between the size check and the map is mapped past
	// its end, and the first read there faults the process (SIGBUS).
	// Check the size again now that the map exists. This does not cover a
	// truncation after Map returns (doc.go).
	if fi, err := f.Stat(); err != nil || fi.Size() != size {
		_ = m.Close()
		if err != nil {
			return nil, err
		}
		return nil, core.Fieldf("file", "changed from %d to %d bytes while it was mapped", size, fi.Size())
	}
	return m, nil
}

// Read returns the bytes of f read into memory, as io.ReadAll does: the
// fallback of Map, and what a platform without mmap always gets. It
// refuses what Map refuses, and a file that grows past maxBytes while it
// is read.
func Read(f *os.File, maxBytes int64) (*Mapping, error) {
	if _, err := regularSize(f, maxBytes); err != nil {
		return nil, err
	}
	data, err := readLimited(f, maxBytes)
	if err != nil {
		return nil, err
	}
	return &Mapping{data: data}, nil
}

// readLimited reads f to its end, refusing more than maxBytes bytes.
func readLimited(f *os.File, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, tooLarge(maxBytes)
	}
	return data, nil
}

// regularSize returns the size of f when it is a regular file of at most
// maxBytes bytes (and at most math.MaxInt, the largest slice).
func regularSize(f *os.File, maxBytes int64) (int64, error) {
	fi, err := f.Stat()
	if err != nil {
		return 0, err
	}
	if !fi.Mode().IsRegular() {
		return 0, core.Fieldf("file", "%s is not a regular file", fi.Mode().Type())
	}
	maxBytes = min(maxBytes, math.MaxInt)
	if fi.Size() > maxBytes {
		return 0, tooLarge(maxBytes)
	}
	return fi.Size(), nil
}

func tooLarge(maxBytes int64) error {
	return core.Fieldf("file", "larger than %d bytes", maxBytes)
}
