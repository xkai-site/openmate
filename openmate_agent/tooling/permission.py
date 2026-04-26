from __future__ import annotations

import re
from pathlib import Path

from openmate_agent.approval import directory_prefix_match, extract_tool_directory_prefixes
from openmate_agent.models import ApprovalRequest, GuardDecision, PermissionRule, ToolAction

from .registry import ToolRegistry


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
            return GuardDecision(decision="deny", reason="unsupported tool")
        if not spec.enabled:
            return GuardDecision(decision="deny", reason="tool is disabled")

        if spec.backend not in {"builtin/shell", "builtin/exec", "builtin/command"}:
            if _match_rule(action, allowed_rules=allowed_rules, workspace_root=workspace_root):
                return GuardDecision(decision="allow", reason="allowed by stored permission rule")
            if action.is_safe and action.is_read_only:
                return GuardDecision(decision="allow", reason="allowed by safety flags")
            return GuardDecision(decision="confirm", reason="user confirmation required")

        command = _command_text(action)
        if not command:
            return GuardDecision(decision="deny", reason="missing command")

        deny_patterns = [
            r"\bgit\s+reset\s+--hard\b",
            r"\brm\s+-rf\b",
            r"\bdel\s+/[a-zA-Z]*\s*/[a-zA-Z]*\b",
            r"\bformat\b",
        ]
        for pattern in deny_patterns:
            if re.search(pattern, command, flags=re.IGNORECASE):
                return GuardDecision(decision="deny", reason="dangerous shell command")

        if _match_rule(action, allowed_rules=allowed_rules, workspace_root=workspace_root):
            return GuardDecision(decision="allow", reason="allowed by stored permission rule")
        if action.is_safe and action.is_read_only:
            return GuardDecision(decision="allow", reason="allowed by safety flags")
        return GuardDecision(decision="confirm", reason="user confirmation required")

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
        return ApprovalRequest(
            request_id=f"approval:{node_id}:{action.tool_name}",
            node_id=node_id,
            topic_id=topic_id,
            target_type="tool",
            tool_name=action.tool_name,
            directories=directories,
            reason=reason,
            payload=action.payload,
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
    parts = [str(item).strip() for item in raw if str(item).strip()]
    return " ".join(parts).strip()


def _match_rule(
    action: ToolAction,
    *,
    allowed_rules: list[PermissionRule] | None,
    workspace_root: Path | None,
) -> bool:
    if not allowed_rules or workspace_root is None:
        return False
    directories = extract_tool_directory_prefixes(payload=action.payload, workspace_root=workspace_root)
    if not directories:
        return False
    for rule in allowed_rules:
        if rule.tool_name != action.tool_name:
            continue
        for directory in directories:
            if directory_prefix_match(rule_prefix=rule.normalized_dir_prefix, candidate_dir=directory):
                return True
    return False
