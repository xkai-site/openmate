from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass
from pathlib import Path

from openmate_agent.vos_binary import ensure_vos_binary
from openmate_shared.runtime_paths import default_runtime_db_path, default_vos_state_path

class VosCommandError(RuntimeError):
    pass


class VosNodeNotFoundError(VosCommandError):
    pass


class VosUnavailableError(VosCommandError):
    pass


class VosInvalidPayloadError(VosCommandError):
    pass


@dataclass(frozen=True)
class NodeToolContext:
    parent_id: str | None
    node_name: str
    topic_id: str | None
    topic_workspace: str | None


def run_vos_cli(*, workspace_root: Path, command: list[str]) -> str:
    binary = ensure_vos_binary()
    result = subprocess.run(
        [
            str(binary),
            "--state-file",
            str(default_vos_state_path(workspace_root)),
            "--session-db-file",
            str(default_runtime_db_path(workspace_root)),
            *command,
        ],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        check=False,
        cwd=str(workspace_root),
    )
    stdout = (result.stdout or "").strip()
    stderr = (result.stderr or "").strip()
    if result.returncode == 0:
        return stdout

    message = stderr or stdout or "vos CLI failed"
    try:
        parsed = json.loads(message)
        if isinstance(parsed, dict) and isinstance(parsed.get("error"), str):
            message = parsed["error"]
    except json.JSONDecodeError:
        pass
    raise VosCommandError(message)


def resolve_node_context(*, workspace_root: Path, node_id: str) -> tuple[str | None, str]:
    context = resolve_node_tool_context(workspace_root=workspace_root, node_id=node_id)
    return context.parent_id, context.node_name


def resolve_node_tool_context(*, workspace_root: Path, node_id: str) -> NodeToolContext:
    try:
        stdout = run_vos_cli(workspace_root=workspace_root, command=["node", "tool-context", "--node-id", node_id])
    except VosCommandError as exc:
        message = str(exc).strip()
        normalized = message.lower()
        if "node not found" in normalized:
            raise VosNodeNotFoundError(f"node_not_found: {message}") from exc
        raise VosUnavailableError(f"vos_unavailable: {message}") from exc
    try:
        parsed = json.loads(stdout or "{}")
    except json.JSONDecodeError as exc:
        raise VosInvalidPayloadError(f"invalid_payload: vos node tool-context JSON decode failed: {exc}") from exc
    if not isinstance(parsed, dict):
        raise VosInvalidPayloadError("invalid_payload: vos node tool-context returned invalid payload")

    parent_id = parsed.get("parent_id")
    node_name_raw = parsed.get("node_name")
    if not isinstance(node_name_raw, str):
        node_name_raw = parsed.get("name")
    name = str(node_name_raw or "")
    topic_id = parsed.get("topic_id")
    topic_id_value = topic_id.strip() if isinstance(topic_id, str) else None
    if topic_id_value == "":
        topic_id_value = None

    topic_workspace: str | None = None
    workspace_value = parsed.get("topic_workspace")
    if isinstance(workspace_value, str):
        trimmed = workspace_value.strip()
        if trimmed:
            topic_workspace = trimmed

    return NodeToolContext(
        parent_id=(parent_id if isinstance(parent_id, str) else None),
        node_name=name,
        topic_id=topic_id_value,
        topic_workspace=topic_workspace,
    )
