from jp_quality_gate.build_unihan import _build_table


def test_builds_error_for_chinese_simplified_without_japanese_evidence() -> None:
    props = {
        ord("经"): {
            "kIRG_GSource": ["G0-..."],
            "kTraditionalVariant": ["U+7D93"],
        },
        ord("經"): {
            "kIRG_JSource": ["J0-..."],
            "kJapaneseNewVariant": ["U+7D4C"],
        },
        ord("経"): {
            "kIRG_JSource": ["J0-..."],
        },
        ord("学"): {
            "kIRG_GSource": ["G0-..."],
            "kIRG_JSource": ["J0-..."],
            "kTraditionalVariant": ["U+5B78"],
        },
    }
    table = _build_table(props, unicode_version="test")
    chars = table["characters"]

    assert chars["经"]["severity"] == "error"
    assert chars["经"]["japanese_candidates"] == ["経"]
    assert "学" not in chars
