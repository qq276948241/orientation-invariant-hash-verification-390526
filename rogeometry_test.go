// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"image"
	"image/color"
	"testing"
)

// recorder stores every pixel set on it and serves as a controllable source.
type recorder struct {
	bounds image.Rectangle
	pix    map[image.Point]color.Color
}

func newRecorder(r image.Rectangle) *recorder {
	return &recorder{bounds: r, pix: map[image.Point]color.Color{}}
}

func (r *recorder) ColorModel() color.Model { return color.RGBA64Model }
func (r *recorder) Bounds() image.Rectangle { return r.bounds }
func (r *recorder) At(x, y int) color.Color {
	if c, ok := r.pix[image.Point{x, y}]; ok {
		return c
	}
	return color.RGBA64{}
}

// roForward maps a source coordinate to its canvas coordinate under the
// documented D4 forward transform and the symmetric centering convention.
func roForward(t roTransform, w, h, sx, sy int) (cx, cy int) {
	side := w
	if h > side {
		side = h
	}
	var u, v, tw, th int
	switch t {
	case roIdentity:
		u, v, tw, th = sx, sy, w, h
	case roRotate90:
		u, v, tw, th = h-1-sy, sx, h, w
	case roRotate180:
		u, v, tw, th = w-1-sx, h-1-sy, w, h
	case roRotate270:
		u, v, tw, th = sy, w-1-sx, h, w
	case roFlipH:
		u, v, tw, th = w-1-sx, sy, w, h
	case roFlipHRotate90:
		u, v, tw, th = h-1-sy, w-1-sx, h, w
	case roFlipHRotate180:
		u, v, tw, th = sx, h-1-sy, w, h
	case roFlipHRotate270:
		u, v, tw, th = sy, sx, h, w
	}
	return u + roAxisStart(side, tw), v + roAxisStart(side, th)
}

func TestROCanvasInverseMap(t *testing.T) {
	// Include odd sizes, non-square geometry and non-zero origins so both
	// symmetric background margins and odd-delta edge duplication run.
	dims := []struct{ w, h int }{
		{1, 1}, {1, 2}, {2, 1}, {2, 2}, {3, 2}, {2, 3}, {3, 3},
		{5, 3}, {3, 5}, {7, 4}, {4, 7}, {6, 6}, {8, 3}, {3, 8},
	}
	bg := color.RGBA{10, 20, 30, 255}
	content := color.RGBA{200, 100, 50, 255}
	bg64 := color.RGBA64Model.Convert(bg).(color.RGBA64)
	ct64 := color.RGBA64Model.Convert(content).(color.RGBA64)

	for _, d := range dims {
		for _, origin := range []image.Point{{0, 0}, {-4, 7}, {3, -2}} {
			r := image.Rectangle{
				Min: origin,
				Max: image.Point{origin.X + d.w, origin.Y + d.h},
			}
			src := newRecorder(r)
			for sy := 0; sy < d.h; sy++ {
				for sx := 0; sx < d.w; sx++ {
					src.pix[image.Point{origin.X + sx, origin.Y + sy}] = content
				}
			}

			for tr := roTransform(0); tr < roNumTransforms; tr++ {
				c := newROSquareCanvas(src, tr, bg)
				if c.Bounds() != image.Rect(0, 0, c.side, c.side) {
					t.Fatalf("w=%d h=%d t=%d: bounds not a zero-origin square: %v", d.w, d.h, tr, c.Bounds())
				}

				// Every real source coordinate must map to a unique canvas
				// coordinate that round-trips back to itself.
				seen := map[image.Point]bool{}
				for sy := 0; sy < d.h; sy++ {
					for sx := 0; sx < d.w; sx++ {
						cx, cy := roForward(tr, d.w, d.h, sx, sy)
						if seen[image.Point{cx, cy}] {
							t.Fatalf("w=%d h=%d t=%d: forward map not injective at (%d,%d)", d.w, d.h, tr, cx, cy)
						}
						seen[image.Point{cx, cy}] = true
						rsx, rsy := c.inverseMap(cx, cy)
						if rsx != sx || rsy != sy {
							t.Fatalf("w=%d h=%d t=%d: round trip (%d,%d)->(%d,%d)->(%d,%d)",
								d.w, d.h, tr, sx, sy, cx, cy, rsx, rsy)
						}
						if got := c.At(cx, cy); got != ct64 {
							t.Fatalf("w=%d h=%d t=%d: content color wrong at (%d,%d): %v", d.w, d.h, tr, cx, cy, got)
						}
					}
				}

				// Every canvas pixel is either content (or an edge-duplicated
				// slot carrying the same color) or the exact background.
				bgCount, contentCount := 0, 0
				for y := 0; y < c.side; y++ {
					for x := 0; x < c.side; x++ {
						col := c.At(x, y)
						switch col {
						case bg64:
							bgCount++
						case ct64:
							contentCount++
						default:
							t.Fatalf("w=%d h=%d t=%d: unexpected color %v at (%d,%d)", d.w, d.h, tr, col, x, y)
						}
					}
				}
				if contentCount < d.w*d.h {
					t.Fatalf("w=%d h=%d t=%d: only %d content-colored pixels, want >= %d", d.w, d.h, tr, contentCount, d.w*d.h)
				}
				if bgCount+contentCount != c.side*c.side {
					t.Fatalf("w=%d h=%d t=%d: pixel accounting broken", d.w, d.h, tr)
				}
			}
		}
	}
}

