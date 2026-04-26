from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from tempfile import TemporaryDirectory

from openmate_pool.cli import main


class PoolCliTestCase(unittest.TestCase):
    @staticmethod
    def _write_model_config(path: Path, *, base_url: str, threshold: int = 3) -> None:
        path.write_text(
            json.dumps(
                {
                    "global_max_concurrent": 2,
                    "offline_failure_threshold": threshold,
                    "apis": [
                        {
                            "api_id": "api-1",
                            "model": "gpt-4.1",
                            "base_url": base_url,
                            "api_key": "sk-test",
                            "max_concurrent": 1,
                            "enabled": True,
                        }
                    ],
                }
            ),
            encoding="utf-8",
        )

    @staticmethod
    def _request_json(node_id: str, *, content: str = "hello") -> str:
        return json.dumps(
            {
                "request_id": f"req-{node_id}",
                "node_id": node_id,
                "request": {
                    "input": content,
                },
            }
        )

    def test_help_available(self) -> None:
        self.assertEqual(main(["--help"]), 0)

    def test_invoke_records_and_cap_flow(self) -> None:
        server, thread = _start_gateway_server()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join, 1)
        self.addCleanup(server.shutdown)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(
                model_config,
                base_url=f"http://127.0.0.1:{server.server_port}/v1",
            )
            base = [
                "--db-file",
                str(db_file),
                "--model-config",
                str(model_config),
            ]

            self.assertEqual(
                main(base + ["invoke", "--request-json", self._request_json("node-1", content="hello-cli")]),
                0,
            )
            self.assertEqual(main(base + ["records"]), 0)
            self.assertEqual(main(base + ["usage"]), 0)
            self.assertEqual(main(base + ["cap"]), 0)
            self.assertEqual(main(base + ["sync"]), 0)

    def test_missing_model_config_returns_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            missing_config = Path(tmpdir) / "missing.json"
            self.assertEqual(
                main(
                    [
                        "--db-file",
                        str(db_file),
                        "--model-config",
                        str(missing_config),
                        "cap",
                    ]
                ),
                2,
            )

    def test_invoke_failure_returns_error_code(self) -> None:
        server, thread = _start_gateway_server(fail=True)
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join, 1)
        self.addCleanup(server.shutdown)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(
                model_config,
                base_url=f"http://127.0.0.1:{server.server_port}/v1",
                threshold=1,
            )
            base = [
                "--db-file",
                str(db_file),
                "--model-config",
                str(model_config),
            ]

            self.assertEqual(
                main(base + ["invoke", "--request-json", self._request_json("node-fail")]),
                2,
            )
            self.assertEqual(
                main(base + ["invoke", "--request-json", self._request_json("node-fail-2")]),
                2,
            )

    def test_old_alias_commands_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(model_config, base_url="http://127.0.0.1:1/v1")
            base = [
                "--db-file",
                str(db_file),
                "--model-config",
                str(model_config),
            ]
            self.assertEqual(main(base + ["get"]), 2)


