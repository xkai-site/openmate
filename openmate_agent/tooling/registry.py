from __future__ import annotations

from typing import Iterable

from openmate_agent.models import ToolResult

from .base import Tool, ToolContext


class ToolRegistry:
    def __init__(self, tools: Iterable[Tool] | None = None) -> None:
        self._tools: dict[str, Tool] = {}
        if tools:
            for tool in tools:
                self.register(tool)

    def register(self, tool: Tool) -> None:
        self._tools[tool.name] = tool

    def list_names(self) -> list[str]:
        return sorted(self._tools.keys())

    def execute(self, tool_name: str, context: ToolContext, payload: dict[str, object]) -> ToolResult:
        tool = self._tools.get(tool_name)
        if not tool:
            return ToolResult(tool_name=tool_name, success=False, error="tool not found")
        return tool.run(context=context, payload=payload)
