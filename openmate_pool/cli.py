"""Thin Python wrapper around the Go pool CLI."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from .binary import ensure_pool_binary


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    binary = ensure_pool_binary()
    result = subprocess.run(
        [str(binary), *args],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
        cwd=Path(__file__).resolve().parents[1],
    )
    if result.stdout:
        print(result.stdout, end="")
    if result.stderr:
        print(result.stderr, end="", file=sys.stderr)
    return result.returncode


if __name__ == "__main__":
    raise SystemExit(main())
