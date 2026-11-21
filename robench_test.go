// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"image"
	"image/color"
	"testing"
)

// TestROPropertyOrientationInvariant is a deterministic property test: for a
// table of generated shapes (square, rectangular, odd, single-row, extreme
// aspect ratio), the canonical hash of every D4 orientation must equal the
// hash of the identity orientation at every supported bit length.
func TestROPropertyOrientationInvariant(t *testing.T) {
	shapes := []struct{ w, h int }{
		{1, 1}, {1, 2}, {2, 1}, {2, 2}, {3, 3}, {4, 2}, {2, 4},
		{5, 7}, {7, 5}, {8, 8}, {13, 3}, {3, 13}, {16, 1}, {1, 16}, {31, 29},
	}
	bitLengths := []int{4, 16, 64, 256, 1024}
	bgs := []color.RGBA{
		{0, 0, 0, 255}, {255, 255, 255, 255}, {17, 128, 200, 255},
	}

	for _, bg := range bgs {
		for _, s := range shapes {
			base := asymmetricImage(s.w, s.h)
			for _, bits := range bitLengths {
				want, err := OrientationInvariantPerceptionHash(base, bg, bits)
				if err != nil {
					t.Fatalf("shape=%dx%d bits=%d: %v", s.w, s.h, bits, err)
				}
				for tr := roTransform(0); tr < roNumTransforms; tr++ {
					got, err := OrientationInvariantPerceptionHash(transformRectangle(base, tr), bg, bits)
					if err != nil {
						t.Fatalf("shape=%dx%d bits=%d t=%d: %v", s.w, s.h, bits, tr, err)
					}
					if got.GetKind() != ROHash || got.Bits() != bits {
						t.Fatalf("shape=%dx%d t=%d: metadata mismatch", s.w, s.h, tr)
					}
					if got.ToString() != want.ToString() {
						t.Fatalf("shape=%dx%d bits=%d t=%d: property violated\n got %s\nwant %s",
							s.w, s.h, bits, tr, got.ToString(), want.ToString())
					}
				}
			}
		}
	}
}

// TestROPropertyHammingIsMetric checks basic Hamming semantics on RO hashes:
// self distance zero, symmetry, triangle inequality, and the bit-count guard.
func TestROPropertyHammingIsMetric(t *testing.T) {
	bg := color.RGBA{50, 60, 70, 255}
	imgs := []image.Image{
		asymmetricImage(10, 6),
		asymmetricImage(6, 10),
		transformRectangle(asymmetricImage(10, 6), roRotate90),
		asymmetricImage(12, 9),
	}
	hashes := make([]*ROImageHash, len(imgs))
	for i, im := range imgs {
		h, err := OrientationInvariantPerceptionHash(im, bg, 64)
		if err != nil {
			t.Fatal(err)
		}
		hashes[i] = h
	}
	for i, hi := range hashes {
		d, err := hi.Distance(hi)
		if err != nil || d != 0 {
			t.Fatalf("self distance for image %d = %d, %v", i, d, err)
		}
		for j, hj := range hashes {
			dij, err := hi.Distance(hj)
			if err != nil {
				t.Fatal(err)
			}
			dji, err := hj.Distance(hi)
			if err != nil || dij != dji {
				t.Fatalf("asymmetric distance %d/%d", dij, dji)
			}
			for _, hk := range hashes {
				dik, _ := hi.Distance(hk)
				dkj, _ := hk.Distance(hj)
				if dij > dik+dkj {
					t.Fatalf("triangle inequality violated: d(%d,%d)=%d > %d+%d", i, j, dij, dik, dkj)
				}
			}
		}
	}
}

// benchmarkImage builds a reasonably sized source shared by all benchmarks.
func benchmarkImage() image.Image { return asymmetricImage(128, 96) }

// BenchmarkROHash reports the end-to-end cost of the public entry point,
// which evaluates all eight orientation candidates.
func BenchmarkROHash(b *testing.B) {
	img := benchmarkImage()
	bg := color.White
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := OrientationInvariantPerceptionHash(img, bg, 64); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkROCandidate reports the amortized cost of a single candidate
// (virtual canvas, bilinear square resize, grayscale, 64x64 DCT, median and
// bit packing). Eight times this plus the canonical comparison approximates
// BenchmarkROHash.
func BenchmarkROCandidate(b *testing.B) {
	img := benchmarkImage()
	bg := color.White
	var gray [roDCTSize * roDCTSize]float64
	block := make([]float64, 64)
	canvasBuf := make([]byte, 0)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tr := roTransform(i % roNumTransforms)
		if _, err := roCandidateHash(img, bg, tr, 8, &gray, block, 1, &canvasBuf); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkROHash256 measures the 256-bit output path.
func BenchmarkROHash256(b *testing.B) {
	img := benchmarkImage()
	bg := color.Black
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := OrientationInvariantPerceptionHash(img, bg, 256); err != nil {
			b.Fatal(err)
		}
	}
}
