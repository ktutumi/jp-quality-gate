// Copyright 2026 Jeremy Lilley (jeremy@jlilley.net)
// SPDX-License-Identifier: Apache-2.0
// Packed representation of the CJClassifier 1.0.5 arrays; see third_party/cjclassifier.
package cj

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const PackedHeaderBytes = 136

// PackedMaxBytes limits allocation, not total Worker memory usage.
const PackedMaxBytes = 32 << 20
const PackedParserRevision = 1

// PackedMetadata identifies the fixed v1 layout. Hashes refer to bytes, not text.
type PackedMetadata struct {
	FormatVersion   uint32   `json:"format_version"`
	LanguageOrderID uint32   `json:"language_order_id"`
	TableLayoutID   uint32   `json:"table_layout_id"`
	UnigramCount    uint32   `json:"unigram_count"`
	KeysCount       uint32   `json:"keys_count"`
	OffsetsCount    uint32   `json:"offsets_count"`
	ProbsCount      uint32   `json:"probs_count"`
	Mask            uint32   `json:"mask"`
	Occupied        uint32   `json:"occupied_bigram_entries"`
	PayloadBytes    uint64   `json:"payload_bytes"`
	FileBytes       uint64   `json:"packed_bytes"`
	SourceSHA256    [32]byte `json:"-"`
	ContentSHA256   [32]byte `json:"-"`
}

func packedLength(u, k, o, p uint32) (uint64, error) {
	// uint32 counts widened before arithmetic: the sum cannot overflow uint64.
	n := uint64(u)*8 + uint64(k)*4 + uint64(o)*4 + uint64(p)*4
	if n+PackedHeaderBytes > PackedMaxBytes || n+PackedHeaderBytes > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("packed model exceeds allocation budget")
	}
	if u != uint32(cjRangeSize*langCount) || k != o || k < 16 || k&(k-1) != 0 || p < 1 {
		return 0, fmt.Errorf("invalid packed counts")
	}
	return n, nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func validatePacked(c *Classifier, deep bool) (uint32, error) {
	if c == nil || c.bigramMap == nil {
		return 0, fmt.Errorf("nil packed model")
	}
	m := c.bigramMap
	for _, n := range []int{len(c.unigramLogProbs), len(m.Keys), len(m.ValueOffsets), len(m.Probs)} {
		if uint64(n) > math.MaxUint32 {
			return 0, fmt.Errorf("count overflow")
		}
	}
	if _, err := packedLength(uint32(len(c.unigramLogProbs)), uint32(len(m.Keys)), uint32(len(m.ValueOffsets)), uint32(len(m.Probs))); err != nil {
		return 0, err
	}
	if m.Mask != uint32(len(m.Keys)-1) || math.Float32bits(m.Probs[0]) != 0 || !finite(c.defaultLogProb) || !finite(c.toleratedKanaThreshold) || c.toleratedKanaThreshold < 0 || c.toleratedKanaThreshold > 1 {
		return 0, fmt.Errorf("invalid mask, sentinel, or scalar")
	}
	for _, v := range c.unigramLogProbs {
		if !finite(v) {
			return 0, fmt.Errorf("non-finite unigram")
		}
	}
	for _, v := range m.Probs {
		if !finite(float64(v)) {
			return 0, fmt.Errorf("non-finite bigram")
		}
	}
	var occupied uint32
	for i, k := range m.Keys {
		o := m.ValueOffsets[i]
		if k == 0 {
			if o != 0 {
				return 0, fmt.Errorf("empty key has offset")
			}
			continue
		}
		occupied++
		if o < 1 || (o-1)%3 != 0 || uint64(o)+3 > uint64(len(m.Probs)) {
			return 0, fmt.Errorf("invalid probability offset")
		}
	}
	if uint64(occupied)*4 > uint64(len(m.Keys))*3 || uint64(len(m.Probs)) != 1+3*uint64(occupied) {
		return 0, fmt.Errorf("invalid table occupancy")
	}
	if deep {
		keys := make(map[uint32]bool, occupied)
		offsets := make(map[uint32]bool, occupied)
		for i, k := range m.Keys {
			if k == 0 {
				continue
			}
			o := m.ValueOffsets[i]
			if keys[k] || offsets[o] {
				return 0, fmt.Errorf("duplicate key or probability block")
			}
			keys[k] = true
			offsets[o] = true
			// Check reachability without an unbounded probe on adversarial tables.
			index := mix(k) & m.Mask
			for steps := 0; ; steps++ {
				if steps >= len(m.Keys) || m.Keys[index] == 0 {
					return 0, fmt.Errorf("unreachable key")
				}
				if index == uint32(i) {
					break
				}
				index = (index + 1) & m.Mask
			}
		}
	}
	return occupied, nil
}

