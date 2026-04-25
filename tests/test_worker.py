from __future__ import annotations

import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from openmate_agent.worker import WorkerExecuteRequest, execute_worker_request


class WorkerRoutingTests(unittest.TestCase):
    def test_worker_auto_selects_single_enabled_api(self) -> None:
        with TemporaryDirectory() as tmp:
            model_config = Path(tmp, "model.json")
            model_config.write_text(
                json.dumps(
                    {
                        "apis": [
                            {
                                "api_id": "api-only",
                                "model": "gpt-4.1",
                                "base_url": "http://127.0.0.1:1/v1",
                                "api_key": "sk-test",
                                "max_concurrent": 1,
                                "enabled": True,
                            }
                        ]
                    }
                ),
                encoding="utf-8",
            )
            request = WorkerExecuteRequest.model_validate(
                {
                    "request_id": "req-1",
                    "topic_id": "topic-1",
                    "node_id": "node-1",
                    "node_name": "Node 1",
                    "agent_spec": {
                        "workspace_root": tmp,
                        "pool_model_config": str(model_config),
                    },
                }
            )
            with patch("openmate_agent.worker.AgentCapabilityService", _FakeAgentCapabilityService):
                response = execute_worker_request(request)

        self.assertEqual(response.status, "succeeded")
        self.assertEqual(_FakeAgentCapabilityService.last_build_api_id, "api-only")
        self.assertEqual(_FakeAgentCapabilityService.last_runner_type, "ResponsesExecutionRunner")

    def test_worker_requires_api_id_when_multiple_enabled_apis(self) -> None:
        with TemporaryDirectory() as tmp:
            model_config = Path(tmp, "model.json")
            model_config.write_text(
                json.dumps(
                    {
                        "apis": [
                            {
                                "api_id": "api-1",
                                "model": "gpt-4.1",
                                "base_url": "http://127.0.0.1:1/v1",
                                "api_key": "sk-test",
                                "max_concurrent": 1,
                                "enabled": True,
                            },
                            {
                                "api_id": "api-2",
                                "model": "gpt-4.1-mini",
                                "base_url": "http://127.0.0.1:1/v1",
                                "api_key": "sk-test-2",
                                "max_concurrent": 1,
                                "enabled": True,
                            },
                        ]
                    }
                ),
                encoding="utf-8",
            )
            request = WorkerExecuteRequest.model_validate(
                {
                    "request_id": "req-2",
                    "topic_id": "topic-1",
                    "node_id": "node-1",
                    "node_name": "Node 1",
                    "agent_spec": {
                        "workspace_root": tmp,
                        "pool_model_config": str(model_config),
                    },
                }
            )
            with patch("openmate_agent.worker.AgentCapabilityService", _FakeAgentCapabilityService):
                response = execute_worker_request(request)

        self.assertEqual(response.status, "failed")
        self.assertIn("agent_spec.api_id is required", response.error or "")

    def test_worker_uses_chat_runner_when_api_mode_is_chat(self) -> None:
        with TemporaryDirectory() as tmp:
            model_config = Path(tmp, "model.json")
            model_config.write_text(
                json.dumps(
                    {
                        "apis": [
                            {
                                "api_id": "api-chat",
                                "api_mode": "chat_completions",
                                "model": "gpt-4.1",
                                "base_url": "http://127.0.0.1:1/v1",
                                "api_key": "sk-test",
                                "max_concurrent": 1,
                                "enabled": True,
                            },
                            {
                                "api_id": "api-resp",
                                "api_mode": "responses",
                                "model": "gpt-4.1-mini",
                                "base_url": "http://127.0.0.1:1/v1",
                                "api_key": "sk-test-2",
                                "max_concurrent": 1,
                                "enabled": True,
                            },
                        ]
                    }
                ),
                encoding="utf-8",
            )
            request = WorkerExecuteRequest.model_validate(
                {
                    "request_id": "req-3",
                    "topic_id": "topic-1",
                    "node_id": "node-1",
                    "node_name": "Node 1",
                    "agent_spec": {
                        "workspace_root": tmp,
                        "pool_model_config": str(model_config),
                        "api_id": "api-chat",
                    },
                }
            )
            with patch("openmate_agent.worker.AgentCapabilityService", _FakeAgentCapabilityService):
                response = execute_worker_request(request)

        self.assertEqual(response.status, "succeeded")
        self.assertEqual(_FakeAgentCapabilityService.last_build_api_id, "api-chat")
        self.assertEqual(_FakeAgentCapabilityService.last_runner_type, "ChatExecutionRunner")


class _FakeAgentCapabilityService:
    last_runner_type: str | None = None
    last_build_api_id: str | None = None

    def __init__(self, **kwargs: object) -> None:
        runner = kwargs.get("execution_runner")
        self.__class__.last_runner_type = type(runner).__name__ if runner is not None else None

    def build(self, node_id: str, session_id: str | None = None, api_id: str | None = None) -> object:
        _ = (node_id, session_id)
        self.__class__.last_build_api_id = api_id
        return object()

    def execute_agent(self, build: object) -> str:
        _ = build
        return "worker-ok"


if __name__ == "__main__":
    unittest.main()
