from __future__ import annotations

from dataclasses import dataclass
from importlib.metadata import PackageNotFoundError, version
from typing import Any

from .models import Issue
from .text import iter_cj_segments, line_column


class CJClassifierError(RuntimeError):
    pass


@dataclass(frozen=True, slots=True)
class CJDetection:
    language: str
    gap: float
    scores: str


class CJDetector:
    def __init__(self) -> None:
        try:
            from cjclassifier import CJClassifier
            from cjclassifier.classifier import Results
        except ImportError as exc:
            raise CJClassifierError(
                "cjclassifier is not installed. Install the project dependencies first."
            ) from exc

        self._classifier = CJClassifier.load()
        self._results_type = Results
        try:
            self.package_version = version("cjclassifier")
        except PackageNotFoundError:
            self.package_version = "unknown"

    @staticmethod
    def _language_name(value: Any) -> str:
        name = getattr(value, "name", str(value).split(".")[-1])
        return {
            "JAPANESE": "ja",
            "CHINESE_SIMPLIFIED": "zh-hans",
            "CHINESE_TRADITIONAL": "zh-hant",
            "UNKNOWN": "unknown",
        }.get(name, name.lower())

    def detect(self, text: str) -> CJDetection:
        results = self._results_type()
        self._classifier.detect(text, results)
        return CJDetection(
            language=self._language_name(results.result),
            gap=float(results.gap),
            scores=results.to_short_string(),
        )

    def scan(
        self,
        original_text: str,
        masked_text: str,
        *,
        min_cjk: int = 4,
        min_gap: float = 0.15,
    ) -> list[Issue]:
        candidates: list[Issue] = []
        for segment in iter_cj_segments(masked_text, min_cjk=min_cjk):
            detection = self.detect(segment.text)
            if detection.language not in {"zh-hans", "zh-hant"}:
                continue

            severity = "error" if detection.gap >= min_gap else "warning"
            line, column = line_column(original_text, segment.start)
            candidates.append(
                Issue(
                    rule="chinese_segment",
                    severity=severity,
                    message=(
                        f"Segment classified as {detection.language} "
                        f"(gap={detection.gap:.3f})"
                    ),
                    start=segment.start,
                    end=segment.end,
                    text=original_text[segment.start : segment.end],
                    line=line,
                    column=column,
                    details={
                        "language": detection.language,
                        "gap": detection.gap,
                        "scores": detection.scores,
                        "min_gap": min_gap,
                    },
                )
            )

        return _dedupe_overlapping(candidates)


def _dedupe_overlapping(issues: list[Issue]) -> list[Issue]:
    """Prefer the smallest pinpointing span for overlapping same-language findings."""
    ordered = sorted(issues, key=lambda issue: (issue.end - issue.start, -issue.details["gap"]))
    kept: list[Issue] = []
    for issue in ordered:
        lang = issue.details.get("language")
        if any(
            other.details.get("language") == lang
            and other.start >= issue.start
            and other.end <= issue.end
            for other in kept
        ):
            continue
        kept.append(issue)
    return sorted(kept, key=lambda issue: issue.start)
