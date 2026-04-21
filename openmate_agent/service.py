from __future__ import annotations

from pathlib import Path
from typing import Any

from openmate_pool.pool import PoolGateway
from openmate_shared.runtime_paths import (
    default_model_config_path,
    default_runtime_db_path,
    resolve_workspace_root,
)
from pydantic import ValidationError

from .context_gateway import VosContextGateway
from .context_injector import VosContextInjector
from .defaults import DefaultAssembler, DefaultContextInjector, DefaultSkillInjector, DefaultToolInjector
from .interfaces import Assembler, ContextInjector, LlmGateway, SessionEventGateway, SkillInjector, ToolInjector
from .models import Build, ToolAction, ToolBundle, ToolResult
from .orchestration import ExecutionOrchestrator, ExecutionRunner
from .pipeline import BuildPipeline
from .session_gateway import VosSessionGateway
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
    ToolRegistry,
    ToolContext,
    WriteTool,
)


_TOOL_PARAMETER_SCHEMAS: dict[str, dict[str, Any]] = {
    "read": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "offset": {"type": "integer", "minimum": 0, "default": 0},
            "limit": {"type": "integer", "minimum": 1, "maximum": 2000, "default": 200},
        },
        "required": ["path"],
        "additionalProperties": False,
    },
    "write": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "content": {"type": "string", "default": ""},
        },
        "required": ["path"],
        "additionalProperties": False,
    },
    "edit": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "old_string": {"type": "string"},
            "new_string": {"type": "string", "default": ""},
        },
        "required": ["path", "old_string"],
        "additionalProperties": False,
    },
    "patch": {
        "type": "object",
        "properties": {
            "operations": {
                "type": "array",
                "minItems": 1,
                "items": {
                    "type": "object",
                    "properties": {
                        "type": {"type": "string", "enum": ["replace", "write"]},
                        "path": {"type": "string"},
                        "old_string": {"type": "string"},
                        "new_string": {"type": "string"},
                        "content": {"type": "string"},
                    },
                    "required": ["type", "path"],
                    "additionalProperties": False,
                },
            },
        },
        "required": ["operations"],
        "additionalProperties": False,
    },
    "query": {
        "type": "object",
        "properties": {
            "url": {"type": "string"},
            "method": {"type": "string", "enum": ["GET", "POST"], "default": "GET"},
            "params": {"type": "object", "default": {}},
            "headers": {"type": "object", "default": {}},
            "body": {"type": "object", "default": {}},
            "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 120, "default": 10},
        },
        "required": ["url"],
        "additionalProperties": False,
    },
    "grep": {
        "type": "object",
        "properties": {
            "pattern": {"type": "string"},
            "scope": {"type": "string", "default": "."},
            "max_results": {"type": "integer", "minimum": 1, "maximum": 5000, "default": 100},
            "file_glob": {"type": ["string", "null"], "default": None},
        },
        "required": ["pattern"],
        "additionalProperties": False,
    },
    "glob": {
        "type": "object",
        "properties": {
            "pattern": {"type": "string"},
            "scope": {"type": "string", "default": "."},
            "max_results": {"type": "integer", "minimum": 1, "maximum": 10000, "default": 1000},
        },
        "required": ["pattern"],
        "additionalProperties": False,
    },
    "exec": {
        "type": "object",
        "properties": {
            "command": {
                "type": "array",
                "items": {"type": "string"},
                "minItems": 1,
            },
            "cwd": {"type": ["string", "null"], "default": None},
            "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "default": 30},
            "expect_json": {"type": "boolean", "default": False},
        },
        "required": ["command"],
        "additionalProperties": False,
    },
    "shell": {
        "type": "object",
        "properties": {
            "command": {"type": "string"},
            "cwd": {"type": ["string", "null"], "default": None},
            "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "default": 30},
        },
        "required": ["command"],
        "additionalProperties": False,
    },
}


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
        session_gateway: SessionEventGateway | None = None,
        vos_state_file: str | Path | None = None,
        vos_session_db_file: str | Path | None = None,
        vos_binary_path: str | Path | None = None,
    ) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)
        if context_injector is not None:
            self._context_injector = context_injector
        elif any(value is not None for value in [vos_state_file, vos_session_db_file, vos_binary_path]):
            self._context_injector = VosContextInjector(
                VosContextGateway(
                    workspace_root=self._workspace_root,
                    state_file=vos_state_file,
                    session_db_file=vos_session_db_file,
                    binary_path=vos_binary_path,
                )
            )
        else:
            self._context_injector = DefaultContextInjector()
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
        self._file_time = FileTimeStore(self._workspace_root)
        self._lock_manager = FileLockManager(self._workspace_root)
        self._permission_gateway = permission_gateway or PermissionGateway()
        self._tool_registry = tool_registry or self._build_default_registry()
        if session_gateway is not None:
            self._session_gateway = session_gateway
        elif any(value is not None for value in [vos_state_file, vos_session_db_file, vos_binary_path]):
            self._session_gateway = VosSessionGateway(
                workspace_root=self._workspace_root,
                state_file=vos_state_file,
                session_db_file=vos_session_db_file,
                binary_path=vos_binary_path,
            )
        else:
            self._session_gateway = None
        self._execution_orchestrator = execution_orchestrator or ExecutionOrchestrator(
            gateway=self._gateway,
            tool_executor=self._execute_model_tool,
            session_gateway=self._session_gateway,
            runner=execution_runner,
        )

    def build(self, node_id: str, session_id: str | None = None) -> Build:
        return Build(node_id=node_id, session_id=session_id)

    def execute(self, build: Build) -> str:
        agent_input = self._build_pipeline.build(build.node_id)
        tools_payload = self._build_openai_tools(agent_input.tools)
        return self._execution_orchestrator.execute(
            build=build,
            agent_input=agent_input,
            tools_payload=tools_payload,
        )

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

    def _execute_model_tool(self, node_id: str, tool_name: str, payload: dict[str, Any]) -> ToolResult:
        return self.run_tool(
            node_id=node_id,
            tool_name=tool_name,
            payload=payload,
            is_safe=True,
            is_read_only=True,
        )

    @staticmethod
    def _build_openai_tools(bundle: ToolBundle) -> list[dict[str, object]]:
        payload: list[dict[str, object]] = []
        for tool in bundle.tools:
            payload.append(
                {
                    "type": "function",
                    "name": tool.name,
                    "description": tool.description,
                    "parameters": AgentCapabilityService._tool_parameters_for_name(tool.name),
                }
            )
        return payload

    @staticmethod
    def _tool_parameters_for_name(tool_name: str) -> dict[str, object]:
        default_schema: dict[str, object] = {
            "type": "object",
            "properties": {},
            "additionalProperties": True,
        }
        return _TOOL_PARAMETER_SCHEMAS.get(tool_name, default_schema)

    @staticmethod
    def _build_default_registry() -> ToolRegistry:
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
