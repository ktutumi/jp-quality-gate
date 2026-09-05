// Copyright 2026 Jeremy Lilley (jeremy@jlilley.net)
// SPDX-License-Identifier: Apache-2.0
//
// This file includes a Go port of CJClassifier 1.0.5. See
// third_party/cjclassifier for the license, notice, and provenance.
package cj

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ktutumi/jp-quality-gate/internal/embedded"
)

const (
	// Version is the pinned upstream CJClassifier package version.
	Version = "1.0.5"

	cjRangeStart = rune(0x3400)
	cjRangeEnd   = rune(0x9FFF)
	cjRangeSize  = int(cjRangeEnd-cjRangeStart) + 1
	langCount    = 3

	maxLoadFactor = 0.75
	emptyKey      = uint32(0)
)

// BigramMap is the compact open-addressing map used for model bigrams.
// Keys and offsets use four bytes per table slot; probabilities use float32,
// matching Python's array.array('f') storage in cjclassifier 1.0.5.
type BigramMap struct {
	Keys         []uint32
	ValueOffsets []uint32
	Probs        []float32
	Mask         uint32
}

// NewBigramMap creates an empty map with enough capacity for expected entries.
// It is primarily useful for tests and model converters; model parsing uses
// the same 75% load-factor policy as upstream.
func NewBigramMap(expected int) *BigramMap {
	if expected < 0 {
		expected = 0
	}
	capacity := tableSizeFor(expected)
	return &BigramMap{
		Keys:         make([]uint32, capacity),
		ValueOffsets: make([]uint32, capacity),
		Probs:        []float32{0},
		Mask:         uint32(capacity - 1),
	}
}

// GetOffset returns the probability-data offset for a bigram, or zero when it
// is absent. A zero offset is reserved as the absent sentinel.
func (m *BigramMap) GetOffset(c1, c2 rune) uint32 {
	if m == nil || len(m.Keys) == 0 {
		return 0
	}
	key := bigramKey(c1, c2)
	index := mix(key) & m.Mask
	for {
		stored := m.Keys[index]
		if stored == key {
			return m.ValueOffsets[index]
		}
		if stored == emptyKey {
			return 0
		}
		index = (index + 1) & m.Mask
	}
}

// Lookup returns a copy of the three probabilities for a present bigram.
func (m *BigramMap) Lookup(c1, c2 rune) ([langCount]float32, bool) {
	var result [langCount]float32
	offset := m.GetOffset(c1, c2)
	if offset == 0 || int(offset)+langCount > len(m.Probs) {
		return result, false
	}
	copy(result[:], m.Probs[offset:int(offset)+langCount])
	return result, true
}

type bigramMapBuilder struct {
	keys         []uint32
	valueOffsets []uint32
	probData     []float32
	size         int
	mask         uint32
	threshold    int
}

func newBigramMapBuilder(expected int) *bigramMapBuilder {
	capacity := tableSizeFor(expected)
	return &bigramMapBuilder{
		keys:         make([]uint32, capacity),
		valueOffsets: make([]uint32, capacity),
		probData:     []float32{0},
		mask:         uint32(capacity - 1),
		threshold:    int(float64(capacity) * maxLoadFactor),
	}
}

func (b *bigramMapBuilder) put(c1, c2 rune, probs [langCount]float32) {
	key := bigramKey(c1, c2)
	if b.size >= b.threshold {
		b.resize()
	}
	index := mix(key) & b.mask
	for {
		stored := b.keys[index]
		switch stored {
		case emptyKey:
			b.keys[index] = key
			b.valueOffsets[index] = uint32(len(b.probData))
			b.probData = append(b.probData, probs[:]...)
			b.size++
			return
		case key:
			offset := b.valueOffsets[index]
			copy(b.probData[offset:int(offset)+langCount], probs[:])
			return
		default:
			index = (index + 1) & b.mask
		}
	}
}

func (b *bigramMapBuilder) resize() {
	newCapacity := (int(b.mask) + 1) << 1
	oldKeys := b.keys
	oldOffsets := b.valueOffsets
	b.keys = make([]uint32, newCapacity)
	b.valueOffsets = make([]uint32, newCapacity)
	b.mask = uint32(newCapacity - 1)
	b.threshold = int(float64(newCapacity) * maxLoadFactor)
	b.size = 0
	for index, key := range oldKeys {
		if key == emptyKey {
			continue
		}
		b.rehashPut(key, oldOffsets[index])
	}
}

