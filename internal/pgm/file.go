package pgm

import (
	"os"
	"runtime"

	"github.com/rootxkit/uspace-core/internal/mmapfile"
)

// ParseFile parses the grid in the open file f. With mapped the file is
// mapped read-only (mmapfile.Map, which reads it where the platform has no
// memory maps); without, it is read into memory (mmapfile.Read). The file
// is refused past maxFileBytes before it is mapped or read, then parsed by
// Parse with maxBytes, so a truncated file, trailing bytes or a bad header
// give exactly Parse's *core.FieldError. check, when not nil, is the
// caller's own test of the parsed grid (a geoid's Scale, a tile's
// georeferencing); its error is returned as is. On any error the mapping
// is released before ParseFile returns. A mapped grid releases its mapping
// once the grid is unreachable; Raw keeps the grid reachable across each
// read, so the bytes are never released under a reader. The caller may
// close f once ParseFile returns.
func ParseFile(f *os.File, maxFileBytes int64, maxBytes int, mapped bool, check func(*Grid) error) (*Grid, error) {
	var (
		m   *mmapfile.Mapping
		err error
	)
	if mapped {
		m, err = mmapfile.Map(f, maxFileBytes)
	} else {
		m, err = mmapfile.Read(f, maxFileBytes)
	}
	if err != nil {
		return nil, err
	}
	g, err := Parse(m.Bytes(), maxBytes)
	if err == nil && check != nil {
		err = check(g)
	}
	if err != nil {
		_ = m.Close()
		return nil, err
	}
	if m.Mapped() {
		g.mapped = true
		runtime.AddCleanup(g, func(m *mmapfile.Mapping) { _ = m.Close() }, m)
	}
	return g, nil
}

// Mapped reports whether the grid's samples are a read-only memory map of
// its file (ParseFile on linux or darwin) rather than bytes in memory.
func (g *Grid) Mapped() bool { return g != nil && g.mapped }
