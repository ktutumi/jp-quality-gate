// Package report contains the JSON-compatible quality-gate result types.
package report

import (
	"bytes"
	"encoding/json"
)

// Severity is the severity assigned to a finding.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is one quality-gate finding. Start, End, Line, and Column use Unicode
// code-point (rune) offsets, matching Python's str indexing semantics.
type Issue struct {
	Rule     string         `json:"rule"`
	Severity Severity       `json:"severity"`
	Message  string         `json:"message"`
	Start    int            `json:"start"`
	End      int            `json:"end"`
	Text     string         `json:"text"`
	Line     int            `json:"line"`
	Column   int            `json:"column"`
	Details  map[string]any `json:"details"`
}

// MarshalJSON keeps details an object even for a zero-value Issue. Python's
// dataclass default is an empty dict, not null.
func (i Issue) MarshalJSON() ([]byte, error) {
	type issueJSON struct {
		Rule     string         `json:"rule"`
		Severity Severity       `json:"severity"`
		Message  string         `json:"message"`
		Start    int            `json:"start"`
		End      int            `json:"end"`
		Text     string         `json:"text"`
		Line     int            `json:"line"`
		Column   int            `json:"column"`
		Details  map[string]any `json:"details"`
	}
	details := i.Details
	if details == nil {
		details = map[string]any{}
	}
	return marshalJSONNoEscape(issueJSON{
		Rule:     i.Rule,
		Severity: i.Severity,
		Message:  i.Message,
		Start:    i.Start,
		End:      i.End,
		Text:     i.Text,
		Line:     i.Line,
		Column:   i.Column,
		Details:  details,
	})
}

// GateResult is the complete quality-gate report.
type GateResult struct {
	Issues []Issue        `json:"issues"`
	Meta   map[string]any `json:"meta"`
}

// Errors returns findings whose severity is error.
func (r GateResult) Errors() []Issue {
	issues := make([]Issue, 0)
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			issues = append(issues, issue)
		}
	}
	return issues
}

// Warnings returns findings whose severity is warning.
func (r GateResult) Warnings() []Issue {
	issues := make([]Issue, 0)
	for _, issue := range r.Issues {
		if issue.Severity == SeverityWarning {
			issues = append(issues, issue)
		}
	}
	return issues
}

// Passed reports whether the result contains no errors. Warnings do not fail
// the gate unless the caller has promoted them to SeverityError first.
func (r GateResult) Passed() bool {
	for _, issue := range r.Issues {
		if issue.Severity == SeverityError {
			return false
		}
	}
	return true
}

type summary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
	Issues   int `json:"issues"`
}

type gateResultJSON struct {
	Pass    bool           `json:"pass"`
	Summary summary        `json:"summary"`
	Issues  []Issue        `json:"issues"`
	Meta    map[string]any `json:"meta"`
}

// MarshalJSON emits the same top-level shape as GateResult.to_dict() in the
// Python implementation.
func (r GateResult) MarshalJSON() ([]byte, error) {
	errors := r.Errors()
	warnings := r.Warnings()
	meta := r.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	issues := r.Issues
	if issues == nil {
		issues = []Issue{}
	}
	return marshalJSONNoEscape(gateResultJSON{
		Pass: r.Passed(),
		Summary: summary{
			Errors:   len(errors),
			Warnings: len(warnings),
			Issues:   len(issues),
		},
		Issues: issues,
		Meta:   meta,
	})
}

func marshalJSONNoEscape(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

// ToMap returns the JSON-compatible report as ordinary Go values.
func (r GateResult) ToMap() map[string]any {
	errors := r.Errors()
	warnings := r.Warnings()
	issues := r.Issues
	if issues == nil {
		issues = []Issue{}
	}
	meta := r.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	return map[string]any{
		"pass": r.Passed(),
		"summary": map[string]any{
			"errors":   len(errors),
			"warnings": len(warnings),
			"issues":   len(issues),
		},
		"issues": issues,
		"meta":   meta,
	}
}
