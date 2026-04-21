from __future__ import annotations

from pathlib import Path


def resolve_workspace_root(workspace_root: str | Path | None = None) -> Path:
    return Path(workspace_root or Path.cwd()).resolve()


def default_runtime_db_path(workspace_root: str | Path | None = None) -> Path:
    root = resolve_workspace_root(workspace_root)
    return (root / ".openmate" / "runtime" / "openmate.db").resolve()


def default_vos_state_path(workspace_root: str | Path | None = None) -> Path:
    root = resolve_workspace_root(workspace_root)
    return (root / ".openmate" / "runtime" / "vos_state.json").resolve()


def default_model_config_path(workspace_root: str | Path | None = None) -> Path:
    root = resolve_workspace_root(workspace_root)
    return (root / "model.json").resolve()
