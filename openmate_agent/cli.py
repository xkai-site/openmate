from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Sequence

from openmate_shared.runtime_paths import resolve_workspace_root

from .models import CompactRequest, DecomposeRequest, PriorityRequest
from .service import AgentCapabilityService
from .tooling import ToolRegistration, load_tool_registry, save_registry_file, validate_registry_file
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

    tool_search = tool_subparsers.add_parser("search", help="Unified search by content or file pattern.")
    tool_search.add_argument("node_id", help="Node identifier.")
    tool_search.add_argument("--mode", default="content", choices=["content", "file"], help="Search mode.")
    tool_search.add_argument("--pattern", required=True, help="Pattern for search.")
    tool_search.add_argument("--scope", default=".", help="Search scope path.")
    tool_search.add_argument("--max-results", type=int, default=100, help="Max result count.")
    tool_search.add_argument("--file-glob", default=None, help="Optional file glob filter when mode=content.")
    tool_search.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_search.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_command = tool_subparsers.add_parser("command", help="Run command with exec-first strategy.")
    tool_command.add_argument("node_id", help="Node identifier.")
    tool_command.add_argument("--command", dest="command_json", default="", help='JSON array, e.g. ["python","-V"].')
    tool_command.add_argument("--shell-command", default="", help="Shell command string for shell features.")
    tool_command.add_argument("--cwd", default=None, help="Relative working directory under workspace.")
    tool_command.add_argument("--timeout-seconds", type=int, default=30, help="Command timeout in seconds.")
    tool_command.add_argument("--expect-json", action="store_true", default=False, help="Parse stdout as JSON.")
    tool_command.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_command.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_network = tool_subparsers.add_parser("network", help="Run HTTP network request.")
    tool_network.add_argument("node_id", help="Node identifier.")
    tool_network.add_argument("--url", required=True, help="Request URL.")
    tool_network.add_argument("--method", default="GET", choices=["GET", "POST"], help="HTTP method.")
    tool_network.add_argument("--params", default="{}", help="JSON object for query params.")
    tool_network.add_argument("--headers", default="{}", help="JSON object for HTTP headers.")
    tool_network.add_argument("--body", default="{}", help="JSON object for POST body.")
    tool_network.add_argument("--timeout-seconds", type=int, default=10, help="HTTP timeout in seconds.")
    tool_network.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_network.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tool_query_registry = tool_subparsers.add_parser("tool_query", help="Discover non-default tools.")
    tool_query_registry.add_argument("node_id", help="Node identifier.")
    tool_query_registry.add_argument("--by-tag", default=None, help="Filter non-default tools by primary tag.")
    tool_query_registry.add_argument("--keyword", default=None, help="Keyword filter on name/description.")
    tool_query_registry.add_argument("--is-safe", action="store_true", default=False, help="Mark this tool call as safe.")
    tool_query_registry.add_argument(
        "--is-read-only",
        action="store_true",
        default=False,
        help="Mark this tool call as read-only.",
    )

    tools_parser = subparsers.add_parser("tools", help="Manage tool registry.")
    tools_subparsers = tools_parser.add_subparsers(dest="tools_name", required=True)

    tools_list = tools_subparsers.add_parser("list", help="List tools from registry.")
    tools_list.add_argument("--default-only", action="store_true", default=False, help="Show only default tools.")
    tools_list.add_argument("--non-default-only", action="store_true", default=False, help="Show only non-default tools.")
    tools_list.add_argument("--tag", default=None, help="Filter by tag.")

    tools_register = tools_subparsers.add_parser("register", help="Register tool into JSON registry.")
    tools_register.add_argument("--name", required=True, help="Tool name.")
    tools_register.add_argument("--description", required=True, help="Tool description.")
    tools_register.add_argument("--primary-tag", required=True, help="Primary tag.")
    tools_register.add_argument("--secondary-tags", default="", help="Comma-separated secondary tags.")
    tools_register.add_argument("--backend", required=True, help="Backend mapping, e.g. builtin/exec.")
    tools_register.add_argument("--enabled", action="store_true", default=False, help="Enable this tool.")
    tools_register.add_argument("--disabled", action="store_true", default=False, help="Disable this tool.")

    tools_update = tools_subparsers.add_parser("update", help="Update registered JSON tool fields.")
    tools_update.add_argument("--name", required=True, help="Tool name.")
    tools_update.add_argument("--description", default=None, help="New description.")
    tools_update.add_argument("--primary-tag", default=None, help="New primary tag.")
    tools_update.add_argument("--secondary-tags", default=None, help="Comma-separated secondary tags.")
    tools_update.add_argument("--backend", default=None, help="New backend.")
    tools_update.add_argument("--enabled", action="store_true", default=False, help="Set enabled=true.")
    tools_update.add_argument("--disabled", action="store_true", default=False, help="Set enabled=false.")

    tools_enable = tools_subparsers.add_parser("enable", help="Enable a JSON-registered tool.")
    tools_enable.add_argument("--name", required=True, help="Tool name.")

    tools_disable = tools_subparsers.add_parser("disable", help="Disable a JSON-registered tool.")
    tools_disable.add_argument("--name", required=True, help="Tool name.")

    tools_subparsers.add_parser("validate", help="Validate tool registry configuration.")

    worker_parser = subparsers.add_parser("worker", help="Execute schedule worker action.")
    worker_subparsers = worker_parser.add_subparsers(dest="worker_name", required=True)

    worker_run = worker_subparsers.add_parser("run", help="Run one worker request (JSON in/out).")
    worker_run.add_argument("--request-json", default="", help="Inline WorkerExecuteRequest JSON.")
    worker_run.add_argument("--request-file", default="", help="Path to WorkerExecuteRequest JSON file.")

    decompose_parser = subparsers.add_parser("decompose", help="Execute decompose agent action.")
    decompose_subparsers = decompose_parser.add_subparsers(dest="decompose_name", required=True)
    decompose_run = decompose_subparsers.add_parser("run", help="Run one decompose request (JSON in/out).")
    decompose_run.add_argument("--request-json", default="", help="Inline DecomposeRequest JSON.")
    decompose_run.add_argument("--request-file", default="", help="Path to DecomposeRequest JSON file.")

    priority_parser = subparsers.add_parser("priority", help="Execute priority agent action.")
    priority_subparsers = priority_parser.add_subparsers(dest="priority_name", required=True)
    priority_run = priority_subparsers.add_parser("run", help="Run one priority request (JSON in/out).")
    priority_run.add_argument("--request-json", default="", help="Inline PriorityRequest JSON.")
    priority_run.add_argument("--request-file", default="", help="Path to PriorityRequest JSON file.")

    compact_parser = subparsers.add_parser("compact", help="Execute compact agent action.")
    compact_subparsers = compact_parser.add_subparsers(dest="compact_name", required=True)
    compact_run = compact_subparsers.add_parser("run", help="Run one compact request (JSON in/out).")
    compact_run.add_argument("--request-json", default="", help="Inline CompactRequest JSON.")
    compact_run.add_argument("--request-file", default="", help="Path to CompactRequest JSON file.")

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
        elif args.tool_name == "search":
            payload["mode"] = args.mode
            payload["pattern"] = args.pattern
            payload["scope"] = args.scope
            payload["max_results"] = args.max_results
            payload["file_glob"] = args.file_glob
        elif args.tool_name == "command":
            payload["cwd"] = args.cwd
            payload["timeout_seconds"] = args.timeout_seconds
            payload["expect_json"] = args.expect_json
            if args.command_json:
                try:
                    payload["command"] = json.loads(args.command_json)
                except json.JSONDecodeError as exc:
                    print(json.dumps({"success": False, "error": f"invalid json argument: {exc}"}))
                    return 1
            if args.shell_command:
                payload["shell_command"] = args.shell_command
        elif args.tool_name == "network":
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
        elif args.tool_name == "tool_query":
            payload["by_tag"] = args.by_tag
            payload["keyword"] = args.keyword

        result = service.run_tool(
            node_id=args.node_id,
            tool_name=args.tool_name,
            payload=payload,
            is_safe=args.is_safe,
            is_read_only=args.is_read_only,
        )
        print(result.model_dump_json(indent=2))
        return 0 if result.success else 1

    if args.command == "tools":
        workspace_root = resolve_workspace_root(Path.cwd())
        try:
            registry = load_tool_registry(workspace_root=workspace_root)
        except Exception as exc:
            print(json.dumps({"success": False, "error": f"load tool registry failed: {exc}"}))
            return 2

        if args.tools_name == "list":
            if args.default_only and args.non_default_only:
                print(json.dumps({"success": False, "error": "--default-only and --non-default-only are mutually exclusive"}))
                return 2
            default_only: bool | None = None
            if args.default_only:
                default_only = True
            elif args.non_default_only:
                default_only = False
            specs = registry.list_specs(enabled_only=False, default_only=default_only, tag=args.tag)
            data = [
                {
                    "name": spec.name,
                    "description": spec.description,
                    "enabled": spec.enabled,
                    "is_default": spec.is_default,
                    "primary_tag": spec.primary_tag,
                    "secondary_tags": spec.secondary_tags,
                    "backend": spec.backend,
                    "source": spec.source,
                }
                for spec in specs
            ]
            print(json.dumps({"success": True, "tools": data}, ensure_ascii=False, indent=2))
            return 0

        if args.tools_name == "register":
            enabled = True
            if args.enabled and args.disabled:
                print(json.dumps({"success": False, "error": "--enabled and --disabled are mutually exclusive"}))
                return 2
            if args.disabled:
                enabled = False
            secondary_tags = [tag.strip() for tag in args.secondary_tags.split(",") if tag.strip()]
            try:
                spec = ToolRegistration(
                    name=args.name,
                    description=args.description,
                    enabled=enabled,
                    is_default=False,
                    primary_tag=args.primary_tag,
                    secondary_tags=secondary_tags,
                    backend=args.backend,
                )
                registry.register_json_tool(spec)
                path = save_registry_file(registry=registry, workspace_root=workspace_root)
            except Exception as exc:
                print(json.dumps({"success": False, "error": str(exc)}))
                return 2
            print(json.dumps({"success": True, "action": "register", "name": spec.name, "path": str(path)}, ensure_ascii=False))
            return 0

        if args.tools_name == "update":
            if args.enabled and args.disabled:
                print(json.dumps({"success": False, "error": "--enabled and --disabled are mutually exclusive"}))
                return 2
            updates: dict[str, object] = {}
            if args.description is not None:
                updates["description"] = args.description
            if args.primary_tag is not None:
                updates["primary_tag"] = args.primary_tag
            if args.secondary_tags is not None:
                updates["secondary_tags"] = [tag.strip() for tag in args.secondary_tags.split(",") if tag.strip()]
            if args.backend is not None:
                updates["backend"] = args.backend
            if args.enabled:
                updates["enabled"] = True
            if args.disabled:
                updates["enabled"] = False
            if not updates:
                print(json.dumps({"success": False, "error": "no update fields provided"}))
                return 2
            try:
                updated = registry.update_json_tool(args.name, updates)
                path = save_registry_file(registry=registry, workspace_root=workspace_root)
            except Exception as exc:
                print(json.dumps({"success": False, "error": str(exc)}))
                return 2
            print(json.dumps({"success": True, "action": "update", "name": updated.name, "path": str(path)}, ensure_ascii=False))
            return 0

        if args.tools_name in {"enable", "disable"}:
            enabled = args.tools_name == "enable"
            try:
                registry.update_json_tool(args.name, {"enabled": enabled})
                path = save_registry_file(registry=registry, workspace_root=workspace_root)
            except Exception as exc:
                print(json.dumps({"success": False, "error": str(exc)}))
                return 2
            print(
                json.dumps(
                    {"success": True, "action": args.tools_name, "name": args.name, "path": str(path)},
                    ensure_ascii=False,
                )
            )
            return 0

        if args.tools_name == "validate":
            errors = validate_registry_file(workspace_root=workspace_root)
            if errors:
                print(json.dumps({"success": False, "errors": errors}, ensure_ascii=False, indent=2))
                return 2
            print(json.dumps({"success": True, "message": "tool registry validation passed"}, ensure_ascii=False))
            return 0

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

    if args.command == "decompose":
        if args.decompose_name != "run":
            print(json.dumps({"status": "failed", "error": f"unknown decompose command: {args.decompose_name}"}))
            return 2
        if bool(args.request_json) == bool(args.request_file):
            print(
                json.dumps(
                    {"status": "failed", "error": "decompose run requires exactly one of --request-json or --request-file"}
                )
            )
            return 2
        raw = args.request_json
        if args.request_file:
            try:
                raw = Path(args.request_file).read_text(encoding="utf-8")
            except OSError as exc:
                print(json.dumps({"status": "failed", "error": f"read request file failed: {exc}"}))
                return 2
        try:
            request = DecomposeRequest.model_validate_json(raw)
        except Exception as exc:
            print(json.dumps({"status": "failed", "error": f"invalid decompose request json: {exc}"}))
            return 2
        response = service.decompose_agent(request)
        print(response.model_dump_json(indent=2))
        return 0 if response.status == "succeeded" else 1

    if args.command == "priority":
        if args.priority_name != "run":
            print(json.dumps({"status": "failed", "error": f"unknown priority command: {args.priority_name}"}))
            return 2
        if bool(args.request_json) == bool(args.request_file):
            print(
                json.dumps(
                    {"status": "failed", "error": "priority run requires exactly one of --request-json or --request-file"}
                )
            )
            return 2
        raw = args.request_json
        if args.request_file:
            try:
                raw = Path(args.request_file).read_text(encoding="utf-8")
            except OSError as exc:
                print(json.dumps({"status": "failed", "error": f"read request file failed: {exc}"}))
                return 2
        try:
            request = PriorityRequest.model_validate_json(raw)
        except Exception as exc:
            print(json.dumps({"status": "failed", "error": f"invalid priority request json: {exc}"}))
            return 2
        response = service.priority_agent(request)
        print(response.model_dump_json(indent=2))
        return 0 if response.status == "succeeded" else 1

    if args.command == "compact":
        if args.compact_name != "run":
            print(json.dumps({"status": "failed", "error": f"unknown compact command: {args.compact_name}"}))
            return 2
        if bool(args.request_json) == bool(args.request_file):
            print(
                json.dumps(
                    {"status": "failed", "error": "compact run requires exactly one of --request-json or --request-file"}
                )
            )
            return 2
        raw = args.request_json
        if args.request_file:
            try:
                raw = Path(args.request_file).read_text(encoding="utf-8")
            except OSError as exc:
                print(json.dumps({"status": "failed", "error": f"read request file failed: {exc}"}))
                return 2
        try:
            request = CompactRequest.model_validate_json(raw)
        except Exception as exc:
            print(json.dumps({"status": "failed", "error": f"invalid compact request json: {exc}"}))
            return 2
        response = service.compact_agent(request)
        print(response.model_dump_json(indent=2))
        return 0 if response.status == "succeeded" else 1

    parser.print_help()
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
