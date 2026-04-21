from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, Field

ToolName = Literal["read", "write", "edit", "patch", "query", "grep", "glob", "exec", "shell"]
GuardState = Literal["allow", "deny", "confirm"]
AgentMode = Literal["execution", "decompose", "priority"]


class Build(BaseModel):
    node_id: str = Field(min_length=1)
    session_id: str | None = None


class ContextBundle(BaseModel):
    node_id: str = Field(min_length=1)
    payload: str = ""


class ToolSpec(BaseModel):
    name: ToolName
    description: str = Field(min_length=1)


class ToolBundle(BaseModel):
    node_id: str = Field(min_length=1)
    tools: list[ToolSpec] = Field(default_factory=list)


class SkillSpec(BaseModel):
    name: str = Field(min_length=1)
    config: dict[str, Any] = Field(default_factory=dict)


class SkillBundle(BaseModel):
    node_id: str = Field(min_length=1)
    skills: list[SkillSpec] = Field(default_factory=list)


class ToolAction(BaseModel):
    node_id: str = Field(min_length=1)
    tool_name: ToolName
    payload: dict[str, Any] = Field(default_factory=dict)
    is_safe: bool = False
    is_read_only: bool = False


class GuardDecision(BaseModel):
    decision: GuardState
    reason: str = ""


class AgentInput(BaseModel):
    node_id: str = Field(min_length=1)
    context: ContextBundle
    tools: ToolBundle
    skills: SkillBundle
    prompt: str = Field(min_length=1)


class ToolResult(BaseModel):
    tool_name: str = Field(min_length=1)
    success: bool = True
    output: str = ""
    error: str | None = None


class DecomposeTask(BaseModel):
    title: str = Field(min_length=1)
    description: str = ""
    status: Literal["pending", "ready"] = "pending"


class DecomposeRequest(BaseModel):
    request_id: str = Field(min_length=1)
    topic_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    node_name: str = Field(min_length=1)
    mode: Literal["decompose"] = "decompose"
    hint: str | None = None
    max_items: int = Field(default=3, ge=1, le=20)
    session_id: str | None = None


class DecomposeResponse(BaseModel):
    request_id: str = Field(min_length=1)
    topic_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    status: Literal["succeeded", "failed"] = "succeeded"
    output: str = ""
    error: str | None = None
    duration_ms: int = Field(default=0, ge=0)
    tasks: list[DecomposeTask] = Field(default_factory=list)


class PriorityLevel(BaseModel):
    label: str = Field(min_length=1)
    rank: int = Field(ge=0)


class PriorityCandidate(BaseModel):
    node_id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    status: str = Field(min_length=1)
    current_priority: PriorityLevel
    entered_priority_at: datetime
    last_worked_at: datetime | None = None


class PriorityAssignment(BaseModel):
    node_id: str = Field(min_length=1)
    label: str = Field(min_length=1)
    rank: int = Field(ge=0)


class PriorityRequest(BaseModel):
    request_id: str = Field(min_length=1)
    topic_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    node_name: str = Field(min_length=1)
    mode: Literal["priority"] = "priority"
    hint: str | None = None
    candidates: list[PriorityCandidate] = Field(default_factory=list)


class PriorityResponse(BaseModel):
    request_id: str = Field(min_length=1)
    topic_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    status: Literal["succeeded", "failed"] = "succeeded"
    output: str = ""
    error: str | None = None
    duration_ms: int = Field(default=0, ge=0)
    priority_plan: list[PriorityAssignment] = Field(default_factory=list)
