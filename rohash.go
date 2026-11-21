// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/corona10/goimagehash/etcs"
	"github.com/corona10/goimagehash/transforms"
	"github.com/nfnt/resize"
)

// ROHash is the Kind of the rotation/reflection invariant perceptual hash.
// It is appended after the historical kinds so the numeric values of
// Unknown, AHash, PHash, DHash and WHash, and therefore every existing gob
// serialization, remain byte-for-byte stable.
const ROHash Kind = 5

const (
	// roSerializationMagic marks the self-describing binary frame of an
	// ROImageHash. The legacy hashes use gob and have no such marker.
	roSerializationMagic = "GOIH"
	// roSerializationVersion is the only accepted binary format version.
	roSerializationVersion uint16 = 1
	// roDCTSize is the fixed DCT grid side: every candidate is normalized to
	// a 64x64 grayscale square before the deterministic 2-D DCT.
	roDCTSize = 64
	// roMaxBits bounds the requested output length to what the 64x64 DCT grid
	// can supply (k x k with k <= 64).
	roMaxBits = roDCTSize * roDCTSize
)

// ROImageHash is a canonical, orientation invariant perceptual hash.
//
// The hash is computed once, at construction time, from eight D4 candidates
// (identity, 90/180/270 degree rotations and the horizontal mirror of each).
// Rotating or mirroring the source image only permutes those candidates, so
// the same content in any of the eight orientations produces the identical
// bit string. Because the canonical bits are stored on the value itself,
// Dump/Load round trips preserve the orientation semantics: comparisons after
// deserialization are plain Hamming distances and never retry rotations.
//
// ROImageHash is immutable: constructors copy their inputs and GetHash
// returns a copy of the underlying words.
type ROImageHash struct {
	hash []uint64
	kind Kind
	bits int
}

// validateROBits checks the requested output length before any buffer is
// allocated. The length must be a perfect square k*k with 2 <= k <= 64, so the
// top-left k x k block of the 64x64 DCT grid can be sampled deterministically.
func validateROBits(bits int) error {
	if bits < 4 || bits > roMaxBits {
		return fmt.Errorf("bits must be in range [4, %d] and a perfect square, got %d", roMaxBits, bits)
	}
	k := int(math.Sqrt(float64(bits)))
	if k*k != bits {
		return fmt.Errorf("bits must be a perfect square (k*k), got %d", bits)
	}
	return nil
}

// roWordsFor returns the number of 64-bit words needed for bits.
func roWordsFor(bits int) int { return (bits + 63) / 64 }

// roUnusedMask masks the padding bits of the final word. Bits are packed from
// the most significant bit downward, so any unused positions are the low bits.
func roUnusedMask(bits int) uint64 {
	rem := bits % 64
	if rem == 0 {
		return 0
	}
	return uint64(1)<<uint(64-rem) - 1
}

// NewROImageHash creates an ROImageHash from canonical words.
// The kind is forced to ROHash and the slice is copied, so later changes to
// the caller's slice cannot mutate the hash. It panics on an impossible
// combination of bits and words; use it only with values produced by
// OrientationInvariantPerceptionHash or the documented decoders.
func NewROImageHash(hash []uint64, bits int) *ROImageHash {
	if err := validateROBits(bits); err != nil {
		panic("goimagehash: NewROImageHash: " + err.Error())
	}
	want := roWordsFor(bits)
	if len(hash) != want {
		panic(fmt.Sprintf("goimagehash: NewROImageHash: need %d words for %d bits, got %d", want, bits, len(hash)))
	}
	if mask := roUnusedMask(bits); mask != 0 && hash[want-1]&mask != 0 {
		panic("goimagehash: NewROImageHash: unused padding bits of the last word must be zero")
	}
	cp := make([]uint64, len(hash))
	copy(cp, hash)
	return &ROImageHash{hash: cp, kind: ROHash, bits: bits}
}

// Bits returns the declared hash length in bits.
func (h *ROImageHash) Bits() int { return h.bits }

// GetKind returns ROHash.
func (h *ROImageHash) GetKind() Kind { return h.kind }

