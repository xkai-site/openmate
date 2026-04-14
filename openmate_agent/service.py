from __future__ import annotations

import json
from pathlib import Path
from typing import Any
from uuid import uuid4

from openmate_pool.models import InvokeRequest, OpenAIResponsesRequest
from openmate_pool.pool import PoolGateway
from pydantic import BaseModel, Field, ValidationError

from .defaults import DefaultAssembler, DefaultContextInjector, DefaultSkillInjector, DefaultToolInjector
from .interfaces import Assembler, ContextInjector, LlmGateway, SessionEventGateway, SkillInjector, ToolInjector
from .models import Build, ToolAction, ToolBundle, ToolResult
from .session_gateway import VosSessionGateway
from .session_models import AppendSessionEventInput, SessionItemType, SessionRole, SessionStatus
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


class _ParsedFunctionCall(BaseModel):
    call_id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    arguments: dict[str, Any] = Field(default_factory=dict)
    provider_item_id: str | None = None


class AgentCapabilityService:
    def __init__(
        self,
        context_injector: ContextInjector | None = None,
        tool_injector: ToolInjector | None = None,
        skill_injector: SkillInjector | None = None,
        assembler: Assembler | None = None,
        gateway: LlmGateway | None = None,
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
        self._workspace_root = Path(workspace_root or Path.cwd()).resolve()
        self._context_injector = context_injector or DefaultContextInjector()
        self._tool_injector = tool_injector or DefaultToolInjector()
        self._skill_injector = skill_injector or DefaultSkillInjector()
        self._assembler = assembler or DefaultAssembler()
        self._gateway = gateway or PoolGateway(
            db_path=Path(pool_db_path or self._workspace_root / ".pool_state.db"),
            model_config_path=Path(pool_model_config_path or self._workspace_root / "model.json"),
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

    def build(self, node_id: str, session_id: str | None = None) -> Build:
        return Build(node_id=node_id, session_id=session_id)

    def execute(self, build: Build) -> str:
        context = self._context_injector.inject(build.node_id)
        tools = self._tool_injector.inject(build.node_id)
        skills = self._skill_injector.inject(build.node_id)
        assembled = self._assembler.assemble(context=context, tools=tools, skills=skills)
        tools_payload = self._build_openai_tools(tools)

        previous_response_id: str | None = None
        current_input: object = assembled.prompt
        session_id = build.session_id
        session_started = False
        last_call_id: str | None = None

        try:
            if self._session_gateway is not None:
                session_id = self._session_gateway.ensure_session(node_id=build.node_id, session_id=session_id)
                session_started = True

            while True:
                request = OpenAIResponsesRequest(
                    input=current_input,
                    tools=tools_payload if previous_response_id is None and tools_payload else None,
                    tool_choice="auto" if previous_response_id is None and tools_payload else None,
                    parallel_tool_calls=False if previous_response_id is None and tools_payload else None,
                    previous_response_id=previous_response_id,
                )
                response = self._gateway.invoke(
                    InvokeRequest(
                        request_id=str(uuid4()),
                        node_id=build.node_id,
                        request=request,
                    )
                )

                if response.response and response.response.id:
                    previous_response_id = response.response.id

                output_items = response.response.output if response.response else None
                function_calls = self._extract_function_calls(output_items)
                if not function_calls:
                    output_text = response.output_text or self._extract_output_text(output_items)
                    if output_text is None:
                        raise RuntimeError(f"gateway returned empty output for node={build.node_id}")
                    if session_started and self._session_gateway and session_id:
                        self._session_gateway.append_event(
                            AppendSessionEventInput(
                                session_id=session_id,
                                item_type=SessionItemType.MESSAGE,
                                role=SessionRole.ASSISTANT,
                                payload_json=self._build_message_payload(
                                    output_text=output_text,
                                    output_items=output_items,
                                    response_id=previous_response_id,
                                ),
                                next_status=SessionStatus.COMPLETED,
                            )
                        )
                    return output_text

                if previous_response_id is None:
                    raise RuntimeError("gateway response missing response.id for tool continuation")

                tool_outputs: list[dict[str, object]] = []
                for function_call in function_calls:
                    last_call_id = function_call.call_id
                    if session_started and self._session_gateway and session_id:
                        self._session_gateway.append_event(
                            AppendSessionEventInput(
                                session_id=session_id,
                                item_type=SessionItemType.FUNCTION_CALL,
                                call_id=function_call.call_id,
                                provider_item_id=function_call.provider_item_id,
                                role=SessionRole.ASSISTANT,
                                payload_json={
                                    "name": function_call.name,
                                    "arguments": function_call.arguments,
                                    "role": SessionRole.ASSISTANT.value,
                                },
                                next_status=SessionStatus.WAITING,
                            )
                        )

                    tool_result = self.run_tool(
                        node_id=build.node_id,
                        tool_name=function_call.name,
                        payload=function_call.arguments,
                        is_safe=True,
                        is_read_only=True,
                    )
                    tool_output_payload = self._build_tool_output_payload(tool_result)

                    if session_started and self._session_gateway and session_id:
                        self._session_gateway.append_event(
                            AppendSessionEventInput(
                                session_id=session_id,
                                item_type=SessionItemType.FUNCTION_CALL_OUTPUT,
                                call_id=function_call.call_id,
                                role=SessionRole.TOOL,
                                payload_json=tool_output_payload,
                                next_status=SessionStatus.ACTIVE,
                            )
                        )

                    model_output = {
                        "ok": tool_output_payload["ok"],
                        "output": tool_output_payload["output"],
                        "error": tool_output_payload["error"],
                    }
                    tool_outputs.append(
                        {
                            "type": "function_call_output",
                            "call_id": function_call.call_id,
                            "output": json.dumps(model_output, ensure_ascii=False),
                        }
                    )

                current_input = tool_outputs
        except Exception as exc:
            if session_started and self._session_gateway and session_id:
                try:
                    if last_call_id:
                        self._session_gateway.append_event(
                            AppendSessionEventInput(
                                session_id=session_id,
                                item_type=SessionItemType.FUNCTION_CALL_OUTPUT,
                                call_id=last_call_id,
                                role=SessionRole.TOOL,
                                payload_json={
                                    "output": None,
                                    "ok": False,
                                    "error": {
                                        "code": "AGENT_EXECUTION_FAILED",
                                        "message": str(exc),
                                        "retryable": False,
                                    },
                                    "role": SessionRole.TOOL.value,
                                },
                                next_status=SessionStatus.FAILED,
                            )
                        )
                    else:
                        self._session_gateway.append_event(
                            AppendSessionEventInput(
                                session_id=session_id,
                                item_type=SessionItemType.MESSAGE,
                                role=SessionRole.SYSTEM,
                                payload_json={
                                    "role": SessionRole.SYSTEM.value,
                                    "error": {
                                        "code": "AGENT_EXECUTION_FAILED",
                                        "message": str(exc),
                                        "retryable": False,
                                    },
                                },
                                next_status=SessionStatus.FAILED,
                            )
                        )
                except Exception:
                    pass
            raise

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
    def _extract_function_calls(output_items: list[dict[str, Any]] | None) -> list[_ParsedFunctionCall]:
        if not output_items:
            return []

        calls: list[_ParsedFunctionCall] = []
        for item in output_items:
            if not isinstance(item, dict) or item.get("type") != "function_call":
                continue
            name = item.get("name")
            if not isinstance(name, str) or not name.strip():
                continue
            call_id_raw = item.get("call_id") or item.get("id") or str(uuid4())
            call_id = call_id_raw if isinstance(call_id_raw, str) else str(call_id_raw)
            provider_item_id = item.get("id") if isinstance(item.get("id"), str) else None
            calls.append(
                _ParsedFunctionCall(
                    call_id=call_id,
                    name=name,
                    arguments=AgentCapabilityService._normalize_tool_arguments(item.get("arguments")),
                    provider_item_id=provider_item_id,
                )
            )
        return calls

    @staticmethod
    def _normalize_tool_arguments(raw_arguments: object) -> dict[str, Any]:
        if isinstance(raw_arguments, dict):
            return raw_arguments
        if isinstance(raw_arguments, str):
            try:
                parsed = json.loads(raw_arguments)
            except json.JSONDecodeError:
                return {"_raw_arguments": raw_arguments}
            if isinstance(parsed, dict):
                return parsed
            return {"_value": parsed}
        return {}

    @staticmethod
    def _extract_output_text(output_items: list[dict[str, Any]] | None) -> str | None:
        if not output_items:
            return None
        texts: list[str] = []
        for item in output_items:
            if not isinstance(item, dict) or item.get("type") != "message":
                continue
            content = item.get("content")
            if not isinstance(content, list):
                continue
            for content_item in content:
                if not isinstance(content_item, dict):
                    continue
                if content_item.get("type") not in {"output_text", "text"}:
                    continue
                text = content_item.get("text")
                if isinstance(text, str) and text:
                    texts.append(text)
        if not texts:
            return None
        return "".join(texts)

    @staticmethod
    def _build_tool_output_payload(tool_result: ToolResult) -> dict[str, Any]:
        output_content = {"tool_name": tool_result.tool_name, "content": tool_result.output} if tool_result.output else None
        if tool_result.success:
            return {
                "output": output_content,
                "ok": True,
                "error": None,
                "role": SessionRole.TOOL.value,
            }
        return {
            "output": output_content,
            "ok": False,
            "error": {
                "code": "TOOL_EXECUTION_FAILED",
                "message": tool_result.error or "tool execution failed",
                "retryable": False,
            },
            "role": SessionRole.TOOL.value,
        }

    @staticmethod
    def _build_message_payload(
        output_text: str,
        output_items: list[dict[str, Any]] | None,
        response_id: str | None,
    ) -> dict[str, Any]:
        return {
            "role": SessionRole.ASSISTANT.value,
            "output_text": output_text,
            "response_id": response_id,
            "content": output_items or [],
        }

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
