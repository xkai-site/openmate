"""Model configuration schema and loader."""

from __future__ import annotations

from pathlib import Path

from pydantic import BaseModel, ConfigDict, Field


class ApiEndpointConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    api_id: str = Field(min_length=1)
    model: str = Field(min_length=1)
    base_url: str = Field(min_length=1)
    api_key: str = Field(min_length=1)
    max_concurrent: int = Field(ge=1)
    enabled: bool = True


class ModelConfig(BaseModel):
    model_config = ConfigDict(extra="forbid")

    global_max_concurrent: int | None = Field(default=None, ge=1)
    offline_failure_threshold: int = Field(default=3, ge=1)
    apis: list[ApiEndpointConfig] = Field(default_factory=list)


def load_model_config(path: Path) -> ModelConfig:
    if not path.exists():
        raise FileNotFoundError(f"model config not found: {path}")
    payload = path.read_text(encoding="utf-8").strip()
    if not payload:
        raise ValueError(f"model config is empty: {path}")
    return ModelConfig.model_validate_json(payload)