// ValidatePackedModel performs generator/CI semantic checks, including reachability.
// Runtime decoding deliberately uses linear safety checks without these maps.
func ValidatePackedModel(c *Classifier) error { _, err := validatePacked(c, true); return err }

// EqualPackedModel compares every logical element, including signed zero bits.
func EqualPackedModel(a, b *Classifier) error {
	if a == nil || b == nil || a.bigramMap == nil || b.bigramMap == nil {
		return fmt.Errorf("nil model")
	}
	if math.Float64bits(a.defaultLogProb) != math.Float64bits(b.defaultLogProb) || math.Float64bits(a.toleratedKanaThreshold) != math.Float64bits(b.toleratedKanaThreshold) || a.bigramMap.Mask != b.bigramMap.Mask {
		return fmt.Errorf("scalar mismatch")
	}
	if len(a.unigramLogProbs) != len(b.unigramLogProbs) || len(a.bigramMap.Keys) != len(b.bigramMap.Keys) || len(a.bigramMap.ValueOffsets) != len(b.bigramMap.ValueOffsets) || len(a.bigramMap.Probs) != len(b.bigramMap.Probs) {
		return fmt.Errorf("array length mismatch")
	}
	for i, v := range a.unigramLogProbs {
		if math.Float64bits(v) != math.Float64bits(b.unigramLogProbs[i]) {
			return fmt.Errorf("unigram mismatch at %d", i)
		}
	}
	for i, v := range a.bigramMap.Keys {
		if v != b.bigramMap.Keys[i] || a.bigramMap.ValueOffsets[i] != b.bigramMap.ValueOffsets[i] {
			return fmt.Errorf("table mismatch at %d", i)
		}
	}
	for i, v := range a.bigramMap.Probs {
		if math.Float32bits(v) != math.Float32bits(b.bigramMap.Probs[i]) {
			return fmt.Errorf("bigram mismatch at %d", i)
		}
	}
	return nil
}

func writePacked(w io.Writer, header []byte, c *Classifier) error {
	out := bufio.NewWriterSize(w, 32*1024)
	if _, err := out.Write(header); err != nil {
		return err
	}
	var buf [8]byte
	put := func(v uint64, n int) error {
		binary.LittleEndian.PutUint64(buf[:], v)
		_, err := out.Write(buf[:n])
		return err
	}
	for _, v := range c.unigramLogProbs {
		if err := put(math.Float64bits(v), 8); err != nil {
			return err
		}
	}
	for _, array := range [][]uint32{c.bigramMap.Keys, c.bigramMap.ValueOffsets} {
		for _, v := range array {
			if err := put(uint64(v), 4); err != nil {
				return err
			}
		}
	}
	for _, v := range c.bigramMap.Probs {
		if err := put(uint64(math.Float32bits(v)), 4); err != nil {
			return err
		}
	}
	return out.Flush()
}

