// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package transforms

// DCT2D64 computes an unscaled DCT-II (type 2) over a square 64x64 grid.
//
// The input is a row-major slice of length 64*64 and the result is returned
// as a new row-major [64 * 64]float64 array. The transform is fully
// deterministic: it runs single-threaded in a fixed row/column order and uses
// the same static Byeong Gi Lee (1984) DCT tables as the rest of the package.
// The input slice is never modified, which lets callers reuse it for every
// orientation candidate.
func DCT2D64(input []float64) [64 * 64]float64 {
	if len(input) != 64*64 {
		panic("incorrect input size, wanted 64x64.")
	}

	var output [64 * 64]float64

	// Transform every row.
	for y := 0; y < 64; y++ {
		row := output[y*64 : y*64+64]
		copy(row, input[y*64:y*64+64])
		forwardDCT64(row)
	}

	// Transform every column, reusing a single scratch row.
	var col [64]float64
	for x := 0; x < 64; x++ {
		for y := 0; y < 64; y++ {
			col[y] = output[y*64+x]
		}
		forwardDCT64(col[:])
		for y := 0; y < 64; y++ {
			output[y*64+x] = col[y]
		}
	}

	return output
}
