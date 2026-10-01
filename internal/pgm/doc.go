// Package pgm parses the binary PGM (P5) container shared by the geoid
// grids (GeographicLib's layout, LESSONS D-08) and the DEM tiles the lab
// tooling writes:
//
//	P5
//	# Key value          zero or more: Offset, Scale, Description, Dataset, ...
//	width height
//	65535
//	width*height big-endian uint16 samples, row-major
//
// Parse refuses a wrong magic, a maximum value other than 65535, a size
// that is not positive or whose samples would exceed the caller's byte
// bound, truncated or trailing data and a comment line without a key. Each
// refusal is a *core.FieldError whose Field names the byte offset
// ("pgm[17]") of the line at fault; nothing panics on untrusted input.
// Raw reads a sample and returns 0 outside the grid; Number reads a header
// value as a finite number. Encode writes the same layout, for tests and
// tools.
package pgm
