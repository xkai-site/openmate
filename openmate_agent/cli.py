from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Sequence

from .service import AgentCapabilityService
from .worker import WorkerExecuteRequest, execute_worker_request


def create_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="openmate-agent", description="OpenMate Agent tool CLI")
    subparsers = parser.add_subparsers(dest="command", required=True)

    tool_parser = subparsers.add_parser("tool", help="Execute tool runtime action.")
    tool_subparsers = tool_parser.add_subparsers(dest="tool_name", required=True)

    tool_read = tool_subparsers.add_parser("read", help="Read a file.")
    tool_read.add_argument("node_id", help="Node identifier.")
    tool_read.add_argument("--path", required=True, help="Target file path.")
    tool_read.add_argument("--offset", type=int, default=0, help="Line offset.")
    tool_read.add_argument("--limit", type=int, default=200, help="Line limit.")
    tool_read.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_read.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_write = tool_subparsers.add_parser("write", help="Write a file.")
    tool_write.add_argument("node_id", help="Node identifier.")
    tool_write.add_argument("--path", required=True, help="Target file path.")
    tool_write.add_argument("--content", required=True, help="Content to write.")
    tool_write.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_write.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_edit = tool_subparsers.add_parser("edit", help="Replace old string with new string in file.")
    tool_edit.add_argument("node_id", help="Node identifier.")
    tool_edit.add_argument("--path", required=True, help="Target file path.")
    tool_edit.add_argument("--old-string", required=True, help="Original text block.")
    tool_edit.add_argument("--new-string", required=True, help="Replacement text block.")
    tool_edit.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_edit.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_patch = tool_subparsers.add_parser("patch", help="Apply structured multi-file patch operations.")
    tool_patch.add_argument("node_id", help="Node identifier.")
    tool_patch.add_argument("--operations", required=True, help="JSON array of patch operations.")
    tool_patch.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_patch.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_query = tool_subparsers.add_parser("query", help="Query remote HTTP endpoint.")
    tool_query.add_argument("node_id", help="Node identifier.")
    tool_query.add_argument("--url", required=True, help="Request URL.")
    tool_query.add_argument("--method", default="GET", choices=["GET", "POST"], help="HTTP method.")
    tool_query.add_argument("--params", default="{}", help="JSON object for query params.")
    tool_query.add_argument("--headers", default="{}", help="JSON object for HTTP headers.")
    tool_query.add_argument("--body", default="{}", help="JSON object for POST body.")
    tool_query.add_argument("--timeout-seconds", type=int, default=10, help="HTTP timeout in seconds.")
    tool_query.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_query.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_grep = tool_subparsers.add_parser("grep", help="Search content by regex.")
    tool_grep.add_argument("node_id", help="Node identifier.")
    tool_grep.add_argument("--pattern", required=True, help="Regex pattern.")
    tool_grep.add_argument("--scope", default=".", help="Search scope path.")
    tool_grep.add_argument("--file-glob", default=None, help="Optional file glob filter, e.g. *.py")
    tool_grep.add_argument("--max-results", type=int, default=100, help="Max result count.")
    tool_grep.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_grep.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_glob = tool_subparsers.add_parser("glob", help="Search files by glob pattern.")
    tool_glob.add_argument("node_id", help="Node identifier.")
    tool_glob.add_argument("--pattern", required=True, help="Glob pattern, e.g. **/*.ts")
    tool_glob.add_argument("--scope", default=".", help="Search scope path.")
    tool_glob.add_argument("--max-results", type=int, default=1000, help="Max result count.")
    tool_glob.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_glob.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_exec = tool_subparsers.add_parser("exec", help="Run structured command without shell string interpolation.")
    tool_exec.add_argument("node_id", help="Node identifier.")
    tool_exec.add_argument(
        "--command",
        dest="exec_command",
        required=True,
        help='JSON array command, e.g. ["python","-m","pytest"].',
    )
    tool_exec.add_argument("--cwd", default=None, help="Relative working directory under workspace.")
    tool_exec.add_argument("--timeout-seconds", type=int, default=30, help="Command timeout in seconds.")
    tool_exec.add_argument("--expect-json", action="store_true", default=False, help="Parse stdout as JSON.")
    tool_exec.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_exec.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_shell = tool_subparsers.add_parser("shell", help="Run shell command.")
    tool_shell.add_argument("node_id", help="Node identifier.")
    tool_shell.add_argument("--cmd", "--command", dest="shell_cmd", required=True, help="Shell command string.")
    tool_shell.add_argument("--cwd", default=None, help="Relative working directory under workspace.")
    tool_shell.add_argument("--timeout-seconds", type=int, default=30, help="Shell timeout in seconds.")
    tool_shell.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_shell.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    worker_parser = subparsers.add_parser("worker", help="Execute schedule worker action.")
    worker_subparsers = worker_parser.add_subparsers(dest="worker_name", required=True)

    worker_run = worker_subparsers.add_parser("run", help="Run one worker request (JSON in/out).")
    worker_run.add_argument("--request-json", default="", help="Inline WorkerExecuteRequest JSON.")
    worker_run.add_argument("--request-file", default="", help="Path to WorkerExecuteRequest JSON file.")

    return parser


