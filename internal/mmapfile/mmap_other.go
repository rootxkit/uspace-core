//go:build !(linux || darwin)

package mmapfile

import (
	"errors"
	"os"
)

// Supported reports whether Map maps files on this platform. It is false
// here: Map reads the file into memory (Read).
const Supported = false

// supported is Supported, switched off only by tests of the fallback.
var supported = Supported

var errUnsupported = errors.New("mmapfile: memory maps are not supported on this platform")

func mmap(*os.File, int) ([]byte, error) { return nil, errUnsupported }

func unmap([]byte) error { return errUnsupported }
