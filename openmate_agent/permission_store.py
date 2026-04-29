from __future__ import annotations

import json
from pathlib import Path
from typing import Any, Protocol

from openmate_shared.runtime_paths import resolve_workspace_root

from .approval import normalize_dir_prefix
from .models import PermissionRule, UserSkillAllow
from .vos_cli import run_vos_cli


class PermissionStore(Protocol):
    def list_topic_tool_allows(self, *, topic_id: str) -> list[PermissionRule]: ...

    def add_topic_tool_allow(self, *, topic_id: str, tool_name: str, dir_prefix: str) -> None: ...

    def list_user_skill_allows(self) -> list[UserSkillAllow]: ...

    def upsert_user_skill_allow(
        self, *, skill_name: str, skill_path: str, skill_mtime: str, allowed_roots: list[str]
    ) -> None: ...


class VosPermissionStore:
    def __init__(self, *, workspace_root: str | Path) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)

    def list_topic_tool_allows(self, *, topic_id: str) -> list[PermissionRule]:
        stdout = run_vos_cli(
            workspace_root=self._workspace_root,
            command=["permission", "topic", "list", "--topic-id", topic_id],
        )
        payload = _parse_json(stdout)
        rows = payload.get("tool_allows")
        if rows is None:
            rows = payload.get("items")
        rules: list[PermissionRule] = []
        if isinstance(rows, list):
            for row in rows:
                if not isinstance(row, dict):
                    continue
                tool_name = str(row.get("tool_name", "")).strip()
                dir_prefix = normalize_dir_prefix(str(row.get("dir_prefix", "")).strip())
                if tool_name and dir_prefix:
                    rules.append(PermissionRule(tool_name=tool_name, normalized_dir_prefix=dir_prefix))
        return rules

    def add_topic_tool_allow(self, *, topic_id: str, tool_name: str, dir_prefix: str) -> None:
        normalized = normalize_dir_prefix(dir_prefix)
        if normalized == "":
            return
        run_vos_cli(
            workspace_root=self._workspace_root,
            command=[
                "permission",
                "topic",
                "add",
                "--topic-id",
                topic_id,
                "--tool-name",
                tool_name,
                "--dir-prefix",
                normalized,
            ],
        )

    def list_user_skill_allows(self) -> list[UserSkillAllow]:
        stdout = run_vos_cli(
            workspace_root=self._workspace_root,
            command=["permission", "user", "list"],
        )
        payload = _parse_json(stdout)
        rows = payload.get("skill_allows")
        if rows is None:
            rows = payload.get("items")
        records: list[UserSkillAllow] = []
        if isinstance(rows, list):
            for row in rows:
                if not isinstance(row, dict):
                    continue
                skill_name = str(row.get("skill_name", "")).strip()
                skill_path = str(row.get("skill_path", "")).strip()
                skill_mtime = str(row.get("skill_mtime", "")).strip()
                roots_raw = row.get("allowed_roots", [])
                allowed_roots = [str(root).strip() for root in roots_raw] if isinstance(roots_raw, list) else []
                if skill_name and skill_path and skill_mtime:
                    records.append(
                        UserSkillAllow(
                            skill_name=skill_name,
                            skill_path=skill_path,
                            skill_mtime=skill_mtime,
                            allowed_roots=allowed_roots,
                        )
                    )
        return records

    def upsert_user_skill_allow(
        self, *, skill_name: str, skill_path: str, skill_mtime: str, allowed_roots: list[str]
    ) -> None:
        run_vos_cli(
            workspace_root=self._workspace_root,
            command=[
                "permission",
                "user",
                "add",
                "--skill-name",
                skill_name,
                "--skill-path",
                skill_path,
                "--skill-mtime",
                skill_mtime,
                *[token for root in allowed_roots for token in ("--allow-root", root)],
            ],
        )


def _parse_json(text: str) -> dict[str, Any]:
    value = json.loads(text or "{}")
    if isinstance(value, dict):
        return value
    if isinstance(value, list):
        return {"items": value}
    return {}
