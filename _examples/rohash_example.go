// Orientation invariant perceptual hash example.
//
// It computes the canonical rotation/reflection invariant hash of an image
// and shows that the same picture rotated 90 degrees yields the identical
// bit string, while Dump/Load preserve that semantics without any
// rotation-aware distance computation.
//
// Run from the _examples directory:
//
//	go run rohash_example.go sample1.jpg
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"os"

	"github.com/corona10/goimagehash"
)

func main() {
	path := "sample1.jpg"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	file, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		panic(err)
	}

	// Explicit background used for padding and alpha compositing. The hidden
	// RGB of fully transparent pixels never reaches the hash.
	bg := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	const bits = 64 // must be a perfect square; 16, 64, 256 and 1024 are common

	hash, err := goimagehash.OrientationInvariantPerceptionHash(img, bg, bits)
	if err != nil {
		panic(err)
	}
	fmt.Printf("canonical RO hash: %s (%d bits, kind %d)\n", hash.ToString(), hash.Bits(), hash.GetKind())

	// Rotating or mirroring the source only permutes the eight internal
	// candidates, so the canonical output is identical.
	rotated := rotate90(img)
	rotatedHash, err := goimagehash.OrientationInvariantPerceptionHash(rotated, bg, bits)
	if err != nil {
		panic(err)
	}
	distance, err := hash.Distance(rotatedHash)
	if err != nil {
		panic(err)
	}
	fmt.Printf("distance to the 90-degree rotation: %d\n", distance)

	// The orientation semantics live in the stored canonical bits, so a
	// Dump/Load round trip compares as a plain Hamming distance.
	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	if err := hash.Dump(writer); err != nil {
		panic(err)
	}
	writer.Flush()
	loaded, err := goimagehash.LoadROImageHash(bufio.NewReader(&buf))
	if err != nil {
		panic(err)
	}
	postLoadDistance, err := loaded.Distance(rotatedHash)
	if err != nil {
		panic(err)
	}
	fmt.Printf("distance after Dump/Load:            %d\n", postLoadDistance)

	// The textual form embeds the bit count and round trips exactly.
	fromString, err := goimagehash.ROImageHashFromString(hash.ToString())
	if err != nil {
		panic(err)
	}
	fmt.Printf("string round trip:                   %v\n", fromString.ToString() == hash.ToString())
}

// rotate90 returns a 90-degree clockwise rotation without modifying the input.
func rotate90(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	out := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			out.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}
