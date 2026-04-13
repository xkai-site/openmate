"""Pydantic models for the OpenMate LLM gateway."""

from __future__ import annotations

from datetime import UTC, datetime
from enum import Enum
from typing import Any

from pydantic import BaseModel, ConfigDict, Field


def utc_now() -> datetime:
    return datetime.now(UTC)


class ApiStatus(str, Enum):
    AVAILABLE = "available"
    LEASED = "leased"
    OFFLINE = "offline"


class InvocationStatus(str, Enum):
    RUNNING = "running"
    SUCCESS = "success"
    FAILURE = "failure"


class MessageRole(str, Enum):
    SYSTEM = "system"
    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"


class ResponseMode(str, Enum):
    TEXT = "text"


class LlmMessage(BaseModel):
    model_config = ConfigDict(extra="forbid")

    role: MessageRole
    content: str = Field(min_length=1)


class RoutePolicy(BaseModel):
    model_config = ConfigDict(extra="forbid")

    api_id: str | None = None


class InvokeRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    messages: list[LlmMessage] = Field(min_length=1)
    response_mode: ResponseMode = ResponseMode.TEXT
    temperature: float | None = Field(default=None, ge=0.0, le=2.0)
    max_output_tokens: int | None = Field(default=None, ge=1)
    timeout_ms: int | None = Field(default=None, ge=1)
    route_policy: RoutePolicy = Field(default_factory=RoutePolicy)
    metadata: dict[str, Any] = Field(default_factory=dict)


class UsageMetrics(BaseModel):
    model_config = ConfigDict(extra="forbid")

    prompt_tokens: int | None = Field(default=None, ge=0)
    completion_tokens: int | None = Field(default=None, ge=0)
    total_tokens: int | None = Field(default=None, ge=0)
    latency_ms: int | None = Field(default=None, ge=0)
    cost_usd: float | None = Field(default=None, ge=0.0)


class RouteDecision(BaseModel):
    model_config = ConfigDict(extra="forbid")

    api_id: str = Field(min_length=1)
    provider: str = Field(min_length=1)
    model: str = Field(min_length=1)


class GatewayError(BaseModel):
    model_config = ConfigDict(extra="forbid")

    code: str = Field(min_length=1)
    message: str = Field(min_length=1)
    retryable: bool = False
    provider_status_code: int | None = Field(default=None, ge=100, le=599)
    details: dict[str, Any] = Field(default_factory=dict)


class InvocationTiming(BaseModel):
    model_config = ConfigDict(extra="forbid")

    started_at: datetime = Field(default_factory=utc_now)
    finished_at: datetime | None = None
    latency_ms: int | None = Field(default=None, ge=0)


class InvocationAttempt(BaseModel):
    model_config = ConfigDict(extra="forbid")

    attempt_id: str = Field(min_length=1)
    route: RouteDecision
    status: InvocationStatus
    timing: InvocationTiming
    usage: UsageMetrics | None = None
    error: GatewayError | None = None


class InvokeResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    invocation_id: str = Field(min_length=1)
    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    status: InvocationStatus
    route: RouteDecision | None = None
    output_text: str | None = None
    raw_response: dict[str, Any] | None = None
    usage: UsageMetrics | None = None
    timing: InvocationTiming
    error: GatewayError | None = None


class InvocationRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    invocation_id: str = Field(min_length=1)
    request: InvokeRequest
    status: InvocationStatus
    route: RouteDecision | None = None
    output_text: str | None = None
    raw_response: dict[str, Any] | None = None
    usage: UsageMetrics | None = None
    timing: InvocationTiming
    error: GatewayError | None = None
    attempts: list[InvocationAttempt] = Field(default_factory=list)


class UsageSummary(BaseModel):
    model_config = ConfigDict(extra="forbid")

    node_id: str | None = None
    limit: int | None = Field(default=None, ge=1)
    invocation_count: int = Field(ge=0)
    success_count: int = Field(ge=0)
    failure_count: int = Field(ge=0)
    attempt_count: int = Field(ge=0)
    retry_count: int = Field(ge=0)
    prompt_tokens: int = Field(ge=0)
    completion_tokens: int = Field(ge=0)
    total_tokens: int = Field(ge=0)
    total_cost_usd: float | None = Field(default=None, ge=0.0)
    avg_latency_ms: int | None = Field(default=None, ge=0)
    max_latency_ms: int | None = Field(default=None, ge=0)
    generated_at: datetime = Field(default_factory=utc_now)


class CapacitySnapshot(BaseModel):
    model_config = ConfigDict(extra="forbid")

    total_apis: int = Field(ge=0)
    total_slots: int = Field(ge=0)
    available_slots: int = Field(ge=0)
    leased_slots: int = Field(ge=0)
    offline_apis: int = Field(ge=0)
    throttled: bool
    updated_at: datetime = Field(default_factory=utc_now)


class SyncResult(BaseModel):
    model_config = ConfigDict(extra="forbid")

    synced: bool
    capacity: CapacitySnapshot
