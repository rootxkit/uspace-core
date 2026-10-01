package pgm

import (
	"bytes"
	"testing"
)

// syntheticGeoid is the grid of terrain_geoid.json's header: 36 x 19 at
// 10 deg, Offset -100, Scale 0.01, sample(row, col) = 1000 + 100*row + col.
func syntheticGeoid() []byte {
	s := make([]uint16, 36*19)
	for r := 0; r < 19; r++ {
		for c := 0; c < 36; c++ {
			s[r*36+c] = uint16(1000 + 100*r + c)
		}
	}
	return Encode(36, 19, []HeaderLine{{"Description", "synthetic"}, {"Offset", "-100"}, {"Scale", "0.01"}}, s)
}

// syntheticTile is the tile of terrain_geoid.json's header.
func syntheticTile() []byte {
	s := make([]uint16, 25)
	for r := 0; r < 5; r++ {
		for c := 0; c < 5; c++ {
			s[r*5+c] = uint16((400 + 10*r + c + 500) * 5)
		}
	}
	s[6] = 0xFFFF
	return Encode(5, 5, []HeaderLine{
		{"Dataset", "COP-DEM GLO-30"}, {"Offset", "-500.0"}, {"Scale", "0.2"},
		{"LatFirst", "42.0"}, {"LonFirst", "44.0"}, {"LatStep", "0.25"}, {"LonStep", "0.25"},
	}, s)
}

// FuzzParsePGM: no panic, the size never exceeds the bound, every sample
// in range is readable, and an accepted grid re-encodes to a grid with the
// same size, header numbers and samples.
func FuzzParsePGM(f *testing.F) {
	f.Add(syntheticGeoid())
	f.Add(syntheticTile())
	f.Add(minimal())
	f.Add([]byte("P5\n#\n1 1\n65535\n\x00\x00"))
	f.Add([]byte("P5\n1 1\n255\n\x00"))
	const bound = 1 << 16
	f.Fuzz(func(t *testing.T, data []byte) {
		g, err := Parse(data, bound)
		if err != nil {
			if g != nil {
				t.Fatal("grid with error")
			}
			return
		}
		if g.Width*g.Height*2 > bound || g.Width <= 0 || g.Height <= 0 {
			t.Fatalf("size %d x %d past the bound", g.Width, g.Height)
		}
		samples := make([]uint16, 0, g.Width*g.Height)
		for iy := 0; iy < g.Height; iy++ {
			for ix := 0; ix < g.Width; ix++ {
				samples = append(samples, g.Raw(ix, iy))
			}
		}
		_ = g.Raw(g.Width, g.Height)
		header := make([]HeaderLine, 0, len(g.Header))
		for k, v := range g.Header {
			_, _ = g.Number(k)
			header = append(header, HeaderLine{k, v})
		}
		again, err := Parse(Encode(g.Width, g.Height, header, samples), bound)
		if err != nil {
			t.Fatalf("re-encoded grid refused: %v", err)
		}
		if !bytes.Equal(again.data, g.data) || again.Width != g.Width || again.Height != g.Height {
			t.Fatal("round trip changed the samples")
		}
		for k, v := range g.Header {
			if again.Header[k] != v {
				t.Fatalf("header %q: %q became %q", k, v, again.Header[k])
			}
		}
	})
}
