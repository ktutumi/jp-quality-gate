from __future__ import annotations

from pathlib import Path

from .cj import CJDetector
from .models import GateResult, Issue
from .text import mask_markdown_nonprose
from .unihan import UnihanScanner


class QualityGate:
    def __init__(self, *, unihan_table: Path, cj_detector: CJDetector | None = None) -> None:
        self.unihan = UnihanScanner(unihan_table)
        self.cj = cj_detector or CJDetector()

    def check(
        self,
        text: str,
        *,
        include_code: bool = False,
        cj_min_cjk: int = 4,
        cj_min_gap: float = 0.15,
        warnings_as_errors: bool = False,
    ) -> GateResult:
        masked = mask_markdown_nonprose(text, include_code=include_code)
        issues: list[Issue] = []
        issues.extend(self.unihan.scan(text, masked))
        issues.extend(
            self.cj.scan(
                text,
                masked,
                min_cjk=cj_min_cjk,
                min_gap=cj_min_gap,
            )
        )

        if warnings_as_errors:
            for issue in issues:
                if issue.severity == "warning":
                    issue.severity = "error"

        issues.sort(key=lambda issue: (issue.start, 0 if issue.severity == "error" else 1))
        return GateResult(
            issues=issues,
            meta={
                "unicode_version": self.unihan.unicode_version,
                "cjclassifier_version": getattr(self.cj, "package_version", "injected"),
                "cj_min_cjk": cj_min_cjk,
                "cj_min_gap": cj_min_gap,
                "include_code": include_code,
            },
        )
