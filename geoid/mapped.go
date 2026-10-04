package geoid

import (
	"os"
	"path/filepath"

	"github.com/rootxkit/uspace-core/internal/pgm"
)

// LoadMapped reads the geoid grid file at path like Load, but maps it
// read-only into memory instead of copying it into the heap, where the
// platform can (linux and darwin): every process mapping the same file
// shares its pages in the kernel's page cache, and a page is read from
// disk on first touch. Elsewhere it reads the file as Load does; Mapped
// says which. The grid answers exactly what Load's answers, bit for bit.
//
// The file is refused past the bound Load applies before it is mapped,
// and then parsed and checked as Parse does: a truncated file, trailing
// bytes and a bad header are the same *core.FieldError, and a refused
// file is unmapped before the error returns. An accepted grid's mapping
// is released once the Grid is unreachable; there is no Close.
//
// The file must not be truncated or rewritten in place while mapped (the
// process would fault with SIGBUS): install a new grid by renaming it
// over the old one.
func LoadMapped(path string) (*Grid, error) {
	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // G703: the path is the caller's configured grid file; opening it is the purpose
	if err != nil {
		return nil, err
	}
	defer f.Close() //nolint:errcheck // read-only file; the mapping does not need it
	var grid *Grid
	if _, err := pgm.ParseFile(f, maxFileBytes, pgm.DefaultMaxBytes, true, func(g *pgm.Grid) (err error) {
		grid, err = fromPGM(g)
		return err
	}); err != nil {
		return nil, err
	}
	return grid, nil
}

// Mapped reports whether the grid's samples are a read-only memory map of
// its file (LoadMapped on linux or darwin) rather than bytes in memory.
func (g *Grid) Mapped() bool { return g.grid.Mapped() }
