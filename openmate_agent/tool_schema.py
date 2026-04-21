from __future__ import annotations

from typing import Any

from .models import ToolBundle

_TOOL_PARAMETER_SCHEMAS: dict[str, dict[str, Any]] = {
    "read": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "offset": {"type": "integer", "minimum": 0, "default": 0},
            "limit": {"type": "integer", "minimum": 1, "maximum": 2000, "default": 200},
        },
        "required": ["path"],
        "additionalProperties": False,
    },
    "write": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "content": {"type": "string", "default": ""},
        },
        "required": ["path"],
        "additionalProperties": False,
    },
    "edit": {
        "type": "object",
        "properties": {
            "path": {"type": "string"},
            "old_string": {"type": "string"},
            "new_string": {"type": "string", "default": ""},
        },
        "required": ["path", "old_string"],
        "additionalProperties": False,
    },
    "patch": {
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
            },
        },
        "required": ["operations"],
        "additionalProperties": False,
    },
    "query": {
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
    },
    "grep": {
        "type": "object",
        "properties": {
            "pattern": {"type": "string"},
            "scope": {"type": "string", "default": "."},
            "max_results": {"type": "integer", "minimum": 1, "maximum": 5000, "default": 100},
            "file_glob": {"type": ["string", "null"], "default": None},
        },
        "required": ["pattern"],
        "additionalProperties": False,
    },
    "glob": {
        "type": "object",
        "properties": {
            "pattern": {"type": "string"},
            "scope": {"type": "string", "default": "."},
            "max_results": {"type": "integer", "minimum": 1, "maximum": 10000, "default": 1000},
        },
        "required": ["pattern"],
        "additionalProperties": False,
    },
    "exec": {
        "type": "object",
        "properties": {
            "command": {
                "type": "array",
                "items": {"type": "string"},
                "minItems": 1,
            },
            "cwd": {"type": ["string", "null"], "default": None},
            "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "default": 30},
            "expect_json": {"type": "boolean", "default": False},
        },
        "required": ["command"],
        "additionalProperties": False,
    },
    "shell": {
        "type": "object",
        "properties": {
            "command": {"type": "string"},
            "cwd": {"type": ["string", "null"], "default": None},
            "timeout_seconds": {"type": "integer", "minimum": 1, "maximum": 300, "default": 30},
        },
        "required": ["command"],
        "additionalProperties": False,
    },
}


def build_openai_tools(bundle: ToolBundle) -> list[dict[str, object]]:
    payload: list[dict[str, object]] = []
    for tool in bundle.tools:
        payload.append(
            {
                "type": "function",
                "name": tool.name,
                "description": tool.description,
                "parameters": tool_parameters_for_name(tool.name),
            }
        )
    return payload


def tool_parameters_for_name(tool_name: str) -> dict[str, object]:
    default_schema: dict[str, object] = {
        "type": "object",
        "properties": {},
        "additionalProperties": True,
    }
    return _TOOL_PARAMETER_SCHEMAS.get(tool_name, default_schema)

