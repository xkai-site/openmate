from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Iterable

from pydantic import BaseModel, Field, field_validator

from openmate_agent.models import ToolResult
from openmate_shared.runtime_paths import default_tool_registry_path

from .base import Tool, ToolContext
from .tools import (
    CommandTool,
    EditTool,
    ExecTool,
    GlobTool,
    GrepTool,
    NetworkTool,
    PatchTool,
    QueryTool,
    ReadTool,
    SearchTool,
    ShellTool,
    ToolQueryTool,
    WriteTool,
)

TOOL_QUERY_THRESHOLD = 10

DEFAULT_TOOL_SPECS: tuple[tuple[str, str, str], ...] = (
    ("read", "Read file or directory with paging and line numbers.", "builtin/read"),
    ("write", "Write full file content with conflict check and diff preview.", "builtin/write"),
    ("search", "Unified file/content search across workspace.", "builtin/search"),
    ("command", "Run commands, defaulting to structured exec with controlled shell fallback.", "builtin/command"),
    ("network", "Perform HTTP requests.", "builtin/network"),
    ("tool_query", "Discover non-default tools by list/tag drill-down.", "builtin/tool_query"),
)

ALLOWED_BACKENDS: dict[str, str] = {
    "builtin/read": "read",
    "builtin/write": "write",
    "builtin/search": "search",
    "builtin/command": "command",
    "builtin/network": "network",
    "builtin/tool_query": "tool_query",
    "builtin/edit": "edit",
    "builtin/patch": "patch",
    "builtin/grep": "grep",
    "builtin/glob": "glob",
    "builtin/exec": "exec",
    "builtin/shell": "shell",
    "builtin/query": "query",
}

_NAME_PATTERN = re.compile(r"^[a-z][a-z0-9_]{0,63}$")
_TAG_PATTERN = re.compile(r"^[a-z][a-z0-9_-]{0,63}$")


class ToolRegistration(BaseModel):
    name: str = Field(min_length=1, max_length=64)
    description: str = Field(min_length=1)
    enabled: bool = True
    is_default: bool = False
    primary_tag: str | None = None
    secondary_tags: list[str] = Field(default_factory=list)
    backend: str = Field(min_length=1)
    parameters_schema: dict[str, Any] = Field(default_factory=dict)
    source: str = "json"

    @field_validator("name")
    @classmethod
    def _validate_name(cls, value: str) -> str:
        name = value.strip()
        if not _NAME_PATTERN.match(name):
            raise ValueError("invalid tool name")
        return name

    @field_validator("primary_tag")
    @classmethod
    def _validate_primary_tag(cls, value: str | None) -> str | None:
        if value is None:
            return None
        tag = value.strip()
        if not tag:
            return None
        if not _TAG_PATTERN.match(tag):
            raise ValueError("invalid primary_tag")
        return tag

    @field_validator("secondary_tags")
    @classmethod
    def _validate_secondary_tags(cls, value: list[str]) -> list[str]:
        seen: set[str] = set()
        tags: list[str] = []
        for raw in value:
            tag = str(raw).strip()
            if not tag:
                continue
            if not _TAG_PATTERN.match(tag):
                raise ValueError("invalid secondary_tags")
            if tag not in seen:
                seen.add(tag)
                tags.append(tag)
        return tags

    def model_dump_for_file(self) -> dict[str, Any]:
        data = self.model_dump(mode="json")
        data.pop("source", None)
        return data


class ToolRegistryFile(BaseModel):
    version: int = 1
    tools: list[ToolRegistration] = Field(default_factory=list)


@dataclass(frozen=True)
class RuntimeToolEntry:
    spec: ToolRegistration
    tool: Tool


