import json
from pathlib import Path

from jp_quality_gate.unihan import UnihanScanner


def test_detects_simplified_form_and_preserves_location(tmp_path: Path) -> None:
    table = {
        "schema_version": 1,
        "unicode_version": "test",
        "characters": {
            "经": {
                "rule": "simplified_chinese_form",
                "severity": "error",
                "traditional_variants": ["經"],
                "japanese_candidates": ["経"],
            }
        },
    }
    path = tmp_path / "unihan.json"
    path.write_text(json.dumps(table, ensure_ascii=False), encoding="utf-8")

    scanner = UnihanScanner(path)
    text = "これは\n经済です。"
    issues = scanner.scan(text, text)

    assert len(issues) == 1
    assert issues[0].text == "经"
    assert issues[0].line == 2
    assert issues[0].column == 1
    assert issues[0].details["japanese_candidates"] == ["経"]
