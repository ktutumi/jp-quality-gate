package cj

import (
	"fmt"
	"sort"

	"github.com/ktutumi/jp-quality-gate/internal/report"
	jptext "github.com/ktutumi/jp-quality-gate/internal/text"
)

// Detector applies a CJClassifier to the sentence and clause candidates
// produced by internal/text.
type Detector struct {
	Classifier *Classifier
}

// NewDetector creates a scanner around classifier.
func NewDetector(classifier *Classifier) *Detector {
	return &Detector{Classifier: classifier}
}

// Detect returns the classifier's language, gap, and compact scores string.
func (detector *Detector) Detect(text string) Detection {
	if detector == nil || detector.Classifier == nil {
		return Detection{Language: Unknown}
	}
	return detector.Classifier.DetectWithResults(text)
}

// Scan reports Chinese simplified/traditional candidates. originalText is
// used for issue offsets and text; maskedText is used for classification.
func (detector *Detector) Scan(originalText, maskedText string, minCJK int, minGap float64) []report.Issue {
	if detector == nil || detector.Classifier == nil {
		return nil
	}
	originalRunes := []rune(originalText)
	candidates := make([]report.Issue, 0)
	for _, segment := range jptext.IterCJSegments(maskedText, minCJK) {
		detection := detector.Detect(segment.Text)
		if detection.Language != ChineseSimplified && detection.Language != ChineseTraditional {
			continue
		}
		language := detection.Language.ISOCode()
		severity := report.SeverityWarning
		if detection.Gap >= minGap {
			severity = report.SeverityError
		}
		line, column := jptext.LineColumn(originalText, segment.Start)
		issueText := ""
		if segment.Start >= 0 && segment.Start <= len(originalRunes) && segment.End >= segment.Start {
			end := segment.End
			if end > len(originalRunes) {
				end = len(originalRunes)
			}
			issueText = string(originalRunes[segment.Start:end])
		}
		candidates = append(candidates, report.Issue{
			Rule:     "chinese_segment",
			Severity: severity,
			Message:  fmt.Sprintf("Segment classified as %s (gap=%.3f)", language, detection.Gap),
			Start:    segment.Start,
			End:      segment.End,
			Text:     issueText,
			Line:     line,
			Column:   column,
			Details: map[string]any{
				"language": language,
				"gap":      detection.Gap,
				"scores":   detection.Scores,
				"min_gap":  minGap,
			},
		})
	}
	return DedupeOverlapping(candidates)
}

// Scan applies the embedded classifier without requiring callers to build a
// Detector explicitly.
func (classifier *Classifier) Scan(originalText, maskedText string, minCJK int, minGap float64) []report.Issue {
	return NewDetector(classifier).Scan(originalText, maskedText, minCJK, minGap)
}

// DedupeOverlapping keeps the smallest same-language span, breaking equal-size
// ties by larger gap, and returns the survivors in source order.
func DedupeOverlapping(issues []report.Issue) []report.Issue {
	ordered := append([]report.Issue(nil), issues...)
	sort.SliceStable(ordered, func(i, j int) bool {
		lengthI := ordered[i].End - ordered[i].Start
		lengthJ := ordered[j].End - ordered[j].Start
		if lengthI != lengthJ {
			return lengthI < lengthJ
		}
		return issueGap(ordered[i]) > issueGap(ordered[j])
	})
	kept := make([]report.Issue, 0, len(ordered))
	for _, issue := range ordered {
		language := issueLanguage(issue)
		contained := false
		for _, other := range kept {
			if issueLanguage(other) == language &&
				other.Start >= issue.Start && other.End <= issue.End {
				contained = true
				break
			}
		}
		if !contained {
			kept = append(kept, issue)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return kept[i].Start < kept[j].Start
	})
	return kept
}

func issueLanguage(issue report.Issue) string {
	if value, ok := issue.Details["language"].(string); ok {
		return value
	}
	return ""
}

func issueGap(issue report.Issue) float64 {
	if value, ok := issue.Details["gap"].(float64); ok {
		return value
	}
	return 0
}
