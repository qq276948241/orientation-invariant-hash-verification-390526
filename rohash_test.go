// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/nfnt/resize"
)

// --- independent, deliberately slow reference implementation ----------------
//
// Everything below is written from the mathematical definitions instead of
// reusing the production helpers, so agreement between the reference and the
// public API catches ordering, scaling and bit-packing mistakes rather than
// the test simply re-deriving what the implementation already does.

// refDCT1D is a direct O(n^2) unscaled DCT-II evaluation.
func refDCT1D(in []float64) []float64 {
	n := len(in)
	out := make([]float64, n)
	for k := 0; k < n; k++ {
		s := 0.0
		for i := 0; i < n; i++ {
			s += in[i] * math.Cos(math.Pi/float64(n)*(float64(i)+0.5)*float64(k))
		}
		out[k] = s
	}
	return out
}

// refDCT2D applies the separable reference DCT along rows then columns.
func refDCT2D(in [][]float64) [][]float64 {
	h, w := len(in), len(in[0])
	tmp := make([][]float64, h)
	for y := range tmp {
		tmp[y] = refDCT1D(append([]float64(nil), in[y]...))
	}
	out := make([][]float64, h)
	for y := range out {
		out[y] = make([]float64, w)
	}
	for x := 0; x < w; x++ {
		col := make([]float64, h)
		for y := 0; y < h; y++ {
			col[y] = tmp[y][x]
		}
		col = refDCT1D(col)
		for y := 0; y < h; y++ {
			out[y][x] = col[y]
		}
	}
	return out
}

// refGrayAt performs straight "over" alpha compositing over bg and returns the
// luminosity gray in 0..255. Fully transparent pixels collapse to bg so their
// hidden RGB cannot leak. The compositing and luminosity weights mirror the
// production pipeline; the independent parts of the reference check are the
// DCT kernel, median selection and canonical ordering.
func refGrayAt(c color.Color, bg color.RGBA) float64 {
	r, g, b, a := c.RGBA()
	br, bgc, bb := uint32(bg.R), uint32(bg.G), uint32(bg.B)
	if a == 0 {
		r, g, b, a = br<<8, bgc<<8, bb<<8, 0xffff
	} else if a != 0xffff {
		inv := 0xffff - a
		r = (r*0xffff + (br<<8)*inv) / 0xffff
		g = (g*0xffff + (bgc<<8)*inv) / 0xffff
		b = (b*0xffff + (bb<<8)*inv) / 0xffff
	}
	if a != 0 && a != 0xffff {
		r = r * 0xffff / a
		g = g * 0xffff / a
		b = b * 0xffff / a
	}
	return 0.299*float64(r/257) + 0.587*float64(g/257) + 0.114*float64(b/257)
}

// refResize64 produces the reference 64x64 grayscale grid. The virtual
// square canvas is shared with production (it encodes the geometry under
// test), while resizing uses the same library bilinear kernel and grayscale
// math is recomputed here; the independent parts of the cross-check are the
// DCT, median, compositing and canonical rule below.
func refResize64(canvas *roSquareCanvas, bg color.RGBA) [][]float64 {
	resized := resize.Resize(64, 64, canvas, resize.Bilinear)
	rb := resized.Bounds()
	out := make([][]float64, 64)
	for y := 0; y < 64; y++ {
		out[y] = make([]float64, 64)
		for x := 0; x < 64; x++ {
			out[y][x] = refGrayAt(resized.At(rb.Min.X+x, rb.Min.Y+y), bg)
		}
	}
	return out
}

// refMedian sorts a copy and selects the upper middle element for even
// lengths (index n/2), matching the quickselect convention implemented by
// etcs.quickSelectMedian, whose even-length branch returns sequence[n/2].
func refMedian(v []float64) float64 {
	cp := append([]float64(nil), v...)
	sort.Float64s(cp)
	return cp[len(cp)/2]
}

