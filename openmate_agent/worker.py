from __future__ import annotations

import json
import time
from datetime import datetime
from pathlib import Path
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field

from openmate_shared.runtime_paths import default_model_config_path, resolve_workspace_root

from .orchestration import ChatExecutionRunner, ExecutionRunner, ResponsesExecutionRunner
from .service import AgentCapabilityService


class WorkerAgentSpec(BaseModel):
    model_config = ConfigDict(extra="ignore")

    mode: str = ""
    workspace_root: str | None = None
    pool_db_file: str | None = None
    pool_model_config: str | None = None
    pool_binary: str | None = None
    api_id: str | None = None
    vos_state_file: str | None = None
    vos_session_db: str | None = None
    vos_binary: str | None = None
    use_session_event: bool = False


class WorkerPriority(BaseModel):
    label: str = Field(min_length=1)
    rank: int = Field(ge=0)


class WorkerCandidateNode(BaseModel):
    node_id: str = Field(min_length=1)
    name: str = Field(min_length=1)
    status: str = Field(min_length=1)
    current_priority: WorkerPriority
    entered_priority_at: datetime
    last_worked_at: datetime | None = None


class WorkerExecuteRequest(BaseModel):
    model_config = ConfigDict(extra="forbid")

    request_id: str = Field(min_length=1)
    topic_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    node_name: str = Field(min_length=1)
    node_kind: Literal["normal", "priority"] = "normal"
    agent_spec: WorkerAgentSpec = Field(default_factory=WorkerAgentSpec)
    session_id: str | None = None
    event_id: str | None = None
    timeout_ms: int = Field(default=120000, ge=1)
    cancel_token: str | None = None
    priority_candidates: list[WorkerCandidateNode] = Field(default_factory=list)


class WorkerPriorityAssignment(BaseModel):
    node_id: str = Field(min_length=1)
    label: str = Field(min_length=1)
    rank: int = Field(ge=0)


class WorkerExecuteResponse(BaseModel):
    model_config = ConfigDict(extra="forbid")

    request_id: str = Field(min_length=1)
    topic_id: str = Field(min_length=1)
    node_id: str = Field(min_length=1)
    session_id: str | None = None
    event_id: str | None = None
    status: Literal["succeeded", "failed"] = "succeeded"
    next_node_status: str | None = None
    output: str = ""
    error: str | None = None
    retryable: bool = False
    duration_ms: int = Field(ge=0)
    priority_plan: list[WorkerPriorityAssignment] = Field(default_factory=list)


def execute_worker_request(request: WorkerExecuteRequest) -> WorkerExecuteResponse:
    started = time.perf_counter()
    try:
        if request.node_kind == "priority":
            plan = _build_priority_plan(request.priority_candidates)
            return WorkerExecuteResponse(
                request_id=request.request_id,
                topic_id=request.topic_id,
                node_id=request.node_id,
                session_id=request.session_id,
                event_id=request.event_id,
                status="succeeded",
                output=f"priority plan generated for {len(plan)} nodes",
                retryable=False,
                duration_ms=_duration_ms(started),
                priority_plan=plan,
            )

        mode = request.agent_spec.mode.strip().lower()
        if mode == "simulate_fail":
            raise RuntimeError("simulated worker failure")
        if mode == "simulate_success":
            return WorkerExecuteResponse(
                request_id=request.request_id,
                topic_id=request.topic_id,
                node_id=request.node_id,
                session_id=request.session_id,
                event_id=request.event_id,
                status="succeeded",
                output=f"simulated success for node={request.node_id}",
                retryable=False,
                duration_ms=_duration_ms(started),
            )

        api_id, api_mode = _resolve_agent_route(request.agent_spec)
        service = AgentCapabilityService(
            workspace_root=request.agent_spec.workspace_root,
            pool_db_path=request.agent_spec.pool_db_file,
            pool_model_config_path=request.agent_spec.pool_model_config,
            pool_binary_path=request.agent_spec.pool_binary,
            vos_state_file=request.agent_spec.vos_state_file if request.agent_spec.use_session_event else None,
            vos_session_db_file=request.agent_spec.vos_session_db if request.agent_spec.use_session_event else None,
            vos_binary_path=request.agent_spec.vos_binary if request.agent_spec.use_session_event else None,
            execution_runner=_build_execution_runner(api_mode),
        )
        output = service.execute_agent(
            service.build(
                node_id=request.node_id,
                session_id=request.session_id,
                api_id=api_id,
            )
        )
        return WorkerExecuteResponse(
            request_id=request.request_id,
            topic_id=request.topic_id,
            node_id=request.node_id,
            session_id=request.session_id,
            event_id=request.event_id,
            status="succeeded",
            output=output,
            retryable=False,
            duration_ms=_duration_ms(started),
        )
    except Exception as exc:
        return WorkerExecuteResponse(
            request_id=request.request_id,
            topic_id=request.topic_id,
            node_id=request.node_id,
            session_id=request.session_id,
            event_id=request.event_id,
            status="failed",
            error=str(exc),
            retryable=False,
            duration_ms=_duration_ms(started),
        )


