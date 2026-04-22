from __future__ import annotations

import json
import time
from datetime import datetime
from typing import Any
from uuid import uuid4

from openmate_pool.models import InvokeRequest, OpenAIResponsesRequest

from .interfaces import LlmGateway
from .models import (
    Build,
    DecomposeRequest,
    DecomposeResponse,
    DecomposeTask,
    PriorityCandidate,
    PriorityAssignment,
    PriorityRequest,
    PriorityResponse,
    ToolBundle,
)
from .orchestration import ExecutionOrchestrator
from .pipeline import BuildPipeline

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


class ExecutionAgentService:
    def __init__(self, *, build_pipeline: BuildPipeline, execution_orchestrator: ExecutionOrchestrator) -> None:
        self._build_pipeline = build_pipeline
        self._execution_orchestrator = execution_orchestrator

    def run(self, build: Build) -> str:
        agent_input = self._build_pipeline.build(build.node_id)
        tools_payload = _build_openai_tools(agent_input.tools)
        return self._execution_orchestrator.execute(
            build=build,
            agent_input=agent_input,
            tools_payload=tools_payload,
        )


class DecomposeAgentService:
    def __init__(self, *, build_pipeline: BuildPipeline, gateway: LlmGateway) -> None:
        self._build_pipeline = build_pipeline
        self._gateway = gateway

    def run(self, request: DecomposeRequest) -> DecomposeResponse:
        started = time.perf_counter()
        try:
            agent_input = self._build_pipeline.build(request.node_id)
            prompt = self._build_decompose_prompt(request=request, context_payload=agent_input.context.payload)
            model_response = self._gateway.invoke(
                InvokeRequest(
                    request_id=request.request_id or str(uuid4()),
                    node_id=request.node_id,
                    request=OpenAIResponsesRequest(
                        input=self._build_initial_input(prompt),
                        temperature=0.2,
                        text={"format": {"type": "json_object"}},
                    ),
                )
            )
            raw_output = self._extract_response_text(model_response)
            tasks = self._parse_tasks_from_output(raw_output=raw_output, max_items=request.max_items)
            return DecomposeResponse(
                request_id=request.request_id,
                topic_id=request.topic_id,
                node_id=request.node_id,
                status="succeeded",
                output=f"decompose plan generated for {len(tasks)} tasks",
                duration_ms=_duration_ms(started),
                tasks=tasks,
            )
        except Exception as exc:
            return DecomposeResponse(
                request_id=request.request_id,
                topic_id=request.topic_id,
                node_id=request.node_id,
                status="failed",
                error=str(exc),
                duration_ms=_duration_ms(started),
            )

    @staticmethod
    def _build_initial_input(prompt: str) -> list[dict[str, str]]:
        return [{"role": "user", "content": prompt}]

    @staticmethod
    def _build_decompose_prompt(*, request: DecomposeRequest, context_payload: str) -> str:
        context_json = context_payload.strip() if context_payload and context_payload.strip() else "{}"
        external_context = json.dumps(request.context_snapshot or {}, ensure_ascii=False)
        user_hint = (request.hint or "").strip()
        return (
            "You are OpenMate Decompose Agent.\n"
            "Goal: produce one-level executable child tasks for the target node.\n"
            "Hard rules:\n"
            "1) Decompose by business/domain outcomes first, not by technical stack.\n"
            "2) Keep one-level granularity only; do not create nested subtasks.\n"
            "3) Tasks must be directly executable and independently trackable.\n"
            "4) Return strict JSON only.\n\n"
            f"request_id={request.request_id}\n"
            f"topic_id={request.topic_id}\n"
            f"node_id={request.node_id}\n"
            f"node_name={request.node_name}\n"
            f"max_items={request.max_items}\n"
            f"user_hint={user_hint}\n"
            f"context_snapshot_json={context_json}\n"
            f"external_context_snapshot_json={external_context}\n\n"
            "Return JSON schema:\n"
            "{\n"
            '  "tasks": [\n'
            '    {"title": "string", "description": "string", "status": "ready|pending"}\n'
            "  ]\n"
            "}\n"
        )

    @staticmethod
    def _extract_response_text(response: Any) -> str:
        if response is None:
            raise ValueError("decompose model returned empty response")
        status_value = getattr(response, "status", "")
        status_text = str(getattr(status_value, "value", status_value)).lower()
        if status_text == "failure":
            error = getattr(response, "error", None)
            if error is not None:
                message = getattr(error, "message", "") or str(error)
                raise ValueError(f"decompose model invocation failed: {message}")
            raise ValueError("decompose model invocation failed")

        output_text = getattr(response, "output_text", None)
        if isinstance(output_text, str) and output_text.strip():
            return output_text.strip()

        response_payload = getattr(response, "response", None)
        output_items = getattr(response_payload, "output", None)
        if not isinstance(output_items, list):
            raise ValueError("decompose model returned empty output")

        fragments: list[str] = []
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
                if isinstance(text, str) and text.strip():
                    fragments.append(text)
        if not fragments:
            raise ValueError("decompose model returned empty output")
        return "".join(fragments).strip()

    @staticmethod
    def _parse_tasks_from_output(*, raw_output: str, max_items: int) -> list[DecomposeTask]:
        candidates = [raw_output.strip()]
        stripped_fence = DecomposeAgentService._strip_code_fence(raw_output)
        if stripped_fence != candidates[0]:
            candidates.append(stripped_fence)

        payload: Any = None
        parse_errors: list[str] = []
        for candidate in candidates:
            if not candidate:
                continue
            try:
                payload = json.loads(candidate)
                break
            except json.JSONDecodeError as exc:
                parse_errors.append(str(exc))
        if payload is None:
            raise ValueError(f"decompose output is not valid JSON: {'; '.join(parse_errors) or 'empty output'}")

        tasks_raw: Any
        if isinstance(payload, dict):
            tasks_raw = payload.get("tasks")
        elif isinstance(payload, list):
            tasks_raw = payload
        else:
            raise ValueError("decompose output JSON must be an object or task array")
        if not isinstance(tasks_raw, list):
            raise ValueError("decompose output tasks must be a JSON array")

        tasks: list[DecomposeTask] = []
        for entry in tasks_raw:
            if not isinstance(entry, dict):
                continue
            title = str(entry.get("title", "")).strip()
            if not title:
                continue
            description = str(entry.get("description", "")).strip()
            status_raw = str(entry.get("status", "pending")).strip().lower()
            status = "ready" if status_raw == "ready" else "pending"
            tasks.append(DecomposeTask(title=title, description=description, status=status))

        if len(tasks) == 0:
            raise ValueError("decompose output contains no valid tasks")
        return tasks[:max_items]

    @staticmethod
    def _strip_code_fence(raw_output: str) -> str:
        text = raw_output.strip()
        if not text.startswith("```"):
            return text
        lines = text.splitlines()
        if len(lines) <= 1:
            return text
        if lines[0].startswith("```"):
            lines = lines[1:]
        if lines and lines[-1].strip() == "```":
            lines = lines[:-1]
        return "\n".join(lines).strip()