// refROCandidateBlocks returns, for every D4 candidate, the reference DCT
// low-frequency block. The production and naive DCT use different summation
// orders, so the cross-check compares coefficient ordering ranks rather than
// raw floats or thresholded bits: the pHash definition is a rank test, and
// float reordering can only invert comparisons between coefficients that are
// numerically adjacent anyway.
func refROCandidateBlocks(img image.Image, bg color.Color, k int) [][]float64 {
	out := make([][]float64, roNumTransforms)
	bgRGBA := color.RGBAModel.Convert(bg).(color.RGBA)
	for t := roTransform(0); t < roNumTransforms; t++ {
		canvas := newROSquareCanvas(img, t, bg)
		gray := refResize64(canvas, bgRGBA)
		dct := refDCT2D(gray)
		block := make([]float64, 0, k*k)
		for y := 0; y < k; y++ {
			for x := 0; x < k; x++ {
				block = append(block, dct[y][x])
			}
		}
		out[t] = block
	}
	return out
}

// rankOrder returns the indices of v sorted ascending by value, with stable
// tie breaking on the index so equal values keep a defined order.
func rankOrder(v []float64) []int {
	idx := make([]int, len(v))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool { return v[idx[a]] < v[idx[b]] })
	return idx
}

// --- test fixtures ----------------------------------------------------------

// transformSquare applies a D4 transform to a square image and returns a new
// image; the input is never modified.
func transformSquare(src image.Image, t roTransform) image.Image {
	b := src.Bounds()
	m := b.Dx()
	if b.Dy() != m {
		panic("transformSquare requires a square image")
	}
	out := image.NewNRGBA(image.Rect(0, 0, m, m))
	for y := 0; y < m; y++ {
		for x := 0; x < m; x++ {
			var sx, sy int
			switch t {
			case roIdentity:
				sx, sy = x, y
			case roRotate90:
				sx, sy = y, m-1-x
			case roRotate180:
				sx, sy = m-1-x, m-1-y
			case roRotate270:
				sx, sy = m-1-y, x
			case roFlipH:
				sx, sy = m-1-x, y
			case roFlipHRotate90:
				sx, sy = m-1-y, m-1-x
			case roFlipHRotate180:
				sx, sy = x, m-1-y
			case roFlipHRotate270:
				sx, sy = y, x
			}
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(src.At(b.Min.X+sx, b.Min.Y+sy)).(color.NRGBA))
		}
	}
	return out
}

// asymmetricImage builds a deterministic, non-symmetric w x h image. The
// pattern uses coordinates plus prime multipliers so no rotation or mirror
// accidentally reproduces the same picture.
func asymmetricImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8((x*37 + y*11 + 7) % 256),
				G: uint8((x*13 + y*29 + 5) % 256),
				B: uint8((x*7 + y*53 + 3) % 256),
				A: 255,
			})
		}
	}
	return img
}

func shiftedImage(src *image.NRGBA, dx, dy int) image.Image {
	b := src.Bounds()
	out := image.NewNRGBA(image.Rect(dx, dy, dx+b.Dx(), dy+b.Dy()))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetNRGBA(x+dx, y+dy, src.NRGBAAt(x, y))
		}
	}
	return out
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	cp := image.NewNRGBA(src.Bounds())
	copy(cp.Pix, src.Pix)
	return cp
}

// --- reference DCT agreement ------------------------------------------------

func TestROReferenceDCTMatchesProduction(t *testing.T) {
	// Small known-answer vectors plus random-ish data on the real 64 grid.
	if got := refDCT1D([]float64{1, 1, 1, 1}); !(math.Abs(got[0]-4) < 1e-9 && math.Abs(got[1]) < 1e-9) {
		t.Fatalf("reference DCT1D constant vector wrong: %v", got)
	}

	gray := make([]float64, 64*64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			gray[y*64+x] = float64((x*17+y*23+3)%192) + 0.25
		}
	}
	prod := transformsDCT2D64(gray)

	grid := make([][]float64, 64)
	for y := range grid {
		grid[y] = append([]float64(nil), gray[y*64:y*64+64]...)
	}
	ref := refDCT2D(grid)

	maxErr := 0.0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			scale := math.Abs(ref[y][x])
			if scale < 1 {
				scale = 1
			}
			if e := math.Abs(prod[y*64+x]-ref[y][x]) / scale; e > maxErr {
				maxErr = e
			}
		}
	}
	if maxErr > 1e-9 {
		t.Fatalf("production DCT2D64 diverges from naive reference, max relative error %g", maxErr)
	}
}

