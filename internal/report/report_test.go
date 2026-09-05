package report

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestGateResultJSONMatchesPythonShape(t *testing.T) {
	result := GateResult{
		Issues: []Issue{
			{
				Rule:     "chinese_segment",
				Severity: SeverityWarning,
				Message:  "warning",
				Start:    2,
				End:      4,
				Text:     "经済",
				Line:     1,
				Column:   3,
				Details:  map[string]any{"gap": 0.1},
			},
		},
		Meta: map[string]any{"include_code": false},
	}

	if !result.Passed() {
		t.Fatal("warning-only result should pass")
	}
	if got := len(result.Errors()); got != 0 {
		t.Fatalf("errors = %d, want 0", got)
	}
	if got := len(result.Warnings()); got != 1 {
		t.Fatalf("warnings = %d, want 1", got)
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Pass    bool `json:"pass"`
		Summary struct {
			Errors   int `json:"errors"`
			Warnings int `json:"warnings"`
			Issues   int `json:"issues"`
		} `json:"summary"`
		Issues []Issue        `json:"issues"`
		Meta   map[string]any `json:"meta"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.Pass || decoded.Summary.Errors != 0 || decoded.Summary.Warnings != 1 || decoded.Summary.Issues != 1 {
		t.Fatalf("unexpected result JSON: %s", data)
	}
	if decoded.Issues[0].Details["gap"] != 0.1 {
		t.Fatalf("details not retained: %s", data)
	}
}

func TestIssueNilDetailsEncodeAsObject(t *testing.T) {
	data, err := json.Marshal(Issue{})
	if err != nil {
		t.Fatal(err)
	}
	if string(data[len(data)-3:]) != "{}}" {
		t.Fatalf("nil details should encode as {}, got %s", data)
	}
}

func TestGateResultJSONDoesNotEscapeHTML(t *testing.T) {
	result := GateResult{Issues: []Issue{{Message: "<>&"}}}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(result); err != nil {
		t.Fatal(err)
	}
	data := output.Bytes()
	if string(data) == "" || strings.Contains(string(data), `\u003c`) || strings.Contains(string(data), `\u003e`) || strings.Contains(string(data), `\u0026`) {
		t.Fatalf("HTML was escaped: %s", data)
	}
}
