from __future__ import annotations

import json
import subprocess
from pathlib import Path
from typing import Any

from openmate_shared.runtime_paths import (
    default_runtime_db_path,
    default_vos_state_path,
    resolve_workspace_root,
)
from pydantic import BaseModel, ConfigDict, Field

from .session_models import SessionEventRecord, SessionRecord
from .vos_binary import ensure_vos_binary


class ContextReaderError(RuntimeError):
    pass


class ContextSessionHistoryRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    session: SessionRecord
    events: list[SessionEventRecord] = Field(default_factory=list)


class ProcessContextRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    name: str
    status: str
    memory: dict[str, Any] | None = None
    session_events: list[SessionEventRecord] = Field(default_factory=list)


class ContextSnapshotRecord(BaseModel):
    model_config = ConfigDict(extra="forbid")

    node_id: str = Field(min_length=1)
    user_memory: dict[str, Any] | None = None
    topic_memory: dict[str, Any] | None = None
    node_memory: dict[str, Any] | None = None
    global_index: Any | None = None
    session_history: list[ContextSessionHistoryRecord] = Field(default_factory=list)
    process_contexts: list[ProcessContextRecord] = Field(default_factory=list)


class VosContextReader:
    def __init__(
        self,
        *,
        workspace_root: str | Path | None = None,
        state_file: str | Path | None = None,
        session_db_file: str | Path | None = None,
        binary_path: str | Path | None = None,
    ) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)
        self._state_file = Path(state_file).resolve() if state_file is not None else default_vos_state_path(self._workspace_root)
        self._session_db_file = (
            Path(session_db_file).resolve() if session_db_file is not None else default_runtime_db_path(self._workspace_root)
        )
        self._binary_path = binary_path

    def snapshot(self, node_id: str) -> ContextSnapshotRecord:
        stdout = self._run_command(
            [
                "context",
                "snapshot",
                "--node-id",
                node_id,
            ]
        )
        return ContextSnapshotRecord.model_validate_json(stdout)

    def _run_command(self, command: list[str]) -> str:
        binary = ensure_vos_binary(self._binary_path)
        result = subprocess.run(
            [
                str(binary),
                "--state-file",
                str(self._state_file),
                "--session-db-file",
                str(self._session_db_file),
                *command,
            ],
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
            cwd=self._workspace_root,
        )
        stdout = result.stdout.strip()
        stderr = result.stderr.strip()
        if result.returncode == 0:
            if not stdout:
                raise ContextReaderError("vos CLI returned empty stdout")
            return stdout
        message = stderr or stdout or "vos CLI failed"
        try:
            parsed = json.loads(message)
            if isinstance(parsed, dict) and isinstance(parsed.get("error"), str):
                message = parsed["error"]
        except json.JSONDecodeError:
            pass
        raise ContextReaderError(message)