func (b *bigramMapBuilder) rehashPut(key, offset uint32) {
	index := mix(key) & b.mask
	for {
		if b.keys[index] == emptyKey {
			b.keys[index] = key
			b.valueOffsets[index] = offset
			b.size++
			return
		}
		index = (index + 1) & b.mask
	}
}

func (b *bigramMapBuilder) build() *BigramMap {
	return &BigramMap{
		Keys:         b.keys,
		ValueOffsets: b.valueOffsets,
		Probs:        b.probData,
		Mask:         b.mask,
	}
}

func tableSizeFor(expected int) int {
	minCapacity := int(float64(expected)/maxLoadFactor) + 1
	capacity := 1
	for capacity < minCapacity {
		capacity <<= 1
	}
	if capacity < 16 {
		return 16
	}
	return capacity
}

func bigramKey(c1, c2 rune) uint32 {
	return (uint32(c1) << 16) | uint32(c2)
}

// mix is the upstream hash mixer. All operations intentionally wrap at 32
// bits, as they do in Python's & 0xFFFFFFFF implementation.
func mix(key uint32) uint32 {
	key ^= key >> 16
	key *= 0x85EBCA6B
	key ^= key >> 13
	return key
}

// Scores accumulates the model terms and hit counts for one detection.
type Scores struct {
	UnigramScores      []float64
	BigramScores       []float64
	UnigramHitsPerLang []int
	BigramHitsPerLang  []int
	KanaCount          int
	CJCharCount        int
}

// NewScores returns an initialized score accumulator.
func NewScores() *Scores {
	scores := &Scores{}
	scores.ensure()
	return scores
}

func (scores *Scores) ensure() {
	if scores.UnigramScores == nil || len(scores.UnigramScores) != langCount {
		scores.UnigramScores = make([]float64, langCount)
	}
	if scores.BigramScores == nil || len(scores.BigramScores) != langCount {
		scores.BigramScores = make([]float64, langCount)
	}
	if scores.UnigramHitsPerLang == nil || len(scores.UnigramHitsPerLang) != langCount {
		scores.UnigramHitsPerLang = make([]int, langCount)
	}
	if scores.BigramHitsPerLang == nil || len(scores.BigramHitsPerLang) != langCount {
		scores.BigramHitsPerLang = make([]int, langCount)
	}
}

// Clear resets all accumulated values.
func (scores *Scores) Clear() {
	if scores == nil {
		return
	}
	scores.ensure()
	clear(scores.UnigramScores)
	clear(scores.BigramScores)
	clear(scores.UnigramHitsPerLang)
	clear(scores.BigramHitsPerLang)
	scores.KanaCount = 0
	scores.CJCharCount = 0
}

// AnyHits reports whether any unigram matched at least one language.
func (scores *Scores) AnyHits() bool {
	if scores == nil {
		return false
	}
	for _, hits := range scores.UnigramHitsPerLang {
		if hits > 0 {
			return true
		}
	}
	return false
}

// Results contains all model details and the selected language.
type Results struct {
	Scores      Scores
	TotalScores []float64
	Boosts      []float64
	Result      Language
	HasResult   bool
	Gap         float64
}

// NewResults creates an initialized result container.
func NewResults() *Results {
	results := &Results{}
	results.ensure()
	return results
}

func (results *Results) ensure() {
	results.Scores.ensure()
	if results.TotalScores == nil || len(results.TotalScores) != langCount {
		results.TotalScores = make([]float64, langCount)
	}
	if results.Boosts == nil || len(results.Boosts) != langCount {
		results.Boosts = make([]float64, langCount)
	}
}

// Clear resets scores and computed output.
func (results *Results) Clear() {
	if results == nil {
		return
	}
	results.ensure()
	results.Scores.Clear()
	clear(results.TotalScores)
	clear(results.Boosts)
	results.Result = Unknown
	results.HasResult = false
	results.Gap = 0
}

// ToShortString returns the compact scores representation emitted by the
// Python Results.to_short_string().
func (results *Results) ToShortString() string {
	if results == nil || !results.HasResult || results.Result == Unknown {
		return ""
	}
	if results.Scores.KanaCount > 0 && results.Result == Japanese {
		return "ja:1.0,zh-hans:0,zh-hant:0"
	}
	if len(results.TotalScores) < langCount {
		return ""
	}
	order := []int{0, 1, 2}
	sort.SliceStable(order, func(i, j int) bool {
		return results.TotalScores[order[i]] > results.TotalScores[order[j]]
	})
	best := results.TotalScores[order[0]]
	if best == 0 {
		return ""
	}
	parts := make([]string, 0, langCount)
	for _, index := range order {
		ratio := 0.0
		if score := results.TotalScores[index]; score != 0 {
			ratio = best / score
		}
		parts = append(parts, fmt.Sprintf("%s:%.2f", Languages[index].ISOCode(), ratio))
	}
	return strings.Join(parts, ",")
}

