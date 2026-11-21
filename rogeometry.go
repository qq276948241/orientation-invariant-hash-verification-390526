// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"image"
	"image/color"
)

// roTransform identifies one of the eight dihedral (D4) transformations of a
// rectangle: the four right-angle rotations and the horizontal mirror of each
// rotation. The values are also the fixed, documented candidate order used by
// OrientationInvariantPerceptionHash.
type roTransform int

const (
	roIdentity       roTransform = iota // 0: identity
	roRotate90                          // 1: 90 degrees clockwise
	roRotate180                         // 2: 180 degrees
	roRotate270                         // 3: 270 degrees clockwise
	roFlipH                             // 4: horizontal mirror (left/right)
	roFlipHRotate90                     // 5: mirror then rotate 90 clockwise
	roFlipHRotate180                    // 6: mirror then rotate 180
	roFlipHRotate270                    // 7: mirror then rotate 270 clockwise
)

// roNumTransforms is the number of D4 orientation candidates.
const roNumTransforms = 8

// roMaxCanvasSide bounds the side length of the normalized square canvas.
// It keeps every intermediate allocation well inside machine integer range;
// realistic images never approach it.
const roMaxCanvasSide = 1 << 20

// roSquareCanvas presents an image.Image as a virtual, zero-origin square
// canvas that contains one D4 transform of the source image, normalized
// without any aspect-ratio stretching.
//
// Normalization (identical rule on both axes and for every candidate):
//
//	M = max(w, h) is the square side. The transformed content rectangle is
//	centered inside it using a delta that is split symmetrically:
//
//		start = floor(delta/2), so left/top = start and right/bottom =
//		delta-start.
//
// When delta is even the two margins are equal and are filled with the
// caller supplied background color. When delta is odd the two margins differ
// by exactly one pixel; instead of an asymmetric background strip (which a
// rotation would move to another side and which therefore breaks exact
// orientation invariance), the outermost content row/column is duplicated
// into the extra slot. In other words every normalized coordinate maps to a
// source coordinate by clamping, which makes the normalization commute with
// the D4 group: transform-then-normalize and normalize-then-transform are
// pixel identical for all eight transforms. Consequently, rotating or
// mirroring the source merely permutes the eight candidate canvases.
//
// The wrapper never copies or modifies source pixels: At maps each requested
// coordinate back through the inverse transform and reads the underlying
// image, so the source image is left untouched and repeated calls are stable.
type roSquareCanvas struct {
	src    image.Image
	minX   int // source bounds origin
	minY   int
	srcW   int // source content width w
	srcH   int // source content height h
	side   int // M: square canvas side
	t      roTransform
	bgR    uint32 // straight background components, 16-bit scale
	bgG    uint32
	bgB    uint32
	bgA    uint32
	bounds image.Rectangle
}

// newROSquareCanvas builds the virtual square canvas for transform t.
func newROSquareCanvas(src image.Image, t roTransform, bg color.Color) *roSquareCanvas {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	side := w
	if h > side {
		side = h
	}

	br, bgc, bb, ba := bg.RGBA()
	return &roSquareCanvas{
		src:    src,
		minX:   b.Min.X,
		minY:   b.Min.Y,
		srcW:   w,
		srcH:   h,
		side:   side,
		t:      t,
		bgR:    br,
		bgG:    bgc,
		bgB:    bb,
		bgA:    ba,
		bounds: image.Rect(0, 0, side, side),
	}
}

// Bounds returns a zero-origin square rectangle. A source with a non-zero
// bounds origin is normalized to zero origin here.
func (c *roSquareCanvas) Bounds() image.Rectangle { return c.bounds }

// ColorModel reports the premultiplied 64-bit model returned by At.
func (c *roSquareCanvas) ColorModel() color.Model { return color.RGBA64Model }

// clamp forces v into [0, max-1]. It implements both the symmetric background
// margins (coordinates outside the content range) and edge duplication (the
// single extra slot when an axis delta is odd).
func roClamp(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max-1 {
		return max - 1
	}
	return v
}

