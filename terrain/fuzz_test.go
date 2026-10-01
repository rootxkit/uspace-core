package terrain

import (
	"math"
	"testing"

	"github.com/rootxkit/uspace-core/core"
)

// FuzzParseTile: no panic on any file, and an accepted tile answers any
// position with nil or a finite elevation inside the stored range.
func FuzzParseTile(f *testing.F) {
	f.Add(syntheticTileBytes())
	f.Add(cellTile(41, 44, 512.4))
	f.Add([]byte("P5\n# Dataset d\n# Offset -500\n# Scale 0.2\n# LatFirst 1\n# LonFirst 1\n# LatStep 1e-300\n# LonStep 1e300\n2 2\n65535\n\x00\x00\x00\x00\xff\xff\x00\x01"))
	f.Fuzz(func(t *testing.T, data []byte) {
		tile, err := ParseTile(data)
		if err != nil {
			return
		}
		for _, p := range []core.LatLon{
			{LatDeg: 41.5, LonDeg: 44.5}, {LatDeg: 90, LonDeg: 180}, {LatDeg: -90, LonDeg: -180},
			{LatDeg: 1e308, LonDeg: -1e308}, {LatDeg: math.NaN(), LonDeg: 0},
		} {
			e := tile.ElevationM(p)
			if e == nil {
				continue
			}
			if math.IsNaN(*e) || *e < OffsetM-1e-9 || *e > OffsetM+ScaleM*(NoData-1)+1e-9 {
				t.Fatalf("%+v: elevation %v outside the stored range", p, *e)
			}
		}
		_ = tile.SpacingM()
		if tile.Dataset() == "" {
			t.Fatal("accepted a tile without a dataset")
		}
	})
}

// FuzzParseIndex: no panic, and every accepted entry is a cell name that
// CellName can produce, with a non-empty dataset.
func FuzzParseIndex(f *testing.F) {
	f.Add([]byte(`{"N41E044": "COP-DEM GLO-30", "N40E044": "sea"}`))
	f.Add([]byte(`{"bbox": [39.9, 41.0, 46.8, 43.6], "fetched_at": "x", "cells": {"S01W001": "sea"}}`))
	f.Add([]byte(`{"cells": null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		idx, err := ParseIndex(data)
		if err != nil {
			return
		}
		if len(idx) > MaxIndexCells {
			t.Fatalf("%d cells past the bound", len(idx))
		}
		for cell, ds := range idx {
			if !validCellName(cell) || ds == "" {
				t.Fatalf("accepted %q: %q", cell, ds)
			}
		}
	})
}
