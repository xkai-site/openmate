from __future__ import annotations

import json
import logging
import os
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable
from uuid import uuid4

_LOGGER = logging.getLogger(__name__)

from openmate_pool.models import InvokeRequest, OpenAIResponsesRequest
from openmate_shared.runtime_paths import resolve_workspace_root

from .interfaces import LlmGateway
from .models import (
    ApprovalDecision,
    ApprovalRequest,
    Build,
    CompactProcessInput,
    CompactRequest,
    CompactResponse,
    CompactedProcess,
    DecomposeRequest,
    DecomposeResponse,
    DecomposeTask,
    PriorityCandidate,
    PriorityAssignment,
    PriorityRequest,
    PriorityResponse,
    SkillSpec,
    ToolBundle,
    UserSkillAllow,
)
from .orchestration import ContextTooLargeError, ExecutionOrchestrator
from .permission_store import PermissionStore
from .pipeline import BuildPipeline
from .skill_catalog import SkillCatalog
from .skill_monitor import SkillMonitorService

class ExecutionAgentService:
    def __init__(
        self,
        *,
        build_pipeline: BuildPipeline,
        execution_orchestrator: ExecutionOrchestrator,
        approval_resolver: Callable[[ApprovalRequest], ApprovalDecision] | None = None,
        permission_store: PermissionStore | None = None,
        workspace_root: str | Path | None = None,
        skill_catalog: SkillCatalog | None = None,
        skill_monitor: SkillMonitorService | None = None,
    ) -> None:
        self._build_pipeline = build_pipeline
        self._execution_orchestrator = execution_orchestrator
        self._approval_resolver = approval_resolver
        self._permission_store = permission_store
        self._workspace_root = resolve_workspace_root(workspace_root or Path.cwd())
        self._skill_catalog = skill_catalog or SkillCatalog(workspace_root=workspace_root or Path.cwd())
        self._skill_monitor = skill_monitor or SkillMonitorService(workspace_root=workspace_root or Path.cwd())

    def run(self, build: Build) -> str:
        agent_input = self._build_pipeline.build(build.node_id)
        agent_input = self._apply_skill_permissions(build=build, agent_input=agent_input)
        agent_input = agent_input.model_copy(update={"prompt": self._build_execution_prompt(agent_input)})
        tools_payload = _build_openai_tools(agent_input.tools)
        try:
            return self._execution_orchestrator.execute(
                build=build,
                agent_input=agent_input,
                tools_payload=tools_payload,
            )
        except ContextTooLargeError:
            # Context was compacted by the runner. Rebuild and retry once.
            _LOGGER.info("ContextTooLargeError, rebuilding context and retrying for node=%s", build.node_id)
            agent_input = self._build_pipeline.build(build.node_id)
            agent_input = self._apply_skill_permissions(build=build, agent_input=agent_input)
            agent_input = agent_input.model_copy(update={"prompt": self._build_execution_prompt(agent_input)})
            tools_payload = _build_openai_tools(agent_input.tools)
            return self._execution_orchestrator.execute(
                build=build,
                agent_input=agent_input,
                tools_payload=tools_payload,
            )

    def _apply_skill_permissions(self, *, build: Build, agent_input: Any) -> Any:
        if not agent_input.skills.skills:
            return agent_input
        allow_records: list[UserSkillAllow] = []
        if self._permission_store is not None:
            try:
                allow_records = self._permission_store.list_user_skill_allows()
            except Exception:
                allow_records = []

        context_payload = self._parse_context_payload(agent_input.context.payload)
        requested = set(self._extract_requested_skills(context_payload))
        approved: list[SkillSpec] = []
        for skill in agent_input.skills.skills:
            if skill.name == "skill_query":
                approved.append(skill)
                continue
            if requested and skill.name not in requested:
                continue
            record, failed_check = self._find_allow_record(skill=skill, records=allow_records)
            if record is not None:
                approved.append(self._materialize_skill(skill=skill, node_id=build.node_id, allow_record=record))
                continue
            approved_skill = self._materialize_skill_with_approval(
                skill=skill, node_id=build.node_id, precomputed_check=failed_check
            )
            if approved_skill is not None:
                approved.append(approved_skill)
        next_skills = agent_input.skills.model_copy(update={"skills": approved})
        return agent_input.model_copy(update={"skills": next_skills})

    @staticmethod
    def _build_execution_prompt(agent_input: Any) -> str:
        context_payload = ExecutionAgentService._parse_context_payload(agent_input.context.payload)
        skill_query_config = {"threshold": 10, "description_truncate": 25}
        for skill in agent_input.skills.skills:
            if skill.name == "skill_query":
                skill_query_config = {
                    "threshold": int(skill.config.get("threshold", 10)),
                    "description_truncate": int(skill.config.get("description_truncate", 25)),
                }
                break
        system_prompt = {
            "preset": (
                "你是 OpenMate Agent。保持输出可执行、可追踪、可回放；"
                "优先利用工具与技能完成任务。"
            ),
            "tool_management": {
                "default_tools": [
                    {"name": tool.name, "description": tool.description}
                    for tool in agent_input.tools.tools
                ],
                "discovery_policy": (
                    "Default tools are pre-injected only. Discover and drill down non-default tools via tool_query."
                ),
            },
            "skill_management": [
                {"name": skill.name, "config": skill.config}
                for skill in agent_input.skills.skills
            ],
            "skill_discovery_policy": {
                "default": "Do not preload all skills; discover first, then request approval and inject.",
                "skill_query": skill_query_config,
            },
            "memory_update_confirmation_rule": (
                "当你的回答可能更新 user_memory 或 topic_memory 时，先询问用户是否更新。"
            ),
        }

        user_prompt = {
            "node_id": context_payload.get("node_id", agent_input.node_id),
            "user_memory": context_payload.get("user_memory"),
            "topic_memory": context_payload.get("topic_memory"),
            "process_contexts": context_payload.get("process_contexts", []),
            "session_history": context_payload.get("session_history", []),
        }

        payload = {
            "SystemPrompt": system_prompt,
            "UserPrompt": user_prompt,
        }
        return json.dumps(payload, ensure_ascii=False, indent=2)

    @staticmethod
    def _parse_context_payload(payload_text: str) -> dict[str, Any]:
        try:
            parsed = json.loads(payload_text)
            if isinstance(parsed, dict):
                # Backward-compatible read for old payload shape.
                if "UserPrompt" in parsed and isinstance(parsed["UserPrompt"], dict):
                    legacy_user = parsed["UserPrompt"]
                    legacy_system = parsed.get("SystemPrompt", {})
                    memory = legacy_system.get("memory", {}) if isinstance(legacy_system, dict) else {}
                    return {
                        "node_id": parsed.get("node_id"),
                        "user_memory": memory.get("user_memory"),
                        "topic_memory": memory.get("topic_memory"),
                        "process_contexts": legacy_system.get("process_contexts", []),
                        "session_history": legacy_user.get("session", []),
                    }
                return parsed
        except json.JSONDecodeError:
            pass
        return {
            "user_memory": None,
            "topic_memory": None,
            "process_contexts": [],
            "session_history": [],
        }

    @staticmethod
    def _extract_requested_skills(context_payload: dict[str, Any]) -> list[str]:
        raw = context_payload.get("requested_skills")
        if not isinstance(raw, list):
            return []
        result: list[str] = []
        for item in raw:
            name = str(item).strip()
            if name:
                result.append(name)
        return result

    def _materialize_skill_with_approval(
        self, *, skill: SkillSpec, node_id: str, precomputed_check: dict[str, Any] | None = None
    ) -> SkillSpec | None:
        check = precomputed_check or self._validate_skill_against_allowlist(skill=skill, allow_record=None)
        if check["allowed"]:
            return self._materialize_skill(skill=skill, node_id=node_id, allow_record=None)
        if self._approval_resolver is None:
            return None
        request = ApprovalRequest(
            request_id=f"approval:{node_id}:skill:{skill.name}",
            node_id=node_id,
            target_type="skill",
            skill_name=skill.name,
            reason="skill injection requires user confirmation",
            payload=check["payload"],
        )
        decision = self._approval_resolver(request)
        if decision.choice not in {"allow_once", "allow_and_remember"}:
            return None
        materialized = self._materialize_skill(skill=skill, node_id=node_id, allow_record=None)
        if decision.choice == "allow_and_remember" and self._permission_store is not None and materialized is not None:
            try:
                payload = check["payload"]
                self._permission_store.upsert_user_skill_allow(
                    skill_name=skill.name,
                    skill_path=str(payload.get("resolved_path", "")),
                    skill_mtime=str(payload.get("current_mtime", "")),
                    allowed_roots=[str(self._workspace_root)],
                )
            except Exception:
                pass
        return materialized

    def _materialize_skill(self, *, skill: SkillSpec, node_id: str, allow_record: UserSkillAllow | None) -> SkillSpec:
        path_raw = skill.config.get("path")
        if not isinstance(path_raw, str) or not path_raw.strip():
            return skill
        resolved_path = self._resolve_skill_path(path_raw)
        skill_path = resolved_path
        started = time.perf_counter()
        try:
            check = self._validate_skill_against_allowlist(skill=skill, allow_record=allow_record)
            if not check["allowed"]:
                return skill
            self._skill_monitor.record_before(
                node_id=node_id,
                source="model",
                skill_name=skill.name,
                skill_path=str(skill_path),
            )
            content = skill_path.read_text(encoding="utf-8")
            config = dict(skill.config)
            config["content"] = content
            duration_ms = max(0, int((time.perf_counter() - started) * 1000))
            self._skill_monitor.record_after(
                node_id=node_id,
                source="model",
                skill_name=skill.name,
                skill_path=str(skill_path),
                success=True,
                error_code=None,
                error=None,
                duration_ms=duration_ms,
            )
            return skill.model_copy(update={"config": config})
        except Exception as exc:
            duration_ms = max(0, int((time.perf_counter() - started) * 1000))
            self._skill_monitor.record_after(
                node_id=node_id,
                source="model",
                skill_name=skill.name,
                skill_path=str(skill_path),
                success=False,
                error_code="SKILL_READ_FAILED",
                error=str(exc),
                duration_ms=duration_ms,
            )
            return skill

    def _find_allow_record(self, *, skill: SkillSpec, records: list[UserSkillAllow]) -> tuple[UserSkillAllow | None, dict[str, Any] | None]:
        best_failed: dict[str, Any] | None = None
        for record in records:
            if record.skill_name != skill.name:
                continue
            check = self._validate_skill_against_allowlist(skill=skill, allow_record=record)
            if check["allowed"]:
                return record, None
            best_failed = check
        return None, best_failed

    def _resolve_skill_path(self, raw_path: str) -> Path:
        candidate = Path(raw_path.strip())
        if candidate.is_absolute():
            return candidate.resolve()
        return (self._workspace_root / candidate).resolve()

    def _validate_skill_against_allowlist(self, *, skill: SkillSpec, allow_record: UserSkillAllow | None) -> dict[str, Any]:
        path_raw = str(skill.config.get("path", "")).strip()
        if not path_raw:
            return {"allowed": True, "payload": {}}
        resolved_path = self._resolve_skill_path(path_raw)
        current_mtime = ""
        try:
            stat = os.stat(resolved_path)
            current_mtime = datetime.fromtimestamp(stat.st_mtime, tz=timezone.utc).isoformat().replace("+00:00", "Z")
        except Exception:
            pass
        payload: dict[str, Any] = {
            "skill_name": skill.name,
            "path_raw": path_raw,
            "resolved_path": str(resolved_path),
            "reason_code": "ALLOW_RECORD_NOT_FOUND",
            "current_mtime": current_mtime or None,
            "recorded_mtime": None,
        }
        if allow_record is None:
            return {"allowed": False, "payload": payload}
        roots = [self._workspace_root, *[Path(root).resolve() for root in allow_record.allowed_roots if root.strip()]]
        in_root = any(root == resolved_path or root in resolved_path.parents for root in roots)
        if not in_root:
            payload["reason_code"] = "PATH_NOT_ALLOWED"
            payload["recorded_mtime"] = allow_record.skill_mtime or None
            return {"allowed": False, "payload": payload}
        if Path(allow_record.skill_path).resolve() != resolved_path:
            payload["reason_code"] = "PATH_NOT_ALLOWED"
            payload["recorded_mtime"] = allow_record.skill_mtime or None
            return {"allowed": False, "payload": payload}
        payload["recorded_mtime"] = allow_record.skill_mtime or None
        if current_mtime and allow_record.skill_mtime:
            current_dt = _parse_utc_timestamp(current_mtime)
            recorded_dt = _parse_utc_timestamp(allow_record.skill_mtime)
            if current_dt is None or recorded_dt is None or current_dt != recorded_dt:
                payload["reason_code"] = "MTIME_MISMATCH"
                return {"allowed": False, "payload": payload}
        payload["reason_code"] = "ALLOW_MATCHED"
        return {"allowed": True, "payload": payload}


