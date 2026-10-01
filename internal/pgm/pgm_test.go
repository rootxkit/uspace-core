package pgm

import (
	"errors"
	"strings"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

func minimal() []byte {
	return Encode(2, 3, []HeaderLine{{"Offset", "-108"}, {"Scale", "0.003"}, {"Description", "WGS84 EGM2008, 2.5-minute grid"}},
		[]uint16{0x0102, 0xFFFF, 3, 4, 5, 0x8000})
}

func TestParseAcceptsMinimalGrid(t *testing.T) {
	g, err := Parse(minimal(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if g.Width != 2 || g.Height != 3 || g.MaxVal != 65535 {
		t.Fatalf("size %d x %d maxval %d", g.Width, g.Height, g.MaxVal)
	}
	if got := g.Header["Description"]; got != "WGS84 EGM2008, 2.5-minute grid" {
		t.Errorf("Description %q", got)
	}
	want := [][]uint16{{0x0102, 0xFFFF}, {3, 4}, {5, 0x8000}}
	for iy, row := range want {
		for ix, v := range row {
			if got := g.Raw(ix, iy); got != v {
				t.Errorf("Raw(%d, %d) = %#x, want %#x", ix, iy, got, v)
			}
		}
	}
	off, err := g.Number("Offset")
	if err != nil || off != -108 {
		t.Errorf("Offset %v %v", off, err)
	}
	sc, err := g.Number("Scale")
	if err != nil || sc != 0.003 {
		t.Errorf("Scale %v %v", sc, err)
	}
}

func TestParseAcceptsHeaderVariants(t *testing.T) {
	// CRLF line ends, blank lines, a comment without a space after "#",
	// a key with no value, a number followed by a unit, and a repeated key.
	data := []byte("P5\r\n\n#Offset -500 m\r\n# Empty\n# Scale 1\n# Scale 0.2\n  \n3 1\r\n65535\n\x00\x01\x00\x02\x00\x03")
	g, err := Parse(data, 6)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := g.Number("Offset"); err != nil || v != -500 {
		t.Errorf("Offset %v %v", v, err)
	}
	if v, err := g.Number("Scale"); err != nil || v != 0.2 {
		t.Errorf("Scale %v %v (a later line replaces an earlier one)", v, err)
	}
	if v, ok := g.Header["Empty"]; !ok || v != "" {
		t.Errorf("Empty %q %v", v, ok)
	}
	if g.Raw(2, 0) != 3 {
		t.Errorf("Raw(2,0) = %d", g.Raw(2, 0))
	}
}

func TestRawOutOfRangeIsZero(t *testing.T) {
	g, err := Parse(minimal(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]int{{-1, 0}, {0, -1}, {2, 0}, {0, 3}, {1 << 40, 1 << 40}} {
		if got := g.Raw(c[0], c[1]); got != 0 {
			t.Errorf("Raw(%d, %d) = %d, want 0", c[0], c[1], got)
		}
	}
	// In range it is not zero (E-01 twin).
	if g.Raw(1, 0) != 0xFFFF {
		t.Errorf("Raw(1, 0) = %#x", g.Raw(1, 0))
	}
	var nilGrid *Grid
	if nilGrid.Raw(0, 0) != 0 {
		t.Error("nil grid")
	}
	short := &Grid{Width: 4, Height: 4, data: []byte{1, 2}}
	if short.Raw(3, 3) != 0 {
		t.Error("a grid shorter than its size must read 0")
	}
}

func TestNumberRefusals(t *testing.T) {
	g, err := Parse(Encode(1, 1, []HeaderLine{{"Text", "abc"}, {"Inf", "+Inf"}, {"NaN", "NaN"}, {"Blank", ""}, {"Ok", "1.5e3"}}, []uint16{0}), 0)
	if err != nil {
		t.Fatal(err)
	}
	for key, phrase := range map[string]string{
		"Missing": "missing", "Text": "not a finite number", "Inf": "not a finite number",
		"NaN": "not a finite number", "Blank": "empty",
	} {
		_, err := g.Number(key)
		var fe *core.FieldError
		if !errors.As(err, &fe) || fe.Field != "header."+key || !strings.Contains(fe.Reason, phrase) {
			t.Errorf("Number(%q) = %v, want header.%s: %s", key, err, key, phrase)
		}
	}
	if v, err := g.Number("Ok"); err != nil || v != 1500 {
		t.Errorf("Ok %v %v", v, err)
	}
}

// TestParseRefusals pairs every refused form with the accepted grid
// above (E-01) and checks the byte offset the error names.
func TestParseRefusals(t *testing.T) {
	good := string(minimal())
	_, rest, _ := strings.Cut(good, "\n2 3\n")
	body := "2 3\n" + rest
	cases := []struct {
		name, data string
		maxBytes   int
		field      string
		phrase     string
	}{
		{"empty", "", 0, "pgm[0]", "header ends"},
		{"no newline after magic", "P5", 0, "pgm[0]", "header ends"},
		{"ascii pgm", "P2\n2 3\n65535\n", 0, "pgm[0]", "not a binary PGM"},
		{"pbm", "P4\n" + body, 0, "pgm[0]", "not a binary PGM"},
		{"comment without key", "P5\n# Offset 1\n#   \n" + body, 0, "pgm[14]", "without a key"},
		{"bare hash", "P5\n#\n" + body, 0, "pgm[3]", "without a key"},
		{"header never ends", "P5\n# Offset 1\n", 0, "pgm[14]", "header ends"},
		{"size line one word", "P5\n7\n65535\n", 0, "pgm[3]", "not \"width height\""},
		{"size line three words", "P5\n1 2 3\n65535\n", 0, "pgm[3]", "not \"width height\""},
		{"width not integer", "P5\nx 2\n65535\n", 0, "pgm[3]", "width"},
		{"height not integer", "P5\n2 1.5\n65535\n", 0, "pgm[3]", "height"},
		{"zero width", "P5\n0 2\n65535\n", 0, "pgm[3]", "not positive"},
		{"negative height", "P5\n2 -1\n65535\n", 0, "pgm[3]", "not positive"},
		{"absurd size", "P5\n100000 100000\n65535\n", 0, "pgm[3]", "exceeds the bound"},
		{"overflowing size", "P5\n9223372036854775807 9223372036854775807\n65535\n", 0, "pgm[3]", "exceeds the bound"},
		{"past maxBytes", good, 11, "pgm[", "exceeds the bound"},
		{"no maxval line", "P5\n2 3\n", 0, "pgm[7]", "before the maximum value"},
		{"maxval 255", "P5\n1 1\n255\n\x00", 0, "pgm[7]", "maximum value 255"},
		{"maxval text", "P5\n1 1\nmax\n\x00\x00", 0, "pgm[7]", "not an integer"},
		{"truncated data", good[:len(good)-1], 0, "pgm[" + itoa(len(good)-1) + "]", "truncated"},
		{"trailing data", good + "\x00", 0, "pgm[" + itoa(len(good)) + "]", "after the"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := Parse([]byte(c.data), c.maxBytes)
			if g != nil {
				t.Fatalf("accepted: %+v", g)
			}
			var fe *core.FieldError
			if !errors.As(err, &fe) {
				t.Fatalf("error %T %v is not a *core.FieldError", err, err)
			}
			if !strings.HasPrefix(fe.Field, c.field) || !strings.Contains(fe.Reason, c.phrase) {
				t.Errorf("got %q, want field %s and reason containing %q", err, c.field, c.phrase)
			}
		})
	}
	// The bound is inclusive: exactly maxBytes of samples is accepted.
	if _, err := Parse(minimal(), 12); err != nil {
		t.Errorf("12 bytes of samples within a bound of 12: %v", err)
	}
}

func TestTruncateLongEcho(t *testing.T) {
	_, err := Parse([]byte("P5\n"+strings.Repeat("x", 100)+" 1\n65535\n"), 0)
	if err == nil || len(err.Error()) > 120 {
		t.Errorf("error %q should echo a truncated width", err)
	}
}

func TestEncodePadsAndTrims(t *testing.T) {
	g, err := Parse(Encode(2, 1, nil, []uint16{7}), 0)
	if err != nil {
		t.Fatal(err)
	}
	if g.Raw(0, 0) != 7 || g.Raw(1, 0) != 0 {
		t.Errorf("padding: %d %d", g.Raw(0, 0), g.Raw(1, 0))
	}
	g, err = Parse(Encode(1, 1, nil, []uint16{7, 8, 9}), 0)
	if err != nil || g.Raw(0, 0) != 7 {
		t.Errorf("trim: %v", err)
	}
	if out := Encode(-1, 1, nil, nil); !strings.HasSuffix(string(out), "65535\n") {
		t.Errorf("negative size wrote samples: %q", out)
	}
}

func itoa(n int) string { return field(n)[4 : len(field(n))-1] }
