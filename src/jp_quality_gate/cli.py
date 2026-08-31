from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

from .cj import CJClassifierError
from .gate import QualityGate
from .paths import DEFAULT_UNICODE_VERSION, default_unihan_table
from .unihan import UnihanTableError


def _read_input(path: Path | None) -> str:
    if path is None or str(path) == "-":
        return sys.stdin.read()
    return path.read_text(encoding="utf-8")


def main() -> None:
    parser = argparse.ArgumentParser(description="Check Japanese quality of LLM-generated text")
    parser.add_argument("file", nargs="?", type=Path, help="Input file; omit or use - for stdin")
    parser.add_argument("--unicode-version", default=DEFAULT_UNICODE_VERSION)
    parser.add_argument("--unihan-table", type=Path)
    parser.add_argument("--cj-min-cjk", type=int, default=4)
    parser.add_argument("--cj-min-gap", type=float, default=0.15)
    parser.add_argument("--include-code", action="store_true")
    parser.add_argument("--warnings-as-errors", action="store_true")
    parser.add_argument("--pretty", action="store_true")
    args = parser.parse_args()

    if not (0.0 <= args.cj_min_gap <= 1.0):
        parser.error("--cj-min-gap must be between 0 and 1")
    if args.cj_min_cjk < 1:
        parser.error("--cj-min-cjk must be >= 1")

    table = args.unihan_table or default_unihan_table(args.unicode_version)

    try:
        text = _read_input(args.file)
        gate = QualityGate(unihan_table=table)
        result = gate.check(
            text,
            include_code=args.include_code,
            cj_min_cjk=args.cj_min_cjk,
            cj_min_gap=args.cj_min_gap,
            warnings_as_errors=args.warnings_as_errors,
        )
    except (OSError, UnihanTableError, CJClassifierError) as exc:
        print(json.dumps({"pass": False, "internal_error": str(exc)}, ensure_ascii=False))
        raise SystemExit(2) from exc

    print(
        json.dumps(
            result.to_dict(),
            ensure_ascii=False,
            indent=2 if args.pretty else None,
        )
    )
    raise SystemExit(0 if result.passed else 1)


if __name__ == "__main__":
    main()