def _build_priority_plan(candidates: list[WorkerCandidateNode]) -> list[WorkerPriorityAssignment]:
    def sort_key(candidate: WorkerCandidateNode) -> tuple[int, int, datetime, str]:
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
        return (
            status_bucket,
            candidate.current_priority.rank,
            candidate.entered_priority_at,
            candidate.node_id,
        )

    ordered = sorted(candidates, key=sort_key)
    plan: list[WorkerPriorityAssignment] = []
    for index, candidate in enumerate(ordered):
        label = "now" if index < 2 else "next"
        plan.append(
            WorkerPriorityAssignment(
                node_id=candidate.node_id,
                label=label,
                rank=index + 1,
            )
        )
    return plan


def _duration_ms(started: float) -> int:
    return max(0, int((time.perf_counter() - started) * 1000))


def _build_execution_runner(api_mode: str) -> ExecutionRunner:
    if api_mode == "chat_completions":
        return ChatExecutionRunner()
    return ResponsesExecutionRunner()


def _resolve_agent_route(spec: WorkerAgentSpec) -> tuple[str, str]:
    workspace_root = resolve_workspace_root(spec.workspace_root)
    model_config_path = (
        Path(spec.pool_model_config).resolve()
        if spec.pool_model_config is not None
        else default_model_config_path(workspace_root)
    )
    payload = json.loads(model_config_path.read_text(encoding="utf-8"))
    apis_raw = payload.get("apis")
    if not isinstance(apis_raw, list):
        raise ValueError("model config is invalid: apis must be an array")

    enabled_apis: list[dict[str, object]] = []
    for entry in apis_raw:
        if not isinstance(entry, dict):
            continue
        enabled_raw = entry.get("enabled", True)
        enabled = bool(enabled_raw) if isinstance(enabled_raw, bool) else True
        if enabled:
            enabled_apis.append(entry)

    if len(enabled_apis) == 0:
        raise ValueError("model config has no enabled api")

    if spec.api_id:
        target_api = next((item for item in enabled_apis if str(item.get("api_id", "")).strip() == spec.api_id), None)
        if target_api is None:
            raise ValueError(f"agent_spec.api_id is not found in enabled apis: {spec.api_id}")
    else:
        if len(enabled_apis) != 1:
            raise ValueError("multiple enabled apis detected, agent_spec.api_id is required")
        target_api = enabled_apis[0]

    api_id = str(target_api.get("api_id", "")).strip()
    if api_id == "":
        raise ValueError("model config is invalid: api_id is required for enabled api")
    api_mode_raw = str(target_api.get("api_mode", "responses")).strip() or "responses"
    if api_mode_raw not in {"responses", "chat_completions"}:
        raise ValueError(f"model config is invalid: api_mode is invalid for api_id={api_id}")
    return api_id, api_mode_raw
