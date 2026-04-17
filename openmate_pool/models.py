"""Pydantic models for the OpenMate LLM gateway."""

from __future__ import annotations

from datetime import UTC, datetime
from enum import Enum
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, model_validator


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


class RoutePolicy(BaseModel):
    model_config = ConfigDict(extra="forbid")

    api_id: str | None = None


class OpenAIResponsesRequest(BaseModel):
    model_config = ConfigDict(extra="allow")

    input: Any
    instructions: str | None = None
    tools: list[dict[str, Any]] | None = None
    tool_choice: str | dict[str, Any] | None = None
    parallel_tool_calls: bool | None = None
    previous_response_id: str | None = None
    max_output_tokens: int | None = Field(default=None, ge=1)
    temperature: float | None = Field(default=None, ge=0.0, le=2.0)
    top_p: float | None = Field(default=None, ge=0.0, le=1.0)
    metadata: dict[str, Any] | None = None
    user: str | None = None
    store: bool | None = None
    stream: bool | None = None
    service_tier: str | None = None
    reasoning: dict[str, Any] | None = None
    text: dict[str, Any] | None = None
    truncation: str | None = None

    @model_validator(mode="after")
    def validate_reserved_fields(self) -> "OpenAIResponsesRequest":
        extras = self.model_extra or {}
        if "model" in extras:
            raise ValueError("request.model must not be set; model comes from model.json")
        legacy_chat_fields = {
            "messages",
            "functions",
            "function_call",
            "tool_calls",
            "max_tokens",
        }
        for field in sorted(legacy_chat_fields):
            if field in extras:
                raise ValueError(
                    f"request.{field} is ChatCompletions-only and is not supported; use Responses API fields"
                )
        if self.stream is True:
            raise ValueError("request.stream is not supported yet")
        return self


class OpenAIChatCompletionsRequest(BaseModel):
    model_config = ConfigDict(extra="allow")

    messages: list[dict[str, Any]]
    tools: list[dict[str, Any]] | None = None
    tool_choice: str | dict[str, Any] | None = None
    response_format: dict[str, Any] | None = None
    temperature: float | None = Field(default=None, ge=0.0, le=2.0)
    top_p: float | None = Field(default=None, ge=0.0, le=1.0)
    max_tokens: int | None = Field(default=None, ge=1)
    user: str | None = None
    store: bool | None = None
    stream: bool | None = None

    @model_validator(mode="after")
    def validate_reserved_fields(self) -> "OpenAIChatCompletionsRequest":
        if len(self.messages) == 0:
            raise ValueError("chat_request.messages is required")
        extras = self.model_extra or {}
        if "model" in extras:
            raise ValueError("chat_request.model must not be set; model comes from model.json")
        if self.stream is True:
            raise ValueError("chat_request.stream is not supported yet")
        return self


class InvokeRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    request: OpenAIResponsesRequest | None = None
    chat_request: OpenAIChatCompletionsRequest | None = None
    timeout_ms: int | None = Field(default=None, ge=1)
    route_policy: RoutePolicy = Field(default_factory=RoutePolicy)

    @model_validator(mode="after")
    def validate_request_shape(self) -> "InvokeRequest":
        has_request = self.request is not None
        has_chat_request = self.chat_request is not None
        if has_request == has_chat_request:
            raise ValueError("exactly one of request or chat_request is required")
        return self


class UsageMetrics(BaseModel):
    model_config = ConfigDict(extra="forbid")

    input_tokens: int | None = Field(default=None, ge=0)
    output_tokens: int | None = Field(default=None, ge=0)
    total_tokens: int | None = Field(default=None, ge=0)
    cached_input_tokens: int | None = Field(default=None, ge=0)
    reasoning_tokens: int | None = Field(default=None, ge=0)
    latency_ms: int | None = Field(default=None, ge=0)
    cost_usd: float | None = Field(default=None, ge=0.0)


class OpenAIResponseUsage(BaseModel):
    model_config = ConfigDict(extra="allow")

    input_tokens: int | None = Field(default=None, ge=0)
    input_tokens_details: dict[str, Any] | None = None
    output_tokens: int | None = Field(default=None, ge=0)
    output_tokens_details: dict[str, Any] | None = None
    total_tokens: int | None = Field(default=None, ge=0)


class OpenAIResponsesResponse(BaseModel):
    model_config = ConfigDict(extra="allow")

    id: str | None = None
    object: str | None = None
    model: str | None = None
    status: str | None = None
    output: list[dict[str, Any]] | None = None
    usage: OpenAIResponseUsage | None = None
    error: dict[str, Any] | None = None
    incomplete_details: dict[str, Any] | None = None
    previous_response_id: str | None = None
    service_tier: str | None = None
    metadata: dict[str, Any] | None = None


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
    response: OpenAIResponsesResponse | None = None
    output_text: str | None = None
    usage: UsageMetrics | None = None
    timing: InvocationTiming
    error: GatewayError | None = None


class InvocationRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    invocation_id: str = Field(min_length=1)
    request: InvokeRequest
    status: InvocationStatus
    route: RouteDecision | None = None
    response: OpenAIResponsesResponse | None = None
    output_text: str | None = None
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
    input_tokens: int = Field(ge=0)
    output_tokens: int = Field(ge=0)
    total_tokens: int = Field(ge=0)
    cached_input_tokens: int = Field(ge=0)
    reasoning_tokens: int = Field(ge=0)
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
