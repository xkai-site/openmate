from __future__ import annotations

import json
import subprocess
from pathlib import Path

from openmate_shared.runtime_paths import (
    default_runtime_db_path,
    default_vos_state_path,
    resolve_workspace_root,
)

from .session_models import (
    AppendSessionEventInput,
    SessionEventRecord,
    SessionRecord,
    SessionStatus,
)
from .vos_binary import ensure_vos_binary


class SessionWriterError(RuntimeError):
    pass


class VosSessionWriter:
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

    def ensure_session(self, node_id: str, session_id: str | None = None) -> str:
        resolved_session_id = session_id.strip() if session_id else ""
        if resolved_session_id:
            existing = self._try_get_session(resolved_session_id)
            if existing is not None:
                if existing.node_id != node_id:
                    raise SessionWriterError(
                        f"session {resolved_session_id} belongs to node {existing.node_id}, expected {node_id}"
                    )
                return existing.id

        command = [
            "session",
            "create",
            "--node-id",
            node_id,
            "--status",
            SessionStatus.ACTIVE.value,
        ]
        if resolved_session_id:
            command.extend(["--session-id", resolved_session_id])
        stdout = self._run_command(command)
        session = SessionRecord.model_validate_json(stdout)
        return session.id

    def append_event(self, event: AppendSessionEventInput) -> SessionEventRecord:
        command = [
            "session",
            "append-event",
            "--session-id",
            event.session_id,
            "--item-type",
            event.item_type,
            "--payload-json",
            json.dumps(event.payload_json, ensure_ascii=False),
        ]
        if event.call_id:
            command.extend(["--call-id", event.call_id])
        if event.provider_item_id:
            command.extend(["--provider-item-id", event.provider_item_id])
        if event.role:
            command.extend(["--role", event.role.value])
        if event.next_status:
            command.extend(["--next-status", event.next_status.value])
        stdout = self._run_command(command)
        return SessionEventRecord.model_validate_json(stdout)

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
                raise SessionWriterError("vos CLI returned empty stdout")
            return stdout
        raise SessionWriterError(stderr or stdout or "vos CLI failed")

    def _try_get_session(self, session_id: str) -> SessionRecord | None:
        try:
            stdout = self._run_command(
                [
                    "session",
                    "get",
                    "--session-id",
                    session_id,
                ]
            )
        except SessionWriterError as exc:
            if _is_session_not_found_error(str(exc)):
                return None
            raise
        return SessionRecord.model_validate_json(stdout)


def _is_session_not_found_error(message: str) -> bool:
    normalized = (message or "").strip().lower()
    return "session not found" in normalized
