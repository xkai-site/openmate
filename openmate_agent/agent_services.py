from __future__ import annotations

import time
from datetime import datetime

from .models import (
    Build,
    DecomposeRequest,
    DecomposeResponse,
    DecomposeTask,
    PriorityCandidate,
    PriorityAssignment,
    PriorityRequest,
    PriorityResponse,
)
from .orchestration import ExecutionOrchestrator
from .pipeline import BuildPipeline
from .tool_schema import build_openai_tools


class ExecutionAgentService:
    def __init__(self, *, build_pipeline: BuildPipeline, execution_orchestrator: ExecutionOrchestrator) -> None:
        self._build_pipeline = build_pipeline
        self._execution_orchestrator = execution_orchestrator

    def run(self, build: Build) -> str:
        agent_input = self._build_pipeline.build(build.node_id)
        tools_payload = build_openai_tools(agent_input.tools)
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
