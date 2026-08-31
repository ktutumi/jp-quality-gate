from __future__ import annotations

import re
from dataclasses import dataclass
from typing import Iterator

_INLINE_CODE_RE = re.compile(r"(`+)([^\n]*?)\1")
_URL_RE = re.compile(r"https?://[^\s<>()]+")
_SENTENCE_BOUNDARY_RE = re.compile(r"[\n。！？!?]+")
_CLAUSE_BOUNDARY_RE = re.compile(r"[、，,:：；;（）()［］\[\]{}]+")


@dataclass(frozen=True, slots=True)
class Segment:
    start: int
    end: int
    text: str


def _blank_preserve_newlines(text: str) -> str:
    return "".join("\n" if char == "\n" else " " for char in text)


def mask_markdown_nonprose(text: str, *, include_code: bool = False) -> str:
    """Mask Markdown code and URLs while preserving offsets and newlines."""
    if include_code:
        masked = text
    else:
        lines = text.splitlines(keepends=True)
        output: list[str] = []
        fence_char: str | None = None
        fence_len = 0

        for line in lines:
            stripped = line.lstrip()
            match = re.match(r"(`{3,}|~{3,})", stripped)

            if fence_char is None and match:
                token = match.group(1)
                fence_char, fence_len = token[0], len(token)
                output.append(_blank_preserve_newlines(line))
                continue

            if fence_char is not None:
                output.append(_blank_preserve_newlines(line))
                closing = re.match(rf"{re.escape(fence_char)}{{{fence_len},}}", stripped)
                if closing:
                    fence_char = None
                    fence_len = 0
                continue

            output.append(line)

        masked = "".join(output)
        masked = _INLINE_CODE_RE.sub(lambda m: _blank_preserve_newlines(m.group(0)), masked)

    masked = _URL_RE.sub(lambda m: " " * len(m.group(0)), masked)
    return masked


def is_cjk_relevant(char: str) -> bool:
    cp = ord(char)
    return (
        0x3040 <= cp <= 0x30FF  # Hiragana + Katakana
        or 0x3400 <= cp <= 0x4DBF  # CJK Extension A
        or 0x4E00 <= cp <= 0x9FFF  # CJK Unified Ideographs
        or 0xF900 <= cp <= 0xFAFF  # CJK Compatibility Ideographs
        or 0x20000 <= cp <= 0x323AF  # CJK Extensions B-I (broad range)
    )


def count_cjk_relevant(text: str) -> int:
    return sum(1 for char in text if is_cjk_relevant(char))


def _trim_span(text: str, start: int, end: int) -> tuple[int, int] | None:
    while start < end and text[start].isspace():
        start += 1
    while end > start and text[end - 1].isspace():
        end -= 1
    return (start, end) if start < end else None


def _split_spans(text: str, start: int, end: int, boundary: re.Pattern[str]) -> Iterator[tuple[int, int]]:
    cursor = start
    for match in boundary.finditer(text, start, end):
        trimmed = _trim_span(text, cursor, match.start())
        if trimmed:
            yield trimmed
        cursor = match.end()
    trimmed = _trim_span(text, cursor, end)
    if trimmed:
        yield trimmed


def iter_cj_segments(masked_text: str, *, min_cjk: int = 4) -> list[Segment]:
    """Return sentence and clause spans suitable for CJClassifier.

    Smaller clause spans are included to catch a Chinese phrase embedded in an
    otherwise Japanese sentence. Duplicates are removed while offsets are kept.
    """
    spans: set[tuple[int, int]] = set()

    for sentence_start, sentence_end in _split_spans(
        masked_text, 0, len(masked_text), _SENTENCE_BOUNDARY_RE
    ):
        sentence = masked_text[sentence_start:sentence_end]
        if count_cjk_relevant(sentence) >= min_cjk:
            spans.add((sentence_start, sentence_end))

        for clause_start, clause_end in _split_spans(
            masked_text, sentence_start, sentence_end, _CLAUSE_BOUNDARY_RE
        ):
            clause = masked_text[clause_start:clause_end]
            if count_cjk_relevant(clause) >= min_cjk:
                spans.add((clause_start, clause_end))

    return [Segment(start, end, masked_text[start:end]) for start, end in sorted(spans)]


def line_column(text: str, offset: int) -> tuple[int, int]:
    line = text.count("\n", 0, offset) + 1
    previous_newline = text.rfind("\n", 0, offset)
    column = offset + 1 if previous_newline < 0 else offset - previous_newline
    return line, column
