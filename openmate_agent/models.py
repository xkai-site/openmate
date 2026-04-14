from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field

ToolName = Literal["read", "write", "edit", "patch", "query", "grep", "glob", "exec", "shell"]
GuardState = Literal["allow", "deny", "confirm"]


class Build(BaseModel):
    node_id: str = Field(min_length=1)
    session_id: str | None = None


class ContextBundle(BaseModel):
    node_id: str = Field(min_length=1)
    snippets: list[str] = Field(default_factory=list)
    summary: str | None = None


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
