from __future__ import annotations

from datetime import datetime
from typing import Any, Literal

from pydantic import BaseModel, Field

GuardState = Literal["allow", "deny", "confirm"]
AgentMode = Literal["execution", "decompose", "priority", "compact"]
ApprovalChoice = Literal["allow_and_remember", "allow_once", "deny", "supplement"]


class Build(BaseModel):
    node_id: str = Field(min_length=1)
    session_id: str | None = None
    api_id: str | None = None


class ContextBundle(BaseModel):
    node_id: str = Field(min_length=1)
    payload: str = ""


class ToolSpec(BaseModel):
    name: str = Field(min_length=1)
    description: str = Field(min_length=1)
    is_default: bool = False
    primary_tag: str | None = None
    secondary_tags: list[str] = Field(default_factory=list)
    backend: str | None = None
    parameters_schema: dict[str, Any] = Field(default_factory=dict)


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
    tool_name: str = Field(min_length=1)
    payload: dict[str, Any] = Field(default_factory=dict)
    is_safe: bool = False
    is_read_only: bool = False


class GuardDecision(BaseModel):
    decision: GuardState
    reason: str = ""


class PermissionRule(BaseModel):
    tool_name: str = Field(min_length=1)
    normalized_dir_prefix: str = Field(min_length=1)


class ApprovalRequest(BaseModel):
    request_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    topic_id: str | None = None
    target_type: Literal["tool", "skill"]
    tool_name: str | None = None
    skill_name: str | None = None
    directories: list[str] = Field(default_factory=list)
    reason: str = ""
    payload: dict[str, Any] = Field(default_factory=dict)


class ApprovalDecision(BaseModel):
    choice: ApprovalChoice
    supplement_text: str | None = None


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
    error_code: str | None = None
    error: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


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
    context_snapshot: dict[str, Any] | None = None


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


class CompactProcessInput(BaseModel):
    """A single process and its uncompacted session IDs."""
    process: dict[str, Any] = Field(default_factory=dict)
    uncompacted_session_ids: list[str] = Field(default_factory=list)


class CompactRequest(BaseModel):
    request_id: str = ""
    node_id: str = Field(min_length=1)
    processes: list[CompactProcessInput] = Field(default_factory=list)
    context: dict[str, Any] | None = None


class MemoryProposalEntry(BaseModel):
    key: str = Field(min_length=1)
    value: Any = None


class MemoryProposalCandidate(BaseModel):
    propose_update: bool = False
    entries: list[MemoryProposalEntry] = Field(default_factory=list)
    evidence: list[str] = Field(default_factory=list)
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)
    reason: str = ""


class CompactedProcess(BaseModel):
    """Result of compacting a single process."""
    process_id: str = ""
    name: str = ""
    summary: dict[str, Any] = Field(default_factory=dict)
    compacted_session_ids: list[str] = Field(default_factory=list)
    memory_proposals: list[MemoryProposalCandidate] = Field(default_factory=list)


class CompactResponse(BaseModel):
    status: Literal["succeeded", "failed"] = "succeeded"
    compacted: list[CompactedProcess] = Field(default_factory=list)
    error: str | None = None