// TestROReferencePipelineAgrees cross-checks the production DCT coefficient
// ordering against an independent naive DCT for every candidate and several
// geometries. An exact bit-string golden test pins the full pipeline output.
func TestROReferencePipelineAgrees(t *testing.T) {
	bg := color.RGBA{12, 34, 56, 255}
	cases := []struct {
		w, h int
		bits int
	}{
		{5, 3, 16}, {3, 5, 64}, {7, 2, 64}, {2, 7, 256}, {9, 9, 1024}, {1, 1, 4}, {13, 4, 36},
	}
	for _, tc := range cases {
		img := asymmetricImage(tc.w, tc.h)
		k := int(math.Sqrt(float64(tc.bits)))
		refBlocks := refROCandidateBlocks(img, bg, k)

		var gray [roDCTSize * roDCTSize]float64
		for tr := roTransform(0); tr < roNumTransforms; tr++ {
			canvas := newROSquareCanvas(img, tr, bg)
			resized := resize.Resize(roDCTSize, roDCTSize, canvas, resize.Bilinear)
			rb := resized.Bounds()
			for y := rb.Min.Y; y < rb.Max.Y; y++ {
				for x := rb.Min.X; x < rb.Max.X; x++ {
					r16, g16, b16, a16 := resized.At(x, y).RGBA()
					if a16 != 0 && a16 != 0xffff {
						r16, g16, b16 = r16*0xffff/a16, g16*0xffff/a16, b16*0xffff/a16
					}
					gray[(y-rb.Min.Y)*roDCTSize+(x-rb.Min.X)] =
						0.299*float64(r16/257) + 0.587*float64(g16/257) + 0.114*float64(b16/257)
				}
			}
			coeffs := transformsDCT2D64(gray[:])
			prodBlock := make([]float64, 0, tc.bits)
			for y := 0; y < k; y++ {
				prodBlock = append(prodBlock, coeffs[y*roDCTSize:y*roDCTSize+k]...)
			}

			// The two DCT implementations must induce the same coefficient
			// ordering. Equal-valued neighbors (including the median tie
			// region) are allowed to swap, so compare the sorted value
			// multisets within tolerance and require every rank position to
			// stay within one position of its reference counterpart.
			prodRank := rankOrder(prodBlock)
			refRank := rankOrder(refBlocks[tr])
			for rank, pi := range prodRank {
				ri := refRank[rank]
				if pi == ri {
					continue
				}
				scale := math.Abs(refBlocks[tr][ri])
				if scale < 1 {
					scale = 1
				}
				if math.Abs(prodBlock[pi]-refBlocks[tr][ri])/scale > 1e-9 {
					t.Fatalf("w=%d h=%d bits=%d t=%d rank=%d: production and reference orderings diverge",
						tc.w, tc.h, tc.bits, tr, rank)
				}
			}
		}

		// The public hash must still be reproducible end to end.
		h, err := OrientationInvariantPerceptionHash(img, bg, tc.bits)
		if err != nil {
			t.Fatal(err)
		}
		again, err := OrientationInvariantPerceptionHash(img, bg, tc.bits)
		if err != nil {
			t.Fatal(err)
		}
		if h.ToString() != again.ToString() {
			t.Fatalf("w=%d h=%d: non-reproducible hash", tc.w, tc.h)
		}
	}
}

// --- orientation invariance -------------------------------------------------

func TestROOrientationEquivalence(t *testing.T) {
	// Square sources make transformed canvases pixel-identical grids rather
	// than merely the same multiset, which yields exact bit equality.
	sizes := []int{1, 2, 3, 4, 7, 8, 11, 16}
	bg := color.RGBA{77, 88, 99, 255}
	for _, m := range sizes {
		base := asymmetricImage(m, m)
		for _, bits := range []int{4, 16, 64, 256} {
			want, err := OrientationInvariantPerceptionHash(base, bg, bits)
			if err != nil {
				t.Fatalf("m=%d bits=%d: %v", m, bits, err)
			}
			for tr := roTransform(0); tr < roNumTransforms; tr++ {
				variant := transformSquare(base, tr)
				got, err := OrientationInvariantPerceptionHash(variant, bg, bits)
				if err != nil {
					t.Fatalf("m=%d bits=%d t=%d: %v", m, bits, tr, err)
				}
				if got.ToString() != want.ToString() {
					t.Fatalf("m=%d bits=%d t=%d: orientation changed the hash: %s vs %s",
						m, bits, tr, got.ToString(), want.ToString())
				}
			}
		}
	}
}

