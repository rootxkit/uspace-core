// Package pgm parses the binary PGM (P5) container shared by the geoid
// grids (GeographicLib layout) and the terrain tiles written by the lab
// tooling: a magic line, comment lines of the form "# Key value" carrying
// Offset, Scale and the tile georeferencing, width and height, the maximum
// sample value, and big-endian 16-bit samples. It bounds what it will read
// and names the byte at which a file stops being a PGM (no panics on
// untrusted input). Owned by WP-2.
package pgm
