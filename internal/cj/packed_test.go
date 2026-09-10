package cj

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
)

func packedFixture(t testing.TB) (*Classifier, []byte) {
	t.Helper()
	c, err := ParseModel(strings.NewReader(syntheticModel), "synthetic", 0)
	if err != nil {
		t.Fatal(err)
	}
	c.unigramLogProbs[0] = math.Copysign(0, -1)
	var b bytes.Buffer
	if err := EncodePacked(&b, c, sha256.Sum256([]byte("source"))); err != nil {
		t.Fatal(err)
	}
	return c, b.Bytes()
}
func TestPackedRoundTrip(t *testing.T) {
	c, data := packedFixture(t)
	d, meta, err := DecodePacked(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := EqualPackedModel(c, d); err != nil {
		t.Fatal(err)
	}
	if meta.FileBytes != uint64(len(data)) {
		t.Fatal(meta)
	}
	var out bytes.Buffer
	if err := EncodePacked(&out, d, meta.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out.Bytes(), data) {
		t.Fatal("non-deterministic encoding")
	}
	for i := range data {
		data[i] = 0
	}
	if err := EqualPackedModel(c, d); err != nil {
		t.Fatal("input alias", err)
	}
}
func rehash(data []byte) {
	clear(data[104:136])
	sum := sha256.Sum256(data)
	copy(data[104:136], sum[:])
}
func TestPackedRejectsCorruption(t *testing.T) {
	_, good := packedFixture(t)
	cases := map[string]func([]byte) []byte{
		"range":           func(b []byte) []byte { b[16] ^= 1; rehash(b); return b },
		"language-count":  func(b []byte) []byte { b[24] = 2; rehash(b); return b },
		"source-checksum": func(b []byte) []byte { b[72] ^= 1; return b },
		"infinity": func(b []byte) []byte {
			binary.LittleEndian.PutUint64(b[32:], math.Float64bits(math.Inf(1)))
			rehash(b)
			return b
		},
		"full-table": func(b []byte) []byte {
			for i := 0; i < 16; i++ {
				binary.LittleEndian.PutUint32(b[136+82944*8+i*4:], uint32(i+1))
				binary.LittleEndian.PutUint32(b[136+82944*8+16*4+i*4:], 1)
			}
			rehash(b)
			return b
		},
		"magic":              func(b []byte) []byte { b[0] ^= 1; return b },
		"version":            func(b []byte) []byte { b[8] = 2; rehash(b); return b },
		"flags":              func(b []byte) []byte { b[12] = 1; rehash(b); return b },
		"language":           func(b []byte) []byte { b[28] = 2; rehash(b); return b },
		"layout":             func(b []byte) []byte { b[68] = 2; rehash(b); return b },
		"short":              func(b []byte) []byte { return b[:100] },
		"truncated":          func(b []byte) []byte { return b[:len(b)-1] },
		"trailing":           func(b []byte) []byte { return append(b, 0) },
		"count":              func(b []byte) []byte { binary.LittleEndian.PutUint32(b[56:], ^uint32(0)); rehash(b); return b },
		"mask":               func(b []byte) []byte { b[52] ^= 1; rehash(b); return b },
		"threshold-checksum": func(b []byte) []byte { b[40] ^= 1; return b },
		"threshold-range":    func(b []byte) []byte { binary.LittleEndian.PutUint64(b[40:], math.Float64bits(2)); rehash(b); return b },
		"nan": func(b []byte) []byte {
			binary.LittleEndian.PutUint64(b[136:], math.Float64bits(math.NaN()))
			rehash(b)
			return b
		},
		"offset": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[136+82944*8+16*4:], ^uint32(0))
			rehash(b)
			return b
		},
		"sentinel": func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[136+82944*8+32*4:], 0x80000000)
			rehash(b)
			return b
		},
		"payload": func(b []byte) []byte { b[len(b)-1] ^= 1; return b },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if c, _, err := DecodePacked(mutate(bytes.Clone(good))); err == nil || c != nil {
				t.Fatal("accepted corrupt model")
			}
		})
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("writer failed") }

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
func TestPackedWriterErrors(t *testing.T) {
	c, _ := packedFixture(t)
	for _, w := range []io.Writer{failWriter{}, shortWriter{}} {
		if EncodePacked(w, c, [32]byte{}) == nil {
			t.Fatal("ignored writer error")
		}
	}
	if EncodePacked(io.Discard, nil, [32]byte{}) == nil {
		t.Fatal("nil accepted")
	}
}
func TestPackedCanonicalParity(t *testing.T) {
	raw, err := os.ReadFile("../embedded/data/cjlogprobs.gz")
	if err != nil {
		t.Fatal(err)
	}
	c, err := loadGZIPModel(raw, "canonical", 0)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("../embedded/data/cjmodel-v1.bin")
	if err != nil {
		t.Fatal(err)
	}
	d, meta, err := DecodePacked(data)
	if err != nil {
		t.Fatal(err)
	}
	if meta.SourceSHA256 != sha256.Sum256(raw) {
		t.Fatal("source SHA mismatch")
	}
	if err := EqualPackedModel(c, d); err != nil {
		t.Fatal(err)
	}
	if err := ValidatePackedModel(d); err != nil {
		t.Fatal(err)
	}
	for i, k := range c.bigramMap.Keys {
		if k == 0 {
			continue
		}
		a := c.bigramMap.GetOffset(rune(k>>16), rune(k&65535))
		b := d.bigramMap.GetOffset(rune(k>>16), rune(k&65535))
		if a != b || a != c.bigramMap.ValueOffsets[i] {
			t.Fatal("lookup mismatch", i)
		}
	}
	corpus := []string{"", "日本語の文章です。", "中华人民共和国", "中華民國", "経済政策検討", "甲あ乙", "甲あ", "甲あいう", "abc😀𠮷\n。", "```\n经済\n```"}
	rng := rand.New(rand.NewSource(9))
	alphabet := []rune("経済政策検討中华人民共和国中華民國あいうえお。\n😀𠮷")
	for i := 0; i < 100; i++ {
		r := make([]rune, rng.Intn(200))
		for j := range r {
			r[j] = alphabet[rng.Intn(len(alphabet))]
		}
		corpus = append(corpus, string(r))
	}
	for _, s := range corpus {
		a, b := NewResults(), NewResults()
		c.DetectInto(s, a)
		d.DetectInto(s, b)
		for _, pair := range [][2][]float64{{a.TotalScores, b.TotalScores}, {a.Boosts, b.Boosts}, {a.Scores.UnigramScores, b.Scores.UnigramScores}, {a.Scores.BigramScores, b.Scores.BigramScores}} {
			for i, v := range pair[0] {
				if math.Float64bits(v) != math.Float64bits(pair[1][i]) {
					t.Fatalf("result bits mismatch %q", s)
				}
			}
		}
		if math.Float64bits(a.Gap) != math.Float64bits(b.Gap) || !reflect.DeepEqual(a, b) || a.ToShortString() != b.ToShortString() {
			t.Fatalf("classification mismatch %q", s)
		}
	}
}