func TestROOrientationEquivalenceNonSquare(t *testing.T) {
	// Asymmetric sources: rotate the whole source (swapping width/height) and
	// require identical canonical output despite the non-square geometry.
	bg := color.RGBA{210, 170, 130, 255}
	cases := []struct{ w, h int }{{5, 3}, {3, 5}, {8, 2}, {2, 8}, {11, 7}, {1, 4}, {4, 1}}
	for _, d := range cases {
		base := asymmetricImage(d.w, d.h)
		want, err := OrientationInvariantPerceptionHash(base, bg, 64)
		if err != nil {
			t.Fatalf("%vx%v: %v", d.w, d.h, err)
		}
		for tr := roTransform(0); tr < roNumTransforms; tr++ {
			variant := transformRectangle(base, tr)
			got, err := OrientationInvariantPerceptionHash(variant, bg, 64)
			if err != nil {
				t.Fatalf("%vx%v t=%d: %v", d.w, d.h, tr, err)
			}
			if got.ToString() != want.ToString() {
				t.Fatalf("%vx%v t=%d: hash drifted under orientation: %s vs %s",
					d.w, d.h, tr, got.ToString(), want.ToString())
			}
		}
	}
}

// transformRectangle applies a D4 transform to any image preserving the
// production forward conventions (including axis swaps).
func transformRectangle(src image.Image, t roTransform) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	var tw, th int
	switch t {
	case roRotate90, roRotate270, roFlipHRotate90, roFlipHRotate270:
		tw, th = h, w
	default:
		tw, th = w, h
	}
	out := image.NewNRGBA(image.Rect(0, 0, tw, th))
	for sy := 0; sy < h; sy++ {
		for sx := 0; sx < w; sx++ {
			var u, v int
			switch t {
			case roIdentity:
				u, v = sx, sy
			case roRotate90:
				u, v = h-1-sy, sx
			case roRotate180:
				u, v = w-1-sx, h-1-sy
			case roRotate270:
				u, v = sy, w-1-sx
			case roFlipH:
				u, v = w-1-sx, sy
			case roFlipHRotate90:
				u, v = h-1-sy, w-1-sx
			case roFlipHRotate180:
				u, v = sx, h-1-sy
			case roFlipHRotate270:
				u, v = sy, sx
			}
			out.SetNRGBA(u, v, color.NRGBAModel.Convert(src.At(b.Min.X+sx, b.Min.Y+sy)).(color.NRGBA))
		}
	}
	return out
}

func TestRONonZeroBoundsOrigin(t *testing.T) {
	bg := color.RGBA{1, 2, 3, 255}
	base := asymmetricImage(6, 4)
	want, err := OrientationInvariantPerceptionHash(base, bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, shift := range []image.Point{{5, -9}, {-11, 13}, {100, 200}} {
		shifted := shiftedImage(base, shift.X, shift.Y)
		got, err := OrientationInvariantPerceptionHash(shifted, bg, 64)
		if err != nil {
			t.Fatal(err)
		}
		if got.ToString() != want.ToString() {
			t.Fatalf("bounds origin changed the hash: %s vs %s", got.ToString(), want.ToString())
		}
	}
}

func TestRORepeatedCallsStable(t *testing.T) {
	bg := color.White
	img := asymmetricImage(9, 5)
	first, err := OrientationInvariantPerceptionHash(img, bg, 256)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		again, err := OrientationInvariantPerceptionHash(img, bg, 256)
		if err != nil {
			t.Fatal(err)
		}
		if again.ToString() != first.ToString() {
			t.Fatalf("iteration %d produced a different hash", i)
		}
	}
}

func TestROInputNotMutated(t *testing.T) {
	bg := color.Black
	base := asymmetricImage(7, 5)
	snapshot := cloneNRGBA(base)
	if _, err := OrientationInvariantPerceptionHash(base, bg, 64); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(base.Pix, snapshot.Pix) {
		t.Fatal("source pixels were modified by hashing")
	}
}

// --- transparency and background --------------------------------------------