def _parse_utc_timestamp(value: str) -> datetime | None:
    raw = str(value).strip()
    if not raw:
        return None
    normalized = raw
    if normalized.endswith("Z"):
        normalized = normalized[:-1] + "+00:00"
    try:
        parsed = datetime.fromisoformat(normalized)
    except ValueError:
        return None
    if parsed.tzinfo is None:
        return parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


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
                "parameters": tool.parameters_schema or _tool_parameters_for_name(tool.name),
            }
        )
    return payload


def _tool_parameters_for_name(tool_name: str) -> dict[str, object]:
    _ = tool_name
    return {
        "type": "object",
        "properties": {},
        "additionalProperties": True,
    }


class CompactAgentService:
    """Fixed-workflow agent that compacts process context into summary and proposals.

    For each process with uncompacted session IDs, extracts the relevant
    session events from the context snapshot and calls LLM once to produce:
    1) process summary (written to Process.summary)
    2) topic_memory proposal candidates (must be user-confirmed later)
    """

    def __init__(self, *, gateway: LlmGateway) -> None:
        self._gateway = gateway

    def run(self, request: CompactRequest) -> CompactResponse:
        started = time.perf_counter()
        try:
            compacted = self._compact_processes(request)
            return CompactResponse(
                status="succeeded",
                compacted=compacted,
            )
        except Exception as exc:
            return CompactResponse(
                status="failed",
                error=str(exc),
            )

    def _compact_processes(self, request: CompactRequest) -> list[CompactedProcess]:
        results: list[CompactedProcess] = []
        session_map = _build_session_event_map(request.context) if request.context else {}

        for proc_input in request.processes:
            proc_id = str(proc_input.process.get("id", "")).strip()
            proc_name = proc_input.process.get("name", "")
            if not proc_name:
                continue

            # Collect session events for uncompacted sessions
            session_events: list[dict[str, Any]] = []
            for sid in proc_input.uncompacted_session_ids:
                events = session_map.get(sid, [])
                for event in events:
                    session_events.append(event)

            if not session_events:
                continue

            # Call LLM to compact: summary + proposal candidates
            summary, proposals = self._llm_compact(proc_name, session_events)

            # Merge with existing process summary
            existing_summary = proc_input.process.get("summary") or {}
            if isinstance(existing_summary, dict):
                merged = {**existing_summary, **summary}
            else:
                merged = summary

            results.append(CompactedProcess(
                process_id=proc_id,
                name=proc_name,
                summary=merged,
                compacted_session_ids=proc_input.uncompacted_session_ids,
                memory_proposals=proposals,
            ))

        return results

    def _llm_compact(
        self, process_name: str, session_events: list[dict[str, Any]]
    ) -> tuple[dict[str, Any], list[dict[str, Any]]]:
        """Call LLM to compact session events into summary and memory proposals."""
        events_text = json.dumps(session_events, ensure_ascii=False, indent=2)
        prompt = (
            "You are OpenMate Compact Agent.\n"
            "Goal: compact the given session events for one Process item.\n"
            "Hard rules:\n"
            "1) Extract process summary: key decisions, outcomes, artifacts, and state changes.\n"
            "2) Extract topic_memory proposal only for stable consensus that will impact future project actions.\n"
            "3) Do not propose temporary intent, one-off preference, or speculation.\n"
            "4) Return strict JSON only — no markdown fences, no explanation.\n\n"
            f"process_name={process_name}\n"
            f"session_events={events_text}\n\n"
            'Return JSON schema:\n'
            "{\n"
            '  "summary": {"key_findings": "...", "decisions": "...", "artifacts": "...", "next_steps": "..."},\n'
            '  "memory_proposal": {\n'
            '    "propose_update": true|false,\n'
            '    "entries": [{"key":"...", "value":"..."}],\n'
            '    "evidence": ["..."],\n'
            '    "confidence": 0.0,\n'
            '    "reason": "..."\n'
            "  }\n"
            "}"
        )

        initial_input = [{"role": "user", "content": prompt}]
        model_response = self._gateway.invoke(
            InvokeRequest(
                request_id=str(uuid4()),
                node_id=process_name or "compact-node",
                request=OpenAIResponsesRequest(
                    input=initial_input,
                    temperature=0.2,
                    text={"format": {"type": "json_object"}},
                ),
            )
        )

        raw_output = _extract_compact_response_text(model_response)
        return _parse_compact_payload(raw_output)