// ShortString is an alias for ToShortString.
func (results *Results) ShortString() string { return results.ToShortString() }

// Detection is the compact public view used by the quality-gate scanner.
type Detection struct {
	Language Language
	Gap      float64
	Scores   string
}

// Classifier is a pure-Go port of cjclassifier 1.0.5's model and scorer.
type Classifier struct {
	unigramLogProbs        []float64
	bigramMap              *BigramMap
	defaultLogProb         float64
	toleratedKanaThreshold float64
}

// New constructs a classifier from parsed model data.
func New(unigramLogProbs []float64, bigramMap *BigramMap, defaultLogProb float64) *Classifier {
	if len(unigramLogProbs) != cjRangeSize*langCount {
		padded := make([]float64, cjRangeSize*langCount)
		copy(padded, unigramLogProbs)
		unigramLogProbs = padded
	}
	return &Classifier{
		unigramLogProbs:        unigramLogProbs,
		bigramMap:              bigramMap,
		defaultLogProb:         defaultLogProb,
		toleratedKanaThreshold: 0.01,
	}
}

// DefaultLogProb returns the model's placeholder probability.
func (classifier *Classifier) DefaultLogProb() float64 {
	if classifier == nil {
		return 0
	}
	return classifier.defaultLogProb
}

// ToleratedKanaThreshold returns the kana shortcut threshold.
func (classifier *Classifier) ToleratedKanaThreshold() float64 {
	if classifier == nil {
		return 0
	}
	return classifier.toleratedKanaThreshold
}

// SetToleratedKanaThreshold changes the kana shortcut threshold.
func (classifier *Classifier) SetToleratedKanaThreshold(value float64) {
	if classifier != nil {
		classifier.toleratedKanaThreshold = value
	}
}

// Load loads the embedded cjclassifier model. The loaded model is cached like
// CJClassifier.load()'s bundled singleton.
func Load() (*Classifier, error) {
	embeddedCache.Mutex.Lock()
	defer embeddedCache.Mutex.Unlock()
	if embeddedCache.loaded {
		return embeddedCache.classifier, embeddedCache.err
	}
	embeddedCache.loaded = true
	embeddedCache.classifier, embeddedCache.err = loadGZIPModel(embedded.CJModelGZIP, "bundled:cjlogprobs.gz", 0)
	return embeddedCache.classifier, embeddedCache.err
}

// LoadEmbedded is an explicit spelling for Load.
func LoadEmbedded() (*Classifier, error) { return Load() }

var embeddedCache struct {
	sync.Mutex
	loaded     bool
	classifier *Classifier
	err        error
}

// ClearCachedModels drops the embedded model singleton, primarily for tests.
func ClearCachedModels() {
	embeddedCache.Mutex.Lock()
	defer embeddedCache.Mutex.Unlock()
	embeddedCache.loaded = false
	embeddedCache.classifier = nil
	embeddedCache.err = nil
}

// LoadPath loads a plain-text or .gz model file. The extension behavior is
// intentionally the same case-sensitive check as the Python implementation.
func LoadPath(path string, logProbFloor float64) (*Classifier, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(path, ".gz") {
		return loadGZIPModel(data, path, logProbFloor)
	}
	return ParseModel(bytes.NewReader(data), path, logProbFloor)
}

// LoadFile is a convenience alias using the model's MinLogProb header value.
func LoadFile(path string) (*Classifier, error) { return LoadPath(path, 0) }

func loadGZIPModel(data []byte, label string, logProbFloor float64) (*Classifier, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open gzip model %s: %w", label, err)
	}
	classifier, parseErr := ParseModel(reader, label, logProbFloor)
	closeErr := reader.Close()
	if parseErr != nil {
		return nil, parseErr
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close gzip model %s: %w", label, closeErr)
	}
	return classifier, nil
}

