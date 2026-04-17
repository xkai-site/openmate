from __future__ import annotations

import json
import subprocess
from pathlib import Path

from .context_models import ContextSnapshotRecord
from .vos_binary import ensure_vos_binary


class ContextGatewayError(RuntimeError):
    pass


class VosContextGateway:
    def __init__(
        self,
        *,
        workspace_root: str | Path | None = None,
        state_file: str | Path | None = None,
        session_db_file: str | Path | None = None,
        binary_path: str | Path | None = None,
    ) -> None:
        self._workspace_root = Path(workspace_root or Path.cwd()).resolve()
        self._state_file = Path(state_file or self._workspace_root / ".openmate" / "runtime" / "vos_state.json").resolve()
        self._session_db_file = Path(session_db_file or self._workspace_root / ".openmate" / "runtime" / "openmate.db").resolve()
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
                raise ContextGatewayError("vos CLI returned empty stdout")
            return stdout
        message = stderr or stdout or "vos CLI failed"
        try:
            parsed = json.loads(message)
            if isinstance(parsed, dict) and isinstance(parsed.get("error"), str):
                message = parsed["error"]
        except json.JSONDecodeError:
            pass
        raise ContextGatewayError(message)
