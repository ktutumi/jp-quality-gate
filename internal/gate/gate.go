// Package gate composes Markdown masking, Unihan scanning, and CJ detection.
package gate

import (
	"sort"

	"github.com/ktutumi/jp-quality-gate/internal/cj"
	"github.com/ktutumi/jp-quality-gate/internal/report"
	jptext "github.com/ktutumi/jp-quality-gate/internal/text"
	"github.com/ktutumi/jp-quality-gate/internal/unihan"
)

// Options controls one quality-gate check.
type Options struct {
	IncludeCode      bool
	CJMinCJK         int
	CJMinGap         float64
	WarningsAsErrors bool
}

// Gate holds the loaded static data reused across a check.
type Gate struct {
	Unihan *unihan.Scanner
	CJ     *cj.Classifier
}

// Check returns findings in Python-compatible source order.
func (g *Gate) Check(input string, options Options) report.GateResult {
	masked := jptext.MaskMarkdownNonProse(input, options.IncludeCode)
	issues := g.Unihan.Scan(input, masked)
	issues = append(issues, g.CJ.Scan(input, masked, options.CJMinCJK, options.CJMinGap)...)

	if options.WarningsAsErrors {
		for index := range issues {
			if issues[index].Severity == report.SeverityWarning {
				issues[index].Severity = report.SeverityError
			}
		}
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Start != issues[j].Start {
			return issues[i].Start < issues[j].Start
		}
		return issues[i].Severity == report.SeverityError && issues[j].Severity != report.SeverityError
	})

	return report.GateResult{
		Issues: issues,
		Meta: map[string]any{
			"unicode_version":      g.Unihan.UnicodeVersion,
			"cjclassifier_version": cj.Version,
			"cj_min_cjk":           options.CJMinCJK,
			"cj_min_gap":           options.CJMinGap,
			"include_code":         options.IncludeCode,
		},
	}
}
