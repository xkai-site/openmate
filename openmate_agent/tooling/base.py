from __future__ import annotations

from abc import ABC, abstractmethod
from pathlib import Path
from typing import Any

from pydantic import BaseModel, Field

from openmate_agent.models import ToolResult

from .file_state import FileLockManager, FileTimeStore


class ToolContext(BaseModel):
    node_id: str = Field(min_length=1)
    parent_id: str | None = None
    node_name: str = ""
    workspace_root: Path
    file_time: FileTimeStore
    lock_manager: FileLockManager

    model_config = {"arbitrary_types_allowed": True}


class Tool(ABC):
    name: str
    description: str

    @abstractmethod
    def run(self, context: ToolContext, payload: dict[str, Any]) -> ToolResult:
        raise NotImplementedError
