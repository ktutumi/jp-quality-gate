// Package unihan loads and scans the small, generated Unihan quality-gate
// table used by jp-quality-gate.
package unihan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ktutumi/jp-quality-gate/internal/report"
	jptext "github.com/ktutumi/jp-quality-gate/internal/text"
)

// MaxIssues is the default number of findings returned by Scan.
const MaxIssues = 50

// Record describes one character in a generated Unihan table.
//
// A nil optional slice means that the field was not present in the JSON. An
// empty, non-nil slice is kept when a generated error record has no candidates
// so that the JSON representation remains compatible with the Python builder.
type Record struct {
	Rule                string   `json:"rule"`
	Severity            string   `json:"severity"`
	TraditionalVariants []string `json:"traditional_variants"`
	JapaneseCandidates  []string `json:"japanese_candidates"`
	Evidence            []string `json:"evidence"`
}

// MarshalJSON omits optional fields that were not present while preserving
// explicitly empty arrays produced by the builder.
func (r Record) MarshalJSON() ([]byte, error) {
	value := map[string]any{
		"rule":     r.Rule,
		"severity": r.Severity,
	}
	if r.TraditionalVariants != nil {
		value["traditional_variants"] = r.TraditionalVariants
	}
	if r.JapaneseCandidates != nil {
		value["japanese_candidates"] = r.JapaneseCandidates
	}
	if r.Evidence != nil {
		value["evidence"] = r.Evidence
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

// Table is the schema-versioned JSON table generated from Unihan.zip.
type Table struct {
	SchemaVersion  int               `json:"schema_version"`
	UnicodeVersion string            `json:"unicode_version"`
	Generator      string            `json:"generator"`
	Notes          string            `json:"notes"`
	Characters     map[string]Record `json:"characters"`
}

// Scanner checks masked text against a loaded Table. Offsets are Unicode code
// point (rune) offsets, matching Python's string indexing semantics.
type Scanner struct {
	Path           string
	UnicodeVersion string
	Characters     map[string]Record
}

// TableError reports a table loading or schema error.
type TableError struct {
	message string
}

func (e *TableError) Error() string { return e.message }

// Load reads a schema-versioned Unihan JSON table from path.
func Load(path string) (*Scanner, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &TableError{message: fmt.Sprintf(
				"Unihan table not found: %s. Run jpqg-build-unihan first.", path,
			)}
		}
		return nil, &TableError{message: fmt.Sprintf("Cannot load Unihan table %s: %v", path, err)}
	}
	return LoadBytes(data, path)
}

// LoadBytes loads a table from data. source is used in diagnostics and may be
// a filename or another descriptive label.
func LoadBytes(data []byte, source string) (*Scanner, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, &TableError{message: fmt.Sprintf("Cannot load Unihan table %s: %v", source, err)}
	}

	var schemaVersion int
	schemaRaw, schemaOK := document["schema_version"]
	if !schemaOK || json.Unmarshal(schemaRaw, &schemaVersion) != nil || schemaVersion != 1 {
		return nil, unsupportedSchema(source)
	}
	charactersRaw, charactersOK := document["characters"]
	if !charactersOK || bytes.Equal(bytes.TrimSpace(charactersRaw), []byte("null")) {
		return nil, unsupportedSchema(source)
	}
	var characterValues map[string]json.RawMessage
	if err := json.Unmarshal(charactersRaw, &characterValues); err != nil || characterValues == nil {
		return nil, unsupportedSchema(source)
	}

	characters := make(map[string]Record, len(characterValues))
	for char, raw := range characterValues {
		trimmed := bytes.TrimSpace(raw)
		if bytes.Equal(trimmed, []byte("{}")) || bytes.Equal(trimmed, []byte("null")) {
			// Python's `if not record` skips empty objects and null records.
			continue
		}
		var record Record
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, &TableError{message: fmt.Sprintf("Cannot load Unihan table %s: %v", source, err)}
		}
		if record.Rule == "" || (record.Severity != "error" && record.Severity != "warning") {
			return nil, &TableError{message: fmt.Sprintf("Cannot load Unihan table %s: invalid record for %s", source, char)}
		}
		characters[char] = record
	}

	unicodeVersion := "unknown"
	if raw, ok := document["unicode_version"]; ok {
		var value string
		if json.Unmarshal(raw, &value) == nil {
			unicodeVersion = value
		}
	}
	return &Scanner{
		Path:           source,
		UnicodeVersion: unicodeVersion,
		Characters:     characters,
	}, nil
}

func unsupportedSchema(source string) error {
	return &TableError{message: fmt.Sprintf("Unsupported Unihan table schema: %s", source)}
}

// Scan scans maskedText and uses originalText for locations and issue text.
// The two strings must have identical rune layout; masking is expected to
// preserve that layout by replacing hidden characters with spaces.
func (s *Scanner) Scan(originalText, maskedText string) []report.Issue {
	return s.ScanWithLimit(originalText, maskedText, MaxIssues)
}

// ScanWithLimit is Scan with an explicit maximum number of findings.
func (s *Scanner) ScanWithLimit(originalText, maskedText string, maxIssues int) []report.Issue {
	originalRunes := []rune(originalText)
	maskedRunes := []rune(maskedText)
	capacity := maxIssues
	if capacity < 0 {
		capacity = 0
	}
	issues := make([]report.Issue, 0, capacity)
	for offset, char := range maskedRunes {
		record, ok := s.Characters[string(char)]
		if !ok {
			continue
		}

		line, column := jptext.LineColumn(originalText, offset)
		issueText := string(char)
		if offset < len(originalRunes) {
			issueText = string(originalRunes[offset])
		}
		details := map[string]any{
			"codepoint":       fmt.Sprintf("U+%04X", char),
			"unicode_version": s.UnicodeVersion,
		}
		if len(record.TraditionalVariants) > 0 {
			details["traditional_variants"] = record.TraditionalVariants
		}
		if len(record.JapaneseCandidates) > 0 {
			details["japanese_candidates"] = record.JapaneseCandidates
		}
		if len(record.Evidence) > 0 {
			details["evidence"] = record.Evidence
		}

		message := fmt.Sprintf(
			"Han character has Chinese source evidence but no strong Japanese evidence: %c",
			char,
		)
		if record.Rule == "simplified_chinese_form" {
			message = fmt.Sprintf("Japanese-unattested simplified Chinese form detected: %c", char)
		}
		issues = append(issues, report.Issue{
			Rule:     record.Rule,
			Severity: report.Severity(record.Severity),
			Message:  message,
			Start:    offset,
			End:      offset + 1,
			Text:     issueText,
			Line:     line,
			Column:   column,
			Details:  details,
		})
		if len(issues) >= maxIssues {
			break
		}
	}
	return issues
}
