from __future__ import annotations

import json
import logging
import time
from datetime import datetime
from typing import Any
from uuid import uuid4

_LOGGER = logging.getLogger(__name__)

from openmate_pool.models import InvokeRequest, OpenAIResponsesRequest

from .interfaces import LlmGateway
from .models import (
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
    ToolBundle,
)
from .orchestration import ContextTooLargeError, ExecutionOrchestrator
from .pipeline import BuildPipeline

class ExecutionAgentService:
    def __init__(self, *, build_pipeline: BuildPipeline, execution_orchestrator: ExecutionOrchestrator) -> None:
        self._build_pipeline = build_pipeline
        self._execution_orchestrator = execution_orchestrator

    def run(self, build: Build) -> str:
        agent_input = self._build_pipeline.build(build.node_id)
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
            agent_input = agent_input.model_copy(update={"prompt": self._build_execution_prompt(agent_input)})
            tools_payload = _build_openai_tools(agent_input.tools)
            return self._execution_orchestrator.execute(
                build=build,
                agent_input=agent_input,
                tools_payload=tools_payload,
            )

    @staticmethod
    def _build_execution_prompt(agent_input: Any) -> str:
        context_payload = ExecutionAgentService._parse_context_payload(agent_input.context.payload)
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
