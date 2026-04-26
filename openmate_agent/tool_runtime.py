from __future__ import annotations

from pathlib import Path
from time import perf_counter
from typing import Any

from pydantic import ValidationError
from openmate_shared.runtime_paths import resolve_workspace_root

from .models import ToolAction, ToolResult
from .tool_monitor import ToolMonitorService, ToolMonitorSource
from .tooling import (
    FileLockManager,
    FileTimeStore,
    PermissionGateway,
    ToolContext,
    ToolRegistry,
    load_tool_registry,
)
from .vos_cli import VosCommandError, resolve_node_context
from .vos_cli import resolve_node_tool_context
from .vos_cli import VosInvalidPayloadError, VosNodeNotFoundError, VosUnavailableError

WORKSPACE_AWARE_TOOLS: set[str] = {
    "read",
    "write",
    "edit",
    "patch",
    "grep",
    "glob",
    "exec",
    "shell",
    "search",
    "command",
}


class ToolRuntimeExecutor:
    def __init__(
        self,
        *,
        workspace_root: Path,
        permission_gateway: PermissionGateway | None = None,
        tool_registry: ToolRegistry | None = None,
        file_time_store: FileTimeStore | None = None,
        lock_manager: FileLockManager | None = None,
        monitor_service: ToolMonitorService | None = None,
    ) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)
        resolved_registry = tool_registry or load_tool_registry(workspace_root=self._workspace_root)
        self._file_time = file_time_store or FileTimeStore(self._workspace_root)
        self._lock_manager = lock_manager or FileLockManager(self._workspace_root)
        self._permission_gateway = permission_gateway or PermissionGateway(tool_registry=resolved_registry)
        self._tool_registry = resolved_registry
        self._monitor = monitor_service or ToolMonitorService(self._workspace_root)

    def run_tool(
        self,
        *,
        node_id: str,
        tool_name: str,
        payload: dict[str, object] | None = None,
        is_safe: bool = False,
        is_read_only: bool = False,
        source: ToolMonitorSource = "unknown",
        request_id: str | None = None,
    ) -> ToolResult:
        payload_data = payload or {}
        started = perf_counter()
        self._record_before(
            node_id=node_id,
            tool_name=tool_name,
            source=source,
            is_safe=is_safe,
            is_read_only=is_read_only,
            request_id=request_id,
        )

        def finalize(result: ToolResult) -> ToolResult:
            duration_ms = max(0, int((perf_counter() - started) * 1000))
            self._record_after(
                node_id=node_id,
                tool_name=tool_name,
                source=source,
                is_safe=is_safe,
                is_read_only=is_read_only,
                request_id=request_id,
                success=result.success,
                error_code=result.error_code,
                duration_ms=duration_ms,
            )
            return result

        try:
            action = ToolAction(
                node_id=node_id,
                tool_name=tool_name,
                payload=payload_data,
                is_safe=is_safe,
                is_read_only=is_read_only,
            )
        except ValidationError as exc:
            return finalize(
                ToolResult(
                    tool_name=tool_name,
                    success=False,
                    error_code="TOOL_ACTION_INVALID",
                    error=f"invalid tool action: {exc.errors()}",
                )
            )

        decision = self._permission_gateway.evaluate(action)
        if decision.decision != "allow":
            return finalize(
                ToolResult(
                    tool_name=action.tool_name,
                    success=False,
                    error_code="TOOL_ACTION_BLOCKED",
                    error=f"tool action blocked: {decision.decision} ({decision.reason})",
                )
            )

        parent_id: str | None = None
        node_name = ""
        topic_id: str | None = None
        topic_workspace: Path | None = None
        effective_workspace = self._workspace_root
        if action.tool_name == "sibling_progress_board":
            try:
                parent_id, node_name = resolve_node_context(workspace_root=self._workspace_root, node_id=node_id)
            except VosNodeNotFoundError as exc:
                return finalize(
                    ToolResult(
                        tool_name=action.tool_name,
                        success=False,
                        error_code="VOS_NODE_NOT_FOUND",
                        error=str(exc),
                    )
                )
            except VosUnavailableError as exc:
                return finalize(
                    ToolResult(
                        tool_name=action.tool_name,
                        success=False,
                        error_code="VOS_UNAVAILABLE",
                        error=str(exc),
                    )
                )
            except VosInvalidPayloadError as exc:
                return finalize(
                    ToolResult(
                        tool_name=action.tool_name,
                        success=False,
                        error_code="VOS_INVALID_PAYLOAD",
                        error=str(exc),
                    )
                )
            except (VosCommandError, ValueError) as exc:
                return finalize(
                    ToolResult(
                        tool_name=action.tool_name,
                        success=False,
                        error_code="TOOL_RUNTIME_ERROR",
                        error=str(exc),
                    )
                )
        elif action.tool_name in WORKSPACE_AWARE_TOOLS:
            try:
                resolved = resolve_node_tool_context(workspace_root=self._workspace_root, node_id=node_id)
                parent_id = resolved.parent_id
                node_name = resolved.node_name
                topic_id = resolved.topic_id
                if not resolved.topic_workspace:
                    return finalize(
                        ToolResult(
                            tool_name=action.tool_name,
                            success=False,
                            error_code="WORKSPACE_UNAVAILABLE",
                            error="workspace_unavailable: topic workspace is not bound",
                        )
                    )
                candidate = Path(resolved.topic_workspace)
                if not candidate.is_absolute():
                    candidate = (self._workspace_root / candidate).resolve()
                else:
                    candidate = candidate.resolve()
                if not candidate.exists() or not candidate.is_dir():
                    return finalize(
                        ToolResult(
                            tool_name=action.tool_name,
                            success=False,
                            error_code="WORKSPACE_UNAVAILABLE",
                            error=f"workspace_unavailable: topic workspace is invalid: {candidate}",
                        )
                    )
                topic_workspace = candidate
                effective_workspace = candidate
            except (VosCommandError, ValueError, OSError) as exc:
                return finalize(
                    ToolResult(
                        tool_name=action.tool_name,
                        success=False,
                        error_code="WORKSPACE_UNAVAILABLE",
                        error=f"workspace_unavailable: failed to resolve topic workspace: {exc}",
                    )
                )
        context = ToolContext(
            node_id=node_id,
            parent_id=parent_id,
            node_name=node_name,
            topic_id=topic_id,
            topic_workspace=topic_workspace,
            runtime_workspace=self._workspace_root,
            workspace=effective_workspace,
            file_time=self._file_time,
            lock_manager=self._lock_manager,
        )
        result = self._tool_registry.execute(tool_name=action.tool_name, context=context, payload=payload_data)
        return finalize(result)

    def execute_model_tool(self, node_id: str, tool_name: str, payload: dict[str, Any]) -> ToolResult:
        return self.run_tool(
            node_id=node_id,
            tool_name=tool_name,
            payload=payload,
            is_safe=True,
            is_read_only=True,
            source="model",
        )

    def _record_before(
        self,
        *,
        node_id: str,
        tool_name: str,
        source: ToolMonitorSource,
        is_safe: bool,
        is_read_only: bool,
        request_id: str | None,
    ) -> None:
        try:
            self._monitor.record_before(
                node_id=node_id,
                tool_name=tool_name,
                source=source,
                is_safe=is_safe,
                is_read_only=is_read_only,
                request_id=request_id,
            )
        except Exception:
            pass

    def _record_after(
        self,
        *,
        node_id: str,
        tool_name: str,
        source: ToolMonitorSource,
        is_safe: bool,
        is_read_only: bool,
        request_id: str | None,
        success: bool,
        error_code: str | None,
        duration_ms: int,
    ) -> None:
        try:
            self._monitor.record_after(
                node_id=node_id,
                tool_name=tool_name,
                source=source,
                is_safe=is_safe,
                is_read_only=is_read_only,
                request_id=request_id,
                success=success,
                error_code=error_code,
                duration_ms=duration_ms,
            )
        except Exception:
            pass


def build_default_registry() -> ToolRegistry:
    return load_tool_registry(workspace_root=Path.cwd())