class AgentCliTests(unittest.TestCase):
    def _run(self, *args: str, cwd: str | None = None) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        repo_root = str(Path(__file__).resolve().parents[1])
        env["PYTHONPATH"] = (
            repo_root if not env.get("PYTHONPATH") else f"{repo_root}{os.pathsep}{env['PYTHONPATH']}"
        )
        return subprocess.run(
            [sys.executable, "-m", "openmate_agent.cli", *args],
            capture_output=True,
            text=True,
            check=False,
            cwd=cwd,
            env=env,
        )

    def test_help(self) -> None:
        result = self._run("--help")
        self.assertEqual(result.returncode, 0)
        self.assertIn("OpenMate Agent tool CLI", result.stdout)
        self.assertNotIn("execute", result.stdout)
        self.assertIn("decompose", result.stdout)
        self.assertIn("priority", result.stdout)
        tool_help = self._run("tool", "--help")
        self.assertEqual(tool_help.returncode, 0)
        self.assertIn("exec", tool_help.stdout)
        self.assertIn("patch", tool_help.stdout)
        self.assertIn("node_process", tool_help.stdout)
        self.assertIn("sibling_progress_board", tool_help.stdout)
        tools_help = self._run("tools", "--help")
        self.assertEqual(tools_help.returncode, 0)
        self.assertIn("register", tools_help.stdout)
        self.assertIn("validate", tools_help.stdout)

    def test_tool_node_process_help(self) -> None:
        result = self._run("tool", "node_process", "--help")
        self.assertEqual(result.returncode, 0)
        self.assertIn("--action", result.stdout)
        self.assertIn("--processes", result.stdout)

    def test_tool_sibling_progress_board_help(self) -> None:
        result = self._run("tool", "sibling_progress_board", "--help")
        self.assertEqual(result.returncode, 0)
        self.assertIn("Node identifier", result.stdout)

    def test_tool_node_process_replace_requires_processes(self) -> None:
        result = self._run(
            "tool",
            "node_process",
            "node-50",
            "--action",
            "replace",
            "--is-safe",
            "--is-read-only",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("--processes is required", result.stdout)

    def test_tool_write_and_read(self) -> None:
        with TemporaryDirectory() as tmp:
            write_result = self._run(
                "tool",
                "write",
                "node-10",
                "--path",
                "sandbox/demo.txt",
                "--content",
                "hello-cli-tool",
                "--is-safe",
                "--is-read-only",
                cwd=tmp,
            )
            self.assertEqual(write_result.returncode, 0)
            self.assertIn('"success": true', write_result.stdout)

            read_result = self._run(
                "tool",
                "read",
                "node-10",
                "--path",
                "sandbox/demo.txt",
                "--is-safe",
                "--is-read-only",
                cwd=tmp,
            )
            self.assertEqual(read_result.returncode, 0)
            self.assertIn("hello-cli-tool", read_result.stdout)

            edit_result = self._run(
                "tool",
                "edit",
                "node-10",
                "--path",
                "sandbox/demo.txt",
                "--old-string",
                "hello-cli-tool",
                "--new-string",
                "hello-cli-edited",
                "--is-safe",
                "--is-read-only",
                cwd=tmp,
            )
            self.assertEqual(edit_result.returncode, 0)
            self.assertIn('"success": true', edit_result.stdout)

    def test_tool_query_http(self) -> None:
        server, thread = _start_echo_server()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join, 1)
        self.addCleanup(server.shutdown)

        url = f"http://127.0.0.1:{server.server_port}/echo"
        result = self._run(
            "tool",
            "query",
            "node-20",
            "--url",
            url,
            "--method",
            "GET",
            "--params",
            '{"q":"cli"}',
            "--is-safe",
            "--is-read-only",
        )
        self.assertEqual(result.returncode, 0)
        self.assertIn('"success": true', result.stdout)
        self.assertIn('\\"q\\": \\"cli\\"', result.stdout)

    def test_tool_shell(self) -> None:
        result = self._run(
            "tool",
            "shell",
            "node-30",
            "--command",
            "Write-Output cli-shell-ok",
            "--is-safe",
            "--is-read-only",
        )
        self.assertEqual(result.returncode, 0)
        self.assertIn("cli-shell-ok", result.stdout)

    def test_tool_exec(self) -> None:
        result = self._run(
            "tool",
            "exec",
            "node-32",
            "--command",
            json.dumps([sys.executable, "-c", "import json; print(json.dumps({'cli': True}))"]),
            "--expect-json",
            "--is-safe",
            "--is-read-only",
        )
        self.assertEqual(result.returncode, 0)
        self.assertIn('"success": true', result.stdout)
        self.assertIn('\\"stdout_json\\"', result.stdout)
        self.assertIn('\\"cli\\": true', result.stdout.lower())

    def test_tool_exec_invalid_command_json(self) -> None:
        result = self._run(
            "tool",
            "exec",
            "node-33",
            "--command",
            "[not-json",
            "--is-safe",
            "--is-read-only",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid json argument", result.stdout.lower())

    def test_tool_patch(self) -> None:
        with TemporaryDirectory() as tmp:
            base = Path(tmp)
            (base / "a.txt").write_text("alpha\nbeta\n", encoding="utf-8")
            (base / "b.txt").write_text("one\n", encoding="utf-8")

            self.assertEqual(
                self._run(
                    "tool",
                    "read",
                    "node-34",
                    "--path",
                    "a.txt",
                    "--is-safe",
                    "--is-read-only",
                    cwd=tmp,
                ).returncode,
                0,
            )
            self.assertEqual(
                self._run(
                    "tool",
                    "read",
                    "node-34",
                    "--path",
                    "b.txt",
                    "--is-safe",
                    "--is-read-only",
                    cwd=tmp,
                ).returncode,
                0,
            )

            result = self._run(
                "tool",
                "patch",
                "node-34",
                "--operations",
                json.dumps(
                    [
                        {"type": "replace", "path": "a.txt", "old_string": "beta", "new_string": "gamma"},
                        {"type": "replace", "path": "b.txt", "old_string": "one", "new_string": "two"},
                    ]
                ),
                "--is-safe",
                "--is-read-only",
                cwd=tmp,
            )
            self.assertEqual(result.returncode, 0)
            self.assertIn('"success": true', result.stdout)
            self.assertEqual((base / "a.txt").read_text(encoding="utf-8"), "alpha\ngamma\n")
            self.assertEqual((base / "b.txt").read_text(encoding="utf-8"), "two\n")

    def test_tool_patch_invalid_operations_json(self) -> None:
        result = self._run(
            "tool",
            "patch",
            "node-35",
            "--operations",
            "[not-json",
            "--is-safe",
            "--is-read-only",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("invalid json argument", result.stdout.lower())

    def test_worker_run_priority_mode(self) -> None:
        request = {
            "request_id": "req-priority-1",
            "topic_id": "topic-1",
            "node_id": "priority-node-1",
            "node_name": "__priority__",
            "node_kind": "priority",
            "agent_spec": {"mode": "priority"},
            "session_id": "session-1",
            "event_id": "event-1",
            "timeout_ms": 120000,
            "priority_candidates": [
                {
                    "node_id": "node-a",
                    "name": "Node A",
                    "status": "ready",
                    "current_priority": {"label": "normal", "rank": 3},
                    "entered_priority_at": "2026-04-14T09:00:00Z",
                },
                {
                    "node_id": "node-b",
                    "name": "Node B",
                    "status": "blocked",
                    "current_priority": {"label": "normal", "rank": 1},
                    "entered_priority_at": "2026-04-14T08:00:00Z",
                },
            ],
        }
        result = self._run("worker", "run", "--request-json", json.dumps(request))
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn('"status": "succeeded"', result.stdout)
        self.assertIn('"priority_plan"', result.stdout)

    def test_decompose_run(self) -> None:
        server, thread = _start_decompose_gateway_server()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join, 1)
        self.addCleanup(server.shutdown)

        request = {
            "request_id": "req-decompose-1",
            "topic_id": "topic-1",
            "node_id": "node-1",
            "node_name": "Build project foundation",
            "mode": "decompose",
            "hint": "focus on backend delivery",
            "max_items": 3,
        }
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
            result = self._run("decompose", "run", "--request-json", json.dumps(request), cwd=tmp)
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn('"status": "succeeded"', result.stdout)
        self.assertIn('"tasks"', result.stdout)

    def test_priority_run(self) -> None:
        request = {
            "request_id": "req-priority-run-1",
            "topic_id": "topic-1",
            "node_id": "priority-node-1",
            "node_name": "__priority__",
            "mode": "priority",
            "candidates": [
                {
                    "node_id": "node-a",
                    "name": "Node A",
                    "status": "ready",
                    "current_priority": {"label": "normal", "rank": 2},
                    "entered_priority_at": "2026-04-21T09:00:00Z",
                },
                {
                    "node_id": "node-b",
                    "name": "Node B",
                    "status": "pending",
                    "current_priority": {"label": "normal", "rank": 1},
                    "entered_priority_at": "2026-04-21T10:00:00Z",
                },
            ],
        }
        result = self._run("priority", "run", "--request-json", json.dumps(request))
        self.assertEqual(result.returncode, 0, msg=result.stdout + result.stderr)
        self.assertIn('"status": "succeeded"', result.stdout)
        self.assertIn('"priority_plan"', result.stdout)

    def test_tool_read_without_flags_should_require_confirm(self) -> None:
        result = self._run(
            "tool",
            "read",
            "node-31",
            "--path",
            "AGENTS.md",
        )
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("confirm", result.stdout.lower())

    def test_tool_grep_and_glob(self) -> None:
        if shutil.which("rg") is None:
            self.skipTest("ripgrep not installed")
        with TemporaryDirectory() as tmp:
            base = Path(tmp)
            (base / ".gitignore").write_text("ignored.py\n", encoding="utf-8")
            (base / "src").mkdir(parents=True, exist_ok=True)
            (base / "src" / "b.py").write_text("def grep_me():\n    return 2\n", encoding="utf-8")
            (base / "ignored.py").write_text("def should_ignore():\n    pass\n", encoding="utf-8")

            grep_result = self._run(
                "tool",
                "grep",
                "node-40",
                "--pattern",
                "grep_.*",
                "--scope",
                ".",
                "--is-safe",
                "--is-read-only",
                cwd=tmp,
            )
            self.assertEqual(grep_result.returncode, 0)
            self.assertIn("b.py", grep_result.stdout)

            glob_result = self._run(
                "tool",
                "glob",
                "node-41",
                "--pattern",
                "**/*.py",
                "--scope",
                ".",
                "--is-safe",
                "--is-read-only",
                cwd=tmp,
            )
            self.assertEqual(glob_result.returncode, 0)
            self.assertIn("src", glob_result.stdout)
            self.assertNotIn("ignored.py", glob_result.stdout)

    def test_tools_registry_lifecycle(self) -> None:
        with TemporaryDirectory() as tmp:
            validate_result = self._run("tools", "validate", cwd=tmp)
            self.assertEqual(validate_result.returncode, 0)
            self.assertIn('"success": true', validate_result.stdout.lower())

            register_result = self._run(
                "tools",
                "register",
                "--name",
                "custom_exec_tool",
                "--description",
                "custom exec",
                "--primary-tag",
                "command_ext",
                "--backend",
                "builtin/exec",
                "--enabled",
                cwd=tmp,
            )
            self.assertEqual(register_result.returncode, 0, msg=register_result.stdout + register_result.stderr)

            list_result = self._run("tools", "list", "--tag", "command_ext", cwd=tmp)
            self.assertEqual(list_result.returncode, 0)
            self.assertIn("custom_exec_tool", list_result.stdout)

            disable_result = self._run("tools", "disable", "--name", "custom_exec_tool", cwd=tmp)
            self.assertEqual(disable_result.returncode, 0)

            enable_result = self._run("tools", "enable", "--name", "custom_exec_tool", cwd=tmp)
            self.assertEqual(enable_result.returncode, 0)

            update_result = self._run(
                "tools",
                "update",
                "--name",
                "custom_exec_tool",
                "--description",
                "custom exec updated",
                cwd=tmp,
            )
            self.assertEqual(update_result.returncode, 0)


class _EchoHandler(BaseHTTPRequestHandler):
    def do_GET(self) -> None:  # noqa: N802
        from urllib.parse import parse_qs, urlparse

        parsed = urlparse(self.path)
        payload = {"path": parsed.path, "query": {k: v[0] for k, v in parse_qs(parsed.query).items()}}
        body = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


class _GatewayHandler(BaseHTTPRequestHandler):
    fail = False

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/responses":
            self.send_error(404)
            return
        body = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode("utf-8")
        payload = json.loads(body)
        if self.fail:
            response = json.dumps({"error": {"message": "gateway failed"}}).encode("utf-8")
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response)))
            self.end_headers()
            self.wfile.write(response)
            return

        text = _extract_input_text(payload.get("input"))
        response = json.dumps(_response_payload_for_text(f"echo:{text}")).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


