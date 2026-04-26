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


ToolMonitorPhase = Literal["before", "after"]
ToolMonitorSource = Literal["model", "cli", "http", "unknown"]


def _utc_now() -> datetime:
    return datetime.now(UTC)


class ToolMonitorEvent(BaseModel):
    event_id: str = Field(min_length=1)
    phase: ToolMonitorPhase
    ts: datetime
    node_id: str = ""
    tool_name: str = ""
    source: ToolMonitorSource = "unknown"
    is_safe: bool = False
    is_read_only: bool = False
    request_id: str | None = None
    success: bool | None = None
    error_code: str | None = None
    duration_ms: int | None = None

    @model_validator(mode="after")
    def _validate_after_fields(self) -> "ToolMonitorEvent":
        if self.phase == "after" and self.success is None:
            raise ValueError("after event requires success")
        return self


class ToolMonitorStore:
    def __init__(self, file_path: str | Path) -> None:
        self._file_path = Path(file_path).resolve()

    @property
    def file_path(self) -> Path:
        return self._file_path

    def append(self, event: ToolMonitorEvent) -> None:
        self._file_path.parent.mkdir(parents=True, exist_ok=True)
        with self._file_path.open("a", encoding="utf-8", newline="\n") as handle:
            handle.write(event.model_dump_json(exclude_none=True))
            handle.write("\n")

    def list_all(self) -> list[ToolMonitorEvent]:
        if not self._file_path.exists():
            return []
        items: list[ToolMonitorEvent] = []
        with self._file_path.open("r", encoding="utf-8") as handle:
            for raw in handle:
                line = raw.strip()
                if not line:
                    continue
                try:
                    items.append(ToolMonitorEvent.model_validate_json(line))
                except Exception:
                    continue
        return items


class ToolMonitorService:
    def __init__(self, workspace_root: str | Path, store: ToolMonitorStore | None = None) -> None:
        root = resolve_workspace_root(workspace_root)
        default_path = root / ".openmate" / "runtime" / "tool_monitor.jsonl"
        self._store = store or ToolMonitorStore(default_path)

    @property
    def store(self) -> ToolMonitorStore:
        return self._store

    def record_before(
        self,
        *,
        node_id: str,
        tool_name: str,
        source: ToolMonitorSource,
        is_safe: bool,
        is_read_only: bool,
        request_id: str | None = None,
        ts: datetime | None = None,
    ) -> None:
        event = ToolMonitorEvent(
            event_id=str(uuid4()),
            phase="before",
            ts=ts or _utc_now(),
            node_id=node_id,
            tool_name=tool_name,
            source=source,
            is_safe=is_safe,
            is_read_only=is_read_only,
            request_id=request_id,
        )
        self._store.append(event)

    def record_after(
        self,
        *,
        node_id: str,
        tool_name: str,
        source: ToolMonitorSource,
        is_safe: bool,
        is_read_only: bool,
        success: bool,
        error_code: str | None,
        duration_ms: int,
        request_id: str | None = None,
        ts: datetime | None = None,
    ) -> None:
        event = ToolMonitorEvent(
            event_id=str(uuid4()),
            phase="after",
            ts=ts or _utc_now(),
            node_id=node_id,
            tool_name=tool_name,
            source=source,
            is_safe=is_safe,
            is_read_only=is_read_only,
            request_id=request_id,
            success=success,
            error_code=error_code,
            duration_ms=duration_ms,
        )
        self._store.append(event)

    def list_events(
        self,
        *,
        tool_name: str | None = None,
        node_id: str | None = None,
        source: ToolMonitorSource | None = None,
        success: bool | None = None,
        limit: int | None = None,
        window_minutes: int | None = None,
    ) -> list[ToolMonitorEvent]:
        events = self._store.list_all()
        if tool_name:
            events = [item for item in events if item.tool_name == tool_name]
        if node_id:
            events = [item for item in events if item.node_id == node_id]
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
        tool_name: str | None = None,
        node_id: str | None = None,
        source: ToolMonitorSource | None = None,
        success: bool | None = None,
        limit: int | None = None,
        window_minutes: int | None = None,
    ) -> list[dict[str, float | int | str]]:
        events = self.list_events(
            tool_name=tool_name,
            node_id=node_id,
            source=source,
            success=success,
            limit=None,
            window_minutes=window_minutes,
        )
        after_events = [item for item in events if item.phase == "after"]
        grouped: dict[str, list[ToolMonitorEvent]] = defaultdict(list)
        for item in after_events:
            grouped[item.tool_name].append(item)

        summary: list[dict[str, float | int | str]] = []
        for name, items in grouped.items():
            count = len(items)
            success_count = sum(1 for item in items if item.success)
            durations = [item.duration_ms for item in items if item.duration_ms is not None]
            avg_duration = float(sum(durations) / len(durations)) if durations else 0.0
            p95_duration = float(self._percentile95(durations)) if durations else 0.0
            summary.append(
                {
                    "tool_name": name,
                    "count": count,
                    "success_rate": float(success_count / count) if count else 0.0,
                    "avg_duration_ms": avg_duration,
                    "p95_duration_ms": p95_duration,
                }
            )
        summary.sort(key=lambda item: (-int(item["count"]), str(item["tool_name"])))
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


def dump_events_json(events: list[ToolMonitorEvent]) -> list[dict[str, object]]:
    rows: list[dict[str, object]] = []
    for item in events:
        payload = item.model_dump(mode="json", exclude_none=True)
        if isinstance(payload.get("ts"), str):
            payload["ts"] = payload["ts"].replace("+00:00", "Z")
        rows.append(payload)
    return rows