def main(argv: Sequence[str] | None = None) -> int:
    parser = create_parser()
    args = parser.parse_args(argv)
    service = AgentCapabilityService()

    if args.command == "tool":
        payload: dict[str, object] = {}
        if args.tool_name == "read":
            payload["path"] = args.path
            payload["offset"] = args.offset
            payload["limit"] = args.limit
        elif args.tool_name == "write":
            payload["path"] = args.path
            payload["content"] = args.content
        elif args.tool_name == "edit":
            payload["path"] = args.path
            payload["old_string"] = args.old_string
            payload["new_string"] = args.new_string
        elif args.tool_name == "patch":
            try:
                payload["operations"] = json.loads(args.operations)
            except json.JSONDecodeError as exc:
                print(json.dumps({"success": False, "error": f"invalid json argument: {exc}"}))
                return 1
        elif args.tool_name == "query":
            payload["url"] = args.url
            payload["method"] = args.method
            payload["timeout_seconds"] = args.timeout_seconds
            try:
                payload["params"] = json.loads(args.params)
                payload["headers"] = json.loads(args.headers)
                payload["body"] = json.loads(args.body)
            except json.JSONDecodeError as exc:
                print(json.dumps({"success": False, "error": f"invalid json argument: {exc}"}))
                return 1
        elif args.tool_name == "grep":
            payload["pattern"] = args.pattern
            payload["scope"] = args.scope
            payload["max_results"] = args.max_results
            payload["file_glob"] = args.file_glob
        elif args.tool_name == "glob":
            payload["pattern"] = args.pattern
            payload["scope"] = args.scope
            payload["max_results"] = args.max_results
        elif args.tool_name == "exec":
            payload["cwd"] = args.cwd
            payload["timeout_seconds"] = args.timeout_seconds
            payload["expect_json"] = args.expect_json
            try:
                payload["command"] = json.loads(args.exec_command)
            except json.JSONDecodeError as exc:
                print(json.dumps({"success": False, "error": f"invalid json argument: {exc}"}))
                return 1
        elif args.tool_name == "shell":
            payload["command"] = args.shell_cmd
            payload["cwd"] = args.cwd
            payload["timeout_seconds"] = args.timeout_seconds

        result = service.run_tool(
            node_id=args.node_id,
            tool_name=args.tool_name,
            payload=payload,
            is_safe=args.is_safe,
            is_read_only=args.is_read_only,
        )
        print(result.model_dump_json(indent=2))
        return 0 if result.success else 1

    if args.command == "worker":
        if args.worker_name != "run":
            print(json.dumps({"status": "failed", "error": f"unknown worker command: {args.worker_name}"}))
            return 2
        if bool(args.request_json) == bool(args.request_file):
            print(json.dumps({"status": "failed", "error": "worker run requires exactly one of --request-json or --request-file"}))
            return 2
        raw = args.request_json
        if args.request_file:
            try:
                raw = Path(args.request_file).read_text(encoding="utf-8")
            except OSError as exc:
                print(json.dumps({"status": "failed", "error": f"read request file failed: {exc}"}))
                return 2
        try:
            request = WorkerExecuteRequest.model_validate_json(raw)
        except Exception as exc:
            print(json.dumps({"status": "failed", "error": f"invalid worker request json: {exc}"}))
            return 2

        response = execute_worker_request(request)
        print(response.model_dump_json(indent=2))
        return 0 if response.status == "succeeded" else 1

    parser.print_help()
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
