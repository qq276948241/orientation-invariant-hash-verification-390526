// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transforms

import (
	"math"
	"testing"
)

// naiveDCT1D is an independent O(n^2) unscaled DCT-II used only as a reference.
func naiveDCT1D(in []float64) []float64 {
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

func TestDCT2D64MatchesNaiveReference(t *testing.T) {
	input := make([]float64, 64*64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			input[y*64+x] = float64((x*17+y*23+3)%192) + 0.25
		}
	}
	got := DCT2D64(input)

	grid := make([][]float64, 64)
	for y := range grid {
		grid[y] = append([]float64(nil), input[y*64:y*64+64]...)
	}
	tmp := make([][]float64, 64)
	for y := range tmp {
		tmp[y] = naiveDCT1D(append([]float64(nil), grid[y]...))
	}
	want := make([][]float64, 64)
	for y := range want {
		want[y] = make([]float64, 64)
	}
	for x := 0; x < 64; x++ {
		col := make([]float64, 64)
		for y := 0; y < 64; y++ {
			col[y] = tmp[y][x]
		}
		col = naiveDCT1D(col)
		for y := 0; y < 64; y++ {
			want[y][x] = col[y]
		}
	}

	maxRel := 0.0
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			scale := math.Abs(want[y][x])
			if scale < 1 {
				scale = 1
			}
			if e := math.Abs(got[y*64+x]-want[y][x]) / scale; e > maxRel {
				maxRel = e
			}
		}
	}
	if maxRel > 1e-9 {
		t.Fatalf("DCT2D64 relative error against naive DCT too large: %g", maxRel)
	}
}

func TestDCT2D64DoesNotMutateInput(t *testing.T) {
	input := make([]float64, 64*64)
	for i := range input {
		input[i] = float64(i%37) + 0.5
	}
	snapshot := append([]float64(nil), input...)
	_ = DCT2D64(input)
	for i := range input {
		if input[i] != snapshot[i] {
			t.Fatalf("DCT2D64 mutated input at index %d", i)
		}
	}
}

func TestDCT2D64IsDeterministic(t *testing.T) {
	input := make([]float64, 64*64)
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			input[y*64+x] = float64((x*29+y*7+1)%200) + 0.125
		}
	}
	first := DCT2D64(input)
	for n := 0; n < 3; n++ {
		again := DCT2D64(input)
		for i := range first {
			if again[i] != first[i] {
				t.Fatalf("DCT2D64 not deterministic at index %d on iteration %d", i, n)
			}
		}
	}
}

func TestDCT2D64PanicsOnWrongSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic for wrong input size")
		}
	}()
	_ = DCT2D64(make([]float64, 63))
}
