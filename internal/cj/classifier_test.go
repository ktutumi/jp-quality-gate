package cj

import (
	"strings"
	"testing"

	"github.com/ktutumi/jp-quality-gate/internal/report"
)

const syntheticModel = `Languages: zh-hans,zh-hant,ja MinLogProb: -10
甲 -1 0 0
乙 0 0 0
甲乙 -1 -2 -3
`

func TestParseModelAndScoreUsesLanguageOrderAndPlaceholders(t *testing.T) {
	classifier, err := ParseModel(strings.NewReader(syntheticModel), "synthetic", 0)
	if err != nil {
		t.Fatal(err)
	}
	results := NewResults()
	if got := classifier.DetectInto("甲", results); got != ChineseSimplified {
		t.Fatalf("language = %v, want zh-hans", got)
	}
	if got, want := results.Gap, 0.9; got != want {
		t.Fatalf("gap = %.17g, want %.17g", got, want)
	}
	if got, want := results.ToShortString(), "zh-hans:1.00,zh-hant:0.10,ja:0.10"; got != want {
		t.Fatalf("scores = %q, want %q", got, want)
	}
	if got, want := results.Scores.UnigramHitsPerLang, []int{1, 0, 0}; !sameInts(got, want) {
		t.Fatalf("unigram hits = %v, want %v", got, want)
	}
	if got, want := results.TotalScores, []float64{-1, -10, -10}; !sameFloats(got, want) {
		t.Fatalf("totals = %v, want %v", got, want)
	}
}

