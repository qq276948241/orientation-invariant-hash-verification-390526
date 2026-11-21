// Copyright 2017 The goimagehash Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package goimagehash

import (
	"bufio"
	"bytes"
	"testing"
)

// legacyImageHashGob is the gob encoding of an ImageHash{Hash: 0x0123456789abcdef,
// Kind: PHash} as produced by the historical Dump implementation. It is a
// captured byte sequence, not regenerated, so it fails if the gob wire format
// or the Kind enum values ever drift and break LoadImageHash compatibility.
var legacyImageHashGob = []byte{
	0x20, 0x7f, 0x03, 0x01, 0x01, 0x01, 0x44, 0x01,
	0xff, 0x80, 0x00, 0x01, 0x02, 0x01, 0x04, 0x48,
	0x61, 0x73, 0x68, 0x01, 0x06, 0x00, 0x01, 0x04,
	0x4b, 0x69, 0x6e, 0x64, 0x01, 0x04, 0x00, 0x00,
	0x00, 0x0f, 0xff, 0x80, 0x01, 0xf8, 0x01, 0x23,
	0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x01, 0x04,
	0x00,
}

// legacyExtImageHashGob is the historical gob frame of
// ExtImageHash{Hash: []uint64{...}, Kind: AHash, Bits: 128}.
var legacyExtImageHashGob = []byte{
	0x2b, 0xff, 0x81, 0x03, 0x01, 0x01, 0x01, 0x44,
	0x01, 0xff, 0x82, 0x00, 0x01, 0x03, 0x01, 0x04,
	0x48, 0x61, 0x73, 0x68, 0x01, 0xff, 0x84, 0x00,
	0x01, 0x04, 0x4b, 0x69, 0x6e, 0x64, 0x01, 0x04,
	0x00, 0x01, 0x04, 0x42, 0x69, 0x74, 0x73, 0x01,
	0x04, 0x00, 0x00, 0x00, 0x16, 0xff, 0x83, 0x02,
	0x01, 0x01, 0x08, 0x5b, 0x5d, 0x75, 0x69, 0x6e,
	0x74, 0x36, 0x34, 0x01, 0xff, 0x84, 0x00, 0x01,
	0x06, 0x00, 0x00, 0x1d, 0xff, 0x82, 0x01, 0x02,
	0xf8, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77,
	0x88, 0xf8, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee,
	0xff, 0x02, 0x01, 0x02, 0x01, 0xfe, 0x01, 0x00,
	0x00,
}

func TestLegacyImageHashGobCompatibility(t *testing.T) {
	h, err := LoadImageHash(bytes.NewReader(legacyImageHashGob))
	if err != nil {
		t.Fatalf("historical ImageHash frame rejected: %v", err)
	}
	if h.GetKind() != PHash || h.GetHash() != 0x0123456789abcdef || h.Bits() != 64 {
		t.Fatalf("decoded legacy hash mismatch: kind=%d hash=%016x bits=%d", h.GetKind(), h.GetHash(), h.Bits())
	}

	// Re-dumping must reproduce the captured bytes exactly.
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := h.Dump(w); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if !bytes.Equal(buf.Bytes(), legacyImageHashGob) {
		t.Fatalf("ImageHash gob output drifted from the historical frame\n got % x\nwant % x",
			buf.Bytes(), legacyImageHashGob)
	}
}

func TestLegacyExtImageHashGobCompatibility(t *testing.T) {
	h, err := LoadExtImageHash(bytes.NewReader(legacyExtImageHashGob))
	if err != nil {
		t.Fatalf("historical ExtImageHash frame rejected: %v", err)
	}
	if h.GetKind() != AHash || h.Bits() != 128 {
		t.Fatalf("decoded legacy ext hash metadata mismatch: kind=%d bits=%d", h.GetKind(), h.Bits())
	}
	want := []uint64{0x1122334455667788, 0x99aabbccddeeff02}
	got := h.GetHash()
	if len(got) != len(want) {
		t.Fatalf("word count %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("word %d = %016x want %016x", i, got[i], want[i])
		}
	}

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	if err := h.Dump(w); err != nil {
		t.Fatal(err)
	}
	w.Flush()
	if !bytes.Equal(buf.Bytes(), legacyExtImageHashGob) {
		t.Fatalf("ExtImageHash gob output drifted from the historical frame\n got % x\nwant % x",
			buf.Bytes(), legacyExtImageHashGob)
	}
}

// TestLegacyStringFormatsUnchanged pins the single-letter string prefixes and
// their round trips; RO hashes use an incompatible, explicitly versioned
// format and must not be parseable through the legacy helpers.
func TestLegacyStringFormatsUnchanged(t *testing.T) {
	cases := []struct {
		s    string
		kind Kind
	}{
		{"a:0000000000000001", AHash},
		{"p:ffffffff00000000", PHash},
		{"d:0123456789abcdef", DHash},
		{"w:fedcba9876543210", WHash},
	}
	for _, tc := range cases {
		h, err := ImageHashFromString(tc.s)
		if err != nil {
			t.Fatalf("legacy string %q rejected: %v", tc.s, err)
		}
		if h.GetKind() != tc.kind || h.ToString() != tc.s {
			t.Fatalf("legacy string round trip broken for %q", tc.s)
		}
	}

	// The RO format must never be misread as a known legacy kind. The legacy
	// parser maps unrecognized single letters to Unknown, which is pinned
	// here rather than changed; the dedicated RO decoder is the only path that
	// recognizes "r".
	roStr := "r:64:862cf9d388205f9f"
	leg, err := ImageHashFromString(roStr)
	if err != nil || leg.GetKind() != Unknown {
		t.Fatalf("legacy parser handling of unknown prefix changed: kind=%d err=%v", leg.GetKind(), err)
	}
	if _, err := ExtImageHashFromString(roStr); err == nil {
		t.Fatal("RO hash string with embedded bit count was accepted by the flat legacy ext parser")
	}
	if _, err := ROImageHashFromString(roStr); err != nil {
		t.Fatalf("dedicated RO parser rejected its own format: %v", err)
	}
}
