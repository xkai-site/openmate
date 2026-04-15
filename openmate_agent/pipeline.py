from __future__ import annotations

from .interfaces import Assembler, ContextInjector, SkillInjector, ToolInjector
from .models import AgentInput


class BuildPipeline:
    """Build AgentInput from replaceable capability injectors."""

    def __init__(
        self,
        *,
        context_injector: ContextInjector,
        tool_injector: ToolInjector,
        skill_injector: SkillInjector,
        assembler: Assembler,
    ) -> None:
        self._context_injector = context_injector
        self._tool_injector = tool_injector
        self._skill_injector = skill_injector
        self._assembler = assembler

    def build(self, node_id: str) -> AgentInput:
        # Build order is fixed so each injector can be replaced independently.
        context = self._context_injector.inject(node_id)
        tools = self._tool_injector.inject(node_id)
        skills = self._skill_injector.inject(node_id)
        return self._assembler.assemble(context=context, tools=tools, skills=skills)
