"""Helpers to build and locate the Go pool CLI."""

from __future__ import annotations

import os
import subprocess
from pathlib import Path

from .errors import PoolTransportError


def ensure_pool_binary(explicit_binary: str | Path | None = None) -> Path:
    if explicit_binary is not None:
        return Path(explicit_binary).resolve()

    env_binary = os.environ.get("OPENMATE_POOL_BIN")
    if env_binary:
        return Path(env_binary).resolve()

    repo_root = _repo_root()
    binary_path = repo_root / ".openmate" / "bin" / _binary_name()
    binary_path.parent.mkdir(parents=True, exist_ok=True)

    if _needs_rebuild(binary_path, repo_root):
        env = os.environ.copy()
        env["GOCACHE"] = str(repo_root / ".openmate" / "go-build-cache")
        env["GOMODCACHE"] = str(repo_root / ".openmate" / "go-mod-cache")
        result = subprocess.run(
            [
                "go",
                "build",
                "-o",
                str(binary_path),
                "./cmd/openmate-pool",
            ],
            cwd=repo_root,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            check=False,
            env=env,
        )
        if result.returncode != 0:
            stderr = result.stderr.strip()
            raise PoolTransportError(stderr or "failed to build Go pool CLI")
    return binary_path


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _binary_name() -> str:
    return "openmate-pool.exe" if os.name == "nt" else "openmate-pool"


def _needs_rebuild(binary_path: Path, repo_root: Path) -> bool:
    if not binary_path.exists():
        return True
    binary_mtime = binary_path.stat().st_mtime
    for source in _iter_sources(repo_root):
        if source.stat().st_mtime > binary_mtime:
            return True
    return False


def _iter_sources(repo_root: Path):
    candidates = [
        repo_root / "go.mod",
        repo_root / "go.sum",
    ]
    for candidate in candidates:
        if candidate.exists():
            yield candidate
    for root in [repo_root / "cmd" / "openmate-pool", repo_root / "internal" / "poolgateway"]:
        if not root.exists():
            continue
        yield from root.rglob("*.go")
