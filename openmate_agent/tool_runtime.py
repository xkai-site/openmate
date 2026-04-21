from __future__ import annotations

from pathlib import Path
from typing import Any

from pydantic import ValidationError

from .models import ToolAction, ToolResult
from .tooling import (
    EditTool,
    ExecTool,
    FileLockManager,
    FileTimeStore,
    GlobTool,
    GrepTool,
    PatchTool,
    PermissionGateway,
    QueryTool,
    ReadTool,
    ShellTool,
    ToolContext,
    ToolRegistry,
    WriteTool,
)


class ToolRuntimeExecutor:
    def __init__(
        self,
        *,
        workspace_root: Path,
        permission_gateway: PermissionGateway | None = None,
        tool_registry: ToolRegistry | None = None,
        file_time_store: FileTimeStore | None = None,
        lock_manager: FileLockManager | None = None,
    ) -> None:
        self._workspace_root = workspace_root
        self._file_time = file_time_store or FileTimeStore(workspace_root)
        self._lock_manager = lock_manager or FileLockManager(workspace_root)
        self._permission_gateway = permission_gateway or PermissionGateway()
        self._tool_registry = tool_registry or build_default_registry()

    def run_tool(
        self,
        *,
        node_id: str,
        tool_name: str,
        payload: dict[str, object] | None = None,
        is_safe: bool = False,
        is_read_only: bool = False,
    ) -> ToolResult:
        payload_data = payload or {}
        try:
            action = ToolAction(
                node_id=node_id,
                tool_name=tool_name,
                payload=payload_data,
                is_safe=is_safe,
                is_read_only=is_read_only,
            )
        except ValidationError as exc:
            return ToolResult(
                tool_name=tool_name,
                success=False,
                error=f"invalid tool action: {exc.errors()}",
            )

        decision = self._permission_gateway.evaluate(action)
        if decision.decision != "allow":
            return ToolResult(
                tool_name=action.tool_name,
                success=False,
                error=f"tool action blocked: {decision.decision} ({decision.reason})",
            )

        context = ToolContext(
            node_id=node_id,
            workspace_root=self._workspace_root,
            file_time=self._file_time,
            lock_manager=self._lock_manager,
        )
        return self._tool_registry.execute(tool_name=action.tool_name, context=context, payload=payload_data)

    def execute_model_tool(self, node_id: str, tool_name: str, payload: dict[str, Any]) -> ToolResult:
        return self.run_tool(
            node_id=node_id,
            tool_name=tool_name,
            payload=payload,
            is_safe=True,
            is_read_only=True,
        )


def build_default_registry() -> ToolRegistry:
    return ToolRegistry(
        tools=[
            ReadTool(),
            WriteTool(),
            EditTool(),
            PatchTool(),
            QueryTool(),
            GrepTool(),
            GlobTool(),
            ExecTool(),
            ShellTool(),
        ]
    )

