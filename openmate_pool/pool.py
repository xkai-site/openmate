"""Python adapter for the Go-backed OpenMate pool CLI."""

from __future__ import annotations

import json
import subprocess
import tempfile
from pathlib import Path

from openmate_shared.runtime_paths import (
    default_model_config_path,
    default_runtime_db_path,
    resolve_workspace_root,
)

from .binary import ensure_pool_binary
from .errors import InvocationFailedError, NoCapacityError, PoolError, PoolTransportError
from .models import CapacitySnapshot, InvocationRecord, InvokeRequest, InvokeResponse, SyncResult, UsageSummary


class PoolGateway:
    """Invoke the Go pool CLI through a small Python adapter."""

    def __init__(
        self,
        *,
        workspace_root: str | Path | None = None,
        db_path: str | Path | None = None,
        model_config_path: str | Path | None = None,
        binary_path: str | Path | None = None,
    ) -> None:
        self._workspace_root = resolve_workspace_root(workspace_root)
        self._db_path = Path(db_path).resolve() if db_path is not None else default_runtime_db_path(self._workspace_root)
        self._model_config_path = (
            Path(model_config_path).resolve()
            if model_config_path is not None
            else default_model_config_path(self._workspace_root)
        )
        self._binary_path = binary_path

    def invoke(self, request: InvokeRequest) -> InvokeResponse:
        request_payload = request.model_dump(mode="json", exclude_none=True)
        with tempfile.NamedTemporaryFile("w", suffix=".json", encoding="utf-8", delete=False) as handle:
            request_file = Path(handle.name)
            json.dump(request_payload, handle, ensure_ascii=False)
        try:
            stdout = self._run_command(
                [
                    "invoke",
                    "--request-file",
                    str(request_file),
                ]
            )
        except NoCapacityError:
            raise
        except InvocationFailedError:
            raise
        finally:
            request_file.unlink(missing_ok=True)

        return InvokeResponse.model_validate_json(stdout)

    def capacity(self) -> CapacitySnapshot:
        stdout = self._run_command(["cap"])
        return CapacitySnapshot.model_validate_json(stdout)

    def records(
        self,
        *,
        node_id: str | None = None,
        limit: int | None = None,
    ) -> list[InvocationRecord]:
        command = ["records"]
        if node_id is not None:
            command.extend(["--node-id", node_id])
        if limit is not None:
            command.extend(["--limit", str(limit)])
        stdout = self._run_command(command)
        payload = json.loads(stdout)
        if not isinstance(payload, list):
            raise PoolTransportError("records output must be a JSON list")
        return [InvocationRecord.model_validate(item) for item in payload]

    def sync(self) -> SyncResult:
        stdout = self._run_command(["sync"])
        return SyncResult.model_validate_json(stdout)

    def usage(
        self,
        *,
        node_id: str | None = None,
        limit: int | None = None,
    ) -> UsageSummary:
        command = ["usage"]
        if node_id is not None:
            command.extend(["--node-id", node_id])
        if limit is not None:
            command.extend(["--limit", str(limit)])
        stdout = self._run_command(command)
        return UsageSummary.model_validate_json(stdout)

    def _run_command(self, command: list[str]) -> str:
        binary = ensure_pool_binary(self._binary_path)
        result = subprocess.run(
            [
                str(binary),
                "--db-file",
                str(self._db_path),
                "--model-config",
                str(self._model_config_path),
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
                raise PoolTransportError("pool CLI returned empty stdout")
            return stdout

        if stdout:
            try:
                response = InvokeResponse.model_validate_json(stdout)
            except Exception:
                response = None
            if response is not None and response.status.value == "failure":
                raise InvocationFailedError(response)

        message = stderr or stdout or "pool CLI failed"
        if "no available API" in message or "global quota reached" in message:
            raise NoCapacityError(message)
        raise PoolError(message)
