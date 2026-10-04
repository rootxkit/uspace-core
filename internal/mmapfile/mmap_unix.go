//go:build linux || darwin

package mmapfile

import (
	"os"
	"syscall"
)

// Supported reports whether Map maps files on this platform (linux and
// darwin) rather than reading them.
const Supported = true

// supported is Supported, switched off only by tests of the fallback.
var supported = Supported

func mmap(f *os.File, size int) ([]byte, error) {
	data, err := syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, &os.PathError{Op: "mmap", Path: f.Name(), Err: err}
	}
	return data, nil
}

func unmap(data []byte) error {
	return syscall.Munmap(data)
}