class ToolRegistry:
    def __init__(self, tools: Iterable[RuntimeToolEntry] | None = None) -> None:
        self._entries: dict[str, RuntimeToolEntry] = {}
        self._json_specs: dict[str, ToolRegistration] = {}
        if tools:
            for entry in tools:
                self.add_runtime(entry.spec, entry.tool)

    def add_runtime(self, spec: ToolRegistration, tool: Tool) -> None:
        if spec.name in self._entries:
            raise ValueError(f"duplicate tool name: {spec.name}")
        self._entries[spec.name] = RuntimeToolEntry(spec=spec, tool=tool)
        if spec.source == "json":
            self._json_specs[spec.name] = spec

    def execute(self, tool_name: str, context: ToolContext, payload: dict[str, object]) -> ToolResult:
        entry = self._entries.get(tool_name)
        if not entry:
            return ToolResult(tool_name=tool_name, success=False, error="tool not found")
        if not entry.spec.enabled:
            return ToolResult(tool_name=tool_name, success=False, error="tool is disabled")
        return entry.tool.run(context=context, payload=payload)

    def list_names(self, *, enabled_only: bool = False) -> list[str]:
        specs = self.list_specs(enabled_only=enabled_only)
        return [spec.name for spec in specs]

    def list_specs(
        self,
        *,
        enabled_only: bool = False,
        default_only: bool | None = None,
        tag: str | None = None,
    ) -> list[ToolRegistration]:
        result: list[ToolRegistration] = []
        for name in sorted(self._entries.keys()):
            spec = self._entries[name].spec
            if enabled_only and not spec.enabled:
                continue
            if default_only is True and not spec.is_default:
                continue
            if default_only is False and spec.is_default:
                continue
            if tag is not None:
                all_tags = [spec.primary_tag, *spec.secondary_tags]
                if tag not in [item for item in all_tags if item]:
                    continue
            result.append(spec)
        return result

    def get_spec(self, name: str) -> ToolRegistration | None:
        entry = self._entries.get(name)
        return None if entry is None else entry.spec

    def is_enabled(self, name: str) -> bool:
        spec = self.get_spec(name)
        return bool(spec and spec.enabled)

    def register_json_tool(self, spec: ToolRegistration) -> None:
        if spec.source != "json":
            spec = spec.model_copy(update={"source": "json"})
        if spec.name in self._entries and spec.name not in self._json_specs:
            raise ValueError(f"cannot override builtin tool: {spec.name}")
        tool = _build_tool_for_backend(spec.backend, registry=self)
        if spec.name in self._entries:
            self._entries[spec.name] = RuntimeToolEntry(spec=spec, tool=tool)
        else:
            self.add_runtime(spec=spec, tool=tool)
        self._json_specs[spec.name] = spec

    def update_json_tool(self, name: str, updates: dict[str, Any]) -> ToolRegistration:
        spec = self._json_specs.get(name)
        if spec is None:
            raise ValueError(f"json tool not found: {name}")
        merged = spec.model_copy(update=updates)
        self.register_json_tool(merged)
        return merged

    def export_json_file(self) -> ToolRegistryFile:
        tools = [self._json_specs[name] for name in sorted(self._json_specs.keys())]
        return ToolRegistryFile(version=1, tools=tools)

    def tool_query(self, *, by_tag: str | None = None, keyword: str | None = None) -> dict[str, Any]:
        candidates = self.list_specs(enabled_only=True, default_only=False)
        if keyword:
            token = keyword.lower()
            candidates = [
                spec for spec in candidates if token in spec.name.lower() or token in spec.description.lower()
            ]

        if by_tag:
            filtered = [spec for spec in candidates if spec.primary_tag == by_tag]
            return {
                "mode": "by_tag",
                "tag": by_tag,
                "remaining_count": len(filtered),
                "tools": [_tool_summary(spec) for spec in filtered],
            }

        if len(candidates) <= TOOL_QUERY_THRESHOLD:
            return {
                "mode": "tools",
                "remaining_count": len(candidates),
                "tools": [_tool_summary(spec) for spec in candidates],
            }

        grouped: dict[str, int] = {}
        for spec in candidates:
            tag = spec.primary_tag or "uncategorized"
            grouped[tag] = grouped.get(tag, 0) + 1
        tags = [{"tag": tag, "count": count} for tag, count in sorted(grouped.items(), key=lambda item: (-item[1], item[0]))]
        return {
            "mode": "tags",
            "remaining_count": len(candidates),
            "tags": tags,
            "threshold": TOOL_QUERY_THRESHOLD,
        }

    def validate_runtime(self) -> list[str]:
        errors: list[str] = []
        for spec in self.list_specs():
            if spec.backend not in ALLOWED_BACKENDS:
                errors.append(f"tool {spec.name}: invalid backend {spec.backend}")
            if not spec.is_default and not spec.primary_tag:
                errors.append(f"tool {spec.name}: non-default tool requires primary_tag")
        for name, _, backend in DEFAULT_TOOL_SPECS:
            spec = self.get_spec(name)
            if spec is None:
                errors.append(f"default tool missing: {name}")
                continue
            if not spec.is_default:
                errors.append(f"default tool {name}: is_default must be true")
            if not spec.enabled:
                errors.append(f"default tool {name}: enabled must be true")
            if spec.backend != backend:
                errors.append(f"default tool {name}: backend must be {backend}")
        return errors


