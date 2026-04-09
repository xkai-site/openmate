from __future__ import annotations

from pathlib import Path

from pydantic import ValidationError

from .defaults import (
    DefaultAgentExecutor,
    DefaultAssembler,
    DefaultContextInjector,
    DefaultSkillInjector,
    DefaultToolInjector,
)
from .interfaces import AgentExecutor, Assembler, ContextInjector, SkillInjector, ToolInjector
from .models import Build, ToolAction, ToolResult
from .tooling import (
    EditTool,
    FileLockManager,
    FileTimeStore,
    GlobTool,
    GrepTool,
    PermissionGateway,
    QueryTool,
    ReadTool,
    ShellTool,
    ToolRegistry,
    ToolContext,
    WriteTool,
)


class AgentCapabilityService:
    def __init__(
        self,
        context_injector: ContextInjector | None = None,
        tool_injector: ToolInjector | None = None,
        skill_injector: SkillInjector | None = None,
        assembler: Assembler | None = None,
        executor: AgentExecutor | None = None,
        tool_registry: ToolRegistry | None = None,
        permission_gateway: PermissionGateway | None = None,
        workspace_root: str | Path | None = None,
    ) -> None:
        self._context_injector = context_injector or DefaultContextInjector()
        self._tool_injector = tool_injector or DefaultToolInjector()
        self._skill_injector = skill_injector or DefaultSkillInjector()
        self._assembler = assembler or DefaultAssembler()
        self._executor = executor or DefaultAgentExecutor()
        self._workspace_root = Path(workspace_root or Path.cwd()).resolve()
        self._file_time = FileTimeStore(self._workspace_root)
        self._lock_manager = FileLockManager(self._workspace_root)
        self._permission_gateway = permission_gateway or PermissionGateway()
        self._tool_registry = tool_registry or self._build_default_registry()

    def build(self, node_id: str) -> Build:
        return Build(node_id=node_id)

    def execute(self, build: Build) -> str:
        context = self._context_injector.inject(build.node_id)
        tools = self._tool_injector.inject(build.node_id)
        skills = self._skill_injector.inject(build.node_id)
        assembled = self._assembler.assemble(context=context, tools=tools, skills=skills)
        return self._executor.execute(assembled)

    def priority(self, node_ids: list[str], hint: str | None = None) -> bool:
        if not node_ids:
            return False
        _ = hint
        return True

    def run_tool(
        self,
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

    @staticmethod
    def _build_default_registry() -> ToolRegistry:
        return ToolRegistry(
            tools=[
                ReadTool(),
                WriteTool(),
                EditTool(),
                QueryTool(),
                GrepTool(),
                GlobTool(),
                ShellTool(),
            ]
        )
