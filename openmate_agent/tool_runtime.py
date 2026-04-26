from __future__ import annotations

from pathlib import Path
from time import perf_counter
from typing import Any, Callable

from pydantic import ValidationError
from openmate_shared.runtime_paths import resolve_workspace_root

from .approval import normalize_dir_prefix
from .models import ToolAction, ToolResult
from .models import ApprovalDecision, ApprovalRequest, PermissionRule
from .permission_store import PermissionStore, VosPermissionStore
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
        approval_resolver: Callable[[ApprovalRequest], ApprovalDecision] | None = None,
        permission_store: PermissionStore | None = None,
    ) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)
        resolved_registry = tool_registry or load_tool_registry(workspace_root=self._workspace_root)
        self._file_time = file_time_store or FileTimeStore(self._workspace_root)
        self._lock_manager = lock_manager or FileLockManager(self._workspace_root)
        self._permission_gateway = permission_gateway or PermissionGateway(tool_registry=resolved_registry)
        self._tool_registry = resolved_registry
        self._monitor = monitor_service or ToolMonitorService(self._workspace_root)
        self._approval_resolver = approval_resolver
        self._permission_store = permission_store or VosPermissionStore(workspace_root=self._workspace_root)

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

        if source != "model":
            direct_decision = self._permission_gateway.evaluate(
                action,
                allowed_rules=[],
                workspace_root=self._workspace_root,
            )
            if direct_decision.decision != "allow":
                return finalize(
                    ToolResult(
                        tool_name=action.tool_name,
                        success=False,
                        error_code="TOOL_ACTION_BLOCKED",
                        error=f"tool action blocked: {direct_decision.decision} ({direct_decision.reason})",
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
            if source == "cli":
                topic_workspace = None
                effective_workspace = self._workspace_root
            else:
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
        allowed_rules: list[PermissionRule] = []
        if topic_id is not None:
            try:
                allowed_rules = self._permission_store.list_topic_tool_allows(topic_id=topic_id)
            except Exception:
                allowed_rules = []

        decision = self._permission_gateway.evaluate(
            action,
            allowed_rules=allowed_rules,
            workspace_root=effective_workspace,
        )
        if decision.decision != "allow":
            if source == "model" and self._approval_resolver is not None and decision.decision == "confirm":
                approval_request = self._permission_gateway.build_approval_request(
                    action=action,
                    node_id=node_id,
                    topic_id=topic_id,
                    workspace_root=effective_workspace,
                    reason=decision.reason,
                )
                approval_decision = self._approval_resolver(approval_request)
                if approval_decision.choice in {"allow_and_remember", "allow_once"}:
                    remembered: list[dict[str, str]] = []
                    if approval_decision.choice == "allow_and_remember" and topic_id is not None:
                        for directory in approval_request.directories:
                            normalized = normalize_dir_prefix(directory)
                            if normalized == "":
                                continue
                            try:
                                self._permission_store.add_topic_tool_allow(
                                    topic_id=topic_id,
                                    tool_name=action.tool_name,
                                    dir_prefix=normalized,
                                )
                                remembered.append(
                                    {
                                        "tool_name": action.tool_name,
                                        "dir_prefix": normalized,
                                    }
                                )
                            except Exception:
                                continue
                    approval_metadata = {
                        "approval": {
                            "request": approval_request.model_dump(mode="json"),
                            "decision": approval_decision.model_dump(mode="json"),
                            "remembered": remembered,
                            "source": "resolver",
                        }
                    }
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
                    if result.metadata:
                        merged = dict(result.metadata)
                        merged.update(approval_metadata)
                        result = result.model_copy(update={"metadata": merged})
                    else:
                        result = result.model_copy(update={"metadata": approval_metadata})
                    return finalize(result)
                if approval_decision.choice == "supplement":
                    return finalize(
                        ToolResult(
                            tool_name=action.tool_name,
                            success=False,
                            output=approval_decision.supplement_text or "",
                            error_code="TOOL_ACTION_SUPPLEMENT",
                            error="tool execution skipped: supplemental user input provided",
                            metadata={
                                "approval": {
                                    "request": approval_request.model_dump(mode="json"),
                                    "decision": approval_decision.model_dump(mode="json"),
                                    "source": "resolver",
                                }
                            },
                        )
                    )
                return finalize(
                    ToolResult(
                        tool_name=action.tool_name,
                        success=False,
                        error_code="TOOL_ACTION_BLOCKED",
                        error="tool action blocked: deny",
                        metadata={
                            "approval": {
                                "request": approval_request.model_dump(mode="json"),
                                "decision": approval_decision.model_dump(mode="json"),
                                "source": "resolver",
                            }
                        },
                    )
                )
            return finalize(
                ToolResult(
                    tool_name=action.tool_name,
                    success=False,
                    error_code="TOOL_ACTION_BLOCKED",
                    error=f"tool action blocked: {decision.decision} ({decision.reason})",
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
            is_safe=False,
            is_read_only=False,
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