// EncodePacked writes deterministic little-endian v1 bytes in two passes.
func EncodePacked(w io.Writer, c *Classifier, source [32]byte) error {
	if err := ValidatePackedModel(c); err != nil {
		return err
	}
	var h [PackedHeaderBytes]byte
	copy(h[:], "JPQGCJ01")
	put := func(off int, v uint32) { binary.LittleEndian.PutUint32(h[off:], v) }
	put(8, 1)
	put(16, uint32(cjRangeStart))
	put(20, uint32(cjRangeEnd))
	put(24, langCount)
	put(28, 1)
	binary.LittleEndian.PutUint64(h[32:], math.Float64bits(c.defaultLogProb))
	binary.LittleEndian.PutUint64(h[40:], math.Float64bits(c.toleratedKanaThreshold))
	put(48, uint32(len(c.unigramLogProbs)))
	put(52, c.bigramMap.Mask)
	put(56, uint32(len(c.bigramMap.Keys)))
	put(60, uint32(len(c.bigramMap.ValueOffsets)))
	put(64, uint32(len(c.bigramMap.Probs)))
	put(68, 1)
	copy(h[72:104], source[:])
	hash := sha256.New()
	if err := writePacked(hash, h[:], c); err != nil {
		return err
	}
	copy(h[104:], hash.Sum(nil))
	return writePacked(w, h[:], c)
}

// DecodePacked safely copies trusted embedded data. This is not an API for
// accepting arbitrary user models; canonical parity supplies semantic trust.
func DecodePacked(data []byte) (*Classifier, PackedMetadata, error) {
	var meta PackedMetadata
	fail := func(reason string) (*Classifier, PackedMetadata, error) {
		return nil, PackedMetadata{}, fmt.Errorf("invalid packed model: %s", reason)
	}
	if len(data) < PackedHeaderBytes {
		return fail("short header")
	}
	get := func(o int) uint32 { return binary.LittleEndian.Uint32(data[o:]) }
	if string(data[:8]) != "JPQGCJ01" || get(8) != 1 || get(12) != 0 || get(16) != uint32(cjRangeStart) || get(20) != uint32(cjRangeEnd) || get(24) != langCount || get(28) != 1 || get(68) != 1 {
		return fail("unsupported header")
	}
	n, err := packedLength(get(48), get(56), get(60), get(64))
	if err != nil {
		return fail(err.Error())
	}
	if uint64(len(data)) != PackedHeaderBytes+n || get(52) != get(56)-1 {
		return fail("length or mask")
	}
	hash := sha256.New()
	hash.Write(data[:104])
	var zeros [32]byte
	hash.Write(zeros[:])
	hash.Write(data[136:])
	var sum [32]byte
	copy(sum[:], hash.Sum(nil))
	var expected [32]byte
	copy(expected[:], data[104:136])
	if sum != expected {
		return fail("checksum")
	}
	c := &Classifier{unigramLogProbs: make([]float64, int(get(48))), bigramMap: &BigramMap{Keys: make([]uint32, int(get(56))), ValueOffsets: make([]uint32, int(get(60))), Probs: make([]float32, int(get(64))), Mask: get(52)}, defaultLogProb: math.Float64frombits(binary.LittleEndian.Uint64(data[32:])), toleratedKanaThreshold: math.Float64frombits(binary.LittleEndian.Uint64(data[40:]))}
	pos := 136
	for i := range c.unigramLogProbs {
		c.unigramLogProbs[i] = math.Float64frombits(binary.LittleEndian.Uint64(data[pos:]))
		pos += 8
	}
	for _, a := range [][]uint32{c.bigramMap.Keys, c.bigramMap.ValueOffsets} {
		for i := range a {
			a[i] = binary.LittleEndian.Uint32(data[pos:])
			pos += 4
		}
	}
	for i := range c.bigramMap.Probs {
		c.bigramMap.Probs[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[pos:]))
		pos += 4
	}
	occupied, err := validatePacked(c, false)
	if err != nil {
		return fail(err.Error())
	}
	meta = PackedMetadata{FormatVersion: 1, LanguageOrderID: 1, TableLayoutID: 1, UnigramCount: get(48), KeysCount: get(56), OffsetsCount: get(60), ProbsCount: get(64), Mask: get(52), Occupied: occupied, PayloadBytes: n, FileBytes: n + 136, ContentSHA256: sum}
	copy(meta.SourceSHA256[:], data[72:104])
	return c, meta, nil
}
func LoadPacked(data []byte) (*Classifier, error) { c, _, err := DecodePacked(data); return c, err }
