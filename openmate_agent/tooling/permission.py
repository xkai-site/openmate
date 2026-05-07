from __future__ import annotations

from dataclasses import dataclass
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

from openmate_agent.approval import directory_prefix_match, extract_tool_directory_prefixes
from openmate_agent.models import ApprovalRequest, GuardDecision, PermissionRule, ToolAction

from .registry import ToolRegistry

_SHELL_META_CHARS = ("|", ";", "&&", "||", ">", "<", "*", "?", "$(", "`")
_DESTRUCTIVE_TOKENS = ("rm", "del", "format", "mkfs", "rmdir")


@dataclass
class _ExecutionIntent:
    command_text: str
    read_paths: list[str]
    write_paths: list[str]
    needs_shell_features: bool
    allow_network: bool
    risk_tags: list[str]


class PermissionGateway:
    def __init__(self, *, tool_registry: ToolRegistry) -> None:
        self._tool_registry = tool_registry

    def evaluate(
        self,
        action: ToolAction,
        *,
        allowed_rules: list[PermissionRule] | None = None,
        workspace_root: Path | None = None,
    ) -> GuardDecision:
        spec = self._tool_registry.get_spec(action.tool_name)
        if spec is None:
            return GuardDecision(decision="deny", reason="unsupported tool", error_code="POLICY_DENIED")
        if not spec.enabled:
            return GuardDecision(decision="deny", reason="tool is disabled", error_code="POLICY_DENIED")

        if workspace_root is None:
            return GuardDecision(decision="deny", reason="workspace root is required", error_code="POLICY_SCOPE_VIOLATION")

        intent = _build_intent(action=action, workspace_root=workspace_root)
        if "destructive" in intent.risk_tags:
            return GuardDecision(
                decision="deny",
                reason="destructive commands are denied by policy; use a recycle-bin delete workflow instead",
                error_code="POLICY_DENIED",
                metadata=_evaluation_payload(intent=intent, matched_rule=None, result="deny"),
            )

        matched_rule = _match_rule(action, intent=intent, allowed_rules=allowed_rules)
        if matched_rule is not None:
            return GuardDecision(
                decision="allow",
                reason="allowed by stored permission rule",
                metadata=_evaluation_payload(intent=intent, matched_rule=matched_rule, result="allow"),
            )
        if action.is_safe and action.is_read_only:
            return GuardDecision(
                decision="allow",
                reason="allowed by safety flags",
                metadata=_evaluation_payload(intent=intent, matched_rule=None, result="allow"),
            )
        if action.is_safe and action.is_confirmed:
            return GuardDecision(
                decision="allow",
                reason="allowed by explicit confirmation",
                metadata=_evaluation_payload(intent=intent, matched_rule=None, result="allow"),
            )
        return GuardDecision(
            decision="confirm",
            reason="user confirmation required",
            error_code="POLICY_CONFIRM_REQUIRED",
            metadata=_evaluation_payload(intent=intent, matched_rule=None, result="confirm"),
        )

    def build_approval_request(
        self,
        *,
        action: ToolAction,
        node_id: str,
        topic_id: str | None,
        workspace_root: Path,
        reason: str,
    ) -> ApprovalRequest:
        directories = extract_tool_directory_prefixes(payload=action.payload, workspace_root=workspace_root)
        intent = _build_intent(action=action, workspace_root=workspace_root)
        return ApprovalRequest(
            request_id=f"approval:{node_id}:{action.tool_name}",
            node_id=node_id,
            topic_id=topic_id,
            target_type="tool",
            tool_name=action.tool_name,
            directories=directories,
            reason=reason,
            payload=action.payload,
            risk_tags=intent.risk_tags,
            requested_capabilities={
                "tool_name": action.tool_name,
                "read_path_prefixes": intent.read_paths,
                "write_path_prefixes": intent.write_paths,
                "allow_network": intent.allow_network,
                "allow_shell_features": intent.needs_shell_features,
                "risk_level": _risk_level(intent.risk_tags),
            },
        )


