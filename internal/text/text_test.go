package text

import "testing"

func TestMaskMarkdownNonProsePreservesRuneOffsets(t *testing.T) {
	input := "説明🙂です。\n```txt\n今天天气很好\n```\n`经済` はコード例。https://example.test/経済"
	masked := MaskMarkdownNonProse(input, false)

	if got, want := len([]rune(masked)), len([]rune(input)); got != want {
		t.Fatalf("masked rune length = %d, want %d", got, want)
	}
	if got, want := countRune(masked, '\n'), countRune(input, '\n'); got != want {
		t.Fatalf("masked newline count = %d, want %d", got, want)
	}
	for _, hidden := range []string{"今天天气很好", "经済", "https://example.test/経済"} {
		if contains(masked, hidden) {
			t.Errorf("masked text still contains %q: %q", hidden, masked)
		}
	}
	if !contains(masked, "説明🙂です") {
		t.Fatalf("prose was masked: %q", masked)
	}
}

func TestMaskMarkdownNonProseIncludeCodeStillMasksURL(t *testing.T) {
	input := "```\n今天天气很好\n``` `经済` https://example.test/経済"
	masked := MaskMarkdownNonProse(input, true)
	if !contains(masked, "今天天气很好") || !contains(masked, "经済") {
		t.Fatalf("code was masked with includeCode=true: %q", masked)
	}
	if contains(masked, "https://example.test/経済") {
		t.Fatalf("URL was not masked with includeCode=true: %q", masked)
	}
}

func TestMaskMarkdownNonProseDoesNotMaskEmptyURL(t *testing.T) {
	input := "説明 https:// 今天天气很好"
	if got := MaskMarkdownNonProse(input, false); got != input {
		t.Fatalf("masked = %q, want %q", got, input)
	}
}

func TestMaskMarkdownNonProseUsesPythonSplitLines(t *testing.T) {
	for _, separator := range []string{"\r\n", "\r", "\v", "\f", "\x1c", "\x1d", "\x1e", "\u0085", "\u2028", "\u2029"} {
		input := "```" + separator + "今天天气很好" + separator + "```" + separator + "説明"
		masked := MaskMarkdownNonProse(input, false)
		if contains(masked, "今天天气很好") || !contains(masked, "説明") {
			t.Fatalf("separator %q was not split like Python: %q", separator, masked)
		}
		if got, want := len([]rune(masked)), len([]rune(input)); got != want {
			t.Fatalf("separator %q changed rune length: got %d, want %d", separator, got, want)
		}
	}
}

func TestIterCJSegmentsUsesRuneOffsetsAndKeepsEmbeddedClause(t *testing.T) {
	input := "🙂🙂これは经済です。𠮷野家について。これは经済です。"
	segments := IterCJSegments(input, 2)
	if len(segments) == 0 {
		t.Fatal("IterCJSegments returned no candidates")
	}

	runes := []rune(input)
	for _, segment := range segments {
		if segment.Start < 0 || segment.End > len(runes) || segment.Start >= segment.End {
			t.Fatalf("invalid rune span: %+v", segment)
		}
		if got, want := segment.Text, string(runes[segment.Start:segment.End]); got != want {
			t.Errorf("segment text = %q, want %q", got, want)
		}
	}
	if got, want := segments[0].Start, 0; got != want {
		t.Errorf("first segment start = %d, want %d", got, want)
	}
}

func TestLineColumnUsesRuneOffset(t *testing.T) {
	input := "🙂🙂\n𠮷経済"
	line, column := LineColumn(input, 3) // 𠮷: code-point offset 3, first column on line 2.
	if line != 2 || column != 1 {
		t.Fatalf("LineColumn = (%d, %d), want (2, 1)", line, column)
	}
	line, column = LineColumn(input, 5)
	if line != 2 || column != 3 {
		t.Fatalf("LineColumn = (%d, %d), want (2, 3)", line, column)
	}
}

func TestCJKRelevantRanges(t *testing.T) {
	for _, r := range []rune{'あ', 'ア', '一', '﨑', '𠮷'} {
		if !IsCJKRelevant(r) {
			t.Errorf("IsCJKRelevant(%U) = false, want true", r)
		}
	}
	for _, r := range []rune{'A', '🙂', '한'} {
		if IsCJKRelevant(r) {
			t.Errorf("IsCJKRelevant(%U) = true, want false", r)
		}
	}
}

func countRune(s string, want rune) int {
	count := 0
	for _, r := range s {
		if r == want {
			count++
		}
	}
	return count
}

func contains(s, want string) bool {
	return len([]rune(s)) >= len([]rune(want)) && stringContains(s, want)
}

func stringContains(s, want string) bool {
	for i := 0; i+len([]rune(want)) <= len([]rune(s)); i++ {
		if string([]rune(s)[i:i+len([]rune(want))]) == want {
			return true
		}
	}
	return false
}
