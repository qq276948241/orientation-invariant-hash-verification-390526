// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"fmt"
	"image"
	"image/color"
	"testing"
)

// uniqueROImage builds an image with a distinct color at every pixel so a
// single mismatched cell is locatable in an equivalence proof.
func uniqueROImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*17 + y*31 + 3) % 256),
				G: uint8((x*7 + y*13 + 11) % 256),
				B: uint8((x*23 + y*5 + 19) % 256),
				A: 255,
			})
		}
	}
	return img
}

func roCanvasSig(c *roSquareCanvas) string {
	s := ""
	for y := 0; y < c.side; y++ {
		for x := 0; x < c.side; x++ {
			col := c.atRGBA64(x, y)
			s += fmt.Sprintf("%04x%04x%04x%04x", col.R, col.G, col.B, col.A)
		}
	}
	return s
}

// TestROCanvasD4Equivariance proves the normalization commutes with D4 at the
// pixel level: for every group element g, the multiset of the eight candidate
// canvases of g(source) equals that of source. This is the geometric basis of
// the orientation invariant hash and must hold for odd, single-row and extreme
// aspect ratios, not just squares.
func TestROCanvasD4Equivariance(t *testing.T) {
	geoms := [][2]int{
		{5, 3}, {3, 5}, {4, 1}, {1, 4}, {7, 7}, {2, 6},
		{6, 2}, {3, 3}, {1, 1}, {8, 3}, {3, 8}, {2, 2},
	}
	bg := color.RGBA{33, 66, 99, 255}
	for _, d := range geoms {
		w, h := d[0], d[1]
		base := uniqueROImage(w, h)
		reference := map[string]int{}
		for tr := roTransform(0); tr < roNumTransforms; tr++ {
			reference[roCanvasSig(newROSquareCanvas(base, tr, bg))]++
		}
		for g := roTransform(1); g < roNumTransforms; g++ {
			rotated := transformRectangle(base, g)
			got := map[string]int{}
			for tr := roTransform(0); tr < roNumTransforms; tr++ {
				got[roCanvasSig(newROSquareCanvas(rotated, tr, bg))]++
			}
			if fmt.Sprint(got) != fmt.Sprint(reference) {
				t.Fatalf("w=%d h=%d g=%d: D4 candidate multisets differ", w, h, g)
			}
		}
	}
}
