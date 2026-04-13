"""Pydantic models for the scheduler runtime snapshot."""

from __future__ import annotations

from datetime import datetime, timezone
from enum import Enum

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


class TopicQueueLevel(str, Enum):
    L0 = "L0"
    L1 = "L1"
    L2 = "L2"
    L3 = "L3"


class NodeStatus(str, Enum):
    PENDING = "pending"
    READY = "ready"
    RUNNING = "running"
    BLOCKED = "blocked"
    RETRY_COOLDOWN = "retry_cooldown"
    WAITING_EXTERNAL = "waiting_external"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"


class NodePriority(BaseModel):
    model_config = ConfigDict(extra="forbid")

    label: str = Field(min_length=1)
    rank: int = Field(ge=0, description="Lower rank means higher priority.")


class TopicNode(BaseModel):
    model_config = ConfigDict(extra="forbid")

    node_id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    priority: NodePriority
    status: NodeStatus = NodeStatus.READY
    entered_priority_at: datetime = Field(default_factory=utc_now)
    last_worked_at: datetime | None = None


class TopicRuntimeState(BaseModel):
    model_config = ConfigDict(extra="forbid")

    topic_id: str = Field(min_length=1)
    active_priority: str | None = None
    current_node_id: str | None = None
    running_node_ids: list[str] = Field(default_factory=list)
    last_worked_node_id: str | None = None
    last_worked_at: datetime | None = None
    switch_count: int = Field(default=0, ge=0)
    priority_dirty: bool = False

    @field_validator("running_node_ids")
    @classmethod
    def validate_running_node_ids(cls, value: list[str]) -> list[str]:
        if len(set(value)) != len(value):
            raise ValueError("running_node_ids must be unique")
        return value


class TopicSnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    topic_id: str = Field(min_length=1)
    queue_level: TopicQueueLevel = TopicQueueLevel.L0
    nodes: list[TopicNode] = Field(default_factory=list)
    runtime: TopicRuntimeState

    @model_validator(mode="after")
    def validate_consistency(self) -> "TopicSnapshot":
        if self.runtime.topic_id != self.topic_id:
            raise ValueError("runtime.topic_id must match topic_id")

        node_ids = [node.node_id for node in self.nodes]
        if len(set(node_ids)) != len(node_ids):
            raise ValueError("node_id must be unique within one topic snapshot")

        known_node_ids = set(node_ids)
        for reference in (
            self.runtime.current_node_id,
            self.runtime.last_worked_node_id,
        ):
            if reference is not None and reference not in known_node_ids:
                raise ValueError(f"unknown node reference: {reference}")

        unknown_running = [node_id for node_id in self.runtime.running_node_ids if node_id not in known_node_ids]
        if unknown_running:
            raise ValueError(f"unknown running node ids: {unknown_running}")

        rank_to_label: dict[int, str] = {}
        for node in self.nodes:
            existing = rank_to_label.get(node.priority.rank)
            if existing is None:
                rank_to_label[node.priority.rank] = node.priority.label
                continue
            if existing != node.priority.label:
                raise ValueError(
                    "priority ranks must map to exactly one label within a topic snapshot"
                )
        return self


class DispatchPlan(BaseModel):
    model_config = ConfigDict(extra="forbid")

    topic_id: str = Field(min_length=1)
    active_priority: str | None = None
    current_node_id: str | None = None
    active_candidate_node_ids: list[str] = Field(default_factory=list)
    dispatch_node_ids: list[str] = Field(default_factory=list)
    stalled: bool = False
    reasons: list[str] = Field(default_factory=list)