// GetHash returns a copy of the packed canonical words.
func (h *ROImageHash) GetHash() []uint64 {
	cp := make([]uint64, len(h.hash))
	copy(cp, h.hash)
	return cp
}

// Distance returns the Hamming distance between two orientation invariant
// hashes. Different kinds, different bit counts or differently sized word
// vectors are rejected with an error instead of being compared, so a mismatch
// can never be mistaken for a valid distance.
func (h *ROImageHash) Distance(other *ROImageHash) (int, error) {
	if other == nil {
		return -1, errNoOther
	}
	if h.GetKind() != other.GetKind() {
		return -1, errors.New("RO image hashes's kind should be identical")
	}
	if h.Bits() != other.Bits() {
		return -1, fmt.Errorf("RO image hash should have an identical bit size but got %v vs %v", h.Bits(), other.Bits())
	}
	lHash, rHash := h.GetHash(), other.GetHash()
	if len(lHash) != len(rHash) {
		return -1, errors.New("RO image hashes's size should be identical")
	}
	distance := 0
	for idx, lh := range lHash {
		distance += popcnt(lh ^ rHash[idx])
	}
	return distance, nil
}

// OrientationInvariantPerceptionHash computes the canonical orientation
// invariant perceptual hash of img.
//
// bg is the background color used for every padding pixel and for alpha
// compositing: source pixels are composited over bg before grayscale
// conversion, so the hidden RGB channels of fully transparent pixels can
// never affect the result. bits selects the output length; it must be a
// perfect square k*k with 2 <= k <= 64 (for example 16, 64 or 256).
//
// All validation (nil image, nil background, empty bounds, illegal bits,
// dimension overflow) happens before any working buffer is allocated. The
// source image is never modified and repeated calls return identical output.
func OrientationInvariantPerceptionHash(img image.Image, bg color.Color, bits int) (*ROImageHash, error) {
	if img == nil {
		return nil, errors.New("image object can not be nil")
	}
	if bg == nil {
		return nil, errors.New("background color can not be nil")
	}
	if err := validateROBits(bits); err != nil {
		return nil, err
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("image bounds must contain at least one pixel")
	}
	// Reject dimensions that exceed the supported canvas side before any
	// buffer is allocated. roMaxCanvasSide is far below MaxInt, so the later
	// side*side*8 multiplication cannot overflow once this passes.
	if w > roMaxCanvasSide || h > roMaxCanvasSide {
		return nil, fmt.Errorf("image dimension %dx%d exceeds the supported canvas side %d", w, h, roMaxCanvasSide)
	}

	k := int(math.Sqrt(float64(bits)))
	words := roWordsFor(bits)

	var gray [roDCTSize * roDCTSize]float64 // reused for every candidate
	block := make([]float64, bits)          // low-frequency block, reused
	canvasBuf := make([]byte, 0)            // materialized square, reused
	var best []uint64

	for t := roTransform(0); t < roNumTransforms; t++ {
		candidate, err := roCandidateHash(img, bg, t, k, &gray, block, words, &canvasBuf)
		if err != nil {
			return nil, err
		}

		// Canonical rule: lexicographically smallest word vector wins.
		// The loop visits candidates in fixed transform order, so equal
		// candidates deterministically keep the earliest index.
		if best == nil || roLexLess(candidate, best) {
			best = candidate
		}
	}

	return &ROImageHash{hash: best, kind: ROHash, bits: bits}, nil
}