class _DecomposeGatewayHandler(BaseHTTPRequestHandler):
    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/responses":
            self.send_error(404)
            return
        _ = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode("utf-8")
        tasks_payload = json.dumps(
            {
                "tasks": [
                    {
                        "title": "Align business scope",
                        "description": "Clarify user value and completion signal.",
                        "status": "ready",
                    },
                    {
                        "title": "Define first delivery slice",
                        "description": "Pick the first directly executable child task.",
                        "status": "pending",
                    },
                    {
                        "title": "Set acceptance criteria",
                        "description": "Document measurable checks for this node.",
                        "status": "pending",
                    },
                ]
            },
            ensure_ascii=False,
        )
        response = json.dumps(_response_payload_for_text(tasks_payload)).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


def _start_echo_server() -> tuple[ThreadingHTTPServer, threading.Thread]:
    server = ThreadingHTTPServer(("127.0.0.1", 0), _EchoHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


def _start_gateway_server(*, fail: bool = False) -> tuple[ThreadingHTTPServer, threading.Thread]:
    handler = type(
        "ConfiguredGatewayHandler",
        (_GatewayHandler,),
        {"fail": fail},
    )
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


def _start_decompose_gateway_server() -> tuple[ThreadingHTTPServer, threading.Thread]:
    server = ThreadingHTTPServer(("127.0.0.1", 0), _DecomposeGatewayHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


def _response_payload_for_text(text: str) -> dict[str, object]:
    return {
        "id": "resp-cli",
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
