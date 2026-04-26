from __future__ import annotations

from pathlib import Path
from typing import Any


_FILE_PATH_FIELDS = {"path"}
_DIR_PATH_FIELDS = {"cwd", "scope"}


def normalize_dir_prefix(path_value: str) -> str:
    text = str(path_value or "").strip()
    if text == "":
        return ""
    normalized = text.replace("\\", "/")
    while "//" in normalized:
        normalized = normalized.replace("//", "/")
    if len(normalized) > 1 and normalized.endswith("/"):
        normalized = normalized.rstrip("/")
    return normalized


def directory_prefix_match(*, rule_prefix: str, candidate_dir: str) -> bool:
    normalized_rule = normalize_dir_prefix(rule_prefix).lower()
    normalized_candidate = normalize_dir_prefix(candidate_dir).lower()
    if normalized_rule == "" or normalized_candidate == "":
        return False
    if normalized_candidate == normalized_rule:
        return True
    return normalized_candidate.startswith(f"{normalized_rule}/")


def extract_tool_directory_prefixes(
    *,
    payload: dict[str, Any],
    workspace_root: Path,
) -> list[str]:
    prefixes: set[str] = set()

    def add_prefix(raw_path: str, *, treat_as_file: bool) -> None:
        path_text = str(raw_path or "").strip()
        if path_text == "":
            return
        raw = Path(path_text)
        resolved = raw if raw.is_absolute() else (workspace_root / raw)
        target = resolved.parent if treat_as_file else resolved
        normalized = normalize_dir_prefix(str(target.resolve()))
        if normalized:
            prefixes.add(normalized)

    for key, value in payload.items():
        if not isinstance(value, str):
            continue
        if key in _FILE_PATH_FIELDS:
            add_prefix(value, treat_as_file=True)
        elif key in _DIR_PATH_FIELDS:
            add_prefix(value, treat_as_file=False)

    operations = payload.get("operations")
    if isinstance(operations, list):
        for operation in operations:
            if not isinstance(operation, dict):
                continue
            path_value = operation.get("path")
            if isinstance(path_value, str):
                add_prefix(path_value, treat_as_file=True)

    if not prefixes:
        fallback = normalize_dir_prefix(str(workspace_root.resolve()))
        if fallback:
            prefixes.add(fallback)
    return sorted(prefixes)
