// Package mmapfile gives the bytes of an open file, mapped read-only into
// memory where the platform can and read into memory otherwise.
//
// On linux and darwin Map maps the file with mmap(PROT_READ, MAP_SHARED)
// through the standard library's syscall package (no cgo): the pages live
// in the kernel's page cache, shared by every process that maps the same
// file, read on first touch and never copied into the Go heap. On every
// other platform Map falls back to Read, which reads the file into memory
// as io.ReadAll does, and Mapping.Mapped reports false. Supported says
// which of the two this build does.
//
// Both refuse a file that is not a regular file or that is larger than
// the caller's bound, before anything is mapped or read; an empty file
// gives empty bytes (mmap cannot map zero bytes) and the caller's parser
// refuses it.
//
// The bytes of a mapped file are read-only (a write faults) and stay valid
// until Close. A mapped file must not be truncated or rewritten in place
// while mapped: the kernel then faults the reader with SIGBUS, which a Go
// program cannot recover from. Replace such a file by renaming a new one
// over it.
package mmapfile