def _command_text(action: ToolAction) -> str:
    if action.tool_name == "shell":
        return str(action.payload.get("command", "")).strip()
    if action.tool_name == "command":
        shell_command = str(action.payload.get("shell_command", "")).strip()
        if shell_command:
            return shell_command
        raw = action.payload.get("command")
        if isinstance(raw, list):
            return " ".join(str(item).strip() for item in raw if str(item).strip()).strip()
        return ""
    raw = action.payload.get("command")
    if not isinstance(raw, list):
        return ""
    return " ".join(str(item).strip() for item in raw if str(item).strip()).strip()


def _match_rule(
    action: ToolAction,
    *,
    intent: _ExecutionIntent,
    allowed_rules: list[PermissionRule] | None,
) -> PermissionRule | None:
    if not allowed_rules:
        return None
    for rule in allowed_rules:
        if rule.tool_name != action.tool_name:
            continue
        read_allow = rule.read_path_prefixes or ([rule.normalized_dir_prefix] if rule.normalized_dir_prefix else [])
        write_allow = rule.write_path_prefixes or ([rule.normalized_dir_prefix] if rule.normalized_dir_prefix else [])
        if any(not _path_allowed(path=item, prefixes=read_allow) for item in intent.read_paths):
            continue
        if any(not _path_allowed(path=item, prefixes=write_allow) for item in intent.write_paths):
            continue
        if not _is_legacy_rule(rule):
            if intent.allow_network and not rule.allow_network:
                continue
            if intent.needs_shell_features and not rule.allow_shell_features:
                continue
        return rule
    return None


def _path_allowed(*, path: str, prefixes: list[str]) -> bool:
    if not prefixes:
        return False
    return any(directory_prefix_match(rule_prefix=prefix, candidate_dir=path) for prefix in prefixes)


def _build_intent(*, action: ToolAction, workspace_root: Path) -> _ExecutionIntent:
    directories = extract_tool_directory_prefixes(payload=action.payload, workspace_root=workspace_root)
    command_text = _command_text(action)
    needs_shell = _needs_shell_features(command_text)
    risk_tags: set[str] = set()
    read_paths: list[str] = []
    write_paths: list[str] = []
    if action.tool_name in {"read", "grep", "glob", "search"}:
        read_paths = directories
        risk_tags.add("filesystem_read")
    elif action.tool_name in {"write", "edit", "patch"}:
        write_paths = directories
        risk_tags.add("filesystem_write")
    elif action.tool_name in {"exec", "shell", "command"}:
        read_paths = directories
        write_paths = directories
        risk_tags.update({"filesystem_read", "filesystem_write"})
    if action.tool_name in {"query", "network"}:
        risk_tags.add("network")
    if needs_shell:
        risk_tags.add("shell_feature")
    if _is_destructive_command(command_text):
        risk_tags.add("destructive")
    return _ExecutionIntent(
        command_text=command_text,
        read_paths=read_paths,
        write_paths=write_paths,
        needs_shell_features=needs_shell,
        allow_network=action.tool_name in {"query", "network"},
        risk_tags=sorted(risk_tags),
    )


def _needs_shell_features(command: str) -> bool:
    text = str(command or "")
    return any(token in text for token in _SHELL_META_CHARS)


def _is_destructive_command(command: str) -> bool:
    normalized = str(command or "").lower()
    if not normalized.strip():
        return False
    return any(token in normalized for token in _DESTRUCTIVE_TOKENS)


def _risk_level(risk_tags: list[str]) -> str:
    tags = set(risk_tags)
    if "destructive" in tags:
        return "high"
    if "shell_feature" in tags or "network" in tags:
        return "medium"
    return "low"


def _evaluation_payload(*, intent: _ExecutionIntent, matched_rule: PermissionRule | None, result: str) -> dict[str, Any]:
    return {
        "result": result,
        "evaluated_at": datetime.now(UTC).isoformat().replace("+00:00", "Z"),
        "risk_tags": intent.risk_tags,
        "requested_capabilities": {
            "read_path_prefixes": intent.read_paths,
            "write_path_prefixes": intent.write_paths,
            "allow_network": intent.allow_network,
            "allow_shell_features": intent.needs_shell_features,
            "risk_level": _risk_level(intent.risk_tags),
        },
        "matched_rule": matched_rule.model_dump(mode="json") if matched_rule is not None else None,
    }


def _is_legacy_rule(rule: PermissionRule) -> bool:
    return bool(rule.normalized_dir_prefix) and not rule.allow_network and not rule.allow_shell_features
