package unihan

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// UnicodeURL is the official source URL template used by the builder CLI.
const UnicodeURL = "https://www.unicode.org/Public/%s/ucd/Unihan.zip"

var (
	codepointRE = regexp.MustCompile(`U\+([0-9A-F]{4,6})`)

	japaneseEvidence = map[string]struct{}{
		"kIRG_JSource":        {},
		"kJis0":               {},
		"kJis1":               {},
		"kJIS0213":            {},
		"kJoyoKanji":          {},
		"kJinmeiyoKanji":      {},
		"kIBMJapan":           {},
		"kMojiJoho":           {},
		"kRSAdobe_Japan1_6":   {},
		"kJapanese":           {},
		"kJapaneseKun":        {},
		"kJapaneseOn":         {},
		"kJapaneseNewVariant": {},
		"kJapaneseOldVariant": {},
	}

	interestingFields = map[string]struct{}{
		"kIRG_JSource":        {},
		"kJis0":               {},
		"kJis1":               {},
		"kJIS0213":            {},
		"kJoyoKanji":          {},
		"kJinmeiyoKanji":      {},
		"kIBMJapan":           {},
		"kMojiJoho":           {},
		"kRSAdobe_Japan1_6":   {},
		"kJapanese":           {},
		"kJapaneseKun":        {},
		"kJapaneseOn":         {},
		"kJapaneseNewVariant": {},
		"kJapaneseOldVariant": {},
		"kIRG_GSource":        {},
		"kTraditionalVariant": {},
		"kSimplifiedVariant":  {},
	}
)

// Properties is the raw, field-oriented data collected from Unihan.zip.
type Properties = map[int]map[string][]string

// JapaneseEvidence reports whether fields contain strong Japanese evidence.
func JapaneseEvidence(fields map[string][]string) bool {
	for field := range fields {
		if _, ok := japaneseEvidence[field]; ok {
			return true
		}
	}
	return false
}

// Codepoints extracts uppercase U+XXXX references from Unihan field values.
func Codepoints(values []string) []int {
	result := make([]int, 0)
	for _, value := range values {
		for _, match := range codepointRE.FindAllStringSubmatch(value, -1) {
			cp, err := strconv.ParseInt(match[1], 16, 32)
			if err != nil || cp > utf8.MaxRune || (cp >= 0xD800 && cp <= 0xDFFF) {
				continue
			}
			result = append(result, int(cp))
		}
	}
	return result
}

// ParseZip reads every .txt member in a Unihan.zip archive. It intentionally
// does not assume a particular member name because Unicode occasionally adds
// or splits files between releases.
func ParseZip(path string) (Properties, error) {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer archive.Close()

	props := make(Properties)
	for _, member := range archive.File {
		if !strings.HasSuffix(member.Name, ".txt") {
			continue
		}
		reader, err := member.Open()
		if err != nil {
			return nil, err
		}
		err = parseTextMember(reader, props)
		closeErr := reader.Close()
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", member.Name, err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s: %w", member.Name, closeErr)
		}
	}
	return props, nil
}

func parseTextMember(reader io.Reader, props Properties) error {
	buffered := bufio.NewReader(reader)
	for {
		line, err := buffered.ReadString('\n')
		if len(line) > 0 {
			if !utf8.ValidString(line) {
				return fmt.Errorf("invalid UTF-8")
			}
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				parts := strings.SplitN(line, "\t", 3)
				if len(parts) == 3 {
					field := parts[1]
					if _, ok := interestingFields[field]; ok {
						cpText := strings.TrimPrefix(parts[0], "U+")
						cp, parseErr := strconv.ParseUint(cpText, 16, 32)
						if parseErr != nil || cp > utf8.MaxRune || (cp >= 0xD800 && cp <= 0xDFFF) {
							return fmt.Errorf("invalid code point %q", parts[0])
						}
						byField := props[int(cp)]
						if byField == nil {
							byField = make(map[string][]string)
							props[int(cp)] = byField
						}
						byField[field] = append(byField[field], parts[2])
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// BuildTable applies the same heuristic as the Python builder.
func BuildTable(props Properties, unicodeVersion string) Table {
	characters := make(map[string]Record)
	for cp, fields := range props {
		if cp < 0 || cp > utf8.MaxRune || (cp >= 0xD800 && cp <= 0xDFFF) {
			continue
		}
		char := string(rune(cp))
		hasJapanese := JapaneseEvidence(fields)
		traditionalCPs := Codepoints(fields["kTraditionalVariant"])
		hasGSource := hasField(fields, "kIRG_GSource")

		if len(traditionalCPs) > 0 && !hasJapanese {
			candidatesSet := make(map[string]struct{})
			for _, traditionalCP := range traditionalCPs {
				traditionalFields := props[traditionalCP]
				for _, candidateCP := range Codepoints(traditionalFields["kJapaneseNewVariant"]) {
					candidatesSet[string(rune(candidateCP))] = struct{}{}
				}
			}
			candidates := make([]string, 0, len(candidatesSet))
			for candidate := range candidatesSet {
				candidates = append(candidates, candidate)
			}
			sort.Strings(candidates)
			evidence := []string{"kTraditionalVariant"}
			if hasGSource {
				evidence = append(evidence, "kIRG_GSource")
			}
			characters[char] = Record{
				Rule:                "simplified_chinese_form",
				Severity:            "error",
				TraditionalVariants: runesToStrings(traditionalCPs),
				JapaneseCandidates:  candidates,
				Evidence:            evidence,
			}
			continue
		}

		if hasGSource && !hasJapanese {
			characters[char] = Record{
				Rule:     "chinese_han_without_japanese_source",
				Severity: "warning",
				Evidence: []string{"kIRG_GSource"},
			}
		}
	}

	return Table{
		SchemaVersion:  1,
		UnicodeVersion: unicodeVersion,
		Generator:      "jp-quality-gate 0.1.0",
		Notes:          "Heuristic table for LLM-output quality gating. It is not a normative statement that warning characters are invalid Japanese.",
		Characters:     characters,
	}
}

func hasField(fields map[string][]string, field string) bool {
	_, ok := fields[field]
	return ok
}

func runesToStrings(values []int) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value >= 0 && value <= utf8.MaxRune && !(value >= 0xD800 && value <= 0xDFFF) {
			result = append(result, string(rune(value)))
		}
	}
	return result
}

// Build parses unihanZip, applies the heuristic, and writes schema-versioned
// JSON to output.
func Build(unihanZip, output, unicodeVersion string) error {
	props, err := ParseZip(unihanZip)
	if err != nil {
		return err
	}
	table := BuildTable(props, unicodeVersion)
	data, err := marshalTable(table)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}

func marshalTable(table Table) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(table); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}