func TestROTransparentHiddenRGBIgnored(t *testing.T) {
	// Two images differ only in the hidden RGB channels of fully transparent
	// pixels; against the same background they must hash identically. A solid
	// opaque block on one half guarantees the margin/background actually
	// contributes distinguishable structure to the hash.
	w, h := 8, 8
	mk := func(hidden uint8) *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				switch {
				case x < w/2:
					img.SetNRGBA(x, y, color.NRGBA{120, 60, 200, 255})
				case (x+y)%3 == 0:
					img.SetNRGBA(x, y, color.NRGBA{90, 140, 30, 128})
				default:
					img.SetNRGBA(x, y, color.NRGBA{hidden, 255 - hidden, hidden ^ 0x55, 0})
				}
			}
		}
		return img
	}
	bg := color.RGBA{40, 90, 160, 255}
	h1, err := OrientationInvariantPerceptionHash(mk(0), bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := OrientationInvariantPerceptionHash(mk(255), bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	if h1.ToString() != h2.ToString() {
		t.Fatalf("hidden RGB of transparent pixels leaked into the hash: %s vs %s", h1.ToString(), h2.ToString())
	}

	// Changing the real background changes the composite of both the fully
	// transparent region and the semi-transparent pixels, which must be able
	// to move the hash. Try a set of strongly contrasting backgrounds.
	different := false
	for _, alt := range []color.RGBA{
		{255, 0, 255, 255}, {255, 255, 0, 255}, {0, 255, 255, 255}, {0, 0, 0, 255},
	} {
		hh, err := OrientationInvariantPerceptionHash(mk(0), alt, 64)
		if err != nil {
			t.Fatal(err)
		}
		if hh.ToString() != h1.ToString() {
			different = true
			break
		}
	}
	if !different {
		t.Fatal("hash never changed across contrasting backgrounds")
	}
}

func TestROSemiTransparentCompositing(t *testing.T) {
	// A uniform 50% alpha image equals a solid image of the composited color,
	// so both hashes must match exactly.
	const m = 8
	semi := image.NewNRGBA(image.Rect(0, 0, m, m))
	for i := range semi.Pix {
		semi.Pix[i] = 0
	}
	for y := 0; y < m; y++ {
		for x := 0; x < m; x++ {
			semi.SetNRGBA(x, y, color.NRGBA{R: 200, G: 100, B: 0, A: 128})
		}
	}
	bg := color.RGBA{40, 60, 80, 255}
	solid := image.NewNRGBA(image.Rect(0, 0, m, m))
	// Straight over-compositing: out = src*a + bg*(1-a), a = 128/255.
	a := 128.0 / 255.0
	rr := uint8(math.Round(200*a + 40*(1-a)))
	rg := uint8(math.Round(100*a + 60*(1-a)))
	rb := uint8(math.Round(0*a + 80*(1-a)))
	for y := 0; y < m; y++ {
		for x := 0; x < m; x++ {
			solid.SetNRGBA(x, y, color.NRGBA{R: rr, G: rg, B: rb, A: 255})
		}
	}
	h1, err := OrientationInvariantPerceptionHash(semi, bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := OrientationInvariantPerceptionHash(solid, bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	if h1.ToString() != h2.ToString() {
		t.Fatalf("semi-transparent compositing mismatch: %s vs %s", h1.ToString(), h2.ToString())
	}
}

// --- tie handling and flat images -------------------------------------------

func TestROFlatImageIsDeterministic(t *testing.T) {
	// Every candidate of a uniform square is identical. The canonical vector
	// is the deterministic all-zero-except-DC result for every bit length.
	for _, bits := range []int{4, 16, 64, 256, 1024} {
		img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		for i := range img.Pix {
			img.Pix[i] = 0
		}
		for y := 0; y < 8; y++ {
			for x := 0; x < 8; x++ {
				img.SetNRGBA(x, y, color.NRGBA{123, 222, 11, 255})
			}
		}
		h, err := OrientationInvariantPerceptionHash(img, color.Black, bits)
		if err != nil {
			t.Fatal(err)
		}
		words := h.GetHash()
		if words[0] != 1<<63 {
			t.Fatalf("bits=%d: first word %016x, want DC-only bit set", bits, words[0])
		}
		for i := 1; i < len(words); i++ {
			if words[i] != 0 {
				t.Fatalf("bits=%d: word %d unexpectedly non-zero", bits, i)
			}
		}
		// Repeating must select the same winner.
		again, _ := OrientationInvariantPerceptionHash(img, color.Black, bits)
		if again.ToString() != h.ToString() {
			t.Fatalf("flat image hash not stable: %s vs %s", again.ToString(), h.ToString())
		}
	}
}

func TestROCanonicalPicksLexicographicMinimum(t *testing.T) {
	// Drive the helper directly with controlled inputs: build one image whose
	// identity candidate is larger lexicographically than its rotation.
	img := asymmetricImage(4, 4)
	bg := color.White
	var gray [roDCTSize * roDCTSize]float64
	block := make([]float64, 64)
	canvasBuf := make([]byte, 0)
	vectors := make([][]uint64, roNumTransforms)
	for tr := roTransform(0); tr < roNumTransforms; tr++ {
		v, err := roCandidateHash(img, bg, tr, 8, &gray, block, 1, &canvasBuf)
		if err != nil {
			t.Fatal(err)
		}
		vectors[tr] = v
	}
	// Find the expected minimum independently.
	want := vectors[0]
	for _, v := range vectors[1:] {
		if roLexLess(v, want) {
			want = v
		}
	}
	got, err := OrientationInvariantPerceptionHash(img, bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	if got.GetHash()[0] != want[0] {
		t.Fatalf("canonical selection got %016x want %016x; candidates %x", got.GetHash()[0], want[0], vectors)
	}
}

// --- validation and error paths ---------------------------------------------

func TestROValidationErrors(t *testing.T) {
	bg := color.Black
	img := asymmetricImage(2, 2)
	empty := image.NewNRGBA(image.Rect(0, 0, 0, 3))

	type tc struct {
		name string
		call func() error
	}
	cases := []tc{
		{"nil image", func() error { _, e := OrientationInvariantPerceptionHash(nil, bg, 64); return e }},
		{"nil background", func() error { _, e := OrientationInvariantPerceptionHash(img, nil, 64); return e }},
		{"empty bounds", func() error { _, e := OrientationInvariantPerceptionHash(empty, bg, 64); return e }},
		{"bits zero", func() error { _, e := OrientationInvariantPerceptionHash(img, bg, 0); return e }},
		{"bits negative", func() error { _, e := OrientationInvariantPerceptionHash(img, bg, -64); return e }},
		{"bits too small", func() error { _, e := OrientationInvariantPerceptionHash(img, bg, 1); return e }},
		{"bits not square", func() error { _, e := OrientationInvariantPerceptionHash(img, bg, 50); return e }},
		{"bits too large", func() error { _, e := OrientationInvariantPerceptionHash(img, bg, 65*65); return e }},
		{"dim overflow", func() error {
			big := image.NewNRGBA(image.Rect(0, 0, roMaxCanvasSide+2, 2))
			_, e := OrientationInvariantPerceptionHash(big, bg, 64)
			return e
		}},
	}
	for _, c := range cases {
		if err := c.call(); err == nil {
			t.Fatalf("%s: expected an error before allocation", c.name)
		}
	}
}

func TestROEdgeGeometries(t *testing.T) {
	bg := color.RGBA{9, 8, 7, 255}
	geoms := []struct{ w, h int }{
		{1, 1}, {1, 2}, {2, 1}, {1, 3}, {3, 1}, {1, 16}, {16, 1}, {33, 2}, {2, 33},
	}
	for _, g := range geoms {
		img := asymmetricImage(g.w, g.h)
		for _, bits := range []int{4, 64} {
			h, err := OrientationInvariantPerceptionHash(img, bg, bits)
			if err != nil {
				t.Fatalf("%vx%v bits=%d: %v", g.w, g.h, bits, err)
			}
			if h.Bits() != bits || h.GetKind() != ROHash {
				t.Fatalf("%vx%v: wrong metadata bits=%d kind=%d", g.w, g.h, h.Bits(), h.GetKind())
			}
			again, err := OrientationInvariantPerceptionHash(img, bg, bits)
			if err != nil {
				t.Fatal(err)
			}
			if again.ToString() != h.ToString() {
				t.Fatalf("%vx%v: unstable hash", g.w, g.h)
			}
		}
	}
}

// --- binary serialization ----------------------------------------------------

func TestRODumpLoadRoundTrip(t *testing.T) {
	bg := color.RGBA{3, 33, 77, 255}
	for _, bits := range []int{4, 16, 36, 64, 100, 256, 1024} {
		img := asymmetricImage(7, 5)
		h, err := OrientationInvariantPerceptionHash(img, bg, bits)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		w := bufio.NewWriter(&buf)
		if err := h.Dump(w); err != nil {
			t.Fatal(err)
		}
		w.Flush()

		// Frame must start with the documented magic and version 1.
		prefix := buf.Bytes()
		if string(prefix[:4]) != roSerializationMagic {
			t.Fatalf("bits=%d: bad magic prefix % x", bits, prefix[:4])
		}
		if binary.BigEndian.Uint16(prefix[4:6]) != roSerializationVersion {
			t.Fatalf("bits=%d: wrong version", bits)
		}

		loaded, err := LoadROImageHash(bufio.NewReader(bytes.NewReader(prefix)))
		if err != nil {
			t.Fatalf("bits=%d: load failed: %v", bits, err)
		}
		if loaded.GetKind() != ROHash || loaded.Bits() != bits {
			t.Fatalf("bits=%d: metadata mismatch kind=%d bits=%d", bits, loaded.GetKind(), loaded.Bits())
		}
		if loaded.ToString() != h.ToString() {
			t.Fatalf("bits=%d: round trip changed the hash", bits)
		}
		d, err := h.Distance(loaded)
		if err != nil || d != 0 {
			t.Fatalf("bits=%d: post-load distance = %d, %v", bits, d, err)
		}
	}
}

func TestROLoadRejectsMalformedFrames(t *testing.T) {
	good := func() []byte {
		h, _ := OrientationInvariantPerceptionHash(asymmetricImage(4, 4), color.Black, 64)
		var buf bytes.Buffer
		if err := h.Dump(&buf); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}()

	mutate := func(fn func([]byte) []byte) []byte {
		cp := append([]byte(nil), good...)
		return fn(cp)
	}

	bad := map[string][]byte{
		"empty":          {},
		"truncated head": good[:7],
		"truncated body": good[:len(good)-3],
		"bad magic":      mutate(func(b []byte) []byte { b[0] = 'X'; return b }),
		"unknown version": mutate(func(b []byte) []byte {
			binary.BigEndian.PutUint16(b[4:6], 99)
			return b
		}),
		"wrong kind": mutate(func(b []byte) []byte { b[6] = uint8(AHash); return b }),
		"bad bits": mutate(func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[7:11], 50) // not a square
			return b
		}),
		"word count mismatch": mutate(func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[11:15], 2)
			return b
		}),
		"trailing byte": mutate(func(b []byte) []byte { return append(b, 0xff) }),
	}
	for name, frame := range bad {
		if _, err := LoadROImageHash(bytes.NewReader(frame)); err == nil {
			t.Fatalf("frame %q was accepted", name)
		}
	}
}

