from __future__ import annotations

import argparse
import json
import re
import urllib.request
import zipfile
from collections import defaultdict
from pathlib import Path
from typing import DefaultDict

from .paths import DEFAULT_UNICODE_VERSION, cache_dir, default_unihan_table

UNICODE_URL = "https://www.unicode.org/Public/{version}/ucd/Unihan.zip"
UCP_RE = re.compile(r"U\+([0-9A-F]{4,6})")

# Strong evidence that the exact character form is represented in Japanese data.
JAPANESE_EVIDENCE = {
    "kIRG_JSource",
    "kJis0",
    "kJis1",
    "kJIS0213",
    "kJoyoKanji",
    "kJinmeiyoKanji",
    "kIBMJapan",
    "kMojiJoho",
    "kRSAdobe_Japan1_6",
    "kJapanese",
    "kJapaneseKun",
    "kJapaneseOn",
    "kJapaneseNewVariant",
    "kJapaneseOldVariant",
}

INTERESTING_FIELDS = JAPANESE_EVIDENCE | {
    "kIRG_GSource",
    "kTraditionalVariant",
    "kSimplifiedVariant",
}


def _download_unihan(version: str, destination: Path) -> None:
    destination.parent.mkdir(parents=True, exist_ok=True)
    url = UNICODE_URL.format(version=version)
    request = urllib.request.Request(url, headers={"User-Agent": "jp-quality-gate/0.1"})
    with urllib.request.urlopen(request, timeout=60) as response, destination.open("wb") as output:
        output.write(response.read())


def _parse_zip(path: Path) -> dict[int, dict[str, list[str]]]:
    props: DefaultDict[int, DefaultDict[str, list[str]]] = defaultdict(lambda: defaultdict(list))
    with zipfile.ZipFile(path) as archive:
        for name in archive.namelist():
            if not name.endswith(".txt"):
                continue
            with archive.open(name) as raw:
                for byte_line in raw:
                    line = byte_line.decode("utf-8").strip()
                    if not line or line.startswith("#"):
                        continue
                    parts = line.split("\t", 2)
                    if len(parts) != 3:
                        continue
                    cp_text, field, value = parts
                    if field not in INTERESTING_FIELDS:
                        continue
                    cp = int(cp_text.removeprefix("U+"), 16)
                    props[cp][field].append(value)
    return {cp: dict(fields) for cp, fields in props.items()}


def _codepoints(values: list[str]) -> list[int]:
    result: list[int] = []
    for value in values:
        result.extend(int(match.group(1), 16) for match in UCP_RE.finditer(value))
    return result


def _has_japanese_evidence(fields: dict[str, list[str]]) -> bool:
    return any(field in fields for field in JAPANESE_EVIDENCE)


def _build_table(
    props: dict[int, dict[str, list[str]]], *, unicode_version: str
) -> dict[str, object]:
    characters: dict[str, object] = {}

    for cp, fields in props.items():
        char = chr(cp)
        has_japanese = _has_japanese_evidence(fields)
        traditional_cps = _codepoints(fields.get("kTraditionalVariant", []))
        has_g_source = "kIRG_GSource" in fields

        if traditional_cps and not has_japanese:
            japanese_candidates: set[str] = set()
            for traditional_cp in traditional_cps:
                traditional_fields = props.get(traditional_cp, {})
                for candidate_cp in _codepoints(traditional_fields.get("kJapaneseNewVariant", [])):
                    japanese_candidates.add(chr(candidate_cp))

            characters[char] = {
                "rule": "simplified_chinese_form",
                "severity": "error",
                "traditional_variants": [chr(value) for value in traditional_cps],
                "japanese_candidates": sorted(japanese_candidates),
                "evidence": ["kTraditionalVariant", *( ["kIRG_GSource"] if has_g_source else [] )],
            }
            continue

        if has_g_source and not has_japanese:
            characters[char] = {
                "rule": "chinese_han_without_japanese_source",
                "severity": "warning",
                "evidence": ["kIRG_GSource"],
            }

    return {
        "schema_version": 1,
        "unicode_version": unicode_version,
        "generator": "jp-quality-gate 0.1.0",
        "notes": (
            "Heuristic table for LLM-output quality gating. It is not a normative statement "
            "that warning characters are invalid Japanese."
        ),
        "characters": characters,
    }


def build(unihan_zip: Path, output: Path, *, unicode_version: str) -> None:
    props = _parse_zip(unihan_zip)
    table = _build_table(props, unicode_version=unicode_version)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(
        json.dumps(table, ensure_ascii=False, separators=(",", ":")),
        encoding="utf-8",
    )


def main() -> None:
    parser = argparse.ArgumentParser(description="Build the static Unihan quality-gate table")
    parser.add_argument("--version", default=DEFAULT_UNICODE_VERSION, help="Unicode version")
    parser.add_argument("--unihan-zip", type=Path, help="Use an existing Unihan.zip")
    parser.add_argument("--output", type=Path, help="Output JSON path")
    args = parser.parse_args()

    output = args.output or default_unihan_table(args.version)
    if args.unihan_zip:
        source = args.unihan_zip
        if not source.exists():
            parser.error(f"Unihan zip does not exist: {source}")
    else:
        cache = cache_dir() / f"Unihan-{args.version}.zip"
        if not cache.exists():
            print(f"Downloading {UNICODE_URL.format(version=args.version)}")
            _download_unihan(args.version, cache)
        source = cache

    build(source, output, unicode_version=args.version)
    data = json.loads(output.read_text(encoding="utf-8"))
    errors = sum(
        1 for record in data["characters"].values() if record["severity"] == "error"
    )
    warnings = len(data["characters"]) - errors
    print(f"Wrote {output} ({errors} error chars, {warnings} warning chars)")


if __name__ == "__main__":
    main()
