package pgm

import (
	"bytes"
	"math"
	"strconv"
	"strings"

	"github.com/rootxkit/uspace-core/core"
)

// DefaultMaxBytes bounds the sample data Parse accepts when the caller
// passes no bound: 512 MiB holds the largest GeographicLib grid
// (egm2008-1, 21600 x 10801 samples, 467 MB) and any terrain tile.
const DefaultMaxBytes = 512 << 20

// MaxVal is the only maximum sample value accepted: 16-bit samples.
const MaxVal = 0xFFFF

// Grid is a parsed binary PGM: its size, its "# Key value" comment lines
// and its big-endian 16-bit samples, row-major from the top-left corner.
// The samples are not copied: Grid keeps a reference to the slice given
// to Parse, which the caller must not modify afterwards. A Grid is
// immutable and safe for concurrent use.
type Grid struct {
	Width, Height int
	MaxVal        int
	// Header holds the comment lines "# Key value": the first word after
	// "#" is the key, the rest of the line (trimmed) the value. A key
	// repeated later in the file replaces the earlier value.
	Header map[string]string
	data   []byte
}

// field names the byte at offset in an error.
func field(offset int) string {
	return "pgm[" + strconv.Itoa(offset) + "]"
}

// Parse reads a binary PGM (P5) with 16-bit samples: the magic line
// "P5", zero or more comment lines "# Key value", the line "width height",
// the line "65535", then exactly width*height*2 bytes. maxBytes bounds the
// sample data (width*height*2); zero or less means DefaultMaxBytes. Every
// refusal is a *core.FieldError whose Field is "pgm[<byte offset>]" of the
// line or byte at which the file stops being such a grid.
func Parse(data []byte, maxBytes int) (*Grid, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	pos := 0
	// line returns the next line without its "\n" (and a trailing "\r"),
	// its start offset, and whether a newline ended it.
	line := func() (string, int, bool) {
		start := pos
		end := bytes.IndexByte(data[pos:], '\n')
		if end < 0 {
			return "", start, false
		}
		text := data[pos : pos+end]
		pos += end + 1
		return string(bytes.TrimRight(text, "\r")), start, true
	}

	magic, at, ok := line()
	if !ok {
		return nil, core.Fieldf(field(at), "header ends before the data")
	}
	if strings.TrimSpace(magic) != "P5" {
		return nil, core.Fieldf(field(at), "not a binary PGM: magic %q, want \"P5\"", truncate(magic))
	}

	header := make(map[string]string)
	for {
		text, at, ok := line()
		if !ok {
			return nil, core.Fieldf(field(at), "header ends before the data")
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if !strings.HasPrefix(text, "#") {
			w, h, err := parseSize(text, at, maxBytes)
			if err != nil {
				return nil, err
			}
			maxLine, atMax, ok := line()
			if !ok {
				return nil, core.Fieldf(field(atMax), "header ends before the maximum value")
			}
			mv, err := strconv.Atoi(strings.TrimSpace(maxLine))
			if err != nil {
				return nil, core.Fieldf(field(atMax), "maximum value %q is not an integer", truncate(maxLine))
			}
			if mv != MaxVal {
				return nil, core.Fieldf(field(atMax), "maximum value %d, want %d (16-bit samples)", mv, MaxVal)
			}
			want := w * h * 2
			got := len(data) - pos
			if got < want {
				return nil, core.Fieldf(field(len(data)), "data truncated: %d bytes of samples, want %d", got, want)
			}
			if got > want {
				return nil, core.Fieldf(field(pos+want), "%d bytes after the %d bytes of samples", got-want, want)
			}
			return &Grid{Width: w, Height: h, MaxVal: mv, Header: header, data: data[pos:]}, nil
		}
		words := strings.Fields(text[1:])
		if len(words) == 0 {
			return nil, core.Fieldf(field(at), "comment line without a key")
		}
		value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text[1:]), words[0]))
		header[words[0]] = value
	}
}

// parseSize reads "width height" and bounds it by maxBytes without
// overflowing.
func parseSize(text string, at, maxBytes int) (int, int, error) {
	words := strings.Fields(text)
	if len(words) != 2 {
		return 0, 0, core.Fieldf(field(at), "size line %q is not \"width height\"", truncate(text))
	}
	w, err := strconv.Atoi(words[0])
	if err != nil {
		return 0, 0, core.Fieldf(field(at), "width %q is not an integer", truncate(words[0]))
	}
	h, err := strconv.Atoi(words[1])
	if err != nil {
		return 0, 0, core.Fieldf(field(at), "height %q is not an integer", truncate(words[1]))
	}
	if w <= 0 || h <= 0 {
		return 0, 0, core.Fieldf(field(at), "size %d x %d is not positive", w, h)
	}
	// w*h*2 > maxBytes, written so that it cannot overflow.
	if w > maxBytes/2/h {
		return 0, 0, core.Fieldf(field(at), "size %d x %d exceeds the bound of %d bytes", w, h, maxBytes)
	}
	return w, h, nil
}

// truncate keeps an echoed input short in an error message.
func truncate(s string) string {
	const n = 32
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Raw returns the stored sample at column ix, row iy (row 0 is the first
// row in the file). Outside [0, Width) x [0, Height) it returns 0 and
// never panics; callers clamp their indices first.
func (g *Grid) Raw(ix, iy int) uint16 {
	if g == nil || ix < 0 || iy < 0 || ix >= g.Width || iy >= g.Height {
		return 0
	}
	at := 2 * (iy*g.Width + ix)
	if at+1 >= len(g.data) {
		return 0
	}
	return uint16(g.data[at])<<8 | uint16(g.data[at+1])
}

// Number returns the header value under key read as a finite number: the
// first word of the value, so "# Offset -108 m" reads -108. The error is
// a *core.FieldError naming "header.<key>".
func (g *Grid) Number(key string) (float64, error) {
	name := "header." + key
	value, ok := g.Header[key]
	if !ok {
		return 0, core.Fieldf(name, "missing")
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return 0, core.Fieldf(name, "empty, want a number")
	}
	x, err := strconv.ParseFloat(words[0], 64)
	if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
		return 0, core.Fieldf(name, "%q is not a finite number", truncate(words[0]))
	}
	return x, nil
}

// HeaderLine is one "# Key value" comment line for Encode.
type HeaderLine struct{ Key, Value string }

// Encode writes a grid in the layout Parse reads: "P5", the header lines
// in order, "width height", "65535" and the samples big-endian. samples
// holds width*height values row-major; missing values are written as 0
// and extra values are ignored. It is used by tests and tools that build
// grids; width and height must be positive.
func Encode(width, height int, header []HeaderLine, samples []uint16) []byte {
	var b bytes.Buffer
	b.WriteString("P5\n")
	for _, h := range header {
		b.WriteString("# " + h.Key + " " + h.Value + "\n")
	}
	b.WriteString(strconv.Itoa(width) + " " + strconv.Itoa(height) + "\n65535\n")
	n := width * height
	if n < 0 {
		n = 0
	}
	out := make([]byte, b.Len(), b.Len()+2*n)
	copy(out, b.Bytes())
	for i := 0; i < n; i++ {
		var v uint16
		if i < len(samples) {
			v = samples[i]
		}
		out = append(out, byte(v>>8), byte(v))
	}
	return out
}
