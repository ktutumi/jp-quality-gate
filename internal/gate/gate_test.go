package gate

import (
	"strings"
	"testing"

	"github.com/ktutumi/jp-quality-gate/internal/cj"
	"github.com/ktutumi/jp-quality-gate/internal/report"
	"github.com/ktutumi/jp-quality-gate/internal/unihan"
)

const testCJModel = `Languages: zh-hans,zh-hant,ja MinLogProb: -10
甲 -1 -3 -4
乙 -1 -3 -4
甲乙 -1 -3 -4
`

func TestCheckComposesScannersAndPromotesWarnings(t *testing.T) {
	classifier, err := cj.ParseModel(strings.NewReader(testCJModel), "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	g := Gate{
		Unihan: &unihan.Scanner{
			UnicodeVersion: "test",
			Characters: map[string]unihan.Record{
				"龘": {Rule: "chinese_han_without_japanese_source", Severity: "warning"},
			},
		},
		CJ: classifier,
	}

	result := g.Check("🙂龘。甲乙", Options{
		CJMinCJK:         2,
		CJMinGap:         0.15,
		WarningsAsErrors: true,
	})
	if result.Passed() {
		t.Fatal("promoted warning and Chinese segment should fail the gate")
	}
	if len(result.Issues) != 2 {
		t.Fatalf("issues = %d, want 2: %#v", len(result.Issues), result.Issues)
	}
	if got := result.Issues[0]; got.Start != 1 || got.End != 2 || got.Severity != report.SeverityError {
		t.Fatalf("first issue = %#v", got)
	}
	if got := result.Issues[1]; got.Rule != "chinese_segment" || got.Start != 3 || got.Text != "甲乙" {
		t.Fatalf("second issue = %#v", got)
	}
	if result.Meta["cjclassifier_version"] != "1.0.5" {
		t.Fatalf("meta = %#v", result.Meta)
	}
}
