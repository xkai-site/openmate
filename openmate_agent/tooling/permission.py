from __future__ import annotations

import re

from openmate_agent.models import GuardDecision, ToolAction

from .registry import ToolRegistry


class PermissionGateway:
    def __init__(self, *, tool_registry: ToolRegistry) -> None:
        self._tool_registry = tool_registry

    def evaluate(self, action: ToolAction) -> GuardDecision:
        spec = self._tool_registry.get_spec(action.tool_name)
        if spec is None:
            return GuardDecision(decision="deny", reason="unsupported tool")
        if not spec.enabled:
            return GuardDecision(decision="deny", reason="tool is disabled")

        if not action.is_safe and not action.is_read_only:
            return GuardDecision(decision="confirm", reason="user confirmation required when both flags are false")
        if not (action.is_safe and action.is_read_only):
            return GuardDecision(decision="confirm", reason="both flags must be true before policy routing")

        if spec.backend not in {"builtin/shell", "builtin/exec", "builtin/command"}:
            return GuardDecision(decision="allow", reason="allowed by registry policy")

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

        return GuardDecision(decision="allow", reason="command allowed by policy")


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