def _build_session_event_map(context: dict[str, Any]) -> dict[str, list[dict[str, Any]]]:
    """Build a map of session_id -> events from the context snapshot."""
    session_map: dict[str, list[dict[str, Any]]] = {}
    history = context.get("session_history") or []
    for entry in history:
        session = entry.get("session") or {}
        sid = session.get("id", "")
        if not sid:
            continue
        events = entry.get("events") or []
        session_map[sid] = list(events)
    return session_map


def _extract_compact_response_text(response: Any) -> str:
    """Extract text from an LLM response."""
    if response is None:
        raise ValueError("compact model returned empty response")
    status_value = getattr(response, "status", "")
    status_text = str(getattr(status_value, "value", status_value)).lower()
    if status_text == "failure":
        error = getattr(response, "error", None)
        if error is not None:
            message = getattr(error, "message", "") or str(error)
            raise ValueError(f"compact model invocation failed: {message}")
        raise ValueError("compact model invocation failed")

    output_text = getattr(response, "output_text", None)
    if isinstance(output_text, str) and output_text.strip():
        return output_text.strip()

    response_payload = getattr(response, "response", None)
    output_items = getattr(response_payload, "output", None)
    if not isinstance(output_items, list):
        raise ValueError("compact model returned empty output")

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
        raise ValueError("compact model returned empty output")
    return "".join(fragments).strip()


