from __future__ import annotations

import json
from typing import Any, Callable, Protocol
from uuid import uuid4

from openmate_pool.models import InvokeRequest, OpenAIResponsesRequest
from pydantic import BaseModel, Field

from .interfaces import LlmGateway, SessionEventGateway
from .models import AgentInput, Build, ToolResult
from .session_models import AppendSessionEventInput, SessionItemType, SessionRole, SessionStatus


class _ParsedFunctionCall(BaseModel):
    call_id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    arguments: dict[str, Any] = Field(default_factory=dict)
    provider_item_id: str | None = None


ToolExecutor = Callable[[str, str, dict[str, Any]], ToolResult]


class ExecutionRunner(Protocol):
    def run(
        self,
        *,
        build: Build,
        agent_input: AgentInput,
        tools_payload: list[dict[str, object]],
        gateway: LlmGateway,
        session_gateway: SessionEventGateway | None,
        tool_executor: ToolExecutor,
    ) -> str: ...


class ResponsesExecutionRunner:
    """Default execution loop based on OpenAI Responses tool-calling semantics."""

    def run(
        self,
        *,
        build: Build,
        agent_input: AgentInput,
        tools_payload: list[dict[str, object]],
        gateway: LlmGateway,
        session_gateway: SessionEventGateway | None,
        tool_executor: ToolExecutor,
    ) -> str:
        previous_response_id: str | None = None
        current_input: object = self._build_initial_input(agent_input.prompt)
        session_id = build.session_id
        session_started = False
        last_call_id: str | None = None

        try:
            if session_gateway is not None:
                session_id = session_gateway.ensure_session(node_id=build.node_id, session_id=session_id)
                session_started = True

            while True:
                # First round sends tool definitions; follow-up rounds continue via previous_response_id.
                request = OpenAIResponsesRequest(
                    input=current_input,
                    tools=tools_payload if previous_response_id is None and tools_payload else None,
                    tool_choice="auto" if previous_response_id is None and tools_payload else None,
                    parallel_tool_calls=False if previous_response_id is None and tools_payload else None,
                    previous_response_id=previous_response_id,
                )
                response = gateway.invoke(
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
                    if session_started and session_gateway and session_id:
                        self._append_assistant_deltas(
                            session_gateway=session_gateway,
                            session_id=session_id,
                            output_text=output_text,
                            response_id=previous_response_id,
                        )
                        session_gateway.append_event(
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
                    # Execute each tool call serially and feed outputs back to the model.
                    last_call_id = function_call.call_id
                    if session_started and session_gateway and session_id:
                        session_gateway.append_event(
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

                    tool_result = tool_executor(build.node_id, function_call.name, function_call.arguments)
                    tool_output_payload = self._build_tool_output_payload(tool_result)

                    if session_started and session_gateway and session_id:
                        session_gateway.append_event(
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
            if session_started and session_gateway and session_id:
                try:
                    if last_call_id:
                        session_gateway.append_event(
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
                        session_gateway.append_event(
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

    @staticmethod
    def _build_initial_input(prompt: str) -> list[dict[str, str]]:
        # Responses API initial turn should use message array input.
        return [
            {
                "role": "user",
                "content": prompt,
            }
        ]

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
                    arguments=ResponsesExecutionRunner._normalize_tool_arguments(item.get("arguments")),
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
        *,
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
    def _append_assistant_deltas(
        *,
        session_gateway: SessionEventGateway,
        session_id: str,
        output_text: str,
        response_id: str | None,
    ) -> None:
        for chunk in ResponsesExecutionRunner._split_output_deltas(output_text):
            session_gateway.append_event(
                AppendSessionEventInput(
                    session_id=session_id,
                    item_type=SessionItemType.ASSISTANT_DELTA,
                    role=SessionRole.ASSISTANT,
                    payload_json={
                        "role": SessionRole.ASSISTANT.value,
                        "delta": chunk,
                        "response_id": response_id,
                    },
                    next_status=SessionStatus.ACTIVE,
                )
            )

    @staticmethod
    def _split_output_deltas(text: str, chunk_size: int = 12) -> list[str]:
        if chunk_size <= 0:
            chunk_size = 12
        if text == "":
            return []
        chunks: list[str] = []
        index = 0
        while index < len(text):
            chunks.append(text[index : index + chunk_size])
            index += chunk_size
        return chunks


class LangGraphExecutionRunner:
    """
    Optional LangGraph adapter seam.

    This adapter intentionally delegates to a fallback runner to keep behavior
    stable while allowing later graph-based orchestration without changing
    service interfaces.
    """

    def __init__(self, fallback: ExecutionRunner | None = None) -> None:
        self._fallback = fallback or ResponsesExecutionRunner()

    def run(
        self,
        *,
        build: Build,
        agent_input: AgentInput,
        tools_payload: list[dict[str, object]],
        gateway: LlmGateway,
        session_gateway: SessionEventGateway | None,
        tool_executor: ToolExecutor,
    ) -> str:
        return self._fallback.run(
            build=build,
            agent_input=agent_input,
            tools_payload=tools_payload,
            gateway=gateway,
            session_gateway=session_gateway,
            tool_executor=tool_executor,
        )


class ExecutionOrchestrator:
    def __init__(
        self,
        *,
        gateway: LlmGateway,
        tool_executor: ToolExecutor,
        session_gateway: SessionEventGateway | None = None,
        runner: ExecutionRunner | None = None,
    ) -> None:
        self._gateway = gateway
        self._tool_executor = tool_executor
        self._session_gateway = session_gateway
        self._runner = runner or ResponsesExecutionRunner()

    def execute(
        self,
        *,
        build: Build,
        agent_input: AgentInput,
        tools_payload: list[dict[str, object]],
    ) -> str:
        return self._runner.run(
            build=build,
            agent_input=agent_input,
            tools_payload=tools_payload,
            gateway=self._gateway,
            session_gateway=self._session_gateway,
            tool_executor=self._tool_executor,
        )
