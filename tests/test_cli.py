import json
import os
import shutil
import subprocess
import sys
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from tempfile import TemporaryDirectory


class AgentCliTests(unittest.TestCase):
    def _run(self, *args: str, cwd: str | None = None) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        repo_root = str(Path(__file__).resolve().parents[1])
        env["PYTHONPATH"] = repo_root if not env.get("PYTHONPATH") else f"{repo_root};{env['PYTHONPATH']}"
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
        server, thread = _start_test_server()
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


def _start_test_server() -> tuple[ThreadingHTTPServer, threading.Thread]:
    server = ThreadingHTTPServer(("127.0.0.1", 0), _EchoHandler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


if __name__ == "__main__":
    unittest.main()
