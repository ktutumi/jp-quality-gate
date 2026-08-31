from __future__ import annotations

from dataclasses import asdict, dataclass, field
from typing import Any, Literal

Severity = Literal["error", "warning"]


@dataclass(slots=True)
class Issue:
    rule: str
    severity: Severity
    message: str
    start: int
    end: int
    text: str
    line: int
    column: int
    details: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return asdict(self)


@dataclass(slots=True)
class GateResult:
    issues: list[Issue]
    meta: dict[str, Any] = field(default_factory=dict)

    @property
    def errors(self) -> list[Issue]:
        return [issue for issue in self.issues if issue.severity == "error"]

    @property
    def warnings(self) -> list[Issue]:
        return [issue for issue in self.issues if issue.severity == "warning"]

    @property
    def passed(self) -> bool:
        return not self.errors

    def to_dict(self) -> dict[str, Any]:
        return {
            "pass": self.passed,
            "summary": {
                "errors": len(self.errors),
                "warnings": len(self.warnings),
                "issues": len(self.issues),
            },
            "issues": [issue.to_dict() for issue in self.issues],
            "meta": self.meta,
        }