def load_tool_registry(*, workspace_root: Path, registry_path: Path | None = None) -> ToolRegistry:
    path = registry_path or default_tool_registry_path(workspace_root)
    builtin_specs = _builtin_specs()
    file_specs = _read_registry_file(path)

    names: set[str] = set()
    for spec in [*builtin_specs, *file_specs]:
        if spec.name in names:
            raise ValueError(f"duplicate tool name: {spec.name}")
        names.add(spec.name)

    registry = ToolRegistry()
    for spec in builtin_specs:
        registry.add_runtime(spec=spec, tool=_build_tool_for_backend(spec.backend, registry=registry))
    for spec in file_specs:
        registry.register_json_tool(spec)

    errors = registry.validate_runtime()
    if errors:
        raise ValueError("tool registry validation failed: " + "; ".join(errors))
    return registry


def save_registry_file(*, registry: ToolRegistry, workspace_root: Path, registry_path: Path | None = None) -> Path:
    path = registry_path or default_tool_registry_path(workspace_root)
    path.parent.mkdir(parents=True, exist_ok=True)
    payload = registry.export_json_file()
    path.write_text(json.dumps(payload.model_dump(mode="json"), ensure_ascii=False, indent=2), encoding="utf-8")
    return path


def validate_registry_file(*, workspace_root: Path, registry_path: Path | None = None) -> list[str]:
    try:
        load_tool_registry(workspace_root=workspace_root, registry_path=registry_path)
    except Exception as exc:
        return [str(exc)]
    return []


def _read_registry_file(path: Path) -> list[ToolRegistration]:
    if not path.exists():
        return []
    payload = json.loads(path.read_text(encoding="utf-8"))
    data = ToolRegistryFile.model_validate(payload)
    specs: list[ToolRegistration] = []
    for spec in data.tools:
        specs.append(spec.model_copy(update={"source": "json"}))
    return specs