var packedBenchmarkSink *Classifier

func BenchmarkLoadLegacyGZIPModel(b *testing.B) {
	data, err := os.ReadFile("../embedded/data/cjlogprobs.gz")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		packedBenchmarkSink, err = loadGZIPModel(data, "canonical", 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkLoadPackedModel(b *testing.B) {
	data, err := os.ReadFile("../embedded/data/cjmodel-v1.bin")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		packedBenchmarkSink, err = LoadPacked(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
func FuzzDecodePacked(f *testing.F) {
	_, data := packedFixture(f)
	f.Add(data)
	f.Add([]byte("JPQGCJ01"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 1<<20 {
			return
		}
		DecodePacked(b)
		if len(b) >= 136 {
			c := bytes.Clone(b)
			rehash(c)
			DecodePacked(c)
		}
	})
}

func TestPackedTableBoundaries(t *testing.T) {
	for _, count := range []int{0, 1, 12, 13, 40} {
		b := newBigramMapBuilder(0)
		// Force collisions including a chain wrapping from the last slot to zero.
		added := 0
		for k := uint32(1); added < count; k++ {
			if mix(k)&15 != 15 {
				continue
			}
			b.put(rune(k>>16), rune(k&65535), [3]float32{-1, -2, -3})
			b.put(rune(k>>16), rune(k&65535), [3]float32{-4, -5, -6})
			added++
		}
		c := New(make([]float64, cjRangeSize*langCount), b.build(), -10)
		var buf bytes.Buffer
		if err := EncodePacked(&buf, c, [32]byte{}); err != nil {
			t.Fatal(count, err)
		}
		d, _, err := DecodePacked(buf.Bytes())
		if err != nil {
			t.Fatal(count, err)
		}
		if err := EqualPackedModel(c, d); err != nil {
			t.Fatal(count, err)
		}
		if err := ValidatePackedModel(d); err != nil {
			t.Fatal(count, err)
		}
	}
}
func TestPackedDeepValidationRejectsDuplicateOffsets(t *testing.T) {
	b := newBigramMapBuilder(0)
	b.put('甲', '乙', [3]float32{-1, -2, -3})
	b.put('乙', '甲', [3]float32{-4, -5, -6})
	c := New(make([]float64, cjRangeSize*langCount), b.build(), -10)
	for i, k := range c.bigramMap.Keys {
		if k != 0 {
			c.bigramMap.ValueOffsets[i] = 1
		}
	}
	if ValidatePackedModel(c) == nil {
		t.Fatal("duplicate probability block accepted")
	}
}

func TestPackedV1LayoutFixture(t *testing.T) {
	if ChineseSimplified != 0 || ChineseTraditional != 1 || Japanese != 2 {
		t.Fatal("v1 language order changed")
	}
	for key, want := range map[uint32]uint32{0: 0, 1: 0x85efe535, 0x75327559: 0x6970d125, 0xffffffff: 0x3594aca8} {
		if mix(key) != want {
			t.Fatal("v1 mixer changed", key)
		}
	}
	if bigramKey('甲', '乙') != 0x75324e59 {
		t.Fatal("v1 key encoding changed")
	}
}
