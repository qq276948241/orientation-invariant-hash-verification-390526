# Orientation invariant perceptual hash

This document describes the algorithm and the compatibility guarantees behind
`OrientationInvariantPerceptionHash` and the `ROImageHash` type.

## Problem statement

The existing `AverageHash`, `DifferenceHash` and `PerceptionHash` encode the
pixels in their given orientation: the same picture rotated by 90/180/270
degrees or mirrored produces a different bit string. Trying rotations only
inside `Distance` cannot work, because after `Dump`/`Load` only the bits
remain — there is no image left to rotate. The orientation semantics must
therefore be settled once, while the source image is available, and the
canonical result must be stored on the hash itself.

## Entry point

```go
func OrientationInvariantPerceptionHash(img image.Image, bg color.Color, bits int) (*ROImageHash, error)
```

* `img` is any `image.Image`; a non-zero `Bounds()` origin is normalized away.
* `bg` is the explicit background color used for every padding pixel and for
  alpha compositing. It is required (nil is rejected).
* `bits` is the output length and must be a perfect square `k*k` with
  `2 <= k <= 64` (4, 9, 16, 25, 36, 49, 64, 100, ..., 4096). All argument
  validation happens before any working buffer is allocated.

## Geometry

1. Let the source content be `w x h`. The square canvas side is
   `M = max(w, h)`; content is never stretched to a fixed aspect ratio.
2. The transformed content rectangle is centered on the canvas using
   `start = floor((M-length)/2)` on each axis.
3. When an axis delta is even, both margins are equal and filled with the
   caller supplied background. When a delta is odd, the two margins differ by
   one pixel: the smaller side gets the background margin and the single extra
   slot on the other side duplicates the nearest content row/column. Every
   normalized coordinate therefore maps to a source coordinate by clamping.

The clamp convention makes the normalization commute with the dihedral group
D4: transform-then-normalize equals normalize-then-transform for all eight
transforms. Consequently rotating or mirroring the source only permutes the
eight candidate canvases, which is what makes the canonical selection exactly
orientation invariant — including for odd dimensions, single rows/columns and
extreme aspect ratios.

## Eight candidates

For each of the fixed-order transforms

| index | transform          |
|-------|--------------------|
| 0     | identity           |
| 1     | rotate 90 CW       |
| 2     | rotate 180         |
| 3     | rotate 270 CW      |
| 4     | horizontal flip    |
| 5     | flip + rotate 90   |
| 6     | flip + rotate 180  |
| 7     | flip + rotate 270  |

the pipeline is:

1. Build the virtual square canvas (no pixel copy; `At` maps coordinates back
   through the inverse transform).
2. Materialize it once into an `RGBA64` buffer and bilinear-resize to a
   deterministic 64x64 grid.
3. Composite every pixel over `bg` *before* grayscale conversion. Fully
   transparent pixels collapse to the background, so their hidden RGB values
   cannot influence the hash. Luminosity uses the same weights as the rest of
   the package (`0.299R + 0.587G + 0.114B`), sampled in fixed row-major order.
4. Apply the deterministic single-threaded 64x64 DCT-II (`transforms.DCT2D64`)
   and read the top-left `k x k` low-frequency block in row-major order.
5. Threshold against the median of the block (`p > median`, strict) and pack
   bits from the most significant bit of the first word downward.

## Canonical selection and tie breaking

The canonical hash is the lexicographically smallest candidate word vector.
Candidates are evaluated in the fixed transform order above and the first
minimum is kept, so equal candidates (for example the eight identical
candidates of a flat image) have a stable, reproducible winner. The output for
a given input is identical on every call and every machine.

## Serialization compatibility

* `ROHash` is appended as `Kind = 5`; the historical values
  `Unknown=0, AHash=1, PHash=2, DHash=3, WHash=4` are untouched, so legacy gob
  frames remain byte-for-byte decodable and re-encodable.
* `ROImageHash.Dump` writes a self-describing, big-endian, versioned frame
  (magic `GOIH`, version, kind, bit count, word count, words).
  `LoadROImageHash` rejects bad magic, unknown versions, wrong kind, truncated
  input, declared lengths that disagree with the payload, non-zero padding
  bits and trailing bytes.
* The string form is `r:<bits>:<hex>` with the bit count embedded;
  `ROImageHashFromString` rejects every malformed or wrong-length input.
* `Distance` only accepts two `ROImageHash` values with identical kind and bit
  count; cross-algorithm or cross-size comparisons return an error rather than
  a number.
