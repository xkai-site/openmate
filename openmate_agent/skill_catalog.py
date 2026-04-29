from __future__ import annotations

import json
from pathlib import Path

from openmate_shared.runtime_paths import resolve_workspace_root

from .models import SkillDescriptor, SkillQueryResult

SKILL_QUERY_THRESHOLD = 10
SKILL_DESCRIPTION_TRUNCATE = 25


class SkillCatalog:
    def __init__(self, *, workspace_root: str | Path) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)

    def query(self, *, extra_path: str | None = None, keyword: str | None = None) -> SkillQueryResult:
        skills = self.scan(extra_path=extra_path)
        if keyword:
            token = keyword.lower()
            skills = [item for item in skills if token in item.name.lower() or token in item.description.lower()]

        if len(skills) <= SKILL_QUERY_THRESHOLD:
            return SkillQueryResult(
                mode="skills",
                remaining_count=len(skills),
                skills=skills,
                threshold=SKILL_QUERY_THRESHOLD,
            )

        truncated: list[SkillDescriptor] = []
        for item in skills:
            truncated.append(
                item.model_copy(
                    update={
                        "description": item.description[:SKILL_DESCRIPTION_TRUNCATE],
                    }
                )
            )
        return SkillQueryResult(
            mode="truncated",
            remaining_count=len(skills),
            skills=truncated,
            threshold=SKILL_QUERY_THRESHOLD,
        )

    def scan(self, *, extra_path: str | None = None) -> list[SkillDescriptor]:
        roots: list[tuple[str, Path]] = [
            ("client", Path.home() / ".skill"),
            ("input", Path(extra_path).resolve() if extra_path else Path("__missing__")),
            ("project", self._workspace_root / ".skill"),
        ]
        discovered: list[SkillDescriptor] = []
        by_name: set[str] = set()
        for source, root in roots:
            if not root.exists() or not root.is_dir():
                continue
            for descriptor in self._scan_root(root=root, source=source):
                if descriptor.name in by_name:
                    continue
                by_name.add(descriptor.name)
                discovered.append(descriptor)
        discovered.sort(key=lambda item: item.name)
        return discovered

    def _scan_root(self, *, root: Path, source: str) -> list[SkillDescriptor]:
        items: list[SkillDescriptor] = []
        for skill_file in sorted(root.rglob("SKILL.md"), key=lambda path: str(path)):
            meta = self._parse_skill_meta(skill_file)
            if meta is None:
                continue
            name, description = meta
            if not name or not description:
                continue
            items.append(
                SkillDescriptor(
                    name=name,
                    description=description,
                    path=str(skill_file.resolve()),
                    source=source,
                )
            )
        return items

    @staticmethod
    def _parse_skill_meta(skill_file: Path) -> tuple[str, str] | None:
        companion = skill_file.parent / "metadata.json"
        if companion.exists() and companion.is_file():
            try:
                payload = json.loads(companion.read_text(encoding="utf-8"))
                if isinstance(payload, dict):
                    name = str(payload.get("name", "")).strip()
                    description = str(payload.get("description", "")).strip()
                    if name and description:
                        return name, description
            except Exception:
                pass

        try:
            lines = skill_file.read_text(encoding="utf-8").splitlines()
        except Exception:
            return None

        name = ""
        description = ""
        for raw in lines:
            line = raw.strip()
            if not line:
                continue
            if not name and line.startswith("#"):
                name = line.lstrip("#").strip()
                continue
            if line.startswith("-") or line.startswith("*") or line.startswith("```"):
                continue
            if ":" in line and line.split(":", 1)[0].lower() in {"name", "description"}:
                key, value = line.split(":", 1)
                if key.lower() == "name" and not name:
                    name = value.strip()
                    continue
                if key.lower() == "description" and not description:
                    description = value.strip()
                    continue
            if not description and not line.startswith("#"):
                description = line
                break
        if not name:
            return None
        if not description:
            return None
        return name, description