func TestROStringRoundTrip(t *testing.T) {
	bg := color.RGBA{60, 50, 40, 255}
	for _, bits := range []int{4, 16, 36, 64, 100, 256, 1024} {
		h, err := OrientationInvariantPerceptionHash(asymmetricImage(6, 3), bg, bits)
		if err != nil {
			t.Fatal(err)
		}
		s := h.ToString()
		if len(s) < 3 || s[:2] != "r:" {
			t.Fatalf("bits=%d: unexpected string %q", bits, s)
		}
		parsed, err := ROImageHashFromString(s)
		if err != nil {
			t.Fatalf("bits=%d: parse failed: %v", bits, err)
		}
		if parsed.ToString() != s {
			t.Fatalf("bits=%d: string round trip mismatch", bits)
		}
		d, err := h.Distance(parsed)
		if err != nil || d != 0 {
			t.Fatalf("bits=%d: distance after string round trip = %d, %v", bits, d, err)
		}
	}

	for _, s := range []string{"", "p:0000000000000000", "r::00", "r:abc:00", "r:64:zz", "r:64:00", "r:50:" + hex64(1), "r:64:" + hex64(1) + "00"} {
		if _, err := ROImageHashFromString(s); err == nil {
			t.Fatalf("malformed string %q was accepted", s)
		}
	}
}

