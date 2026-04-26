import json
import shutil
import sys
import threading
import unittest
from datetime import datetime
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock
from uuid import uuid4

from openmate_pool.models import (
    InvocationStatus,
    InvocationTiming,
    InvokeRequest,
    InvokeResponse,
    OpenAIResponsesResponse,
)
from openmate_agent.service import AgentCapabilityService
from openmate_agent.orchestration import ChatExecutionRunner, ResponsesExecutionRunner
from openmate_agent.session_models import AppendSessionEventInput
from openmate_agent.models import CompactProcessInput, CompactRequest, DecomposeRequest, PriorityCandidate, PriorityLevel, PriorityRequest, ToolResult
from openmate_agent.vos_cli import VosCommandError


class AgentCapabilityServiceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.service = AgentCapabilityService(gateway=_FakeGateway())

    def test_build_returns_build_model(self) -> None:
        build = self.service.build("node-1")
        self.assertEqual(build.node_id, "node-1")

    def test_execute_agent_returns_raw_content(self) -> None:
        build = self.service.build("node-2")
        result = self.service.execute_agent(build)
        self.assertIn("executed node=node-2", result)

    def test_execute_agent_without_tool_call_still_writes_session_event(self) -> None:
        session_writer = _SpySessionWriter()
        service = AgentCapabilityService(gateway=_FakeGateway(), session_writer=session_writer)
        result = service.execute_agent(service.build("node-no-tool", session_id="session-no-tool"))

        self.assertEqual(result, "executed node=node-no-tool")
        self.assertEqual(session_writer.ensure_calls, [("node-no-tool", "session-no-tool")])
        self.assertGreaterEqual(len(session_writer.events), 2)
        delta_events = [event for event in session_writer.events if event.item_type == "assistant_delta"]
        self.assertGreaterEqual(len(delta_events), 1)
        event = session_writer.events[-1]
        self.assertEqual(event.item_type, "message")
        self.assertEqual(event.role.value, "assistant")
        self.assertEqual(event.next_status.value, "completed")
        self.assertEqual(event.call_id, None)
        self.assertEqual(event.payload_json.get("output_text"), "executed node=node-no-tool")

    def test_execute_agent_runs_responses_tool_loop_and_writes_session_events(self) -> None:
        gateway = _ToolLoopGateway()
        session_writer = _SpySessionWriter()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                gateway=gateway,
                session_writer=session_writer,
                workspace_root=tmp,
            )
            result = service.execute_agent(service.build("node-tool-loop", session_id="session-1"))

        self.assertEqual(result, "tool-loop-finished")
        self.assertEqual(session_writer.ensure_calls, [("node-tool-loop", "session-1")])
        self.assertEqual(len(gateway.requests), 2)
        first = gateway.requests[0]
        self.assertIsNotNone(first.request.tools)
        tool_names = [tool.get("name") for tool in (first.request.tools or []) if isinstance(tool, dict)]
        self.assertEqual(tool_names, ["command", "network", "node_process", "read", "search", "tool_query", "write"])
        self.assertEqual(first.request.tool_choice, "auto")
        self.assertEqual(first.request.parallel_tool_calls, False)
        self.assertIsInstance(first.request.input, list)
        self.assertEqual(first.request.input[0]["role"], "user")
        self.assertIsInstance(first.request.input[0]["content"], str)
        first_content = first.request.input[0]["content"]
        self.assertIn('"SystemPrompt"', first_content)
        self.assertIn('"UserPrompt"', first_content)
        self.assertIn('"memory_update_confirmation_rule"', first_content)
        self.assertIn('"user_memory"', first_content)
        self.assertIn('"topic_memory"', first_content)
        self.assertIn('"process_contexts"', first_content)
        self.assertIn('"session_history"', first_content)
        self.assertNotIn('"node_memory"', first_content)
        self.assertNotIn('"global_index"', first_content)
        second = gateway.requests[1]
        self.assertEqual(second.request.previous_response_id, "resp-tool-1")
        self.assertIsInstance(second.request.input, list)
        self.assertEqual(second.request.input[0]["type"], "function_call_output")
        self.assertEqual(second.request.input[0]["call_id"], "call-1")

        self.assertGreaterEqual(len(session_writer.events), 4)
        self.assertEqual(session_writer.events[0].item_type, "function_call")
        self.assertEqual(session_writer.events[0].next_status.value, "waiting")
        self.assertEqual(session_writer.events[1].item_type, "function_call_output")
        self.assertEqual(session_writer.events[1].next_status.value, "active")
        self.assertIn("assistant_delta", [event.item_type for event in session_writer.events])
        self.assertEqual(session_writer.events[-1].item_type, "message")
        self.assertEqual(session_writer.events[-1].next_status.value, "completed")

    def test_execute_agent_runs_chat_tool_loop_and_writes_session_events(self) -> None:
        gateway = _ChatToolLoopGateway()
        session_writer = _SpySessionWriter()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                gateway=gateway,
                session_writer=session_writer,
                workspace_root=tmp,
                execution_runner=ChatExecutionRunner(),
            )
            result = service.execute_agent(service.build("node-chat-tool-loop", session_id="session-chat-1"))

        self.assertEqual(result, "chat-tool-loop-finished")
        self.assertEqual(session_writer.ensure_calls, [("node-chat-tool-loop", "session-chat-1")])
        self.assertEqual(len(gateway.requests), 2)
        first = gateway.requests[0]
        self.assertIsNone(first.request)
        self.assertIsNotNone(first.chat_request)
        self.assertEqual(first.chat_request.tool_choice, "auto")
        self.assertNotIn("previous_response_id", first.chat_request.model_dump(mode="json", exclude_none=True))
        self.assertGreaterEqual(len(first.chat_request.messages), 2)
        self.assertEqual(first.chat_request.messages[0]["role"], "system")
        self.assertEqual(first.chat_request.messages[1]["role"], "user")
        second = gateway.requests[1]
        self.assertIsNotNone(second.chat_request)
        self.assertGreaterEqual(len(second.chat_request.messages), 4)
        self.assertEqual(second.chat_request.messages[-1]["role"], "tool")
        self.assertEqual(second.chat_request.messages[-1]["tool_call_id"], "chat-call-1")

        self.assertGreaterEqual(len(session_writer.events), 4)
        self.assertEqual(session_writer.events[0].item_type, "function_call")
        self.assertEqual(session_writer.events[1].item_type, "function_call_output")
        self.assertIn("assistant_delta", [event.item_type for event in session_writer.events])
        self.assertEqual(session_writer.events[-1].item_type, "message")
        self.assertEqual(session_writer.events[-1].next_status.value, "completed")

    def test_execute_agent_marks_failed_status_when_gateway_raises_after_tool_call(self) -> None:
        gateway = _ToolLoopThenFailGateway()
        session_writer = _SpySessionWriter()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                gateway=gateway,
                session_writer=session_writer,
                workspace_root=tmp,
            )
            with self.assertRaises(RuntimeError):
                service.execute_agent(service.build("node-tool-loop-fail", session_id="session-2"))

        self.assertEqual(len(session_writer.events), 3)
        self.assertEqual(session_writer.events[-1].item_type, "function_call_output")
        self.assertEqual(session_writer.events[-1].next_status.value, "failed")
        self.assertEqual(session_writer.events[-1].payload_json.get("ok"), False)

    def test_execute_agent_can_use_go_cli_gateway(self) -> None:
        server, thread = _start_gateway_server()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with TemporaryDirectory() as tmp:
            model_config = Path(tmp, "model.json")
            model_config.write_text(
                json.dumps(
                    {
                        "global_max_concurrent": 2,
                        "offline_failure_threshold": 3,
                        "apis": [
                            {
                                "api_id": "api-1",
                                "model": "gpt-4.1",
                                "base_url": f"http://127.0.0.1:{server.server_port}/v1",
                                "api_key": "sk-test",
                                "max_concurrent": 1,
                                "enabled": True,
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            service = AgentCapabilityService(workspace_root=tmp)
            result = service.execute_agent(service.build("node-real"))
            self.assertIn("echo:{", result)
            self.assertIn('"SystemPrompt"', result)
            self.assertIn('"UserPrompt"', result)
            self.assertIn('"node_id": "node-real"', result)

    def test_priority_returns_true_for_non_empty_input(self) -> None:
        result = self.service.priority(["n1", "n2"], hint="hot-topic")
        self.assertTrue(result)

    def test_priority_returns_false_for_empty_input(self) -> None:
        result = self.service.priority([])
        self.assertFalse(result)

    def test_decompose_agent_returns_tasks(self) -> None:
        service = AgentCapabilityService(gateway=_DecomposeGateway())
        response = service.decompose_agent(
            DecomposeRequest(
                request_id="req-decompose-service-1",
                topic_id="topic-1",
                node_id="node-1",
                node_name="Implement agent refactor",
                mode="decompose",
                max_items=2,
            )
        )
        self.assertEqual(response.status, "succeeded")
        self.assertEqual(len(response.tasks), 2)
        self.assertEqual(response.tasks[0].status, "ready")

    def test_decompose_agent_returns_failed_on_invalid_json_output(self) -> None:
        service = AgentCapabilityService(gateway=_BadDecomposeGateway())
        response = service.decompose_agent(
            DecomposeRequest(
                request_id="req-decompose-service-invalid",
                topic_id="topic-1",
                node_id="node-1",
                node_name="Invalid decompose output",
                mode="decompose",
                max_items=3,
            )
        )
        self.assertEqual(response.status, "failed")
        self.assertIn("not valid JSON", response.error or "")

    def test_decompose_agent_returns_failed_on_empty_tasks(self) -> None:
        service = AgentCapabilityService(gateway=_EmptyDecomposeGateway())
        response = service.decompose_agent(
            DecomposeRequest(
                request_id="req-decompose-service-empty",
                topic_id="topic-1",
                node_id="node-1",
                node_name="Empty decompose output",
                mode="decompose",
                max_items=3,
            )
        )
        self.assertEqual(response.status, "failed")
        self.assertIn("no valid tasks", response.error or "")

    def test_priority_agent_returns_plan(self) -> None:
        response = self.service.priority_agent(
            PriorityRequest(
                request_id="req-priority-service-1",
                topic_id="topic-1",
                node_id="priority-node",
                node_name="__priority__",
                mode="priority",
                candidates=[
                    PriorityCandidate(
                        node_id="node-a",
                        name="Node A",
                        status="ready",
                        current_priority=PriorityLevel(label="normal", rank=2),
                        entered_priority_at=datetime.fromisoformat("2026-04-21T09:00:00+00:00"),
                    ),
                    PriorityCandidate(
                        node_id="node-b",
                        name="Node B",
                        status="pending",
                        current_priority=PriorityLevel(label="normal", rank=1),
                        entered_priority_at=datetime.fromisoformat("2026-04-21T10:00:00+00:00"),
                    ),
                ],
            )
        )
        self.assertEqual(response.status, "succeeded")
        self.assertEqual(len(response.priority_plan), 2)

    def test_compact_agent_returns_summary_and_pending_proposal_candidates(self) -> None:
        service = AgentCapabilityService(gateway=_CompactGateway())
        response = service.compact_agent(
            CompactRequest(
                node_id="node-compact",
                processes=[
                    CompactProcessInput(
                        process={"id": "proc-1", "name": "Implement", "summary": {"legacy": "keep"}},
                        uncompacted_session_ids=["session-1"],
                    )
                ],
                context={
                    "session_history": [
                        {
                            "session": {"id": "session-1"},
                            "events": [{"seq": 1, "item_type": "message", "payload_json": {"text": "done"}}],
                        }
                    ]
                },
            )
        )
        self.assertEqual(response.status, "succeeded")
        self.assertEqual(len(response.compacted), 1)
        compacted = response.compacted[0]
        self.assertEqual(compacted.process_id, "proc-1")
        self.assertEqual(compacted.summary["legacy"], "keep")
        self.assertIn("key_findings", compacted.summary)
        self.assertEqual(len(compacted.memory_proposals), 1)
        self.assertTrue(compacted.memory_proposals[0].propose_update)
        self.assertEqual(compacted.memory_proposals[0].entries[0].key, "team_norm")

    def test_compact_agent_returns_failed_on_invalid_output(self) -> None:
        service = AgentCapabilityService(gateway=_BadCompactGateway())
        response = service.compact_agent(
            CompactRequest(
                node_id="node-compact-fail",
                processes=[CompactProcessInput(process={"id": "proc-1", "name": "Implement"}, uncompacted_session_ids=["s1"])],
                context={"session_history": [{"session": {"id": "s1"}, "events": [{"seq": 1}]}]},
            )
        )
        self.assertEqual(response.status, "failed")
        self.assertIn("compact output is not valid JSON", response.error or "")

    def test_tool_output_payload_uses_structured_error_code(self) -> None:
        failed = ToolResult(
            tool_name="sibling_progress_board",
            success=False,
            error_code="VOS_NODE_NOT_FOUND",
            error="node_not_found: node not found",
        )
        payload = ResponsesExecutionRunner._build_tool_output_payload(failed)
        self.assertEqual(payload["ok"], False)
        error = payload.get("error") or {}
        self.assertEqual(error.get("code"), "VOS_NODE_NOT_FOUND")

    def test_run_tool_write_read_query(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Tool",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                write_result = service.run_tool(
                    node_id="node-tool",
                    tool_name="write",
                    payload={"path": "notes/demo.txt", "content": "hello tool runtime"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(write_result.success)
                self.assertTrue(Path(tmp, "notes", "demo.txt").exists())

                read_result = service.run_tool(
                    node_id="node-tool",
                    tool_name="read",
                    payload={"path": "notes/demo.txt"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(read_result.success)
                self.assertIn("hello tool runtime", read_result.output)

                edit_result = service.run_tool(
                    node_id="node-tool",
                    tool_name="edit",
                    payload={"path": "notes/demo.txt", "old_string": "hello tool runtime", "new_string": "hello edited"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(edit_result.success)
                self.assertIn("edited:", edit_result.output)

    def test_run_tool_prefers_topic_workspace_for_system_tools(self) -> None:
        with TemporaryDirectory() as repo_root, TemporaryDirectory() as topic_workspace:
            topic_file = Path(topic_workspace, "notes", "from-topic.txt")
            topic_file.parent.mkdir(parents=True, exist_ok=True)
            topic_file.write_text("topic-workspace-content", encoding="utf-8")
            Path(repo_root, "notes").mkdir(parents=True, exist_ok=True)
            Path(repo_root, "notes", "from-topic.txt").write_text("repo-content", encoding="utf-8")

            service = AgentCapabilityService(workspace_root=repo_root)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Topic",
                    topic_id="topic-1",
                    topic_workspace=topic_workspace,
                )
                read_result = service.run_tool(
                    node_id="node-topic-1",
                    tool_name="read",
                    payload={"path": "notes/from-topic.txt"},
                    is_safe=True,
                    is_read_only=True,
                )
            self.assertTrue(read_result.success)
            self.assertIn("topic-workspace-content", read_result.output)
            self.assertNotIn("repo-content", read_result.output)

    def test_run_tool_blocks_when_topic_workspace_lookup_fails(self) -> None:
        with TemporaryDirectory() as repo_root:
            service = AgentCapabilityService(workspace_root=repo_root)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.side_effect = VosCommandError("vos unavailable")
                read_result = service.run_tool(
                    node_id="node-repo-only",
                    tool_name="read",
                    payload={"path": "notes/repo-only.txt"},
                    is_safe=True,
                    is_read_only=True,
                )
            self.assertFalse(read_result.success)
            self.assertEqual(read_result.error_code, "WORKSPACE_UNAVAILABLE")

    def test_run_tool_blocks_when_topic_workspace_not_bound(self) -> None:
        with TemporaryDirectory() as repo_root:
            service = AgentCapabilityService(workspace_root=repo_root)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Topic",
                    topic_id="topic-1",
                    topic_workspace=None,
                )
                read_result = service.run_tool(
                    node_id="node-topic-unbound",
                    tool_name="read",
                    payload={"path": "notes/from-topic.txt"},
                    is_safe=True,
                    is_read_only=True,
                )
            self.assertFalse(read_result.success)
            self.assertEqual(read_result.error_code, "WORKSPACE_UNAVAILABLE")

    def test_run_tool_blocks_when_topic_workspace_is_not_a_directory(self) -> None:
        with TemporaryDirectory() as repo_root:
            file_path = Path(repo_root, "topic-workspace.txt")
            file_path.write_text("not-a-dir", encoding="utf-8")
            service = AgentCapabilityService(workspace_root=repo_root)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Topic",
                    topic_id="topic-1",
                    topic_workspace=str(file_path),
                )
                read_result = service.run_tool(
                    node_id="node-topic-invalid-workspace",
                    tool_name="read",
                    payload={"path": "notes/from-topic.txt"},
                    is_safe=True,
                    is_read_only=True,
                )
            self.assertFalse(read_result.success)
            self.assertEqual(read_result.error_code, "WORKSPACE_UNAVAILABLE")

    def test_run_tool_query_http(self) -> None:
        server, thread = _start_test_server()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join, 1)
        self.addCleanup(server.shutdown)

        service = AgentCapabilityService()
        url = f"http://127.0.0.1:{server.server_port}/echo"
        query_result = service.run_tool(
            node_id="node-tool",
            tool_name="query",
            payload={
                "url": url,
                "method": "GET",
                "params": {"q": "hello"},
                "headers": {"X-Test": "ok"},
                "timeout_seconds": 5,
            },
            is_safe=True,
            is_read_only=True,
        )
        self.assertTrue(query_result.success)
        self.assertIn('"path": "/echo"', query_result.output)
        self.assertIn('"q": "hello"', query_result.output)

    def test_run_tool_shell(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Shell",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                result = service.run_tool(
                    node_id="node-shell",
                    tool_name="shell",
                    payload={"command": "Write-Output shell-ok"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(result.success)
                self.assertIn("shell-ok", result.output)

    def test_run_tool_exec(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Exec",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                result = service.run_tool(
                    node_id="node-exec",
                    tool_name="exec",
                    payload={"command": [sys.executable, "-c", "print('exec-ok')"]},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(result.success)
                self.assertIn("exec-ok", result.output)
                self.assertIn("exit_code:0", result.output)

    def test_run_tool_exec_expect_json_requires_valid_stdout(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Exec JSON",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                ok_result = service.run_tool(
                    node_id="node-exec-json",
                    tool_name="exec",
                    payload={
                        "command": [sys.executable, "-c", "import json; print(json.dumps({'ok': True}))"],
                        "expect_json": True,
                    },
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(ok_result.success)
                self.assertIn('"stdout_json"', ok_result.output)
                self.assertIn('"ok": true', ok_result.output.lower())

                bad_result = service.run_tool(
                    node_id="node-exec-json-bad",
                    tool_name="exec",
                    payload={"command": [sys.executable, "-c", "print('not-json')"], "expect_json": True},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertFalse(bad_result.success)
                self.assertIn("stdout is not valid json", bad_result.error or "")

    def test_run_tool_tool_query_threshold_default(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            result = service.run_tool(
                node_id="node-tool-query",
                tool_name="tool_query",
                payload={},
                is_safe=True,
                is_read_only=True,
            )
            self.assertTrue(result.success)
            payload = json.loads(result.output)
            self.assertEqual(payload.get("mode"), "tools")
            self.assertGreaterEqual(payload.get("remaining_count", 0), 1)

    def test_run_tool_tool_query_threshold_tags_when_over_limit(self) -> None:
        with TemporaryDirectory() as tmp:
            _write_tool_registry(
                Path(tmp),
                tools=[
                    {
                        "name": "cmd_extra_1",
                        "description": "extra command 1",
                        "enabled": True,
                        "is_default": False,
                        "primary_tag": "command_ext",
                        "secondary_tags": [],
                        "backend": "builtin/exec",
                    },
                    {
                        "name": "cmd_extra_2",
                        "description": "extra command 2",
                        "enabled": True,
                        "is_default": False,
                        "primary_tag": "command_ext",
                        "secondary_tags": [],
                        "backend": "builtin/exec",
                    },
                    {
                        "name": "cmd_extra_3",
                        "description": "extra command 3",
                        "enabled": True,
                        "is_default": False,
                        "primary_tag": "command_ext",
                        "secondary_tags": [],
                        "backend": "builtin/exec",
                    },
                    {
                        "name": "net_extra_1",
                        "description": "extra network 1",
                        "enabled": True,
                        "is_default": False,
                        "primary_tag": "network_ext",
                        "secondary_tags": [],
                        "backend": "builtin/query",
                    },
                ],
            )
            service = AgentCapabilityService(workspace_root=tmp)
            result = service.run_tool(
                node_id="node-tool-query-tags",
                tool_name="tool_query",
                payload={},
                is_safe=True,
                is_read_only=True,
            )
            self.assertTrue(result.success)
            payload = json.loads(result.output)
            self.assertEqual(payload.get("mode"), "tags")
            self.assertGreater(payload.get("remaining_count", 0), 10)
            self.assertIsInstance(payload.get("tags"), list)

    def test_run_tool_tool_query_by_tag(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            result = service.run_tool(
                node_id="node-tool-query-tag",
                tool_name="tool_query",
                payload={"by_tag": "command"},
                is_safe=True,
                is_read_only=True,
            )
            self.assertTrue(result.success)
            payload = json.loads(result.output)
            self.assertEqual(payload.get("mode"), "by_tag")
            self.assertEqual(payload.get("tag"), "command")
            self.assertGreaterEqual(payload.get("remaining_count", 0), 2)

    def test_run_tool_node_process_get(self) -> None:
        with TemporaryDirectory() as tmp:
            with (
                mock.patch("openmate_agent.vos_cli.run_vos_cli") as vos_mock_runtime,
                mock.patch("openmate_agent.tooling.tools.run_vos_cli") as vos_mock_tools,
            ):
                vos_mock_runtime.return_value = '{"id":"node-process","name":"ProcessNode","parent_id":"parent-1"}'
                vos_mock_tools.return_value = '[{"id":"proc-1","name":"Design","status":"todo"}]'
                service = AgentCapabilityService(workspace_root=tmp)
                result = service.run_tool(
                    node_id="node-process",
                    tool_name="node_process",
                    payload={"action": "get"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(result.success)
                payload = json.loads(result.output)
                self.assertEqual(payload["node_id"], "node-process")
                self.assertEqual(payload["action"], "get")
                self.assertEqual(payload["processes"][0]["id"], "proc-1")

    def test_run_tool_node_process_replace_requires_processes(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            result = service.run_tool(
                node_id="node-process",
                tool_name="node_process",
                payload={"action": "replace"},
                is_safe=True,
                is_read_only=True,
            )
            self.assertFalse(result.success)
            self.assertIn("processes is required", result.error or "")

    def test_run_tool_sibling_progress_board_root_returns_empty_items(self) -> None:
        with TemporaryDirectory() as tmp:
            with (
                mock.patch("openmate_agent.vos_cli.run_vos_cli") as vos_mock_runtime,
                mock.patch("openmate_agent.tooling.tools.run_vos_cli") as vos_mock_tools,
            ):
                vos_mock_runtime.return_value = '{"id":"node-root","name":"Root","parent_id":null}'
                service = AgentCapabilityService(workspace_root=tmp)
                result = service.run_tool(
                    node_id="node-root",
                    tool_name="sibling_progress_board",
                    payload={},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(result.success)
                payload = json.loads(result.output)
                self.assertEqual(payload["node_id"], "node-root")
                self.assertEqual(payload["parent_id"], None)
                self.assertEqual(payload["items"], [])

    def test_runtime_registry_auto_registers_sibling_progress_board(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            _ = service
            path = Path(tmp, ".openmate", "runtime", "tool_registry.json")
            self.assertTrue(path.exists())
            payload = json.loads(path.read_text(encoding="utf-8"))
            tools = payload.get("tools", [])
            self.assertTrue(any(item.get("name") == "sibling_progress_board" for item in tools))

    def test_run_tool_sibling_progress_board_non_root_returns_items(self) -> None:
        with TemporaryDirectory() as tmp:
            with (
                mock.patch("openmate_agent.vos_cli.run_vos_cli") as vos_mock_runtime,
                mock.patch("openmate_agent.tooling.tools.run_vos_cli") as vos_mock_tools,
            ):
                vos_mock_runtime.return_value = '{"id":"node-1","name":"Child","parent_id":"parent-0"}'
                vos_mock_tools.return_value = '[{"node_id":"node-1","node_name":"Child","processes":[{"id":"p-1","name":"Design","status":"done"}]}]'
                service = AgentCapabilityService(workspace_root=tmp)
                result = service.run_tool(
                    node_id="node-1",
                    tool_name="sibling_progress_board",
                    payload={},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(result.success)
                payload = json.loads(result.output)
                self.assertEqual(payload["node_id"], "node-1")
                self.assertEqual(payload["parent_id"], "parent-0")
                self.assertEqual(len(payload["items"]), 1)
                self.assertEqual(payload["items"][0]["node_name"], "Child")
                self.assertEqual(payload["items"][0]["process_name"], "Design")

    def test_run_tool_sibling_progress_board_invalid_node_returns_error(self) -> None:
        with TemporaryDirectory() as tmp:
            with mock.patch("openmate_agent.vos_cli.run_vos_cli") as vos_mock_runtime:
                vos_mock_runtime.side_effect = VosCommandError("node not found")
                service = AgentCapabilityService(workspace_root=tmp)
                result = service.run_tool(
                    node_id="node-missing",
                    tool_name="sibling_progress_board",
                    payload={},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertFalse(result.success)
                self.assertEqual(result.error_code, "VOS_NODE_NOT_FOUND")
                self.assertIn("node not found", result.error or "")
                self.assertIn("node_not_found", result.error or "")

    def test_run_tool_sibling_progress_board_vos_unavailable_returns_error(self) -> None:
        with TemporaryDirectory() as tmp:
            with mock.patch("openmate_agent.vos_cli.run_vos_cli") as vos_mock_runtime:
                vos_mock_runtime.side_effect = VosCommandError("vos CLI failed")
                service = AgentCapabilityService(workspace_root=tmp)
                result = service.run_tool(
                    node_id="node-any",
                    tool_name="sibling_progress_board",
                    payload={},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertFalse(result.success)
                self.assertEqual(result.error_code, "VOS_UNAVAILABLE")
                self.assertIn("vos_unavailable", result.error or "")

    def test_run_tool_sibling_progress_board_invalid_payload_returns_error(self) -> None:
        with TemporaryDirectory() as tmp:
            with mock.patch("openmate_agent.vos_cli.run_vos_cli") as vos_mock_runtime:
                vos_mock_runtime.return_value = "[]"
                service = AgentCapabilityService(workspace_root=tmp)
                result = service.run_tool(
                    node_id="node-any",
                    tool_name="sibling_progress_board",
                    payload={},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertFalse(result.success)
                self.assertEqual(result.error_code, "VOS_INVALID_PAYLOAD")
                self.assertIn("invalid_payload", result.error or "")

    def test_run_tool_requires_confirmation_when_flags_are_default(self) -> None:
        service = AgentCapabilityService()
        result = service.run_tool(
            node_id="node-need-confirm",
            tool_name="read",
            payload={"path": "AGENTS.md"},
        )
        self.assertFalse(result.success)
        self.assertIn("confirm", result.error or "")

    def test_write_blocked_when_file_modified_externally(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            target = Path(tmp, "a.txt")
            target.write_text("v1", encoding="utf-8")
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Conflict",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                read_result = service.run_tool(
                    node_id="node-conflict",
                    tool_name="read",
                    payload={"path": "a.txt"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(read_result.success)

                target.write_text("v2-external", encoding="utf-8")
                write_result = service.run_tool(
                    node_id="node-conflict",
                    tool_name="write",
                    payload={"path": "a.txt", "content": "v3"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertFalse(write_result.success)
                self.assertIn("modified externally", write_result.error or "")

    def test_run_tool_patch_updates_multiple_files(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            base = Path(tmp)
            first = base / "a.txt"
            second = base / "pkg" / "module.py"
            first.write_text("alpha\nbeta\n", encoding="utf-8")
            second.parent.mkdir(parents=True, exist_ok=True)
            second.write_text("VALUE = 1\n", encoding="utf-8")

            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Patch",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                read_first = service.run_tool(
                    node_id="node-patch",
                    tool_name="read",
                    payload={"path": "a.txt"},
                    is_safe=True,
                    is_read_only=True,
                )
                read_second = service.run_tool(
                    node_id="node-patch",
                    tool_name="read",
                    payload={"path": "pkg/module.py"},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(read_first.success)
                self.assertTrue(read_second.success)

                patch_result = service.run_tool(
                    node_id="node-patch",
                    tool_name="patch",
                    payload={
                        "operations": [
                            {
                                "type": "replace",
                                "path": "a.txt",
                                "old_string": "beta",
                                "new_string": "gamma",
                            },
                            {
                                "type": "replace",
                                "path": "pkg/module.py",
                                "old_string": "VALUE = 1",
                                "new_string": "VALUE = 2",
                            },
                        ]
                    },
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(patch_result.success)
                self.assertIn("patched:2 files, 2 operations", patch_result.output)
                self.assertEqual(first.read_text(encoding="utf-8"), "alpha\ngamma\n")
                self.assertEqual(second.read_text(encoding="utf-8"), "VALUE = 2\n")

    def test_run_tool_patch_is_atomic_when_operation_fails(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)
            base = Path(tmp)
            first = base / "a.txt"
            second = base / "b.txt"
            first.write_text("one\n", encoding="utf-8")
            second.write_text("two\n", encoding="utf-8")

            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Patch Atomic",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                self.assertTrue(
                    service.run_tool(
                        node_id="node-patch-atomic",
                        tool_name="read",
                        payload={"path": "a.txt"},
                        is_safe=True,
                        is_read_only=True,
                    ).success
                )
                self.assertTrue(
                    service.run_tool(
                        node_id="node-patch-atomic",
                        tool_name="read",
                        payload={"path": "b.txt"},
                        is_safe=True,
                        is_read_only=True,
                    ).success
                )

                patch_result = service.run_tool(
                    node_id="node-patch-atomic",
                    tool_name="patch",
                    payload={
                        "operations": [
                            {
                                "type": "replace",
                                "path": "a.txt",
                                "old_string": "one",
                                "new_string": "changed",
                            },
                            {
                                "type": "replace",
                                "path": "b.txt",
                                "old_string": "missing",
                                "new_string": "boom",
                            },
                        ]
                    },
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertFalse(patch_result.success)
                self.assertEqual(first.read_text(encoding="utf-8"), "one\n")
                self.assertEqual(second.read_text(encoding="utf-8"), "two\n")

    def test_run_tool_grep_and_glob(self) -> None:
        if shutil.which("rg") is None:
            self.skipTest("ripgrep not installed")
        with TemporaryDirectory() as tmp:
            base = Path(tmp)
            (base / ".gitignore").write_text("ignored.py\n", encoding="utf-8")
            (base / "src").mkdir(parents=True, exist_ok=True)
            (base / "src" / "a.py").write_text("def hello_tool():\n    return 1\n", encoding="utf-8")
            (base / "ignored.py").write_text("def hidden():\n    pass\n", encoding="utf-8")
            service = AgentCapabilityService(workspace_root=tmp)
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Search",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                grep_result = service.run_tool(
                    node_id="node-search",
                    tool_name="grep",
                    payload={"pattern": "hello_.*", "scope": "."},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(grep_result.success)
                self.assertIn("a.py", grep_result.output)

                glob_result = service.run_tool(
                    node_id="node-search",
                    tool_name="glob",
                    payload={"pattern": "**/*.py", "scope": "."},
                    is_safe=True,
                    is_read_only=True,
                )
                self.assertTrue(glob_result.success)
                self.assertIn("src", glob_result.output)
                self.assertNotIn("ignored.py", glob_result.output)


def _write_tool_registry(workspace_root: Path, tools: list[dict[str, object]]) -> None:
    path = workspace_root / ".openmate" / "runtime" / "tool_registry.json"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps({"version": 1, "tools": tools}, ensure_ascii=False, indent=2), encoding="utf-8")


class _EchoHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802
        from urllib.parse import parse_qs, urlparse

        parsed = urlparse(self.path)
        payload = {
            "path": parsed.path,
            "query": {k: v[0] for k, v in parse_qs(parsed.query).items()},
            "header": self.headers.get("X-Test"),
        }
        body = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


def _start_test_server() -> tuple[ThreadingHTTPServer, threading.Thread]:
    server = ThreadingHTTPServer(("127.0.0.1", 0), _EchoHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


class _GatewayHandler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/responses":
            self.send_error(404)
            return
        body = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode("utf-8")
        payload = json.loads(body)
        text = _extract_input_text(payload.get("input"))
        response = json.dumps(_response_payload_for_text(f"echo:{text}")).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


def _start_gateway_server() -> tuple[ThreadingHTTPServer, threading.Thread]:
    server = ThreadingHTTPServer(("127.0.0.1", 0), _GatewayHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


class _SpySessionWriter:
    def __init__(self) -> None:
        self.ensure_calls: list[tuple[str, str | None]] = []
        self.events: list[AppendSessionEventInput] = []

    def ensure_session(self, node_id: str, session_id: str | None = None) -> str:
        self.ensure_calls.append((node_id, session_id))
        return session_id or f"session-{node_id}"

    def append_event(self, event: AppendSessionEventInput) -> None:
        self.events.append(event)


class _ToolLoopGateway:
    def __init__(self) -> None:
        self.requests: list[InvokeRequest] = []

    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        self.requests.append(request)
        if len(self.requests) == 1:
            payload = {
                "id": "resp-tool-1",
                "object": "response",
                "model": "gpt-4.1",
                "status": "completed",
                "output": [
                    {
                        "id": "item-call-1",
                        "type": "function_call",
                        "call_id": "call-1",
                        "name": "exec",
                        "arguments": json.dumps({"command": [sys.executable, "-c", "print('tool-loop-ok')"]}),
                    }
                ],
                "usage": {
                    "input_tokens": 1,
                    "output_tokens": 1,
                    "total_tokens": 2,
                },
            }
            return InvokeResponse(
                invocation_id=str(uuid4()),
                request_id=request.request_id,
                node_id=request.node_id,
                status=InvocationStatus.SUCCESS,
                response=OpenAIResponsesResponse.model_validate(payload),
                output_text=None,
                timing=InvocationTiming(),
            )

        payload = _response_payload_for_text("tool-loop-finished")
        payload["id"] = "resp-tool-2"
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=OpenAIResponsesResponse.model_validate(payload),
            output_text="tool-loop-finished",
            timing=InvocationTiming(),
        )


class _ChatToolLoopGateway:
    def __init__(self) -> None:
        self.requests: list[InvokeRequest] = []

    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        self.requests.append(request)
        if len(self.requests) == 1:
            payload = {
                "id": "chat-resp-tool-1",
                "object": "response",
                "model": "gpt-4.1",
                "status": "completed",
                "output": [
                    {
                        "id": "chat-item-call-1",
                        "type": "function_call",
                        "call_id": "chat-call-1",
                        "name": "exec",
                        "arguments": json.dumps({"command": [sys.executable, "-c", "print('chat-tool-loop-ok')"]}),
                    }
                ],
                "usage": {
                    "input_tokens": 1,
                    "output_tokens": 1,
                    "total_tokens": 2,
                },
            }
            return InvokeResponse(
                invocation_id=str(uuid4()),
                request_id=request.request_id,
                node_id=request.node_id,
                status=InvocationStatus.SUCCESS,
                response=OpenAIResponsesResponse.model_validate(payload),
                output_text=None,
                timing=InvocationTiming(),
            )

        payload = _response_payload_for_text("chat-tool-loop-finished")
        payload["id"] = "chat-resp-tool-2"
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=OpenAIResponsesResponse.model_validate(payload),
            output_text="chat-tool-loop-finished",
            timing=InvocationTiming(),
        )


class _ToolLoopThenFailGateway:
    def __init__(self) -> None:
        self.requests: list[InvokeRequest] = []

    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        self.requests.append(request)
        if len(self.requests) == 1:
            payload = {
                "id": "resp-fail-1",
                "object": "response",
                "model": "gpt-4.1",
                "status": "completed",
                "output": [
                    {
                        "id": "item-call-fail",
                        "type": "function_call",
                        "call_id": "call-fail-1",
                        "name": "exec",
                        "arguments": json.dumps({"command": [sys.executable, "-c", "print('tool-fail')"]}),
                    }
                ],
                "usage": {
                    "input_tokens": 1,
                    "output_tokens": 1,
                    "total_tokens": 2,
                },
            }
            return InvokeResponse(
                invocation_id=str(uuid4()),
                request_id=request.request_id,
                node_id=request.node_id,
                status=InvocationStatus.SUCCESS,
                response=OpenAIResponsesResponse.model_validate(payload),
                output_text=None,
                timing=InvocationTiming(),
            )
        raise RuntimeError("gateway temporary failure")


class _DecomposeGateway:
    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        _ = request
        payload = _response_payload_for_text(
            json.dumps(
                {
                    "tasks": [
                        {
                            "title": "Clarify scope and constraints",
                            "description": "Pin down business outcome and success metrics.",
                            "status": "ready",
                        },
                        {
                            "title": "Prepare first executable slice",
                            "description": "Define the first direct child node to run.",
                            "status": "pending",
                        },
                    ]
                },
                ensure_ascii=False,
            )
        )
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=OpenAIResponsesResponse.model_validate(payload),
            output_text=json.dumps(
                {
                    "tasks": [
                        {
                            "title": "Clarify scope and constraints",
                            "description": "Pin down business outcome and success metrics.",
                            "status": "ready",
                        },
                        {
                            "title": "Prepare first executable slice",
                            "description": "Define the first direct child node to run.",
                            "status": "pending",
                        },
                    ]
                },
                ensure_ascii=False,
            ),
            timing=InvocationTiming(),
        )


class _BadDecomposeGateway:
    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        _ = request
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=None,
            output_text="not-json-output",
            timing=InvocationTiming(),
        )


class _EmptyDecomposeGateway:
    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        _ = request
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=None,
            output_text=json.dumps({"tasks": []}, ensure_ascii=False),
            timing=InvocationTiming(),
        )


class _CompactGateway:
    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        _ = request
        output = json.dumps(
            {
                "summary": {
                    "key_findings": "implementation completed",
                    "decisions": "use strict typing",
                },
                "memory_proposal": {
                    "propose_update": True,
                    "entries": [{"key": "team_norm", "value": "confirm before writing topic_memory"}],
                    "evidence": ["explicitly repeated by user"],
                    "confidence": 0.92,
                    "reason": "stable cross-turn consensus",
                },
            },
            ensure_ascii=False,
        )
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=None,
            output_text=output,
            timing=InvocationTiming(),
        )


class _BadCompactGateway:
    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        _ = request
        return InvokeResponse(
            invocation_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            status=InvocationStatus.SUCCESS,
            response=None,
            output_text="not-json",
            timing=InvocationTiming(),
        )


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


def _response_payload_for_text(text: str) -> dict[str, object]:
    return {
        "id": "resp-service",
        "object": "response",
        "model": "gpt-4.1",
        "status": "completed",
        "output": [
            {
                "type": "message",
                "role": "assistant",
                "status": "completed",
                "content": [
                    {
                        "type": "output_text",
                        "text": text,
                    }
                ],
            }
        ],
        "usage": {
            "input_tokens": 2,
            "input_tokens_details": {
                "cached_tokens": 0,
            },
            "output_tokens": 3,
            "output_tokens_details": {
                "reasoning_tokens": 0,
            },
            "total_tokens": 5,
        },
    }


def _extract_input_text(value: object) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        parts: list[str] = []
        for item in value:
            if not isinstance(item, dict):
                continue
            role = item.get("role")
            if isinstance(role, str) and role in {"user", "assistant", "system"}:
                content = item.get("content")
                if isinstance(content, str):
                    parts.append(content)
                    continue
                if isinstance(content, list):
                    for content_item in content:
                        if isinstance(content_item, dict) and content_item.get("type") in {"input_text", "text"}:
                            parts.append(str(content_item.get("text", "")))
                continue
            if item.get("type") == "message":
                content = item.get("content", [])
                if isinstance(content, list):
                    for content_item in content:
                        if isinstance(content_item, dict) and content_item.get("type") in {"input_text", "text"}:
                            parts.append(str(content_item.get("text", "")))
        return "".join(parts)
    return ""


if __name__ == "__main__":
    unittest.main()
