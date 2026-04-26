from __future__ import annotations

import json
import logging
import subprocess
from pathlib import Path

from openmate_agent.vos_binary import ensure_vos_binary
from openmate_shared.runtime_paths import default_runtime_db_path, default_vos_state_path

_LOGGER = logging.getLogger(__name__)


class VosCommandError(RuntimeError):
    pass


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
    try:
        stdout = run_vos_cli(workspace_root=workspace_root, command=["node", "get", "--node-id", node_id])
        parsed = json.loads(stdout or "{}")
        if isinstance(parsed, dict):
            parent_id = parsed.get("parent_id")
            name = str(parsed.get("name", "") or "")
            return (parent_id if isinstance(parent_id, str) else None, name)
    except Exception:
        _LOGGER.debug("resolve_node_context failed for node=%s, using defaults", node_id, exc_info=True)
    return (None, "")