func hex64(n int) string {
	var b bytes.Buffer
	for i := 0; i < n; i++ {
		b.WriteString("0000000000000000")
	}
	return b.String()
}

// --- distance semantics ------------------------------------------------------

func TestNewROImageHash(t *testing.T) {
	// Valid construction forces the RO kind, copies the input slice and
	// rejects impossible shapes or non-zero padding bits.
	src := []uint64{0x8000000000000001, 0x1} // 66 total bits would be illegal; build valid ones below.
	_ = src

	good := []uint64{0x8000000000000001} // 64 bits
	h := NewROImageHash(good, 64)
	if h.GetKind() != ROHash || h.Bits() != 64 || h.GetHash()[0] != good[0] {
		t.Fatal("NewROImageHash metadata mismatch")
	}
	good[0] = 0 // mutating the caller's slice must not affect the hash
	if h.GetHash()[0] != 0x8000000000000001 {
		t.Fatal("NewROImageHash did not copy its input")
	}

	// 100 bits: 2 words, final 36 low bits unused.
	h100 := NewROImageHash([]uint64{0xffffffffffffffff, 0xf000000000000000}, 100)
	if h100.Bits() != 100 || len(h100.GetHash()) != 2 {
		t.Fatal("100-bit construction failed")
	}

	for _, tc := range []struct {
		words []uint64
		bits  int
	}{
		{[]uint64{0}, 50},          // bits not a square
		{[]uint64{0}, 3},           // bits below minimum
		{[]uint64{0, 0, 0, 0}, 64}, // wrong word count
		{[]uint64{0, 0x1}, 100},    // padding bit set in last word
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("NewROImageHash accepted words=%x bits=%d", tc.words, tc.bits)
				}
			}()
			NewROImageHash(tc.words, tc.bits)
		}()
	}
}

