// Package prose runs explicitly enabled, preinstalled prose linters.
package prose

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ktutumi/jp-quality-gate/internal/report"
	jptext "github.com/ktutumi/jp-quality-gate/internal/text"
)

type Options struct {
	Textlint              bool
	TextlintBin           string
	TextlintConfig        string
	NaturalJapanese       bool
	NaturalJapaneseScript string
	NaturalJapaneseGenre  string
	UVBin                 string
}

// Runner separates process execution from protocol tests. A nonzero exit status
// accompanies an exit error; failures to start or finish use status -1.
type Runner interface {
	Run(context.Context, string, []string, string) ([]byte, int, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, bin string, args []string, input string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	configureCancellation(cmd)
	cmd.WaitDelay = time.Second
	cmd.Stdin = strings.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, -1, ctx.Err()
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return stdout.Bytes(), exit.ExitCode(), fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err != nil {
		return nil, -1, err
	}
	return stdout.Bytes(), 0, nil
}

func readableFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", path)
	}
	return nil
}

// Check leaves disabled tools completely untouched, including their settings.
// Masking retains rune offsets; textlint offsets refer to this masked input.
func Check(ctx context.Context, input string, options Options, runner Runner) ([]report.Issue, error) {
	if !options.Textlint && !options.NaturalJapanese {
		return nil, nil
	}
	masked := jptext.MaskMarkdownNonProse(input, false)
	var issues []report.Issue
	if options.Textlint {
		bin := options.TextlintBin
		if bin == "" {
			bin = "textlint"
		}
		args := []string{"--stdin", "--stdin-filename", "response.md", "--format", "json"}
		if options.TextlintConfig == "" {
			return nil, fmt.Errorf("textlint: --textlint-config or JPQG_TEXTLINT_CONFIG is required")
		}
		if err := readableFile(options.TextlintConfig); err != nil {
			return nil, fmt.Errorf("textlint config: %w", err)
		}
		args = append(args, "--config", options.TextlintConfig)
		// Preserve code nodes for the Markdown parser; mask URLs that rules may visit.
		textlintInput := jptext.MaskMarkdownNonProse(input, true)
		data, code, err := runner.Run(ctx, bin, args, textlintInput)
		if err != nil && code != 1 {
			return nil, fmt.Errorf("textlint: %w", err)
		}
		if code != 0 && code != 1 {
			return nil, fmt.Errorf("textlint: exit %d: %s", code, strings.TrimSpace(string(data)))
		}
		found, err := parseTextlint(data, input, textlintInput)
		if err != nil {
			return nil, fmt.Errorf("textlint: %w", err)
		}
		issues = append(issues, found...)
	}
	if options.NaturalJapanese {
		switch options.NaturalJapaneseGenre {
		case "", "essay", "tech", "business":
		default:
			return nil, fmt.Errorf("natural-japanese: genre must be essay, tech, or business")
		}
		if err := readableFile(options.NaturalJapaneseScript); err != nil {
			return nil, fmt.Errorf("natural-japanese script: %w", err)
		}
		f, err := os.CreateTemp("", "jpqg-prose-*.md")
		if err != nil {
			return nil, err
		}
		defer os.Remove(f.Name())
		naturalInput := strings.Map(func(r rune) rune {
			switch r {
			case '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
				return ' '
			}
			return r
		}, masked)
		_, writeErr := f.WriteString(naturalInput)
		closeErr := f.Close()
		if writeErr != nil {
			return nil, writeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		bin := options.UVBin
		if bin == "" {
			bin = "uv"
		}
		args := []string{"run", "--offline", "--no-python-downloads", "--no-project", "--script", options.NaturalJapaneseScript, f.Name(), "--json"}
		if options.NaturalJapaneseGenre != "" {
			args = append(args, "--genre", options.NaturalJapaneseGenre)
		}
		data, code, err := runner.Run(ctx, bin, args, "")
		if err != nil {
			return nil, fmt.Errorf("natural-japanese: %w", err)
		}
		if code != 0 {
			return nil, fmt.Errorf("natural-japanese: exit %d", code)
		}
		found, err := parseNaturalJapanese(data, input, naturalInput)
		if err != nil {
			return nil, fmt.Errorf("natural-japanese: %w", err)
		}
		issues = append(issues, found...)
	}
	return issues, nil
}

func issue(input, source, rule, message string, start, end int, details map[string]any) report.Issue {
	line, column := jptext.LineColumn(input, start)
	details["source"], details["rule_id"] = source, rule
	return report.Issue{Rule: source + ":" + rule, Severity: report.SeverityWarning,
		Message: message, Start: start, End: end, Text: string([]rune(input)[start:end]),
		Line: line, Column: column, Details: details}
}

// utf16Offset rejects offsets inside a surrogate pair or outside the input.
func utf16Offset(input string, offset int) (int, error) {
	units, index := 0, 0
	for _, r := range input {
		if units == offset {
			return index, nil
		}
		units++
		if r > 0xffff {
			units++
		}
		index++
	}
	if units == offset {
		return index, nil
	}
	return 0, fmt.Errorf("invalid UTF-16 offset %d", offset)
}

func parseTextlint(data []byte, input, masked string) ([]report.Issue, error) {
	var results []struct {
		Messages []struct {
			RuleID  string `json:"ruleId"`
			Message string `json:"message"`
			Range   []int  `json:"range"`
			Index   *int   `json:"index"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, err
	}
	if results == nil {
		return nil, fmt.Errorf("expected result array")
	}
	var issues []report.Issue
	proseRunes := []rune(jptext.MaskMarkdownNonProse(input, false))
	originalRunes := []rune(input)
	for _, result := range results {
		if result.Messages == nil {
			return nil, fmt.Errorf("missing messages array")
		}
		for _, m := range result.Messages {
			if m.RuleID == "" || m.Message == "" {
				return nil, fmt.Errorf("missing ruleId or message")
			}
			var start, end int
			var err error
			switch {
			case len(m.Range) == 2:
				start, err = utf16Offset(masked, m.Range[0])
				if err != nil {
					return nil, err
				}
				end, err = utf16Offset(masked, m.Range[1])
			case m.Range == nil && m.Index != nil:
				start, err = utf16Offset(masked, *m.Index)
				end = min(start+1, utf8.RuneCountInString(masked))
			default:
				return nil, fmt.Errorf("missing or invalid message range")
			}
			if err != nil {
				return nil, err
			}
			if end < start {
				return nil, fmt.Errorf("reversed message range")
			}
			// Suppress diagnostics confined to code/URL spans, even for rules
			// that inspect raw Markdown instead of prose AST nodes.
			checkEnd := min(max(end, start+1), len(proseRunes))
			if start < len(proseRunes) && strings.TrimSpace(string(proseRunes[start:checkEnd])) == "" && strings.TrimSpace(string(originalRunes[start:checkEnd])) != "" {
				continue
			}
			issues = append(issues, issue(input, "textlint", m.RuleID, m.Message, start, end, map[string]any{}))
		}
	}
	return issues, nil
}

func parseNaturalJapanese(data []byte, input, masked string) ([]report.Issue, error) {
	var result struct {
		Findings []struct {
			Line     int    `json:"line"`
			Category string `json:"category"`
			Excerpt  string `json:"excerpt"`
			Detail   string `json:"detail"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result.Findings == nil {
		return nil, fmt.Errorf("missing findings array")
	}
	lines := strings.Split(masked, "\n")
	starts := make([]int, len(lines))
	for i := 1; i < len(lines); i++ {
		starts[i] = starts[i-1] + utf8.RuneCountInString(lines[i-1]) + 1
	}
	var issues []report.Issue
	for _, f := range result.Findings {
		if f.Category == "" || f.Detail == "" || f.Line < 1 || f.Line > len(lines) {
			return nil, fmt.Errorf("invalid finding category, detail, or line")
		}
		start := starts[f.Line-1]
		end := start // Upstream aggregate excerpts are descriptions, not source spans.
		precision := "line"
		if f.Excerpt != "" {
			if index := strings.Index(lines[f.Line-1], f.Excerpt); index >= 0 {
				start += utf8.RuneCountInString(lines[f.Line-1][:index])
				end = start + utf8.RuneCountInString(f.Excerpt)
				precision = "excerpt"
			}
		}
		issues = append(issues, issue(input, "natural-japanese", f.Category, f.Detail, start, end,
			map[string]any{"excerpt": f.Excerpt, "position_precision": precision}))
	}
	return issues, nil
}
