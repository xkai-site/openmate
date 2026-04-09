"""Pydantic models for OpenMate API pool."""

from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Any

from pydantic import BaseModel, ConfigDict, Field


class ApiStatus(str, Enum):
    AVAILABLE = "available"
    LEASED = "leased"
    OFFLINE = "offline"


class ApiDescriptor(BaseModel):
    model_config = ConfigDict(extra="forbid")

    api_id: str = Field(min_length=1)
    model_class: str = Field(min_length=1)
    max_concurrent: int = Field(ge=1)
    status: ApiStatus = ApiStatus.AVAILABLE


class ApiRuntimeState(BaseModel):
    model_config = ConfigDict(extra="forbid")

    api_id: str = Field(min_length=1)
    lease_count: int = Field(default=0, ge=0)
    failure_count: int = Field(default=0, ge=0)
    last_error: str | None = None


class ApiRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    descriptor: ApiDescriptor
    runtime: ApiRuntimeState


class ExecutionRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    timeout_ms: int | None = Field(default=None, ge=1)
    route_hint: dict[str, Any] | None = None


class DispatchTicket(BaseModel):
    model_config = ConfigDict(extra="forbid")

    ticket_id: str = Field(min_length=1)
    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    api_id: str = Field(min_length=1)
    lease_ms: int = Field(ge=1)
    acquired_at: datetime = Field(default_factory=datetime.utcnow)
    expires_at: datetime


class UsageMetrics(BaseModel):
    model_config = ConfigDict(extra="forbid")

    prompt_tokens: int | None = Field(default=None, ge=0)
    completion_tokens: int | None = Field(default=None, ge=0)
    total_tokens: int | None = Field(default=None, ge=0)
    latency_ms: int | None = Field(default=None, ge=0)
    cost_usd: float | None = Field(default=None, ge=0.0)


class UsageRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    ticket_id: str = Field(min_length=1)
    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    api_id: str = Field(min_length=1)
    released_at: datetime = Field(default_factory=datetime.utcnow)
    usage: UsageMetrics | None = None
    result_summary: str | None = None
    reason: str = Field(default="completed", min_length=1)


class ReleaseReceipt(BaseModel):
    model_config = ConfigDict(extra="forbid")

    ticket_id: str = Field(min_length=1)
    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    api_id: str = Field(min_length=1)
    released_at: datetime = Field(default_factory=datetime.utcnow)
    reason: str = Field(default="completed", min_length=1)
    usage: UsageMetrics | None = None
    result_summary: str | None = None


class CapacitySnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    total_apis: int = Field(ge=0)
    total_slots: int = Field(ge=0)
    available_slots: int = Field(ge=0)
    leased_slots: int = Field(ge=0)
    offline_apis: int = Field(ge=0)
    throttled: bool
    updated_at: datetime = Field(default_factory=datetime.utcnow)


class PoolState(BaseModel):
    model_config = ConfigDict(extra="forbid")

    apis: dict[str, ApiRecord] = Field(default_factory=dict)
    tickets: dict[str, DispatchTicket] = Field(default_factory=dict)
    usage_records: list[UsageRecord] = Field(default_factory=list)
    global_max_concurrent: int | None = Field(default=None, ge=1)
    offline_failure_threshold: int = Field(default=3, ge=1)
