package terrain

import (
	"errors"
	"os"

	"github.com/rootxkit/uspace-core/core"
	"github.com/rootxkit/uspace-core/internal/pgm"
)

// MappedDirOpener returns a StoreOptions.OpenTile function that maps
// <dir>/<cell>.pgm read-only into memory where the platform can (linux and
// darwin) and reads it elsewhere, then checks it as ParseTile does. Like
// DirOpener it opens through an os.Root, so a cell name cannot lead out of
// dir, and refuses a file larger than maxBytes (zero or less: 64 MiB)
// before mapping it. Processes mapping the same tile share its pages in
// the page cache; a tile evicted from a Store is unmapped once no
// goroutine holds it. A tile file must not be truncated or rewritten in
// place while mapped (the process faults with SIGBUS): replace it by
// renaming a new file over it.
func MappedDirOpener(dir string, maxBytes int64) func(cell string) (*Tile, error) {
	if maxBytes <= 0 {
		maxBytes = 64 << 20
	}
	return func(cell string) (*Tile, error) {
		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, err
		}
		defer root.Close() //nolint:errcheck // read-only; a close error changes nothing read
		f, err := root.Open(cell + ".pgm")
		if err != nil {
			return nil, err
		}
		defer f.Close() //nolint:errcheck // read-only; the mapping does not need the file
		var tile *Tile
		if _, err := pgm.ParseFile(f, maxBytes, pgm.DefaultMaxBytes, true, func(g *pgm.Grid) (err error) {
			tile, err = tileFromGrid(g)
			return err
		}); err != nil {
			// Name the tile, as DirOpener does, where the error is about
			// the file rather than a byte in it.
			var fe *core.FieldError
			if errors.As(err, &fe) && fe.Field == "file" {
				return nil, core.Fieldf(cell+".pgm", "%s", fe.Reason)
			}
			return nil, err
		}
		return tile, nil
	}
}

// Mapped reports whether the tile's samples are a read-only memory map of
// its file (MappedDirOpener on linux or darwin) rather than bytes in
// memory.
func (t *Tile) Mapped() bool { return t.grid.Mapped() }
