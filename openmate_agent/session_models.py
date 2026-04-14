from __future__ import annotations

from datetime import datetime
from enum import Enum
from typing import Any

from pydantic import BaseModel, ConfigDict, Field, model_validator


class SessionStatus(str, Enum):
    ACTIVE = "active"
    WAITING = "waiting"
    COMPLETED = "completed"
    FAILED = "failed"


class SessionItemType(str, Enum):
    MESSAGE = "message"
    FUNCTION_CALL = "function_call"
    FUNCTION_CALL_OUTPUT = "function_call_output"
    REASONING = "reasoning"
    WEB_SEARCH_CALL = "web_search_call"
    FILE_SEARCH_CALL = "file_search_call"
    COMPUTER_CALL = "computer_call"
    MCP_CALL = "mcp_call"
    MCP_LIST_TOOLS = "mcp_list_tools"
    MCP_APPROVAL_REQUEST = "mcp_approval_request"
    IMAGE_GENERATION_CALL = "image_generation_call"
    CODE_INTERPRETER_CALL = "code_interpreter_call"


class SessionRole(str, Enum):
    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"
    SYSTEM = "system"


class SessionRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    status: SessionStatus
    created_at: datetime
    updated_at: datetime
    last_seq: int = Field(ge=0)


class SessionEventRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    id: str = Field(min_length=1)
    session_id: str = Field(min_length=1)
    seq: int = Field(ge=1)
    item_type: str = Field(min_length=1)
    provider_item_id: str | None = None
    role: SessionRole | None = None
    call_id: str | None = None
    payload_json: dict[str, Any] = Field(default_factory=dict)
    created_at: datetime


class AppendSessionEventInput(BaseModel):
    model_config = ConfigDict(extra="forbid")

    session_id: str = Field(min_length=1)
    item_type: str = Field(min_length=1)
    call_id: str | None = None
    payload_json: dict[str, Any] = Field(default_factory=dict)
    provider_item_id: str | None = None
    role: SessionRole | None = None
    next_status: SessionStatus | None = None

    @model_validator(mode="after")
    def validate_call_id_for_tool_events(self) -> "AppendSessionEventInput":
        if self.item_type in {SessionItemType.FUNCTION_CALL.value, SessionItemType.FUNCTION_CALL_OUTPUT.value}:
            if self.call_id is None or not self.call_id.strip():
                raise ValueError("call_id is required for tool events")
        return self
