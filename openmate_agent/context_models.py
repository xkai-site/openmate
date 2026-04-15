from __future__ import annotations

from typing import Any

from pydantic import BaseModel, ConfigDict, Field

from .session_models import SessionEventRecord, SessionRecord


class ContextSessionHistoryRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    session: SessionRecord
    events: list[SessionEventRecord] = Field(default_factory=list)


class ContextSnapshotRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    node_id: str = Field(min_length=1)
    user_memory: dict[str, Any] | None = None
    topic_memory: dict[str, Any] | None = None
    node_memory: dict[str, Any] | None = None
    global_index: Any | None = None
    session_history: list[ContextSessionHistoryRecord] = Field(default_factory=list)