def _parse_compact_payload(raw_output: str) -> tuple[dict[str, Any], list[dict[str, Any]]]:
    """Parse LLM JSON output into summary and proposal candidates."""
    text = raw_output.strip()
    # Strip code fences if present
    if text.startswith("```"):
        lines = text.splitlines()
        if lines[0].startswith("```"):
            lines = lines[1:]
        if lines and lines[-1].strip() == "```":
            lines = lines[:-1]
        text = "\n".join(lines).strip()

    try:
        parsed = json.loads(text)
    except json.JSONDecodeError as exc:
        raise ValueError(f"compact output is not valid JSON: {exc}") from exc

    if not isinstance(parsed, dict):
        raise ValueError("compact output must be a JSON object")

    raw_summary = parsed.get("summary")
    if not isinstance(raw_summary, dict):
        raw_summary = {}
    summary: dict[str, Any] = {}
    for key, value in raw_summary.items():
        if isinstance(value, str):
            summary[key] = value
        else:
            summary[key] = json.dumps(value, ensure_ascii=False)

    proposals: list[dict[str, Any]] = []
    raw_proposal = parsed.get("memory_proposal")
    if isinstance(raw_proposal, dict):
        entries_payload = raw_proposal.get("entries")
        entries: list[dict[str, Any]] = []
        if isinstance(entries_payload, list):
            for item in entries_payload:
                if not isinstance(item, dict):
                    continue
                key = str(item.get("key", "")).strip()
                if not key:
                    continue
                entries.append({"key": key, "value": item.get("value")})
        evidence_payload = raw_proposal.get("evidence")
        evidence: list[str] = []
        if isinstance(evidence_payload, list):
            for item in evidence_payload:
                text_item = str(item).strip()
                if text_item:
                    evidence.append(text_item)
        confidence_raw = raw_proposal.get("confidence", 0.0)
        try:
            confidence = float(confidence_raw)
        except (TypeError, ValueError):
            confidence = 0.0
        confidence = max(0.0, min(1.0, confidence))
        proposals.append(
            {
                "propose_update": bool(raw_proposal.get("propose_update", False)),
                "entries": entries,
                "evidence": evidence,
                "confidence": confidence,
                "reason": str(raw_proposal.get("reason", "")).strip(),
            }
        )
    return summary, proposals
