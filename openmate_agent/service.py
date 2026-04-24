from __future__ import annotations

from pathlib import Path

from openmate_pool.pool import PoolGateway
from openmate_shared.runtime_paths import (
    default_model_config_path,
    default_runtime_db_path,
    resolve_workspace_root,
)

from .agent_services import CompactAgentService, DecomposeAgentService, ExecutionAgentService, PriorityAgentService
from .context_reader import VosContextReader
from .context_injector import VosContextInjector
from .defaults import DefaultAssembler, DefaultContextInjector, DefaultSkillInjector, DefaultToolInjector
from .interfaces import Assembler, ContextInjector, LlmGateway, SessionEventWriter, SkillInjector, ToolInjector
from .models import Build, CompactRequest, CompactResponse, DecomposeRequest, DecomposeResponse, PriorityRequest, PriorityResponse, ToolResult
from .orchestration import ExecutionOrchestrator, ExecutionRunner
from .pipeline import BuildPipeline
from .session_writer import VosSessionWriter
from .tool_runtime import ToolRuntimeExecutor
from .tooling import PermissionGateway, ToolRegistry


class AgentCapabilityService:
    def __init__(
        self,
        context_injector: ContextInjector | None = None,
        tool_injector: ToolInjector | None = None,
        skill_injector: SkillInjector | None = None,
        assembler: Assembler | None = None,
        build_pipeline: BuildPipeline | None = None,
        gateway: LlmGateway | None = None,
        execution_orchestrator: ExecutionOrchestrator | None = None,
        execution_runner: ExecutionRunner | None = None,
        tool_registry: ToolRegistry | None = None,
        permission_gateway: PermissionGateway | None = None,
        workspace_root: str | Path | None = None,
        pool_db_path: str | Path | None = None,
        pool_model_config_path: str | Path | None = None,
        pool_binary_path: str | Path | None = None,
        session_writer: SessionEventWriter | None = None,
        vos_state_file: str | Path | None = None,
        vos_session_db_file: str | Path | None = None,
        vos_binary_path: str | Path | None = None,
        execution_agent: ExecutionAgentService | None = None,
        decompose_agent: DecomposeAgentService | None = None,
        priority_agent: PriorityAgentService | None = None,
        compact_agent: CompactAgentService | None = None,
        tool_runtime: ToolRuntimeExecutor | None = None,
    ) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)
        self._context_injector = self._resolve_context_injector(
            context_injector=context_injector,
            vos_state_file=vos_state_file,
            vos_session_db_file=vos_session_db_file,
            vos_binary_path=vos_binary_path,
        )
        self._tool_injector = tool_injector or DefaultToolInjector()
        self._skill_injector = skill_injector or DefaultSkillInjector()
        self._assembler = assembler or DefaultAssembler()
        self._build_pipeline = build_pipeline or BuildPipeline(
            context_injector=self._context_injector,
            tool_injector=self._tool_injector,
            skill_injector=self._skill_injector,
            assembler=self._assembler,
        )
        self._gateway = gateway or PoolGateway(
            db_path=Path(pool_db_path).resolve() if pool_db_path is not None else default_runtime_db_path(self._workspace_root),
            model_config_path=(
                Path(pool_model_config_path).resolve()
                if pool_model_config_path is not None
                else default_model_config_path(self._workspace_root)
            ),
            binary_path=pool_binary_path,
            workspace_root=self._workspace_root,
        )
        self._session_writer = self._resolve_session_writer(
            session_writer=session_writer,
            vos_state_file=vos_state_file,
            vos_session_db_file=vos_session_db_file,
            vos_binary_path=vos_binary_path,
        )
        self._tool_runtime = tool_runtime or ToolRuntimeExecutor(
            workspace_root=self._workspace_root,
            permission_gateway=permission_gateway,
            tool_registry=tool_registry,
        )
        self._execution_orchestrator = execution_orchestrator or ExecutionOrchestrator(
            gateway=self._gateway,
            tool_executor=self._tool_runtime.execute_model_tool,
            session_writer=self._session_writer,
            runner=execution_runner,
        )
        self._execution_agent = execution_agent or ExecutionAgentService(
            build_pipeline=self._build_pipeline,
            execution_orchestrator=self._execution_orchestrator,
        )
        self._decompose_agent = decompose_agent or DecomposeAgentService(
            build_pipeline=self._build_pipeline,
            gateway=self._gateway,
        )
        self._priority_agent = priority_agent or PriorityAgentService()
        self._compact_agent = compact_agent or CompactAgentService(
            gateway=self._gateway,
        )

    def build(self, node_id: str, session_id: str | None = None) -> Build:
        return Build(node_id=node_id, session_id=session_id)

    def execute_agent(self, build: Build) -> str:
        return self._execution_agent.run(build)

    def decompose_agent(self, request: DecomposeRequest) -> DecomposeResponse:
        return self._decompose_agent.run(request)

    def priority_agent(self, request: PriorityRequest) -> PriorityResponse:
        return self._priority_agent.run(request)

    def compact_agent(self, request: CompactRequest) -> CompactResponse:
        return self._compact_agent.run(request)

    def priority(self, node_ids: list[str], hint: str | None = None) -> bool:
        return self._priority_agent.legacy_gate(node_ids=node_ids, hint=hint)

    def run_tool(
        self,
        node_id: str,
        tool_name: str,
        payload: dict[str, object] | None = None,
        is_safe: bool = False,
        is_read_only: bool = False,
    ) -> ToolResult:
        return self._tool_runtime.run_tool(
            node_id=node_id,
            tool_name=tool_name,
            payload=payload,
            is_safe=is_safe,
            is_read_only=is_read_only,
        )

    def _resolve_context_injector(
        self,
        *,
        context_injector: ContextInjector | None,
        vos_state_file: str | Path | None,
        vos_session_db_file: str | Path | None,
        vos_binary_path: str | Path | None,
    ) -> ContextInjector:
        if context_injector is not None:
            return context_injector
        if any(value is not None for value in [vos_state_file, vos_session_db_file, vos_binary_path]):
            return VosContextInjector(
                VosContextReader(
                    workspace_root=self._workspace_root,
                    state_file=vos_state_file,
                    session_db_file=vos_session_db_file,
                    binary_path=vos_binary_path,
                )
            )
        return DefaultContextInjector()

    def _resolve_session_writer(
        self,
        *,
        session_writer: SessionEventWriter | None,
        vos_state_file: str | Path | None,
        vos_session_db_file: str | Path | None,
        vos_binary_path: str | Path | None,
    ) -> SessionEventWriter | None:
        if session_writer is not None:
            return session_writer
        if any(value is not None for value in [vos_state_file, vos_session_db_file, vos_binary_path]):
            return VosSessionWriter(
                workspace_root=self._workspace_root,
                state_file=vos_state_file,
                session_db_file=vos_session_db_file,
                binary_path=vos_binary_path,
            )
        return None
