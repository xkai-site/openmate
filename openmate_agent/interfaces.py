from __future__ import annotations

from typing import Any, Protocol

from openmate_pool.models import InvokeRequest, InvokeResponse

from .models import (
    AgentInput,
    ContextBundle,
    GuardDecision,
    SkillBundle,
    ToolAction,
    ToolBundle,
)
from .session_models import AppendSessionEventInput


class ContextInjector(Protocol):
    def inject(self, node_id: str) -> ContextBundle: ...


class ToolInjector(Protocol):
    def inject(self, node_id: str) -> ToolBundle: ...

    def authorize(self, action: ToolAction) -> GuardDecision: ...


class SkillInjector(Protocol):
    def inject(self, node_id: str) -> SkillBundle: ...


class Assembler(Protocol):
    def assemble(
        self,
        context: ContextBundle,
        tools: ToolBundle,
        skills: SkillBundle,
    ) -> AgentInput: ...


class AgentExecutor(Protocol):
    def execute(self, agent_input: AgentInput) -> str: ...


class LlmGateway(Protocol):
    def invoke(self, request: InvokeRequest) -> InvokeResponse: ...


class SessionEventWriter(Protocol):
    def ensure_session(self, node_id: str, session_id: str | None = None) -> str: ...

    def append_event(self, event: AppendSessionEventInput) -> Any: ...
