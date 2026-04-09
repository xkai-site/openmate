from __future__ import annotations

import re

from openmate_agent.models import GuardDecision, ToolAction


class PermissionGateway:
    def evaluate(self, action: ToolAction) -> GuardDecision:
        whitelist = {"read", "write", "edit", "query", "grep", "glob", "shell"}
        if action.tool_name not in whitelist:
            return GuardDecision(decision="deny", reason="unsupported tool")

        if not action.is_safe and not action.is_read_only:
            return GuardDecision(decision="confirm", reason="user confirmation required when both flags are false")

        if not (action.is_safe and action.is_read_only):
            return GuardDecision(decision="confirm", reason="both flags must be true before policy routing")

        if action.tool_name != "shell":
            return GuardDecision(decision="allow", reason="allowed by whitelist")

        command = str(action.payload.get("command", "")).strip()
        if not command:
            return GuardDecision(decision="deny", reason="missing shell command")

        deny_patterns = [
            r"\bgit\s+reset\s+--hard\b",
            r"\brm\s+-rf\b",
            r"\bdel\s+/[a-zA-Z]*\s*/[a-zA-Z]*\b",
            r"\bformat\b",
        ]
        for pattern in deny_patterns:
            if re.search(pattern, command, flags=re.IGNORECASE):
                return GuardDecision(decision="deny", reason="dangerous shell command")

        return GuardDecision(decision="allow", reason="shell command allowed by policy")