// sourceAt reads a source pixel in premultiplied 16-bit components. Common
// concrete image types use their direct index accessors to avoid the
// interface boxing an image.Image.At call would incur; every other type falls
// back to the generic interface method.
func (c *roSquareCanvas) sourceAt(sx, sy int) (pr, pg, pb, pa uint32) {
	x := c.minX + sx
	y := c.minY + sy
	switch s := c.src.(type) {
	case *image.NRGBA:
		i := s.PixOffset(x, y)
		a := uint32(s.Pix[i+3])
		// NRGBA is straight alpha; return premultiplied 16-bit values.
		pr = uint32(s.Pix[i+0]) * a * 0xffff / 255 / 255
		pg = uint32(s.Pix[i+1]) * a * 0xffff / 255 / 255
		pb = uint32(s.Pix[i+2]) * a * 0xffff / 255 / 255
		pa = a * 0xffff / 255
		return
	case *image.RGBA:
		i := s.PixOffset(x, y)
		pr = uint32(s.Pix[i+0]) * 0xffff / 255
		pg = uint32(s.Pix[i+1]) * 0xffff / 255
		pb = uint32(s.Pix[i+2]) * 0xffff / 255
		pa = uint32(s.Pix[i+3]) * 0xffff / 255
		return
	case *image.RGBA64:
		// Direct Pix indexing keeps this compatible with Go versions before
		// the RGBA64At accessor was added.
		i := s.PixOffset(x, y)
		pr = uint32(s.Pix[i+0])<<8 | uint32(s.Pix[i+1])
		pg = uint32(s.Pix[i+2])<<8 | uint32(s.Pix[i+3])
		pb = uint32(s.Pix[i+4])<<8 | uint32(s.Pix[i+5])
		pa = uint32(s.Pix[i+6])<<8 | uint32(s.Pix[i+7])
		return
	case *image.NRGBA64:
		i := s.PixOffset(x, y)
		sr := uint32(s.Pix[i+0])<<8 | uint32(s.Pix[i+1])
		sg := uint32(s.Pix[i+2])<<8 | uint32(s.Pix[i+3])
		sb := uint32(s.Pix[i+4])<<8 | uint32(s.Pix[i+5])
		a := uint32(s.Pix[i+6])<<8 | uint32(s.Pix[i+7])
		pr = sr * a / 0xffff
		pg = sg * a / 0xffff
		pb = sb * a / 0xffff
		pa = a
		return
	case *image.Gray:
		i := s.PixOffset(x, y)
		v := uint32(s.Pix[i]) * 0xffff / 255
		return v, v, v, 0xffff
	case *image.Gray16:
		v := uint32(s.Gray16At(x, y).Y)
		return v, v, v, 0xffff
	case *image.YCbCr:
		return s.YCbCrAt(x, y).RGBA()
	}
	return c.src.At(x, y).RGBA()
}

// atRGBA64 is the allocation-free counterpart of At for the hot materialize
// loop. At remains the image.Image interface implementation.
func (c *roSquareCanvas) atRGBA64(x, y int) color.RGBA64 {
	if !(image.Point{x, y}.In(c.bounds)) {
		return color.RGBA64{}
	}

	sx, sy := c.inverseMap(x, y)
	if c.axisIsBackground(x, true) || c.axisIsBackground(y, false) {
		return color.RGBA64{R: uint16(c.bgR), G: uint16(c.bgG), B: uint16(c.bgB), A: uint16(c.bgA)}
	}

	pr, pg, pb, pa := c.sourceAt(sx, sy)
	if pa == 0 {
		return color.RGBA64{R: uint16(c.bgR), G: uint16(c.bgG), B: uint16(c.bgB), A: uint16(c.bgA)}
	}
	if pa == 0xffff && c.bgA == 0xffff {
		return color.RGBA64{R: uint16(pr), G: uint16(pg), B: uint16(pb), A: 0xffff}
	}

	// Straight ("over") compositing, emitted premultiplied in 16-bit units.
	a := pa
	invA := 0xffff - a
	outR := (pr*0xffff + c.bgR*invA) / 0xffff
	outG := (pg*0xffff + c.bgG*invA) / 0xffff
	outB := (pb*0xffff + c.bgB*invA) / 0xffff
	outA := a + c.bgA*invA/0xffff
	return color.RGBA64{R: uint16(outR), G: uint16(outG), B: uint16(outB), A: uint16(outA)}
}

// At returns the premultiplied RGBA color of canvas coordinate (x, y).
func (c *roSquareCanvas) At(x, y int) color.Color {
	return c.atRGBA64(x, y)
}

