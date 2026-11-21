![GitHub Action](https://github.com/corona10/goimagehash/workflows/goimagehash%20workflow/badge.svg)
[![GoDoc](https://godoc.org/github.com/corona10/goimagehash?status.svg)](https://godoc.org/github.com/corona10/goimagehash)
[![Go Report Card](https://goreportcard.com/badge/github.com/corona10/goimagehash)](https://goreportcard.com/report/github.com/corona10/goimagehash)

# goimagehash
> Inspired by [imagehash](https://github.com/JohannesBuchner/imagehash)

A image hashing library written in Go. ImageHash supports:
* [Average hashing](http://www.hackerfactor.com/blog/index.php?/archives/432-Looks-Like-It.html)
* [Difference hashing](http://www.hackerfactor.com/blog/index.php?/archives/529-Kind-of-Like-That.html)
* [Perception hashing](http://www.hackerfactor.com/blog/index.php?/archives/432-Looks-Like-It.html)
* Orientation invariant perception hashing (rotation/reflection canonical hash)
* [Wavelet hashing](https://fullstackml.com/wavelet-image-hash-in-python-3504fdd282b5) [TODO]

## Installation
```
go get github.com/corona10/goimagehash
```
## Special thanks to
* [Haeun Kim](https://github.com/haeungun/)

## Usage

``` Go
func main() {
        file1, _ := os.Open("sample1.jpg")
        file2, _ := os.Open("sample2.jpg")
        defer file1.Close()
        defer file2.Close()

        img1, _ := jpeg.Decode(file1)
        img2, _ := jpeg.Decode(file2)
        hash1, _ := goimagehash.AverageHash(img1)
        hash2, _ := goimagehash.AverageHash(img2)
        distance, _ := hash1.Distance(hash2)
        fmt.Printf("Distance between images: %v\n", distance)

        hash1, _ = goimagehash.DifferenceHash(img1)
        hash2, _ = goimagehash.DifferenceHash(img2)
        distance, _ = hash1.Distance(hash2)
        fmt.Printf("Distance between images: %v\n", distance)
        width, height := 8, 8
        hash3, _ := goimagehash.ExtAverageHash(img1, width, height)
        hash4, _ := goimagehash.ExtAverageHash(img2, width, height)
        distance, _ = hash3.Distance(hash4)
        fmt.Printf("Distance between images: %v\n", distance)
        fmt.Printf("hash3 bit size: %v\n", hash3.Bits())
        fmt.Printf("hash4 bit size: %v\n", hash4.Bits())

        var b bytes.Buffer
        foo := bufio.NewWriter(&b)
        _ = hash4.Dump(foo)
        foo.Flush()
        bar := bufio.NewReader(&b)
        hash5, _ := goimagehash.LoadExtImageHash(bar)
}
```

### Orientation invariant perceptual hash

`OrientationInvariantPerceptionHash` evaluates the four right-angle rotations
and the horizontal mirror of each (eight D4 candidates), normalizes every
candidate onto a square canvas without aspect-ratio stretching, and stores the
lexicographically smallest candidate as the canonical hash. The same content
rotated by 90/180/270 degrees or flipped therefore produces the identical bit
string. The orientation decision is made at hash construction time, so
`Dump`/`Load` keep the semantics without any rotation-aware distance logic.
See `rohash_design.md` for the full geometry and serialization specification.

``` Go
func main() {
        file, _ := os.Open("sample1.jpg")
        defer file.Close()
        img, _, _ := image.Decode(file)

        // Background used for padding and alpha compositing; the hidden RGB
        // of fully transparent pixels never reaches the hash.
        bg := color.RGBA{R: 255, G: 255, B: 255, A: 255}
        hash1, err := goimagehash.OrientationInvariantPerceptionHash(img, bg, 64)
        if err != nil {
                log.Fatal(err)
        }
        hash2, _ := goimagehash.OrientationInvariantPerceptionHash(rotated90(img), bg, 64)
        distance, _ := hash1.Distance(hash2) // 0: orientation invariant

        // Versioned binary frame (magic GOIH; rejects unknown versions,
        // truncated input and length mismatches).
        var b bytes.Buffer
        _ = hash1.Dump(&b)
        loaded, _ := goimagehash.LoadROImageHash(&b)
        fmt.Println(hash1.ToString()) // r:64:...
        parsed, _ := goimagehash.ROImageHashFromString(hash1.ToString())
        _, _ = loaded, parsed
}
```

A runnable example lives in `_examples/rohash_example.go`. The `_examples`
directory contains several standalone programs, so run it as a single file:

```
cd _examples && go run rohash_example.go sample1.jpg
```

An offline, committed golden fixture (`testdata/ro_golden.png`) pins the exact
64-bit output bit string in `TestROGoldenSample`.

## Release Note
### v1.1.0
- The performance of Perceptionhash is enhanced.

### v1.0.3
- Add workflow for GithubAction
- Fix typo on the GoDoc for LoadImageHash

### v1.0.2
- go.mod is now used for install goimagehash

### v1.0.1
- Perception/ExtPerception hash creation times are reduced

### v1.0.0
**IMPORTANT**
goimagehash v1.0.0 does not have compatible with the before version for future features

- More flexible extended hash APIs are provided ([ExtAverageHash](https://godoc.org/github.com/corona10/goimagehash#ExtAverageHash), [ExtPerceptionHash](https://godoc.org/github.com/corona10/goimagehash#ExtPerceptionHash), [ExtDifferenceHash](https://godoc.org/github.com/corona10/goimagehash#ExtDifferenceHash))
- New serialization APIs are provided([ImageHash.Dump](https://godoc.org/github.com/corona10/goimagehash#ImageHash.Dump), [ExtImageHash.Dump](https://godoc.org/github.com/corona10/goimagehash#ExtImageHash.Dump))
- [ExtImageHashFromString](https://godoc.org/github.com/corona10/goimagehash#ExtImageHashFromString), [ImageHashFromString](https://godoc.org/github.com/corona10/goimagehash#ImageHashFromString) is deprecated and will be removed
- New deserialization APIs are provided([LoadImageHash](https://godoc.org/github.com/corona10/goimagehash#LoadImageHash), [LoadExtImageHash](https://godoc.org/github.com/corona10/goimagehash#LoadExtImageHash))
- Bits APIs are provided to measure actual bit size of hash

### v0.3.0
- Support DifferenceHashExtend.
- Support AverageHashExtend.
- Support PerceptionHashExtend by @TokyoWolFrog.

### v0.2.0
- Perception Hash is updated.
- Fix a critical bug of finding median value.

### v0.1.0
- Support Average hashing
- Support Difference hashing
- Support Perception hashing
- Use bits.OnesCount64 for computing Hamming distance by @dominikh
- Support hex serialization methods to ImageHash by @brunoro
