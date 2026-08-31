from __future__ import annotations

import os
from pathlib import Path

DEFAULT_UNICODE_VERSION = "18.0.0"


def cache_dir() -> Path:
    if value := os.environ.get("XDG_CACHE_HOME"):
        return Path(value) / "jp-quality-gate"
    return Path.home() / ".cache" / "jp-quality-gate"


def default_unihan_table(version: str = DEFAULT_UNICODE_VERSION) -> Path:
    if value := os.environ.get("JPQG_UNIHAN_TABLE"):
        return Path(value).expanduser()
    return cache_dir() / f"unihan-suspicious-{version}.json"
