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
        self.assertNotIn("priority", result.stdout)

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
