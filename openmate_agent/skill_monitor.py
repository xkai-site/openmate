from __future__ import annotations

import json
import math
from collections import defaultdict
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Literal
from uuid import uuid4

from pydantic import BaseModel, Field, model_validator

from openmate_shared.runtime_paths import resolve_workspace_root

SkillMonitorPhase = Literal["before", "after"]
SkillMonitorSource = Literal["model", "cli", "http", "unknown"]


def _utc_now() -> datetime:
    return datetime.now(UTC)


class SkillMonitorEvent(BaseModel):
    event_id: str = Field(min_length=1)
    phase: SkillMonitorPhase
    ts: datetime
    target_type: Literal["skill"] = "skill"
    node_id: str = ""
    source: SkillMonitorSource = "unknown"
    skill_name: str = ""
    skill_path: str = ""
    success: bool | None = None
    error_code: str | None = None
    error: str | None = None
    duration_ms: int | None = None

    @model_validator(mode="after")
    def _validate_after_fields(self) -> "SkillMonitorEvent":
        if self.phase == "after" and self.success is None:
            raise ValueError("after event requires success")
        return self


class SkillMonitorStore:
    def __init__(self, file_path: str | Path) -> None:
        self._file_path = Path(file_path).resolve()

    def append(self, event: SkillMonitorEvent) -> None:
        self._file_path.parent.mkdir(parents=True, exist_ok=True)
        with self._file_path.open("a", encoding="utf-8", newline="\n") as handle:
            handle.write(event.model_dump_json(exclude_none=True))
            handle.write("\n")

    def list_all(self) -> list[SkillMonitorEvent]:
        if not self._file_path.exists():
            return []
        items: list[SkillMonitorEvent] = []
        with self._file_path.open("r", encoding="utf-8") as handle:
            for raw in handle:
                line = raw.strip()
                if not line:
                    continue
                try:
                    items.append(SkillMonitorEvent.model_validate_json(line))
                except Exception:
                    continue
        return items


class SkillMonitorService:
    def __init__(self, workspace_root: str | Path, store: SkillMonitorStore | None = None) -> None:
        root = resolve_workspace_root(workspace_root)
        default_path = root / ".openmate" / "runtime" / "skill_monitor.jsonl"
        self._store = store or SkillMonitorStore(default_path)

    def record_before(
        self,
        *,
        node_id: str,
        source: SkillMonitorSource,
        skill_name: str,
        skill_path: str,
        ts: datetime | None = None,
    ) -> None:
        self._store.append(
            SkillMonitorEvent(
                event_id=str(uuid4()),
                phase="before",
                ts=ts or _utc_now(),
                node_id=node_id,
                source=source,
                skill_name=skill_name,
                skill_path=skill_path,
            )
        )

    def record_after(
        self,
        *,
        node_id: str,
        source: SkillMonitorSource,
        skill_name: str,
        skill_path: str,
        success: bool,
        error_code: str | None,
        error: str | None,
        duration_ms: int,
        ts: datetime | None = None,
    ) -> None:
        self._store.append(
            SkillMonitorEvent(
                event_id=str(uuid4()),
                phase="after",
                ts=ts or _utc_now(),
                node_id=node_id,
                source=source,
                skill_name=skill_name,
                skill_path=skill_path,
                success=success,
                error_code=error_code,
                error=error,
                duration_ms=duration_ms,
            )
        )

    def list_events(
        self,
        *,
        skill_name: str | None = None,
        source: SkillMonitorSource | None = None,
        success: bool | None = None,
        window_minutes: int | None = None,
        limit: int | None = None,
    ) -> list[SkillMonitorEvent]:
        events = self._store.list_all()
        if skill_name:
            events = [item for item in events if item.skill_name == skill_name]
        if source:
            events = [item for item in events if item.source == source]
        if success is not None:
            events = [item for item in events if item.phase == "after" and item.success == success]
        if window_minutes is not None and window_minutes > 0:
            cutoff = _utc_now() - timedelta(minutes=window_minutes)
            events = [item for item in events if item.ts >= cutoff]
        events.sort(key=lambda item: item.ts, reverse=True)
        if limit is not None and limit >= 0:
            return events[:limit]
        return events

    def summarize(
        self,
        *,
        skill_name: str | None = None,
        source: SkillMonitorSource | None = None,
        success: bool | None = None,
        window_minutes: int | None = None,
        limit: int | None = None,
    ) -> list[dict[str, float | int | str]]:
        events = self.list_events(
            skill_name=skill_name,
            source=source,
            success=success,
            window_minutes=window_minutes,
            limit=None,
        )
        after_events = [item for item in events if item.phase == "after"]
        grouped: dict[str, list[SkillMonitorEvent]] = defaultdict(list)
        for item in after_events:
            grouped[item.skill_name].append(item)

        summary: list[dict[str, float | int | str]] = []
        for name, items in grouped.items():
            count = len(items)
            success_count = sum(1 for item in items if item.success)
            durations = [item.duration_ms for item in items if item.duration_ms is not None]
            avg_duration = float(sum(durations) / len(durations)) if durations else 0.0
            p95_duration = float(self._percentile95(durations)) if durations else 0.0
            summary.append(
                {
                    "skill_name": name,
                    "count": count,
                    "success_rate": float(success_count / count) if count else 0.0,
                    "avg_duration_ms": avg_duration,
                    "p95_duration_ms": p95_duration,
                }
            )
        summary.sort(key=lambda item: (-int(item["count"]), str(item["skill_name"])))
        if limit is not None and limit >= 0:
            return summary[:limit]
        return summary

    @staticmethod
    def _percentile95(values: list[int]) -> int:
        if not values:
            return 0
        sorted_values = sorted(values)
        rank = int(math.ceil(0.95 * len(sorted_values))) - 1
        rank = max(0, min(rank, len(sorted_values) - 1))
        return sorted_values[rank]


def dump_events_json(events: list[SkillMonitorEvent]) -> list[dict[str, object]]:
    rows: list[dict[str, object]] = []
    for item in events:
        payload = item.model_dump(mode="json", exclude_none=True)
        if isinstance(payload.get("ts"), str):
            payload["ts"] = payload["ts"].replace("+00:00", "Z")
        rows.append(payload)
    return rows