// roCandidateHash computes the raw perceptual bit vector of one D4 candidate.
// gray, block and canvasBuf are caller-owned scratch buffers reused across
// candidates so the eight-iteration loop only allocates the resized image
// and the result words. The bit order is fixed: row-major DCT low-frequency
// coefficients, bit 0 in the most significant bit of the first word.
func roCandidateHash(img image.Image, bg color.Color, t roTransform, k int, gray *[roDCTSize * roDCTSize]float64, block []float64, words int, canvasBuf *[]byte) ([]uint64, error) {
	canvas := newROSquareCanvas(img, t, bg)

	// Materialize the virtual square once into a reused RGBA64 buffer.
	// resize.Resize keeps a specialized RGBA64 path for concrete images;
	// feeding it the virtual image.Image instead would box a color on every
	// At call.
	side := canvas.side
	pixLen := side * side * 8
	if side > 0 && pixLen/8/side != side {
		return nil, errors.New("ro hash: canvas size overflows the pixel buffer")
	}
	if cap(*canvasBuf) < pixLen {
		*canvasBuf = make([]byte, pixLen)
	}
	materialized := &image.RGBA64{
		Pix:    (*canvasBuf)[:pixLen],
		Stride: side * 8,
		Rect:   image.Rect(0, 0, side, side),
	}
	pix := materialized.Pix
	stride := materialized.Stride
	for y := 0; y < side; y++ {
		row := pix[y*stride:]
		for x := 0; x < side; x++ {
			c := canvas.atRGBA64(x, y)
			off := x * 8
			binary.BigEndian.PutUint16(row[off:off+2], c.R)
			binary.BigEndian.PutUint16(row[off+2:off+4], c.G)
			binary.BigEndian.PutUint16(row[off+4:off+6], c.B)
			binary.BigEndian.PutUint16(row[off+6:off+8], c.A)
		}
	}
	resized := resize.Resize(roDCTSize, roDCTSize, materialized, resize.Bilinear)

	// Deterministic row-major grayscale sampling. The canvas emits
	// premultiplied 16-bit values; un-premultiply before luminosity so
	// semi-transparent content is measured against straight color.
	rb := resized.Bounds()
	for y := rb.Min.Y; y < rb.Max.Y; y++ {
		for x := rb.Min.X; x < rb.Max.X; x++ {
			r16, g16, b16, a16 := resized.At(x, y).RGBA()
			if a16 != 0 && a16 != 0xffff {
				r16 = r16 * 0xffff / a16
				g16 = g16 * 0xffff / a16
				b16 = b16 * 0xffff / a16
			}
			gray[(y-rb.Min.Y)*roDCTSize+(x-rb.Min.X)] =
				0.299*float64(r16/257) + 0.587*float64(g16/257) + 0.114*float64(b16/257)
		}
	}

	coeffs := transforms.DCT2D64(gray[:])

	// Low-frequency block in the same row-major order the bit index uses.
	for y := 0; y < k; y++ {
		copy(block[y*k:(y+1)*k], coeffs[y*roDCTSize:y*roDCTSize+k])
	}
	// MedianOfPixels copies its input, so block is not mutated and remains
	// valid for the coefficient comparison below.
	median := etcs.MedianOfPixels(block)

	candidate := make([]uint64, words)
	for idx, p := range block {
		if p > median {
			candidate[idx/64] |= 1 << uint(63-idx%64)
		}
	}
	return candidate, nil
}

// roLexLess reports whether a is lexicographically smaller than b for
// equal-length unsigned word slices.
func roLexLess(a, b []uint64) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// --- versioned binary serialization ----------------------------------------
//
// Frame layout (all integers big-endian):
//
//	offset  size  field
//	0       4     magic "GOIH"
//	4       2     version (currently 1)
//	6       1     kind (ROHash)
//	7       4     bits
//	11      4     number of uint64 words
//	15      8*n   words, most significant word first
//
// Unknown versions, wrong magic/kind, truncated input, declared lengths that
// disagree with the payload and non-zero unused bits are all rejected.

type roBinaryHeader struct {
	Magic   [4]byte
	Version uint16
	Kind    uint8
	Bits    uint32
	NWords  uint32
}

// Dump writes the versioned binary serialization of the hash to w.
func (h *ROImageHash) Dump(w io.Writer) error {
	if w == nil {
		return errors.New("writer can not be nil")
	}
	header := roBinaryHeader{
		Version: roSerializationVersion,
		Kind:    uint8(ROHash),
		Bits:    uint32(h.bits),
		NWords:  uint32(len(h.hash)),
	}
	copy(header.Magic[:], roSerializationMagic)
	if err := binary.Write(w, binary.BigEndian, &header); err != nil {
		return err
	}
	for _, word := range h.hash {
		if err := binary.Write(w, binary.BigEndian, word); err != nil {
			return err
		}
	}
	return nil
}

