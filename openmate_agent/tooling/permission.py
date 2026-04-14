from __future__ import annotations

import re

from openmate_agent.models import GuardDecision, ToolAction


class PermissionGateway:
    def evaluate(self, action: ToolAction) -> GuardDecision:
        whitelist = {"read", "write", "edit", "patch", "query", "grep", "glob", "exec", "shell"}
        if action.tool_name not in whitelist:
            return GuardDecision(decision="deny", reason="unsupported tool")

        if not action.is_safe and not action.is_read_only:
            return GuardDecision(decision="confirm", reason="user confirmation required when both flags are false")

        if not (action.is_safe and action.is_read_only):
            return GuardDecision(decision="confirm", reason="both flags must be true before policy routing")

        if action.tool_name not in {"shell", "exec"}:
            return GuardDecision(decision="allow", reason="allowed by whitelist")

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

    raw = action.payload.get("command")
    if not isinstance(raw, list):
        return ""
    parts = [str(item).strip() for item in raw if str(item).strip()]
    return " ".join(parts).strip()
