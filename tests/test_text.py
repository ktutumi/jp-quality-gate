from jp_quality_gate.text import iter_cj_segments, mask_markdown_nonprose


def test_masks_fenced_and_inline_code_preserving_offsets() -> None:
    text = "説明です。\n```txt\n今天天气很好\n```\n`经済` はコード例。"
    masked = mask_markdown_nonprose(text)
    assert len(masked) == len(text)
    assert masked.count("\n") == text.count("\n")
    assert "今天天气很好" not in masked
    assert "经済" not in masked
    assert "説明です" in masked


def test_segments_include_embedded_clause() -> None:
    text = "設定を変更できます。可以使用以下方法，その後に再起動します。"
    segments = iter_cj_segments(text, min_cjk=4)
    values = [segment.text for segment in segments]
    assert any("可以使用以下方法" in value for value in values)