def _builtin_specs() -> list[ToolRegistration]:
    specs: list[ToolRegistration] = []
    for name, description, backend in DEFAULT_TOOL_SPECS:
        specs.append(
            ToolRegistration(
                name=name,
                description=description,
                enabled=True,
                is_default=True,
                primary_tag=None,
                secondary_tags=[],
                backend=backend,
                parameters_schema=_schema_for_tool(name),
                source="builtin",
            )
        )

    specs.extend(
        [
            ToolRegistration(
                name="edit",
                description="Edit file by replacing old_string with new_string.",
                enabled=True,
                is_default=False,
                primary_tag="file",
                secondary_tags=["replace"],
                backend="builtin/edit",
                parameters_schema=_schema_for_tool("edit"),
                source="builtin",
            ),
            ToolRegistration(
                name="patch",
                description="Apply structured multi-file patch operations.",
                enabled=True,
                is_default=False,
                primary_tag="file",
                secondary_tags=["batch"],
                backend="builtin/patch",
                parameters_schema=_schema_for_tool("patch"),
                source="builtin",
            ),
            ToolRegistration(
                name="grep",
                description="Search file content with regex via ripgrep.",
                enabled=True,
                is_default=False,
                primary_tag="search",
                secondary_tags=["content"],
                backend="builtin/grep",
                parameters_schema=_schema_for_tool("grep"),
                source="builtin",
            ),
            ToolRegistration(
                name="glob",
                description="Search files by glob pattern via ripgrep.",
                enabled=True,
                is_default=False,
                primary_tag="search",
                secondary_tags=["file"],
                backend="builtin/glob",
                parameters_schema=_schema_for_tool("glob"),
                source="builtin",
            ),
            ToolRegistration(
                name="exec",
                description="Run structured command in workspace without shell interpolation.",
                enabled=True,
                is_default=False,
                primary_tag="command",
                secondary_tags=["structured"],
                backend="builtin/exec",
                parameters_schema=_schema_for_tool("exec"),
                source="builtin",
            ),
            ToolRegistration(
                name="shell",
                description="Run shell command in workspace.",
                enabled=True,
                is_default=False,
                primary_tag="command",
                secondary_tags=["shell"],
                backend="builtin/shell",
                parameters_schema=_schema_for_tool("shell"),
                source="builtin",
            ),
            ToolRegistration(
                name="query",
                description="Perform HTTP query request.",
                enabled=True,
                is_default=False,
                primary_tag="network",
                secondary_tags=["http"],
                backend="builtin/query",
                parameters_schema=_schema_for_tool("query"),
                source="builtin",
            ),
        ]
    )
    return specs


def _build_tool_for_backend(backend: str, *, registry: ToolRegistry) -> Tool:
    if backend == "builtin/read":
        return ReadTool()
    if backend == "builtin/write":
        return WriteTool()
    if backend == "builtin/search":
        return SearchTool()
    if backend == "builtin/command":
        return CommandTool()
    if backend == "builtin/network":
        return NetworkTool()
    if backend == "builtin/tool_query":
        return ToolQueryTool(registry_provider=lambda: registry)
    if backend == "builtin/edit":
        return EditTool()
    if backend == "builtin/patch":
        return PatchTool()
    if backend == "builtin/grep":
        return GrepTool()
    if backend == "builtin/glob":
        return GlobTool()
    if backend == "builtin/exec":
        return ExecTool()
    if backend == "builtin/shell":
        return ShellTool()
    if backend == "builtin/query":
        return QueryTool()
    raise ValueError(f"invalid backend: {backend}")


def _tool_summary(spec: ToolRegistration) -> dict[str, Any]:
    return {
        "name": spec.name,
        "description": spec.description,
        "primary_tag": spec.primary_tag,
        "secondary_tags": spec.secondary_tags,
        "backend": spec.backend,
        "parameters": spec.parameters_schema,
    }