// ParseModel parses the upstream text model format from an uncompressed
// reader. logProbFloor==0 uses the header's MinLogProb field; otherwise the
// greater of the explicit floor and header floor is used.
func ParseModel(reader io.Reader, label string, logProbFloor float64) (*Classifier, error) {
	buffered := bufio.NewReader(reader)
	header, err := buffered.ReadString('\n')
	if err != nil && len(header) == 0 {
		if err == io.EOF {
			return nil, fmt.Errorf("invalid model file (bad header): %s", label)
		}
		return nil, fmt.Errorf("read model header %s: %w", label, err)
	}
	header = strings.TrimSuffix(header, "\n")
	header = strings.TrimSuffix(header, "\r")
	if !strings.HasPrefix(header, "Languages: ") {
		return nil, fmt.Errorf("invalid model file (bad header): %s", label)
	}
	headerParts := strings.Split(header, " ")
	if len(headerParts) < 2 || headerParts[1] == "" {
		return nil, fmt.Errorf("invalid model file (bad header): %s", label)
	}
	langCodes := strings.Split(headerParts[1], ",")
	langMap := make([]Language, len(langCodes))
	for index, code := range langCodes {
		language := ParseLanguage(code)
		if language == Unknown {
			return nil, fmt.Errorf("unknown CJ language in header: %s in %s", code, label)
		}
		langMap[index] = language
	}

	var parsedMinProb *float64
	for index := 0; index+1 < len(headerParts); index++ {
		if headerParts[index] != "MinLogProb:" {
			continue
		}
		value, parseErr := strconv.ParseFloat(headerParts[index+1], 64)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid MinLogProb in header: %s in %s", headerParts[index+1], label)
		}
		parsedMinProb = &value
		break
	}
	defaultLogProb := logProbFloor
	if logProbFloor == 0 {
		if parsedMinProb == nil {
			return nil, fmt.Errorf("no MinLogProb in header and no explicit log_prob_floor: %s", label)
		}
		defaultLogProb = *parsedMinProb
	} else if parsedMinProb != nil && *parsedMinProb > defaultLogProb {
		defaultLogProb = *parsedMinProb
	}

	unigramLogProbs := make([]float64, cjRangeSize*langCount)
	bigramBuilder := newBigramMapBuilder(16 * 1024)
	unigramCount := 0
	bigramCount := 0
	scanner := bufio.NewScanner(buffered)
	// The distributed model's lines are short, but keep the parser robust to
	// synthetic models with a larger vocabulary entry.
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	var probs [langCount]float32
	lineNumber := 1
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		parts := strings.Split(line, " ")
		key := parts[0]
		if len(parts) != len(langMap)+1 {
			return nil, fmt.Errorf("column count mismatch on line: %s in %s", line, label)
		}
		keyRunes := []rune(key)
		switch len(keyRunes) {
		case 1:
			codepoint := keyRunes[0]
			index := int(codepoint - cjRangeStart)
			if index < 0 || index >= cjRangeSize {
				continue
			}
			base := index * langCount
			for column, language := range langMap {
				value, parseErr := strconv.ParseFloat(parts[column+1], 64)
				if parseErr != nil {
					return nil, fmt.Errorf("invalid probability on line %d: %w", lineNumber, parseErr)
				}
				unigramLogProbs[base+int(language)] = maxProbability(value, defaultLogProb)
			}
			unigramCount++
		case 2:
			anyHigher := false
			for column, language := range langMap {
				value, parseErr := strconv.ParseFloat(parts[column+1], 64)
				if parseErr != nil {
					return nil, fmt.Errorf("invalid probability on line %d: %w", lineNumber, parseErr)
				}
				if value < defaultLogProb || value == 0 {
					value = defaultLogProb
				} else {
					anyHigher = true
				}
				probs[int(language)] = float32(value)
			}
			if anyHigher {
				bigramBuilder.put(keyRunes[0], keyRunes[1], probs)
				bigramCount++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read model %s: %w", label, err)
	}
	_ = unigramCount
	_ = bigramCount
	return New(unigramLogProbs, bigramBuilder.build(), defaultLogProb), nil
}

// Detect classifies text and returns only the language, matching the upstream
// CJClassifier.detect() return value.
func (classifier *Classifier) Detect(text string) Language {
	results := NewResults()
	return classifier.DetectInto(text, results)
}

// DetectInto classifies text and populates results with score details.
func (classifier *Classifier) DetectInto(text string, results *Results) Language {
	if results == nil {
		return Unknown
	}
	results.Clear()
	classifier.AddText(text, &results.Scores)
	return classifier.ComputeResult(results)
}

// DetectWithResults returns language, gap, and Python-compatible score text.
func (classifier *Classifier) DetectWithResults(text string) Detection {
	results := NewResults()
	classifier.DetectInto(text, results)
	return Detection{
		Language: results.Result,
		Gap:      results.Gap,
		Scores:   results.ToShortString(),
	}
}

// Classify is an alias for DetectWithResults.
func (classifier *Classifier) Classify(text string) Detection {
	return classifier.DetectWithResults(text)
}

