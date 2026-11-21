// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import "github.com/corona10/goimagehash/transforms"

// transformsDCT2D64 is a thin test shim around the production deterministic
// DCT so the reference DCT test does not import the transforms package twice.
func transformsDCT2D64(gray []float64) [64 * 64]float64 {
	return transforms.DCT2D64(gray)
}

// roGolden64 is the exact canonical 64-bit RO hash bit string produced for
// testdata/ro_golden.png against an opaque white background. It is generated
// once from the committed fixture and pinned here so any change in geometry,
// compositing, DCT ordering, median rule or canonical selection fails this
// exact-string test.
const roGolden64 = "r:64:862cf9d388205f9f"
