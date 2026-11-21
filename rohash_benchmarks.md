# RO hash benchmark report

Measured on the development machine (darwin/arm64, Apple M4) with
`go test -bench BenchmarkRO -benchmem -count=3`. The benchmark source is a
128x96 asymmetric image, which exercises non-square geometry, background
margins and edge duplication on every candidate.

| Benchmark            | iterations | time/op        | B/op       | allocs/op | Meaning |
|----------------------|-----------:|---------------:|-----------:|----------:|---------|
| BenchmarkROHash      | ~510       | ~2.33 ms       | ~1,232,500 | 33,220    | full public entry point: all 8 D4 candidates, canonical selection, 64-bit output |
| BenchmarkROCandidate | ~4,170     | ~0.29 ms       | ~137,640   | 4,153     | one candidate: virtual canvas, RGBA64 materialization, 64x64 bilinear resize, grayscale, DCT, median, packing |
| BenchmarkROHash256   | ~510       | ~2.33 ms       | ~1,246,500 | 33,219    | same pipeline with a 256-bit output |

## Reading the numbers

* The eight-candidate cost is approximately eight times the single-candidate
  cost; the canonical lexicographic comparison adds negligible work.
* Output bit count (64 vs 256) barely affects time or memory: the dominant
  work is the fixed 64x64 resize and 2-D DCT, shared by every bit length.
* The remaining allocations are almost entirely internal to the pinned
  `nfnt/resize` dependency (per-pass RGBA64 buffers, filter weights and
  interface boxing inside its filter loops). The package's own scratch buffers
  — the 64x64 grayscale grid, the DCT block and the materialized square
  pixel buffer — are allocated once and reused across all eight candidates.
* Measurements are deterministic across repeated runs because every stage
  (candidate order, sampling order, DCT coefficient order, tie breaking) is
  fixed; `go test -count` shows no output drift.

Reproduce locally:

```
go test -run '^$' -bench 'BenchmarkRO' -benchmem -count=3 .
```
