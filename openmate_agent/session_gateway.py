from __future__ import annotations

import json
import subprocess
from pathlib import Path

from .session_models import (
    AppendSessionEventInput,
    SessionEventRecord,
    SessionRecord,
    SessionStatus,
)
from .vos_binary import ensure_vos_binary


class SessionGatewayError(RuntimeError):
    pass


class VosSessionGateway:
    def __init__(
        self,
        *,
        workspace_root: str | Path | None = None,
        state_file: str | Path | None = None,
        session_db_file: str | Path | None = None,
        binary_path: str | Path | None = None,
    ) -> None:
        self._workspace_root = Path(workspace_root or Path.cwd()).resolve()
        self._state_file = Path(state_file or self._workspace_root / ".vos_state.json").resolve()
        self._session_db_file = Path(session_db_file or self._workspace_root / ".vos_sessions.db").resolve()
        self._binary_path = binary_path

    def ensure_session(self, node_id: str, session_id: str | None = None) -> str:
        command = [
            "session",
            "create",
            "--node-id",
            node_id,
            "--status",
            SessionStatus.ACTIVE.value,
        ]
        if session_id:
            command.extend(["--session-id", session_id])
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
            check=False,
            cwd=self._workspace_root,
        )
        stdout = result.stdout.strip()
        stderr = result.stderr.strip()
        if result.returncode == 0:
            if not stdout:
                raise SessionGatewayError("vos CLI returned empty stdout")
            return stdout
        raise SessionGatewayError(stderr or stdout or "vos CLI failed")
