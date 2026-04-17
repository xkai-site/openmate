import json
import shutil
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from tempfile import TemporaryDirectory
from uuid import uuid4

from openmate_pool.models import (
    InvocationStatus,
    InvocationTiming,
    InvokeRequest,
    InvokeResponse,
    OpenAIResponsesResponse,
)
from openmate_agent.service import AgentCapabilityService
from openmate_agent.session_models import AppendSessionEventInput


class AgentCapabilityServiceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.service = AgentCapabilityService(gateway=_FakeGateway())

    def test_build_returns_build_model(self) -> None:
        build = self.service.build("node-1")
        self.assertEqual(build.node_id, "node-1")

    def test_execute_returns_raw_content(self) -> None:
        build = self.service.build("node-2")
        result = self.service.execute(build)
        self.assertIn("executed node=node-2", result)

    def test_execute_without_tool_call_still_writes_session_event(self) -> None:
        session_gateway = _SpySessionGateway()
        service = AgentCapabilityService(gateway=_FakeGateway(), session_gateway=session_gateway)
        result = service.execute(service.build("node-no-tool", session_id="session-no-tool"))

        self.assertEqual(result, "executed node=node-no-tool")
        self.assertEqual(session_gateway.ensure_calls, [("node-no-tool", "session-no-tool")])
        self.assertGreaterEqual(len(session_gateway.events), 2)
        delta_events = [event for event in session_gateway.events if event.item_type == "assistant_delta"]
        self.assertGreaterEqual(len(delta_events), 1)
        event = session_gateway.events[-1]
        self.assertEqual(event.item_type, "message")
        self.assertEqual(event.role.value, "assistant")
        self.assertEqual(event.next_status.value, "completed")
        self.assertEqual(event.call_id, None)
        self.assertEqual(event.payload_json.get("output_text"), "executed node=node-no-tool")

    def test_execute_runs_responses_tool_loop_and_writes_session_events(self) -> None:
        gateway = _ToolLoopGateway()
        session_gateway = _SpySessionGateway()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                gateway=gateway,
                session_gateway=session_gateway,
                workspace_root=tmp,
            )
            result = service.execute(service.build("node-tool-loop", session_id="session-1"))

        self.assertEqual(result, "tool-loop-finished")
        self.assertEqual(session_gateway.ensure_calls, [("node-tool-loop", "session-1")])
        self.assertEqual(len(gateway.requests), 2)
        first = gateway.requests[0]
        self.assertIsNotNone(first.request.tools)
        self.assertEqual(first.request.tool_choice, "auto")
        self.assertEqual(first.request.parallel_tool_calls, False)
        second = gateway.requests[1]
        self.assertEqual(second.request.previous_response_id, "resp-tool-1")
        self.assertIsInstance(second.request.input, list)
        self.assertEqual(second.request.input[0]["type"], "function_call_output")
        self.assertEqual(second.request.input[0]["call_id"], "call-1")

        self.assertGreaterEqual(len(session_gateway.events), 4)
        self.assertEqual(session_gateway.events[0].item_type, "function_call")
        self.assertEqual(session_gateway.events[0].next_status.value, "waiting")
        self.assertEqual(session_gateway.events[1].item_type, "function_call_output")
        self.assertEqual(session_gateway.events[1].next_status.value, "active")
        self.assertIn("assistant_delta", [event.item_type for event in session_gateway.events])
        self.assertEqual(session_gateway.events[-1].item_type, "message")
        self.assertEqual(session_gateway.events[-1].next_status.value, "completed")

    def test_execute_marks_failed_status_when_gateway_raises_after_tool_call(self) -> None:
        gateway = _ToolLoopThenFailGateway()
        session_gateway = _SpySessionGateway()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                gateway=gateway,
                session_gateway=session_gateway,
                workspace_root=tmp,
            )
            with self.assertRaises(RuntimeError):
                service.execute(service.build("node-tool-loop-fail", session_id="session-2"))

        self.assertEqual(len(session_gateway.events), 3)
        self.assertEqual(session_gateway.events[-1].item_type, "function_call_output")
        self.assertEqual(session_gateway.events[-1].next_status.value, "failed")
        self.assertEqual(session_gateway.events[-1].payload_json.get("ok"), False)

    def test_execute_can_use_go_cli_gateway(self) -> None:
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
            result = service.execute(service.build("node-real"))
            self.assertIn("echo:node=node-real", result)

    def test_priority_returns_true_for_non_empty_input(self) -> None:
        result = self.service.priority(["n1", "n2"], hint="hot-topic")
        self.assertTrue(result)

    def test_priority_returns_false_for_empty_input(self) -> None:
        result = self.service.priority([])
        self.assertFalse(result)

    def test_run_tool_write_read_query(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)

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


class _SpySessionGateway:
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
            if item.get("type") != "message":
                continue
            content = item.get("content", [])
            if not isinstance(content, list):
                continue
            for content_item in content:
                if isinstance(content_item, dict) and content_item.get("type") in {"input_text", "text"}:
                    parts.append(str(content_item.get("text", "")))
        return "".join(parts)
    return ""


if __name__ == "__main__":
    unittest.main()
