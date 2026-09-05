package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunUsesEmbeddedModelAndRuneOffsets(t *testing.T) {
	table := `{"schema_version":1,"unicode_version":"test","characters":{"经":{"rule":"simplified_chinese_form","severity":"error","traditional_variants":["經"],"japanese_candidates":["経"]}}}`
	path := filepath.Join(t.TempDir(), "unihan.json")
	if err := os.WriteFile(path, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exitCode := run(
		[]string{"ignored.md", "--unihan-table", path, "--pretty"},
		strings.NewReader("🙂🙂これは经済です。"),
		&stdout,
		&stderr,
	)
	if exitCode != 2 {
		t.Fatalf("missing file exit = %d, want 2; stdout=%s stderr=%s", exitCode, &stdout, &stderr)
	}

	stdout.Reset()
	stderr.Reset()
	exitCode = run(
		[]string{"--unihan-table", path, "--pretty"},
		strings.NewReader("🙂🙂これは经済です。"),
		&stdout,
		&stderr,
	)
	if exitCode != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exitCode, &stdout, &stderr)
	}
	var result struct {
		Pass   bool `json:"pass"`
		Issues []struct {
			Start  int    `json:"start"`
			End    int    `json:"end"`
			Column int    `json:"column"`
			Text   string `json:"text"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Pass || len(result.Issues) != 1 {
		t.Fatalf("result = %s", &stdout)
	}
	issue := result.Issues[0]
	if issue.Start != 5 || issue.End != 6 || issue.Column != 6 || issue.Text != "经" {
		t.Fatalf("issue = %#v", issue)
	}
}

func TestInvalidConfigurationUsesExitTwoAndJSON(t *testing.T) {
	for _, value := range []string{"-0.1", "1.1", "NaN"} {
		var stdout, stderr bytes.Buffer
		if got := run([]string{"--cj-min-gap", value}, strings.NewReader(""), &stdout, &stderr); got != 2 {
			t.Fatalf("value %q: exit = %d, want 2", value, got)
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result["pass"] != false || result["internal_error"] == "" {
			t.Fatalf("value %q: result = %#v", value, result)
		}
	}
}

func TestReadInputPreservesStdinNewlines(t *testing.T) {
	got, err := readInput("-", strings.NewReader("a\r\nb\rc"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\r\nb\rc" {
		t.Fatalf("input = %q", got)
	}
}

func TestReadInputUsesUniversalNewlinesForFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "newlines.txt")
	if err := os.WriteFile(path, []byte("a\r\nb\rc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readInput(path, strings.NewReader("ignored"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "a\nb\nc" {
		t.Fatalf("input = %q", got)
	}
}