func TestParseModelAcceptsCRLF(t *testing.T) {
	model := strings.ReplaceAll(syntheticModel, "\n", "\r\n")
	classifier, err := ParseModel(strings.NewReader(model), "crlf", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := classifier.Detect("甲"); got != ChineseSimplified {
		t.Fatalf("language = %v, want zh-hans", got)
	}
}

func TestKanaShortcutIsStrictAndDoesNotBreakBigram(t *testing.T) {
	classifier, err := ParseModel(strings.NewReader(syntheticModel), "synthetic", 0)
	if err != nil {
		t.Fatal(err)
	}
	results := NewResults()
	if got := classifier.DetectInto("甲あ乙", results); got != Japanese {
		t.Fatalf("language = %v, want ja from kana shortcut", got)
	}
	if got, want := results.Gap, 1.0; got != want {
		t.Fatalf("gap = %v, want %v", got, want)
	}
	if got, want := results.Scores.BigramHitsPerLang, []int{1, 1, 1}; !sameInts(got, want) {
		t.Fatalf("bigram hits = %v, want %v", got, want)
	}
	if got, want := results.ToShortString(), "ja:1.0,zh-hans:0,zh-hant:0"; got != want {
		t.Fatalf("scores = %q, want %q", got, want)
	}

	classifier.SetToleratedKanaThreshold(0.5)
	results.Clear()
	if got := classifier.DetectInto("甲あ", results); got == Japanese {
		t.Fatal("kana ratio equal to threshold must not trigger shortcut")
	}
}

func TestLanguageParsing(t *testing.T) {
	tests := map[string]Language{
		"ZH-HANS":        ChineseSimplified,
		"zho-hant":       ChineseTraditional,
		"JAPANESE":       Japanese,
		"jp":             Japanese,
		"not-a-language": Unknown,
	}
	for input, want := range tests {
		if got := ParseLanguage(input); got != want {
			t.Errorf("ParseLanguage(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestBigramMapResizesAndFindsEntries(t *testing.T) {
	builder := newBigramMapBuilder(1)
	want := [langCount]float32{-1, -2, -3}
	for index := 0; index < 64; index++ {
		builder.put(rune(0x3400+index), rune(0x3400+index+1), want)
	}
	mapData := builder.build()
	for index := 0; index < 64; index++ {
		got, ok := mapData.Lookup(rune(0x3400+index), rune(0x3400+index+1))
		if !ok || got != want {
			t.Fatalf("Lookup(%d) = %v, %v; want %v, true", index, got, ok, want)
		}
	}
}

func TestScanUsesRuneSpansAndDeduplicatesSameLanguage(t *testing.T) {
	classifier, err := ParseModel(strings.NewReader(syntheticModel), "synthetic", 0)
	if err != nil {
		t.Fatal(err)
	}
	detector := NewDetector(classifier)
	original := "🙂前置。甲乙。𠮷末尾"
	issues := detector.Scan(original, original, 2, 0.15)
	if len(issues) != 1 {
		t.Fatalf("issues = %d, want one deduplicated finding: %+v", len(issues), issues)
	}
	issue := issues[0]
	if issue.Start != 4 || issue.End != 6 || issue.Text != "甲乙" {
		t.Fatalf("issue span = (%d, %d, %q), want (4, 6, %q)", issue.Start, issue.End, issue.Text, "甲乙")
	}
	if issue.Line != 1 || issue.Column != 5 {
		t.Fatalf("issue location = (%d, %d), want (1, 5)", issue.Line, issue.Column)
	}
	if issue.Rule != "chinese_segment" || issue.Details["language"] != "zh-hans" {
		t.Fatalf("unexpected issue = %+v", issue)
	}
}

func TestDedupeOverlappingPrefersShortestThenGap(t *testing.T) {
	issues := []report.Issue{
		{Start: 0, End: 8, Details: map[string]any{"language": "zh-hans", "gap": 0.99}},
		{Start: 1, End: 4, Details: map[string]any{"language": "zh-hans", "gap": 0.10}},
		{Start: 1, End: 4, Details: map[string]any{"language": "zh-hans", "gap": 0.20}},
		{Start: 2, End: 5, Details: map[string]any{"language": "zh-hant", "gap": 0.05}},
	}
	kept := DedupeOverlapping(issues)
	if len(kept) != 2 {
		t.Fatalf("kept = %+v, want two languages", kept)
	}
	if kept[0].Start != 1 || kept[0].End != 4 || issueGap(kept[0]) != 0.20 {
		t.Fatalf("zh-hans survivor = %+v, want shortest highest-gap issue", kept[0])
	}
	if kept[1].Start != 2 || kept[1].End != 5 {
		t.Fatalf("zh-hant survivor = %+v", kept[1])
	}
}

func TestEmbeddedModelParitySpotCheck(t *testing.T) {
	classifier, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		input  string
		lang   Language
		gap    float64
		scores string
	}{
		{"今天天气很好，我们去公园散步。", ChineseSimplified, 0.14040755613239142, "zh-hans:1.00,zh-hant:0.86,ja:0.75"},
		{"今天天氣很好，我們去公園散步。", ChineseTraditional, 0.1078163527754441, "zh-hant:1.00,zh-hans:0.89,ja:0.79"},
		{"事務所", Japanese, 0.13685722335330475, "ja:1.00,zh-hant:0.86,zh-hans:0.70"},
		{"東京都", Japanese, 0.1747969985506559, "ja:1.00,zh-hant:0.83,zh-hans:0.72"},
		{"é漢字经済", Japanese, 0.02474457297162147, "ja:1.00,zh-hant:0.98,zh-hans:0.97"},
		{"", Unknown, 0, ""},
	}
	for _, test := range tests {
		results := NewResults()
		if got := classifier.DetectInto(test.input, results); got != test.lang {
			t.Errorf("Detect(%q) = %v, want %v", test.input, got, test.lang)
		}
		if got := results.ToShortString(); got != test.scores {
			t.Errorf("scores(%q) = %q, want %q", test.input, got, test.scores)
		}
		if got := results.Gap; got != test.gap {
			t.Errorf("gap(%q) = %.17g, want %.17g", test.input, got, test.gap)
		}
	}
}

func sameInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func sameFloats(got, want []float64) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}
