"""Helpers to build and locate the Go vos CLI."""

from __future__ import annotations

import os
import subprocess
from pathlib import Path


class VosBinaryError(RuntimeError):
    pass


def ensure_vos_binary(explicit_binary: str | Path | None = None) -> Path:
    if explicit_binary is not None:
        return Path(explicit_binary).resolve()

    env_binary = os.environ.get("OPENMATE_VOS_BIN")
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
                "./cmd/vos",
            ],
            cwd=repo_root,
            capture_output=True,
            text=True,
            check=False,
            env=env,
        )
        if result.returncode != 0:
            stderr = result.stderr.strip()
            raise VosBinaryError(stderr or "failed to build vos CLI")

    return binary_path


def _repo_root() -> Path:
    return Path(__file__).resolve().parents[1]


def _binary_name() -> str:
    return "vos.exe" if os.name == "nt" else "vos"


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
    for root in [repo_root / "cmd" / "vos", repo_root / "internal" / "vos"]:
        if not root.exists():
            continue
        yield from root.rglob("*.go")