class PriorityAgentService:
    def run(self, request: PriorityRequest) -> PriorityResponse:
        started = time.perf_counter()
        try:
            plan = _build_priority_plan(request)
            return PriorityResponse(
                request_id=request.request_id,
                topic_id=request.topic_id,
                node_id=request.node_id,
                status="succeeded",
                output=f"priority plan generated for {len(plan)} nodes",
                duration_ms=_duration_ms(started),
                priority_plan=plan,
            )
        except Exception as exc:
            return PriorityResponse(
                request_id=request.request_id,
                topic_id=request.topic_id,
                node_id=request.node_id,
                status="failed",
                error=str(exc),
                duration_ms=_duration_ms(started),
            )

    @staticmethod
    def legacy_gate(node_ids: list[str], hint: str | None = None) -> bool:
        _ = hint
        return len(node_ids) > 0


def _build_priority_plan(request: PriorityRequest) -> list[PriorityAssignment]:
    def sort_key(candidate: PriorityCandidate) -> tuple[int, int, datetime, str]:
        status_bucket = {
            "ready": 0,
            "running": 0,
            "pending": 1,
            "retry_cooldown": 2,
            "waiting_external": 2,
            "blocked": 2,
            "failed": 3,
            "cancelled": 3,
            "succeeded": 3,
        }.get(candidate.status, 1)
        return (status_bucket, candidate.current_priority.rank, candidate.entered_priority_at, candidate.node_id)

    ordered = sorted(request.candidates, key=sort_key)
    plan: list[PriorityAssignment] = []
    for index, candidate in enumerate(ordered):
        label = "now" if index < 2 else "next"
        plan.append(
            PriorityAssignment(
                node_id=candidate.node_id,
                label=label,
                rank=index + 1,
            )
        )
    return plan


def _duration_ms(started: float) -> int:
    return max(0, int((time.perf_counter() - started) * 1000))


def _build_openai_tools(bundle: ToolBundle) -> list[dict[str, object]]:
    payload: list[dict[str, object]] = []
    for tool in bundle.tools:
        payload.append(
            {
                "type": "function",
                "name": tool.name,
                "description": tool.description,
                "parameters": _tool_parameters_for_name(tool.name),
            }
        )
    return payload


def _tool_parameters_for_name(tool_name: str) -> dict[str, object]:
    default_schema: dict[str, object] = {
        "type": "object",
        "properties": {},
        "additionalProperties": True,
    }
    return _TOOL_PARAMETER_SCHEMAS.get(tool_name, default_schema)