def _schema_for_tool(tool_name: str) -> dict[str, Any]:
    if tool_name == "read":
        return {
            "type": "object",
            "properties": {
                "path": {"type": "string"},
                "offset": {"type": "integer", "minimum": 0, "default": 0},
                "limit": {"type": "integer", "minimum": 1, "maximum": 2000, "default": 200},
            },
            "required": ["path"],
            "additionalProperties": False,
        }
    if tool_name == "write":
        return {
            "type": "object",
            "properties": {
                "path": {"type": "string"},
                "content": {"type": "string", "default": ""},
            },
            "required": ["path"],
            "additionalProperties": False,
        }
    if tool_name == "search":
        return {
            "type": "object",
            "properties": {
                "mode": {"type": "string", "enum": ["content", "file"], "default": "content"},
                "pattern": {"type": "string"},
                "scope": {"type": "string", "default": "."},
                "max_results": {"type": "integer", "minimum": 1, "maximum": 10000, "default": 100},
                "file_glob": {"type": ["string", "null"], "default": None},
            },
            "required": ["pattern"],
            "additionalProperties": False,
        }
    if tool_name == "command":
        return {
            "type": "object",
            "properties": {
                "command": {"type": ["array", "null"], "items": {"type": "string"}, "minItems": 1},
                "shell_command": {"type": ["string", "null"], "default": None},
                "cwd": {"type": ["string", "null"], "default": None},
                "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "default": 30},
                "expect_json": {"type": "boolean", "default": False},
            },
            "additionalProperties": False,
        }
    if tool_name == "network":
        return {
            "type": "object",
            "properties": {
                "url": {"type": "string"},
                "method": {"type": "string", "enum": ["GET", "POST"], "default": "GET"},
                "params": {"type": "object", "default": {}},
                "headers": {"type": "object", "default": {}},
                "body": {"type": "object", "default": {}},
                "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 120, "default": 10},
            },
            "required": ["url"],
            "additionalProperties": False,
        }
    if tool_name == "tool_query":
        return {
            "type": "object",
            "properties": {
                "by_tag": {"type": ["string", "null"], "default": None},
                "keyword": {"type": ["string", "null"], "default": None},
            },
            "additionalProperties": False,
        }
    if tool_name == "edit":
        return {
            "type": "object",
            "properties": {
                "path": {"type": "string"},
                "old_string": {"type": "string"},
                "new_string": {"type": "string", "default": ""},
            },
            "required": ["path", "old_string"],
            "additionalProperties": False,
        }
    if tool_name == "patch":
        return {
            "type": "object",
            "properties": {
                "operations": {
                    "type": "array",
                    "minItems": 1,
                    "items": {
                        "type": "object",
                        "properties": {
                            "type": {"type": "string", "enum": ["replace", "write"]},
                            "path": {"type": "string"},
                            "old_string": {"type": "string"},
                            "new_string": {"type": "string"},
                            "content": {"type": "string"},
                        },
                        "required": ["type", "path"],
                        "additionalProperties": False,
                    },
                }
            },
            "required": ["operations"],
            "additionalProperties": False,
        }
    if tool_name == "query":
        return {
            "type": "object",
            "properties": {
                "url": {"type": "string"},
                "method": {"type": "string", "enum": ["GET", "POST"], "default": "GET"},
                "params": {"type": "object", "default": {}},
                "headers": {"type": "object", "default": {}},
                "body": {"type": "object", "default": {}},
                "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 120, "default": 10},
            },
            "required": ["url"],
            "additionalProperties": False,
        }
    if tool_name == "grep":
        return {
            "type": "object",
            "properties": {
                "pattern": {"type": "string"},
                "scope": {"type": "string", "default": "."},
                "max_results": {"type": "integer", "minimum": 1, "maximum": 5000, "default": 100},
                "file_glob": {"type": ["string", "null"], "default": None},
            },
            "required": ["pattern"],
            "additionalProperties": False,
        }
    if tool_name == "glob":
        return {
            "type": "object",
            "properties": {
                "pattern": {"type": "string"},
                "scope": {"type": "string", "default": "."},
                "max_results": {"type": "integer", "minimum": 1, "maximum": 10000, "default": 1000},
            },
            "required": ["pattern"],
            "additionalProperties": False,
        }
    if tool_name == "exec":
        return {
            "type": "object",
            "properties": {
                "command": {"type": "array", "items": {"type": "string"}, "minItems": 1},
                "cwd": {"type": ["string", "null"], "default": None},
                "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "default": 30},
                "expect_json": {"type": "boolean", "default": False},
            },
            "required": ["command"],
            "additionalProperties": False,
        }
    if tool_name == "shell":
        return {
            "type": "object",
            "properties": {
                "command": {"type": "string"},
                "cwd": {"type": ["string", "null"], "default": None},
                "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "default": 30},
            },
            "required": ["command"],
            "additionalProperties": False,
        }
    return {"type": "object", "properties": {}, "additionalProperties": True}
