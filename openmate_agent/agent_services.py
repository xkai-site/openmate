from __future__ import annotations

import time
from datetime import datetime
from typing import Any

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
    def __init__(self, *, build_pipeline: BuildPipeline) -> None:
        self._build_pipeline = build_pipeline

    def run(self, request: DecomposeRequest) -> DecomposeResponse:
        started = time.perf_counter()
        try:
            _ = self._build_pipeline.build(request.node_id)
            tasks = self._build_tasks(request)
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
    def _build_tasks(request: DecomposeRequest) -> list[DecomposeTask]:
        hint = (request.hint or "").strip()
        base = request.node_name.strip()
        templates = [
            ("Clarify objective", f"Refine scope and expected outcome for {base}."),
            ("Split into executable nodes", f"Break {base} into independent node-sized tasks."),
            ("Define acceptance checks", "Write measurable completion criteria and dependencies."),
            ("Prepare execution order", "Set initial order and identify first runnable nodes."),
            ("Review risk and rollback", "List key risks, mitigations, and fallback actions."),
        ]
        if hint:
            templates[0] = ("Clarify objective", f"Refine scope with hint: {hint}")
        max_items = min(request.max_items, len(templates))
        tasks: list[DecomposeTask] = []
        for index in range(max_items):
            title, description = templates[index]
            status = "ready" if index == 0 else "pending"
            tasks.append(DecomposeTask(title=title, description=description, status=status))
        return tasks


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
