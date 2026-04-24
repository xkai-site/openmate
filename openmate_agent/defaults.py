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
    def inject(self, node_id: str) -> ToolBundle:
        return ToolBundle(
            node_id=node_id,
            tools=[
                ToolSpec(name="read", description="Read data."),
                ToolSpec(name="write", description="Write data."),
                ToolSpec(name="edit", description="Patch file content by old/new strings."),
                ToolSpec(name="patch", description="Apply structured multi-file patch operations."),
                ToolSpec(name="query", description="Query network resource."),
                ToolSpec(name="grep", description="Search content with regex via ripgrep."),
                ToolSpec(name="glob", description="Search files by glob via ripgrep."),
                ToolSpec(name="exec", description="Run structured command in workspace."),
                ToolSpec(name="shell", description="Run system shell command."),
            ],
        )

    def authorize(self, action: ToolAction) -> GuardDecision:
        if action.tool_name in {"read", "write", "edit", "patch", "query", "grep", "glob", "exec", "shell"}:
            return GuardDecision(decision="allow", reason="tool is allowed in MVP")
        return GuardDecision(decision="deny", reason="tool is not supported")


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
