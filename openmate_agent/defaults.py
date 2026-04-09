from __future__ import annotations

from .interfaces import AgentExecutor, Assembler, ContextInjector, SkillInjector, ToolInjector
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
        return ContextBundle(
            node_id=node_id,
            snippets=[f"context for node {node_id}"],
            summary=f"default context injected for {node_id}",
        )


class DefaultToolInjector(ToolInjector):
    def inject(self, node_id: str) -> ToolBundle:
        return ToolBundle(
            node_id=node_id,
            tools=[
                ToolSpec(name="read", description="Read data."),
                ToolSpec(name="write", description="Write data."),
                ToolSpec(name="edit", description="Patch file content by old/new strings."),
                ToolSpec(name="query", description="Query network resource."),
                ToolSpec(name="grep", description="Search content with regex via ripgrep."),
                ToolSpec(name="glob", description="Search files by glob via ripgrep."),
                ToolSpec(name="shell", description="Run system shell command."),
            ],
        )

    def authorize(self, action: ToolAction) -> GuardDecision:
        if action.tool_name in {"read", "write", "edit", "query", "grep", "glob", "shell"}:
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
        prompt = (
            f"node={context.node_id}\n"
            f"context={context.summary or 'no-summary'}\n"
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


class DefaultAgentExecutor(AgentExecutor):
    def execute(self, agent_input: AgentInput) -> str:
        return f"executed node={agent_input.node_id}"
