package unihan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestScannerUsesRuneOffsetsAndPreservesEvidence(t *testing.T) {
	table := Table{
		SchemaVersion:  1,
		UnicodeVersion: "test",
		Characters: map[string]Record{
			"经": {
				Rule:                "simplified_chinese_form",
				Severity:            "error",
				TraditionalVariants: []string{"經"},
				JapaneseCandidates:  []string{"経"},
				Evidence:            []string{"kTraditionalVariant", "kIRG_GSource"},
			},
		},
	}
	data, err := json.Marshal(table)
	if err != nil {
		t.Fatal(err)
	}
	scanner, err := LoadBytes(data, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	issues := scanner.Scan("🙂🙂これは经済です。", "🙂🙂これは经済です。")
	if len(issues) != 1 {
		t.Fatalf("issues = %d, want 1", len(issues))
	}
	issue := issues[0]
	if issue.Start != 5 || issue.End != 6 || issue.Text != "经" {
		t.Fatalf("location = (%d, %d, %q), want (5, 6, 经)", issue.Start, issue.End, issue.Text)
	}
	if issue.Line != 1 || issue.Column != 6 {
		t.Fatalf("line/column = (%d, %d), want (1, 6)", issue.Line, issue.Column)
	}
	if issue.Details["japanese_candidates"].([]string)[0] != "経" {
		t.Fatalf("details = %#v", issue.Details)
	}
}

func TestScannerStopsAtFiftyFindings(t *testing.T) {
	var input strings.Builder
	for index := 0; index < MaxIssues+7; index++ {
		input.WriteRune('经')
	}
	scanner := &Scanner{
		UnicodeVersion: "test",
		Characters: map[string]Record{
			"经": {Rule: "simplified_chinese_form", Severity: "error"},
		},
	}
	if got := len(scanner.Scan(input.String(), input.String())); got != MaxIssues {
		t.Fatalf("issues = %d, want %d", got, MaxIssues)
	}
}

func TestScanWithZeroLimitMatchesPythonEarlyStop(t *testing.T) {
	scanner := &Scanner{
		UnicodeVersion: "test",
		Characters: map[string]Record{
			"经": {Rule: "simplified_chinese_form", Severity: "error"},
		},
	}
	if got := len(scanner.ScanWithLimit("经", "经", 0)); got != 1 {
		t.Fatalf("issues = %d, want 1", got)
	}
}

func TestLoadSkipsEmptyCharacterRecord(t *testing.T) {
	scanner, err := LoadBytes([]byte(`{"schema_version":1,"characters":{"经":{}}}`), "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(scanner.Characters); got != 0 {
		t.Fatalf("characters = %d, want 0", got)
	}
}

func TestLoadRejectsUnsupportedSchema(t *testing.T) {
	_, err := LoadBytes([]byte(`{"schema_version":2,"characters":{}}`), "fixture")
	if err == nil || err.Error() != "Unsupported Unihan table schema: fixture" {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsInvalidCharacterRecord(t *testing.T) {
	tests := []string{
		`{"schema_version":1,"characters":{"经":{"rule":"simplified_chinese_form"}}}`,
		`{"schema_version":1,"characters":{"经":{"rule":"simplified_chinese_form","severity":"fatal"}}}`,
	}
	for _, document := range tests {
		if _, err := LoadBytes([]byte(document), "fixture"); err == nil {
			t.Fatalf("LoadBytes(%s) succeeded", document)
		}
	}
}

func TestLoadAcceptsUnknownNonemptyRuleLikePython(t *testing.T) {
	document := `{"schema_version":1,"characters":{"甲":{"rule":"custom_rule","severity":"warning"}}}`
	if _, err := LoadBytes([]byte(document), "fixture"); err != nil {
		t.Fatal(err)
	}
}
