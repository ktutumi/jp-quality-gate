// Package text contains the Markdown masking and CJK segmentation shared by
// the quality-gate scanners.
package text

import (
	"sort"
	"unicode"
)

// Segment is a candidate sentence or clause, with rune offsets into the input.
type Segment struct {
	Start int
	End   int
	Text  string
}

// MaskMarkdownNonProse replaces fenced code, inline code, and URLs with spaces
// while retaining every rune position and newline. When includeCode is true,
// only URLs are masked.
func MaskMarkdownNonProse(input string, includeCode bool) string {
	runes := []rune(input)
	if !includeCode {
		maskFencedCode(runes)
		maskInlineCode(runes)
	}
	maskURLs(runes)
	return string(runes)
}

func maskFencedCode(runes []rune) {
	var fenceChar rune
	fenceLen := 0

	for lineStart := 0; lineStart < len(runes); {
		lineEnd := lineStart
		for lineEnd < len(runes) && !isSplitLineBoundary(runes[lineEnd]) {
			lineEnd++
		}
		nextLine := lineEnd
		if nextLine < len(runes) {
			nextLine++
			if runes[lineEnd] == '\r' && nextLine < len(runes) && runes[nextLine] == '\n' {
				nextLine++
			}
		}

		first := lineStart
		for first < lineEnd && isPythonSpace(runes[first]) {
			first++
		}
		runChar, runLen := fenceAt(runes, first, lineEnd)

		if fenceChar == 0 {
			if runLen >= 3 {
				fenceChar = runChar
				fenceLen = runLen
				blankExceptNewline(runes, lineStart, nextLine)
			}
		} else {
			blankExceptNewline(runes, lineStart, nextLine)
			if runChar == fenceChar && runLen >= fenceLen {
				fenceChar = 0
				fenceLen = 0
			}
		}

		lineStart = nextLine
	}
}

func fenceAt(runes []rune, start, end int) (rune, int) {
	if start >= end || (runes[start] != '`' && runes[start] != '~') {
		return 0, 0
	}
	char := runes[start]
	index := start + 1
	for index < end && runes[index] == char {
		index++
	}
	return char, index - start
}

func blankExceptNewline(runes []rune, start, end int) {
	for index := start; index < end; index++ {
		if runes[index] != '\n' {
			runes[index] = ' '
		}
	}
}

func isSplitLineBoundary(r rune) bool {
	return r == '\n' || r == '\r' || r == '\v' || r == '\f' ||
		(r >= 0x1c && r <= 0x1e) || r == 0x85 || r == 0x2028 || r == 0x2029
}

// maskInlineCode mirrors Python's (`+)([^\n]*?)\1 without relying on RE2
// backreferences. Python's opening quantifier is greedy but may backtrack; the
// descending run lengths below preserve that small edge-case of the reference
// implementation as well as the usual Markdown cases.
func maskInlineCode(runes []rune) {
	for index := 0; index < len(runes); {
		if runes[index] != '`' {
			index++
			continue
		}

		openingLen := backtickRun(runes, index)
		matchEnd := inlineCodeEnd(runes, index, openingLen)
		if matchEnd < 0 {
			index++
			continue
		}
		blankExceptNewline(runes, index, matchEnd)
		index = matchEnd
	}
}

func backtickRun(runes []rune, start int) int {
	end := start
	for end < len(runes) && runes[end] == '`' {
		end++
	}
	return end - start
}

func inlineCodeEnd(runes []rune, start, openingLen int) int {
	for runLen := openingLen; runLen >= 1; runLen-- {
		contentStart := start + runLen
		for index := contentStart; index < len(runes); index++ {
			if runes[index] == '\n' {
				break
			}
			if hasBacktickRun(runes, index, runLen) {
				return index + runLen
			}
		}
	}
	return -1
}

func hasBacktickRun(runes []rune, start, length int) bool {
	if start+length > len(runes) {
		return false
	}
	for index := start; index < start+length; index++ {
		if runes[index] != '`' {
			return false
		}
	}
	return true
}

func maskURLs(runes []rune) {
	for index := 0; index < len(runes); {
		prefixLen := urlPrefixLength(runes, index)
		if prefixLen == 0 {
			index++
			continue
		}
		end := index + prefixLen
		if end == len(runes) || isURLTerminator(runes[end]) {
			index++
			continue
		}
		for end < len(runes) && !isURLTerminator(runes[end]) {
			end++
		}
		blankExceptNewline(runes, index, end)
		index = end
	}
}

func urlPrefixLength(runes []rune, start int) int {
	if start+7 <= len(runes) && equalASCII(runes[start:start+7], "http://") {
		return 7
	}
	if start+8 <= len(runes) && equalASCII(runes[start:start+8], "https://") {
		return 8
	}
	return 0
}

