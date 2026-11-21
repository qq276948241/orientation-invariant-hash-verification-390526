// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"image"
	"image/color"
	"testing"
)

// TestSourceAtMatchesStdlib pins the hand-rolled premultiplication in the
// concrete-type fast paths against the standard library color models. A
// rounding drift here would silently change every hash produced from NRGBA,
// RGBA or Gray sources.
func TestSourceAtMatchesStdlib(t *testing.T) {
	nrgbaCases := []color.NRGBA{
		{0, 0, 0, 0}, {255, 255, 255, 255}, {200, 100, 50, 128},
		{1, 2, 3, 1}, {254, 128, 7, 200}, {17, 240, 99, 64},
	}
	for _, c0 := range nrgbaCases {
		img := image.NewNRGBA(image.Rect(0, 0, 1, 1))
		img.SetNRGBA(0, 0, c0)
		canvas := newROSquareCanvas(img, roIdentity, color.RGBA{0, 0, 0, 255})
		pr, pg, pb, pa := canvas.sourceAt(0, 0)
		sr, sg, sb, sa := color.NRGBAModel.Convert(c0).RGBA()
		if pr != sr || pg != sg || pb != sb || pa != sa {
			t.Fatalf("NRGBA %v: got (%d,%d,%d,%d) stdlib (%d,%d,%d,%d)", c0, pr, pg, pb, pa, sr, sg, sb, sa)
		}
	}

	rgba := image.NewRGBA(image.Rect(0, 0, 1, 1))
	rgba.SetRGBA(0, 0, color.RGBA{10, 20, 30, 128})
	c2 := newROSquareCanvas(rgba, roIdentity, color.Black)
	r1, g1, b1, a1 := c2.sourceAt(0, 0)
	r2, g2, b2, a2 := rgba.At(0, 0).RGBA()
	if r1 != r2 || g1 != g2 || b1 != b2 || a1 != a2 {
		t.Fatal("RGBA fast path mismatch")
	}

	g := image.NewGray(image.Rect(0, 0, 1, 1))
	g.SetGray(0, 0, color.Gray{Y: 137})
	c3 := newROSquareCanvas(g, roIdentity, color.Black)
	gr, gg, gb, ga := c3.sourceAt(0, 0)
	er, eg, eb, ea := g.At(0, 0).RGBA()
	if gr != er || gg != eg || gb != eb || ga != ea {
		t.Fatal("Gray fast path mismatch")
	}
}
