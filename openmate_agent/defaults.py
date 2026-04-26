from __future__ import annotations

import json

from .interfaces import Assembler, ContextInjector, SkillInjector, ToolInjector
from .models import (
    AgentInput,
    ContextBundle,
    GuardDecision,
    SkillBundle,
    ToolAction,
    ToolBundle,
    ToolSpec,
)
from .tooling import ToolRegistry


class DefaultContextInjector(ContextInjector):
    def inject(self, node_id: str) -> ContextBundle:
        payload = {
            "node_id": node_id,
            "user_memory": None,
            "topic_memory": None,
            "process_contexts": [],
            "session_history": [],
        }
        return ContextBundle(
            node_id=node_id,
            payload=json.dumps(payload, ensure_ascii=False),
        )


class DefaultToolInjector(ToolInjector):
    def __init__(self, tool_registry: ToolRegistry) -> None:
        self._tool_registry = tool_registry

    def inject(self, node_id: str) -> ToolBundle:
        default_tools = self._tool_registry.list_specs(enabled_only=True, default_only=True)
        return ToolBundle(
            node_id=node_id,
            tools=[
                ToolSpec(
                    name=spec.name,
                    description=spec.description,
                    is_default=spec.is_default,
                    primary_tag=spec.primary_tag,
                    secondary_tags=list(spec.secondary_tags),
                    backend=spec.backend,
                    parameters_schema=dict(spec.parameters_schema),
                )
                for spec in default_tools
            ],
        )

    def authorize(self, action: ToolAction) -> GuardDecision:
        spec = self._tool_registry.get_spec(action.tool_name)
        if spec is None:
            return GuardDecision(decision="deny", reason="tool is not supported")
        if not spec.enabled:
            return GuardDecision(decision="deny", reason="tool is disabled")
        return GuardDecision(decision="allow", reason="tool is allowed by registry")


class DefaultSkillInjector(SkillInjector):
    def inject(self, node_id: str) -> SkillBundle:
        return SkillBundle(node_id=node_id, skills=[])


class DefaultAssembler(Assembler):
    def assemble(
        self,
        context: ContextBundle,
        tools: ToolBundle,
        skills: SkillBundle,
    ) -> AgentInput:
        tool_names = ", ".join(tool.name for tool in tools.tools) or "none"
        skill_names = ", ".join(skill.name for skill in skills.skills) or "none"
        # Keep context injection as a single payload block in prompt assembly.
        context_payload = context.payload or "{}"
        prompt = (
            f"node={context.node_id}\n"
            f"{context_payload}\n"
            f"tools={tool_names}\n"
            f"skills={skill_names}"
        )
        return AgentInput(
            node_id=context.node_id,
            context=context,
            tools=tools,
            skills=skills,
            prompt=prompt,
        )