func equalASCII(runes []rune, ascii string) bool {
	if len(runes) != len(ascii) {
		return false
	}
	for index, want := range ascii {
		if runes[index] != want {
			return false
		}
	}
	return true
}

func isURLTerminator(r rune) bool {
	return isPythonSpace(r) || r == '<' || r == '>' || r == '(' || r == ')'
}

// IsCJKRelevant reports whether r belongs to the broad candidate range used
// for segment detection. This is intentionally separate from CJClassifier's
// narrower scoring range.
func IsCJKRelevant(r rune) bool {
	return (r >= 0x3040 && r <= 0x30ff) ||
		(r >= 0x3400 && r <= 0x4dbf) ||
		(r >= 0x4e00 && r <= 0x9fff) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0x20000 && r <= 0x323af)
}

// CountCJKRelevant counts runes in the broad candidate range.
func CountCJKRelevant(input string) int {
	count := 0
	for _, r := range input {
		if IsCJKRelevant(r) {
			count++
		}
	}
	return count
}

// IterCJSegments returns sentence and clause spans with at least minCJK
// relevant runes. Both granularities are retained so embedded Chinese clauses
// can be found inside otherwise Japanese sentences.
func IterCJSegments(input string, minCJK int) []Segment {
	runes := []rune(input)
	seen := make(map[[2]int]struct{})

	sentenceSpans := splitSpans(runes, 0, len(runes), isSentenceBoundary)
	for _, span := range sentenceSpans {
		if CountCJKRelevant(string(runes[span[0]:span[1]])) >= minCJK {
			seen[span] = struct{}{}
		}
		for _, clause := range splitSpans(runes, span[0], span[1], isClauseBoundary) {
			if CountCJKRelevant(string(runes[clause[0]:clause[1]])) >= minCJK {
				seen[clause] = struct{}{}
			}
		}
	}

	spans := make([][2]int, 0, len(seen))
	for span := range seen {
		spans = append(spans, span)
	}
	sort.Slice(spans, func(i, j int) bool {
		if spans[i][0] != spans[j][0] {
			return spans[i][0] < spans[j][0]
		}
		return spans[i][1] < spans[j][1]
	})

	segments := make([]Segment, 0, len(spans))
	for _, span := range spans {
		segments = append(segments, Segment{
			Start: span[0],
			End:   span[1],
			Text:  string(runes[span[0]:span[1]]),
		})
	}
	return segments
}

func splitSpans(runes []rune, start, end int, boundary func(rune) bool) [][2]int {
	spans := make([][2]int, 0)
	cursor := start
	for index := start; index < end; {
		if !boundary(runes[index]) {
			index++
			continue
		}
		boundaryEnd := index + 1
		for boundaryEnd < end && boundary(runes[boundaryEnd]) {
			boundaryEnd++
		}
		if trimmed, ok := trimSpan(runes, cursor, index); ok {
			spans = append(spans, trimmed)
		}
		cursor = boundaryEnd
		index = boundaryEnd
	}
	if trimmed, ok := trimSpan(runes, cursor, end); ok {
		spans = append(spans, trimmed)
	}
	return spans
}

func trimSpan(runes []rune, start, end int) ([2]int, bool) {
	for start < end && isPythonSpace(runes[start]) {
		start++
	}
	for end > start && isPythonSpace(runes[end-1]) {
		end--
	}
	return [2]int{start, end}, start < end
}

func isSentenceBoundary(r rune) bool {
	switch r {
	case '\n', '。', '！', '？', '!', '?':
		return true
	default:
		return false
	}
}

func isClauseBoundary(r rune) bool {
	switch r {
	case '、', '，', ',', ':', '：', '；', ';', '（', '）', '(', ')', '［', '］', '[', ']', '{', '}':
		return true
	default:
		return false
	}
}

// LineColumn converts a rune offset to the one-based line and column used by
// the Python implementation.
func LineColumn(input string, offset int) (line, column int) {
	runes := []rune(input)
	if offset < 0 {
		offset = 0
	}
	if offset > len(runes) {
		offset = len(runes)
	}
	line = 1
	previousNewline := -1
	for index := 0; index < offset; index++ {
		if runes[index] == '\n' {
			line++
			previousNewline = index
		}
	}
	if previousNewline < 0 {
		return line, offset + 1
	}
	return line, offset - previousNewline
}

func isPythonSpace(r rune) bool {
	// Python's str.isspace()/re \s include the ASCII information separators;
	// unicode.IsSpace covers the Unicode White_Space property.
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}
