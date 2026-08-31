from __future__ import annotations

import json
from pathlib import Path
from typing import Any

from .models import Issue
from .text import line_column


class UnihanTableError(RuntimeError):
    pass


class UnihanScanner:
    def __init__(self, path: Path):
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except FileNotFoundError as exc:
            raise UnihanTableError(
                f"Unihan table not found: {path}. Run jpqg-build-unihan first."
            ) from exc
        except (json.JSONDecodeError, OSError) as exc:
            raise UnihanTableError(f"Cannot load Unihan table {path}: {exc}") from exc

        if data.get("schema_version") != 1 or not isinstance(data.get("characters"), dict):
            raise UnihanTableError(f"Unsupported Unihan table schema: {path}")

        self.path = path
        self.unicode_version = data.get("unicode_version", "unknown")
        self.characters: dict[str, dict[str, Any]] = data["characters"]

    def scan(self, original_text: str, masked_text: str, *, max_issues: int = 50) -> list[Issue]:
        issues: list[Issue] = []
        for offset, char in enumerate(masked_text):
            record = self.characters.get(char)
            if not record:
                continue

            line, column = line_column(original_text, offset)
            details = {
                "codepoint": f"U+{ord(char):04X}",
                "unicode_version": self.unicode_version,
            }
            for key in ("traditional_variants", "japanese_candidates", "evidence"):
                value = record.get(key)
                if value:
                    details[key] = value

            rule = record["rule"]
            if rule == "simplified_chinese_form":
                message = f"Japanese-unattested simplified Chinese form detected: {char}"
            else:
                message = f"Han character has Chinese source evidence but no strong Japanese evidence: {char}"

            issues.append(
                Issue(
                    rule=rule,
                    severity=record["severity"],
                    message=message,
                    start=offset,
                    end=offset + 1,
                    text=original_text[offset : offset + 1],
                    line=line,
                    column=column,
                    details=details,
                )
            )
            if len(issues) >= max_issues:
                break

        return issues