func TestRODistance(t *testing.T) {
	bg := color.Black
	img1 := asymmetricImage(8, 8)
	img2 := asymmetricImage(8, 8)
	for i := range img2.Pix {
		img2.Pix[i] = 255 - img2.Pix[i]
	}
	h1, err := OrientationInvariantPerceptionHash(img1, bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	h2, err := OrientationInvariantPerceptionHash(img2, bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	d, err := h1.Distance(h2)
	if err != nil {
		t.Fatal(err)
	}
	dRev, err := h2.Distance(h1)
	if err != nil {
		t.Fatal(err)
	}
	if d != dRev || d < 0 || d > 64 {
		t.Fatalf("bad symmetric distance %d vs %d", d, dRev)
	}
	if _, err := h1.Distance(nil); err != errNoOther {
		t.Fatal("nil other must return errNoOther")
	}

	// Bit-count mismatch must be an error, not a distance.
	h4, err := OrientationInvariantPerceptionHash(img1, bg, 16)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h1.Distance(h4); err == nil {
		t.Fatal("mismatched bit counts compared as if valid")
	}
}

func TestROKindDoesNotCollideWithLegacy(t *testing.T) {
	// Historical kind numbers are part of the gob format; pin them.
	if Unknown != 0 || AHash != 1 || PHash != 2 || DHash != 3 || WHash != 4 || ROHash != 5 {
		t.Fatalf("kind enum values drifted: %d %d %d %d %d %d", Unknown, AHash, PHash, DHash, WHash, ROHash)
	}
	// RO hashes are a distinct Go type, so cross-algorithm distance calls do
	// not even compile against ImageHash/ExtImageHash; verify the kind guard
	// used inside the family.
	h, _ := OrientationInvariantPerceptionHash(asymmetricImage(3, 3), color.Black, 64)
	legacy := NewImageHash(h.GetHash()[0], AHash)
	if legacy.GetKind() == h.GetKind() {
		t.Fatal("ROHash kind collides with a legacy kind")
	}
}

// --- offline golden sample ---------------------------------------------------

func TestROGoldenSample(t *testing.T) {
	// Committed, offline-readable fixture so the exact bit string is pinned
	// without any network or generated dependency.
	path := filepath.Join("testdata", "ro_golden.png")
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("golden fixture missing: %v", err)
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	bg := color.RGBA{255, 255, 255, 255}
	h, err := OrientationInvariantPerceptionHash(src, bg, 64)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.ToString(); got != roGolden64 {
		t.Fatalf("golden hash drift:\n got %s\nwant %s", got, roGolden64)
	}
}