func TestROCanvasEdgeDuplicationVsBackground(t *testing.T) {
	// 3x1 source in a 3x3 canvas. Vertical delta = 2 (even): pure background
	// on both rows. Horizontal delta = 0. Then rotate 90: canvas 3x3, content
	// 1x3 centered with delta 2 on x: again symmetric background.
	// A 4x1 source in 4x4: vertical delta = 3 (odd): one edge-duplicated row
	// plus one background row per the documented split.
	bg := color.RGBA{0, 0, 0, 255}
	src := image.NewNRGBA(image.Rect(0, 0, 4, 1))
	for x := 0; x < 4; x++ {
		src.SetNRGBA(x, 0, color.NRGBA{255, 255, 255, 255})
	}
	c := newROSquareCanvas(src, roIdentity, bg)
	start := roAxisStart(4, 1) // (4-1)/2 = 1; content row is y=1.
	rowColors := func(y int) (allBg, allWhite bool) {
		allBg, allWhite = true, true
		for x := 0; x < 4; x++ {
			col := c.At(x, y)
			if col != (color.RGBA64Model.Convert(bg).(color.RGBA64)) {
				allBg = false
			}
			if col != (color.RGBA64{0xffff, 0xffff, 0xffff, 0xffff}) {
				allWhite = false
			}
		}
		return allBg, allWhite
	}
	// Delta 3 is odd: the top slot (start-1 = 0) is the background margin and
	// the bottom slot (start+1 = 2) duplicates the only content row.
	bgTop, whiteTop := rowColors(start - 1)
	if !bgTop || whiteTop {
		t.Fatalf("top odd-delta slot should be background: bg=%v white=%v", bgTop, whiteTop)
	}
	bgBot, whiteBot := rowColors(start + 1)
	if bgBot || !whiteBot {
		t.Fatalf("bottom odd-delta slot should duplicate the edge: bg=%v white=%v", bgBot, whiteBot)
	}
}

func TestROCanvasOutsideBounds(t *testing.T) {
	src := image.NewRGBA(image.Rect(2, 3, 5, 6))
	c := newROSquareCanvas(src, roIdentity, color.Black)
	for _, p := range []image.Point{{-1, 0}, {0, -1}, {c.side, 0}, {0, c.side}} {
		if got := c.At(p.X, p.Y); got != (color.RGBA64{}) {
			t.Fatalf("out-of-bounds pixel %v returned %v, want zero color", p, got)
		}
	}
}