// axisStart returns the start coordinate of the content (or its edge-extended
// range) on one canvas axis for the given content length.
func roAxisStart(side, length int) int { return (side - length) / 2 }

// axisIsBackground reports whether canvas coordinate v on the given axis lies
// in a true background margin rather than a content or duplicated-edge slot.
// horizontal selects the source width axis; false selects the height axis.
// Axis swaps caused by 90/270 degree rotations are handled by the transform.
//
// The centered content covers [start, start+length). When the centering delta
// is odd, one extra slot at start+length duplicates the nearest edge instead
// of being background, so the background region ends at start+length rather
// than start+length+1 on that axis.
func (c *roSquareCanvas) axisIsBackground(v int, horizontal bool) bool {
	var length, side int
	switch c.t {
	case roRotate90, roRotate270, roFlipHRotate90, roFlipHRotate270:
		if horizontal {
			length, side = c.srcH, c.side
		} else {
			length, side = c.srcW, c.side
		}
	default:
		if horizontal {
			length, side = c.srcW, c.side
		} else {
			length, side = c.srcH, c.side
		}
	}
	start := roAxisStart(side, length)
	edgeExtendedEnd := start + length
	if (side-length)&1 == 1 {
		edgeExtendedEnd++
	}
	return v < start || v >= edgeExtendedEnd
}

// inverseMap maps a canvas coordinate to a source content coordinate
// (relative to the source bounds minimum). Background and duplicated-edge
// slots both resolve through clamping; At uses axisIsBackground to separate
// the two cases.
//
// Forward maps over the content rectangle (content coordinates, +y downward),
// with (u, v) the transformed coordinate and (tw, th) the transformed size:
//
//	identity     (w, h): (sx, sy) -> (sx, sy)
//	rot90        (h, w): (sx, sy) -> (h-1-sy, sx)
//	rot180       (w, h): (sx, sy) -> (w-1-sx, h-1-sy)
//	rot270       (h, w): (sx, sy) -> (sy, w-1-sx)
//	flipH        (w, h): (sx, sy) -> (w-1-sx, sy)
//	flipH+90     (h, w): (sx, sy) -> (h-1-sy, w-1-sx)
//	flipH+180    (w, h): (sx, sy) -> (sx, h-1-sy)
//	flipH+270    (h, w): (sx, sy) -> (sy, sx)
func (c *roSquareCanvas) inverseMap(x, y int) (sx, sy int) {
	w, h := c.srcW, c.srcH

	// Content start offsets after centering; axes swap for the transforms
	// whose output size is (h, w).
	var ox, oy int
	switch c.t {
	case roRotate90, roRotate270, roFlipHRotate90, roFlipHRotate270:
		ox, oy = roAxisStart(c.side, h), roAxisStart(c.side, w)
	default:
		ox, oy = roAxisStart(c.side, w), roAxisStart(c.side, h)
	}

	// Transformed coordinates relative to the content rectangle. Values
	// outside the rectangle are clamped by each branch, which reproduces the
	// symmetric background margin (far outside) and edge duplication (the
	// single odd-delta slot).
	u := x - ox
	v := y - oy

	switch c.t {
	case roIdentity:
		// (u, v) = (sx, sy)
		sx, sy = roClamp(u, w), roClamp(v, h)
	case roRotate90:
		// (u, v) = (h-1-sy, sx)
		sx, sy = roClamp(v, w), roClamp(h-1-u, h)
	case roRotate180:
		// (u, v) = (w-1-sx, h-1-sy)
		sx, sy = roClamp(w-1-u, w), roClamp(h-1-v, h)
	case roRotate270:
		// (u, v) = (sy, w-1-sx)
		sx, sy = roClamp(w-1-v, w), roClamp(u, h)
	case roFlipH:
		// (u, v) = (w-1-sx, sy)
		sx, sy = roClamp(w-1-u, w), roClamp(v, h)
	case roFlipHRotate90:
		// (u, v) = (h-1-sy, w-1-sx)
		sx, sy = roClamp(w-1-v, w), roClamp(h-1-u, h)
	case roFlipHRotate180:
		// (u, v) = (sx, h-1-sy)
		sx, sy = roClamp(u, w), roClamp(h-1-v, h)
	case roFlipHRotate270:
		// (u, v) = (sy, sx)
		sx, sy = roClamp(v, w), roClamp(u, h)
	}
	return sx, sy
}