// LoadROImageHash decodes an ROImageHash previously written by Dump.
func LoadROImageHash(r io.Reader) (*ROImageHash, error) {
	if r == nil {
		return nil, errors.New("reader can not be nil")
	}
	br := bufio.NewReader(r)

	var header roBinaryHeader
	if err := binary.Read(br, binary.BigEndian, &header); err != nil {
		return nil, fmt.Errorf("ro hash: reading header: %v", err)
	}
	if string(header.Magic[:]) != roSerializationMagic {
		return nil, errors.New("ro hash: bad magic, not an orientation invariant hash frame")
	}
	if header.Version != roSerializationVersion {
		return nil, fmt.Errorf("ro hash: unsupported serialization version %d, want %d", header.Version, roSerializationVersion)
	}
	if Kind(header.Kind) != ROHash {
		return nil, fmt.Errorf("ro hash: unexpected kind %d, want %d", header.Kind, ROHash)
	}
	bits := int(header.Bits)
	if err := validateROBits(bits); err != nil {
		return nil, fmt.Errorf("ro hash: %v", err)
	}
	nWords := int(header.NWords)
	if nWords <= 0 || nWords != roWordsFor(bits) {
		return nil, fmt.Errorf("ro hash: declared word count %d does not match %d bits", nWords, bits)
	}

	words := make([]uint64, nWords)
	for i := range words {
		if err := binary.Read(br, binary.BigEndian, &words[i]); err != nil {
			return nil, fmt.Errorf("ro hash: truncated payload at word %d: %v", i, err)
		}
	}
	if mask := roUnusedMask(bits); mask != 0 && words[nWords-1]&mask != 0 {
		return nil, errors.New("ro hash: unused padding bits of the last word must be zero")
	}

	// Reject trailing bytes so a wrong-length frame can never parse as valid.
	if extra, err := br.ReadByte(); err == nil {
		return nil, fmt.Errorf("ro hash: unexpected trailing byte 0x%02x after payload", extra)
	} else if err != io.EOF {
		return nil, fmt.Errorf("ro hash: checking frame end: %v", err)
	}

	return &ROImageHash{hash: words, kind: ROHash, bits: bits}, nil
}

// --- string serialization ---------------------------------------------------
//
// Format: "r:<bits>:<hex>" where hex is the big-endian concatenation of the
// words (two hex characters per byte). The embedded bit count makes a
// truncated or wrong-length encoding detectable without guessing.

const roStrPrefix = "r"

// ToString returns the stable textual representation of the hash.
func (h *ROImageHash) ToString() string {
	var buf bytes.Buffer
	for _, word := range h.hash {
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], word)
		buf.Write(b[:])
	}
	return fmt.Sprintf("%s:%d:%s", roStrPrefix, h.bits, hex.EncodeToString(buf.Bytes()))
}

// ROImageHashFromString parses the output of ToString. Unknown prefixes,
// malformed numbers, non-hex payloads and length mismatches are rejected.
func ROImageHashFromString(s string) (*ROImageHash, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 || parts[0] != roStrPrefix || parts[1] == "" || parts[2] == "" {
		return nil, errors.New("couldn't parse RO image hash string: " + s)
	}
	bits, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, errors.New("couldn't parse RO image hash bit count: " + parts[1])
	}
	if err := validateROBits(bits); err != nil {
		return nil, fmt.Errorf("ro hash: %v", err)
	}
	raw, err := hex.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("ro hash: malformed hex payload: %v", err)
	}
	nWords := roWordsFor(bits)
	if len(raw) != nWords*8 {
		return nil, fmt.Errorf("ro hash: hex payload length %d does not match %d bits", len(raw)*8, bits)
	}
	words := make([]uint64, nWords)
	for i := range words {
		words[i] = binary.BigEndian.Uint64(raw[i*8 : i*8+8])
	}
	if mask := roUnusedMask(bits); mask != 0 && words[nWords-1]&mask != 0 {
		return nil, errors.New("ro hash: unused padding bits of the last word must be zero")
	}
	return &ROImageHash{hash: words, kind: ROHash, bits: bits}, nil
}
