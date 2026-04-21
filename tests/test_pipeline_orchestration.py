from __future__ import annotations

import unittest
from uuid import uuid4

from openmate_pool.models import InvocationStatus, InvocationTiming, InvokeRequest, InvokeResponse

from openmate_agent.models import AgentInput, Build, ContextBundle, SkillBundle, SkillSpec, ToolBundle, ToolResult, ToolSpec
from openmate_agent.orchestration import ExecutionOrchestrator, LangGraphExecutionRunner
from openmate_agent.pipeline import BuildPipeline
from openmate_agent.service import AgentCapabilityService


class BuildPipelineTests(unittest.TestCase):
    def test_build_pipeline_calls_components_in_order(self) -> None:
        calls: list[str] = []

        class ContextInjector:
            def inject(self, node_id: str) -> ContextBundle:
                calls.append("context")
                return ContextBundle(node_id=node_id, payload=f'{{"ctx":"{node_id}"}}')

        class ToolInjector:
            def inject(self, node_id: str) -> ToolBundle:
                calls.append("tool")
                return ToolBundle(node_id=node_id, tools=[ToolSpec(name="read", description="read file")])

        class SkillInjector:
            def inject(self, node_id: str) -> SkillBundle:
                calls.append("skill")
                return SkillBundle(node_id=node_id, skills=[SkillSpec(name="summarize")])

        class Assembler:
            def assemble(self, context: ContextBundle, tools: ToolBundle, skills: SkillBundle) -> AgentInput:
                calls.append("assemble")
                return AgentInput(
                    node_id=context.node_id,
                    context=context,
                    tools=tools,
                    skills=skills,
                    prompt=f"prompt:{context.payload}",
                )

        pipeline = BuildPipeline(
            context_injector=ContextInjector(),
            tool_injector=ToolInjector(),
            skill_injector=SkillInjector(),
            assembler=Assembler(),
        )
        result = pipeline.build("node-1")

        self.assertEqual(calls, ["context", "tool", "skill", "assemble"])
        self.assertEqual(result.node_id, "node-1")
        self.assertEqual(result.prompt, 'prompt:{"ctx":"node-1"}')
        self.assertEqual(result.tools.tools[0].name, "read")
        self.assertEqual(result.skills.skills[0].name, "summarize")

    def test_build_pipeline_bubbles_injector_errors(self) -> None:
        class FailingContextInjector:
            def inject(self, node_id: str) -> ContextBundle:
                raise RuntimeError(f"context failed for {node_id}")

        class ToolInjector:
            def inject(self, node_id: str) -> ToolBundle:
                return ToolBundle(node_id=node_id)

        class SkillInjector:
            def inject(self, node_id: str) -> SkillBundle:
                return SkillBundle(node_id=node_id)

        class Assembler:
            def assemble(self, context: ContextBundle, tools: ToolBundle, skills: SkillBundle) -> AgentInput:
                return AgentInput(node_id=context.node_id, context=context, tools=tools, skills=skills, prompt="x")

        pipeline = BuildPipeline(
            context_injector=FailingContextInjector(),
            tool_injector=ToolInjector(),
            skill_injector=SkillInjector(),
            assembler=Assembler(),
        )
        with self.assertRaises(RuntimeError):
            pipeline.build("node-err")


class OrchestrationInjectionTests(unittest.TestCase):
    def test_execution_orchestrator_uses_injected_runner(self) -> None:
        class StubRunner:
            def __init__(self) -> None:
                self.called = False
                self.called_build: Build | None = None
                self.called_tools: list[dict[str, object]] | None = None

            def run(
                self,
                *,
                build: Build,
                agent_input: AgentInput,
                tools_payload: list[dict[str, object]],
                gateway: object,
                session_writer: object,
                tool_executor: object,
            ) -> str:
                _ = (agent_input, gateway, session_writer, tool_executor)
                self.called = True
                self.called_build = build
                self.called_tools = tools_payload
                return "runner-ok"

        runner = StubRunner()
        orchestrator = ExecutionOrchestrator(
            gateway=_FakeGateway(),
            tool_executor=lambda node_id, tool_name, payload: ToolResult(tool_name=tool_name),
            runner=runner,
        )

        build = Build(node_id="node-1")
        agent_input = _minimal_agent_input("node-1")
        result = orchestrator.execute(build=build, agent_input=agent_input, tools_payload=[])

        self.assertEqual(result, "runner-ok")
        self.assertTrue(runner.called)
        self.assertEqual(runner.called_build.node_id if runner.called_build else None, "node-1")
        self.assertEqual(runner.called_tools, [])

    def test_langgraph_runner_delegates_to_fallback_runner(self) -> None:
        class FallbackRunner:
            def __init__(self) -> None:
                self.called = False

            def run(
                self,
                *,
                build: Build,
                agent_input: AgentInput,
                tools_payload: list[dict[str, object]],
                gateway: object,
                session_writer: object,
                tool_executor: object,
            ) -> str:
                _ = (build, agent_input, tools_payload, gateway, session_writer, tool_executor)
                self.called = True
                return "fallback-ok"

        fallback = FallbackRunner()
        runner = LangGraphExecutionRunner(fallback=fallback)
        result = runner.run(
            build=Build(node_id="node-lg"),
            agent_input=_minimal_agent_input("node-lg"),
            tools_payload=[],
            gateway=_FakeGateway(),
            session_writer=None,
            tool_executor=lambda node_id, tool_name, payload: ToolResult(tool_name=tool_name),
        )
        self.assertEqual(result, "fallback-ok")
        self.assertTrue(fallback.called)


class ServicePluggabilityTests(unittest.TestCase):
    def test_service_execute_agent_uses_injected_pipeline_and_orchestrator(self) -> None:
        class StubPipeline:
            def __init__(self) -> None:
                self.called_with: list[str] = []

            def build(self, node_id: str) -> AgentInput:
                self.called_with.append(node_id)
                return _minimal_agent_input(node_id)

        class StubOrchestrator:
            def __init__(self) -> None:
                self.called_with: tuple[Build, AgentInput, list[dict[str, object]]] | None = None

            def execute(
                self,
                *,
                build: Build,
                agent_input: AgentInput,
                tools_payload: list[dict[str, object]],
            ) -> str:
                self.called_with = (build, agent_input, tools_payload)
                return f"orchestrated:{build.node_id}"

        pipeline = StubPipeline()
        orchestrator = StubOrchestrator()
        service = AgentCapabilityService(
            gateway=_FakeGateway(),
            build_pipeline=pipeline,
            execution_orchestrator=orchestrator,
        )

        result = service.execute_agent(service.build("node-plug"))

        self.assertEqual(result, "orchestrated:node-plug")
        self.assertEqual(pipeline.called_with, ["node-plug"])
        self.assertIsNotNone(orchestrator.called_with)
        _, _, tools_payload = orchestrator.called_with or (None, None, None)
        self.assertIsInstance(tools_payload, list)
        self.assertEqual(tools_payload[0]["name"], "read")


def _minimal_agent_input(node_id: str) -> AgentInput:
    context = ContextBundle(node_id=node_id, payload=f'{{"ctx":"{node_id}"}}')
    tools = ToolBundle(node_id=node_id, tools=[ToolSpec(name="read", description="read file")])
    skills = SkillBundle(node_id=node_id, skills=[])
    return AgentInput(node_id=node_id, context=context, tools=tools, skills=skills, prompt=f"prompt:{node_id}")


class _FakeGateway:
    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=None,
            output_text=f"executed node={request.node_id}",
            timing=InvocationTiming(),
        )


if __name__ == "__main__":
    unittest.main()

