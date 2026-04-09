import json
import shutil
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from tempfile import TemporaryDirectory

from openmate_agent.service import AgentCapabilityService


class AgentCapabilityServiceTests(unittest.TestCase):
    def setUp(self) -> None:
        self.service = AgentCapabilityService()

    def test_build_returns_build_model(self) -> None:
        build = self.service.build("node-1")
        self.assertEqual(build.node_id, "node-1")

    def test_execute_returns_raw_content(self) -> None:
        build = self.service.build("node-2")
        result = self.service.execute(build)
        self.assertIn("executed node=node-2", result)

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


if __name__ == "__main__":
    unittest.main()
