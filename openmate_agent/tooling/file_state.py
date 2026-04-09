from __future__ import annotations

import json
import os
import time
from contextlib import contextmanager
from pathlib import Path


class FileTimeStore:
    def __init__(self, workspace_root: Path) -> None:
        self._workspace_root = workspace_root
        self._state_dir = workspace_root / ".openmate"
        self._state_dir.mkdir(parents=True, exist_ok=True)
        self._state_file = self._state_dir / "filetime.json"

    def get(self, path: Path) -> float | None:
        data = self._load()
        return data.get(self._key(path))

    def set(self, path: Path, mtime: float) -> None:
        data = self._load()
        data[self._key(path)] = mtime
        self._save(data)

    def assert_fresh(self, path: Path) -> tuple[bool, str]:
        if not path.exists():
            return True, ""
        recorded = self.get(path)
        if recorded is None:
            return False, "Missing FileTime baseline. Read file first."
        current = path.stat().st_mtime
        if abs(current - recorded) > 1e-6:
            return False, "File has been modified externally."
        return True, ""

    def _key(self, path: Path) -> str:
        return str(path.resolve())

    def _load(self) -> dict[str, float]:
        if not self._state_file.exists():
            return {}
        try:
            return json.loads(self._state_file.read_text(encoding="utf-8"))
        except Exception:
            return {}

    def _save(self, data: dict[str, float]) -> None:
        self._state_file.write_text(json.dumps(data, ensure_ascii=False, indent=2), encoding="utf-8")


class FileLockManager:
    def __init__(self, workspace_root: Path) -> None:
        self._lock_dir = workspace_root / ".openmate" / "locks"
        self._lock_dir.mkdir(parents=True, exist_ok=True)

    @contextmanager
    def with_lock(self, path: Path, timeout_seconds: int = 10):
        lock_path = self._lock_dir / f"{self._safe_name(path)}.lock"
        start = time.time()
        while True:
            try:
                fd = os.open(str(lock_path), os.O_CREAT | os.O_EXCL | os.O_WRONLY)
                os.close(fd)
                break
            except FileExistsError:
                if time.time() - start > timeout_seconds:
                    raise TimeoutError(f"lock timeout: {path}")
                time.sleep(0.05)
        try:
            yield
        finally:
            if lock_path.exists():
                lock_path.unlink(missing_ok=True)

    @staticmethod
    def _safe_name(path: Path) -> str:
        raw = str(path.resolve())
        return raw.replace("\\", "_").replace("/", "_").replace(":", "_")