// AddText adds one chunk of text to a score accumulator. Kana is counted but
// does not alter previous-CJK state, matching the upstream loop exactly.
func (classifier *Classifier) AddText(text string, scores *Scores) {
	if classifier == nil || scores == nil {
		return
	}
	scores.ensure()
	if classifier.bigramMap == nil {
		classifier.bigramMap = NewBigramMap(0)
	}
	prev := rune(0)
	prevInRange := false
	for _, codepoint := range text {
		if isKana(codepoint) {
			scores.KanaCount++
			continue
		}
		inRange := inMainCJRange(codepoint)
		if inRange {
			scores.CJCharCount++
			index := int(codepoint - cjRangeStart)
			base := index * langCount
			for language := 0; language < langCount; language++ {
				probability := classifier.unigramLogProbs[base+language]
				scores.UnigramScores[language] += maxProbability(probability, classifier.defaultLogProb)
				if probability != 0 {
					scores.UnigramHitsPerLang[language]++
				}
			}
			if prevInRange {
				offset := classifier.bigramMap.GetOffset(prev, codepoint)
				if offset != 0 && int(offset)+langCount <= len(classifier.bigramMap.Probs) {
					for language := 0; language < langCount; language++ {
						probability := classifier.bigramMap.Probs[int(offset)+language]
						scores.BigramScores[language] += maxProbability(float64(probability), classifier.defaultLogProb)
						if probability != 0 {
							scores.BigramHitsPerLang[language]++
						}
					}
				}
			}
		}
		prev = codepoint
		prevInRange = inRange
	}
}

// ComputeResult selects the best language and computes its upstream gap.
func (classifier *Classifier) ComputeResult(results *Results) Language {
	if classifier == nil || results == nil {
		return Unknown
	}
	results.ensure()
	scores := &results.Scores
	if scores.KanaCount > 0 {
		kanaRatio := float64(scores.KanaCount) / float64(scores.KanaCount+scores.CJCharCount)
		if kanaRatio > classifier.toleratedKanaThreshold {
			results.Result = Japanese
			results.HasResult = true
			results.Gap = 1.0
			results.TotalScores[Japanese] = 1.0
			return results.Result
		}
	}
	if !scores.AnyHits() {
		results.Result = Unknown
		results.HasResult = true
		results.Gap = 0
		return results.Result
	}
	classifier.computeTotals(results, classifier.defaultLogProb)

	bestIndex := 0
	secondIndex := -1
	for language := 1; language < langCount; language++ {
		switch {
		case results.TotalScores[language] > results.TotalScores[bestIndex]:
			secondIndex = bestIndex
			bestIndex = language
		case secondIndex < 0 || results.TotalScores[language] > results.TotalScores[secondIndex]:
			secondIndex = language
		}
	}
	results.Result = Languages[bestIndex]
	results.HasResult = true
	best := results.TotalScores[bestIndex]
	second := best
	if secondIndex >= 0 {
		second = results.TotalScores[secondIndex]
	}
	if second != 0 {
		results.Gap = 1 - (best / second)
	} else {
		results.Gap = 0
	}
	return results.Result
}

func (classifier *Classifier) computeTotals(results *Results, placeholderScore float64) {
	maxUnigram := maxInt(results.Scores.UnigramHitsPerLang)
	maxBigram := maxInt(results.Scores.BigramHitsPerLang)
	for language := 0; language < langCount; language++ {
		results.TotalScores[language] =
			results.Scores.UnigramScores[language] +
				float64(maxUnigram-results.Scores.UnigramHitsPerLang[language])*placeholderScore +
				results.Scores.BigramScores[language] +
				float64(maxBigram-results.Scores.BigramHitsPerLang[language])*placeholderScore
		// Log probabilities are negative; this mirrors upstream's boost math.
		results.TotalScores[language] -= results.Boosts[language] * results.TotalScores[language]
	}
}

func isKana(codepoint rune) bool {
	return (codepoint >= 0x3040 && codepoint <= 0x30FF) ||
		(codepoint >= 0x31F0 && codepoint <= 0x31FF) ||
		(codepoint >= 0xFF65 && codepoint <= 0xFF9F)
}

func inMainCJRange(codepoint rune) bool {
	return codepoint >= cjRangeStart && codepoint <= cjRangeEnd
}

// maxProbability intentionally uses the comparison direction of Python's
// max(a, b), preserving a first NaN if malformed model data contains one.
func maxProbability(value, floor float64) float64 {
	if value < floor {
		return floor
	}
	return value
}

func maxInt(values []int) int {
	result := 0
	for _, value := range values {
		if value > result {
			result = value
		}
	}
	return result
}

func clear[T any](values []T) {
	for index := range values {
		var zero T
		values[index] = zero
	}
}
