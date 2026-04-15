from __future__ import annotations

import json
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from openmate_pool.errors import InvocationFailedError, NoCapacityError
from openmate_pool.models import InvokeRequest, OpenAIResponsesRequest, RoutePolicy
from openmate_pool.pool import PoolGateway


class PoolGatewayTestCase(unittest.TestCase):
    @staticmethod
    def _write_model_config(
        path: Path,
        *,
        base_url: str,
        threshold: int = 3,
        retry: dict[str, Any] | None = None,
    ) -> None:
        payload: dict[str, Any] = {
            "global_max_concurrent": 2,
            "offline_failure_threshold": threshold,
            "apis": [
                {
                    "api_id": "api-1",
                    "model": "gpt-4.1",
                    "base_url": base_url,
                    "api_key": "sk-test-1",
                    "max_concurrent": 1,
                    "enabled": True,
                },
                {
                    "api_id": "api-2",
                    "model": "gpt-4.1-mini",
                    "base_url": base_url,
                    "api_key": "sk-test-2",
                    "max_concurrent": 1,
                    "enabled": True,
                },
            ],
        }
        if retry is not None:
            payload["retry"] = retry
        path.write_text(json.dumps(payload), encoding="utf-8")

    @staticmethod
    def _request(node_id: str, *, api_id: str | None = None) -> InvokeRequest:
        return InvokeRequest(
            request_id=f"req-{node_id}",
            node_id=node_id,
            request=OpenAIResponsesRequest(input=f"hello from {node_id}"),
            route_policy=RoutePolicy(api_id=api_id),
        )

    def test_request_rejects_chat_completions_fields(self) -> None:
        for field, value in [
            ("messages", [{"role": "user", "content": "hello"}]),
            ("functions", [{"name": "demo"}]),
            ("function_call", {"name": "demo"}),
            ("tool_calls", [{"id": "tc-1"}]),
            ("max_tokens", 100),
        ]:
            with self.subTest(field=field):
                with self.assertRaises(ValidationError):
                    OpenAIResponsesRequest.model_validate(
                        {
                            "input": "hello",
                            field: value,
                        }
                    )

    def test_invoke_success_persists_record(self) -> None:
        server, thread = _start_gateway_server()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(model_config, base_url=f"http://127.0.0.1:{server.server_port}/v1")

            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )

            response = gateway.invoke(self._request("node-1"))

            self.assertEqual(response.status.value, "success")
            self.assertEqual(response.output_text, "echo:hello from node-1")
            self.assertIsNotNone(response.route)
            self.assertEqual(response.route.api_id, "api-1")
            self.assertEqual(response.usage.total_tokens, 5)

            records = gateway.records()
            self.assertEqual(len(records), 1)
            self.assertEqual(records[0].request.node_id, "node-1")
            self.assertEqual(len(records[0].attempts), 1)
            self.assertEqual(records[0].attempts[0].route.api_id, "api-1")

            capacity = gateway.capacity()
            self.assertEqual(capacity.leased_slots, 0)

    def test_route_policy_can_pin_api(self) -> None:
        server, thread = _start_gateway_server()
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(model_config, base_url=f"http://127.0.0.1:{server.server_port}/v1")

            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )

            response = gateway.invoke(self._request("node-2", api_id="api-2"))

            self.assertIsNotNone(response.route)
            self.assertEqual(response.route.api_id, "api-2")

    def test_failure_threshold_moves_api_offline(self) -> None:
        server, thread = _start_gateway_server(fail=True)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(
                model_config,
                base_url=f"http://127.0.0.1:{server.server_port}/v1",
                threshold=1,
            )

            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )

            with self.assertRaises(InvocationFailedError):
                gateway.invoke(self._request("node-3", api_id="api-1"))

            records = gateway.records()
            self.assertEqual(len(records), 1)
            self.assertEqual(records[0].status.value, "failure")
            self.assertEqual(records[0].error.code, "provider_http_error")

            capacity = gateway.capacity()
            self.assertEqual(capacity.offline_apis, 1)

            with self.assertRaises(NoCapacityError):
                gateway.invoke(self._request("node-4", api_id="api-1"))

    def test_retryable_failure_can_succeed_after_rate_limit(self) -> None:
        server, thread = _start_gateway_server_plan(
            [
                {
                    "status": 429,
                    "body": {"error": {"message": "rate limited"}},
                },
                {
                    "status": 200,
                    "body": {
                        "choices": [
                            {
                                "message": {
                                    "role": "assistant",
                                    "content": "retry-ok",
                                }
                            }
                        ],
                        "usage": {
                            "prompt_tokens": 2,
                            "completion_tokens": 3,
                            "total_tokens": 5,
                        },
                    },
                },
            ]
        )
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(
                model_config,
                base_url=f"http://127.0.0.1:{server.server_port}/v1",
                threshold=1,
            )

            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )

            response = gateway.invoke(self._request("node-retry", api_id="api-1"))

            self.assertEqual(response.status.value, "success")
            self.assertEqual(response.output_text, "retry-ok")

            records = gateway.records()
            self.assertEqual(len(records), 1)
            self.assertEqual(len(records[0].attempts), 2)
            self.assertIsNotNone(records[0].attempts[0].error)
            self.assertEqual(records[0].attempts[0].error.code, "provider_rate_limited")
            self.assertEqual(records[0].attempts[1].status.value, "success")

            capacity = gateway.capacity()
            self.assertEqual(capacity.offline_apis, 0)

    def test_invalid_json_failure_does_not_take_api_offline(self) -> None:
        server, thread = _start_gateway_server_plan(
            [
                {
                    "status": 200,
                    "raw_body": b"not-json",
                    "content_type": "application/json",
                }
            ]
        )
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(
                model_config,
                base_url=f"http://127.0.0.1:{server.server_port}/v1",
                threshold=1,
            )

            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )

            with self.assertRaises(InvocationFailedError) as caught:
                gateway.invoke(self._request("node-invalid", api_id="api-1"))

            self.assertIsNotNone(caught.exception.response.error)
            self.assertEqual(caught.exception.response.error.code, "provider_invalid_json")

            capacity = gateway.capacity()
            self.assertEqual(capacity.offline_apis, 0)

    def test_retry_config_can_disable_retry(self) -> None:
        server, thread = _start_gateway_server_plan(
            [
                {
                    "status": 429,
                    "body": {"error": {"message": "rate limited"}},
                },
                {
                    "status": 200,
                    "body": {
                        "choices": [
                            {
                                "message": {
                                    "role": "assistant",
                                    "content": "should-not-run",
                                }
                            }
                        ]
                    },
                },
            ]
        )
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(
                model_config,
                base_url=f"http://127.0.0.1:{server.server_port}/v1",
                threshold=1,
                retry={"max_attempts": 1, "base_backoff_ms": 0},
            )

            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )

            with self.assertRaises(InvocationFailedError) as caught:
                gateway.invoke(self._request("node-no-retry", api_id="api-1"))

            self.assertIsNotNone(caught.exception.response.error)
            self.assertEqual(caught.exception.response.error.code, "provider_rate_limited")

            records = gateway.records()
            self.assertEqual(len(records), 1)
            self.assertEqual(len(records[0].attempts), 1)

    def test_usage_summary_aggregates_records(self) -> None:
        server, thread = _start_gateway_server_plan(
            [
                {
                    "status": 200,
                    "body": {
                        "choices": [
                            {
                                "message": {
                                    "role": "assistant",
                                    "content": "first-ok",
                                }
                            }
                        ],
                        "usage": {
                            "prompt_tokens": 2,
                            "completion_tokens": 3,
                            "total_tokens": 5,
                        },
                    },
                },
                {
                    "status": 429,
                    "body": {"error": {"message": "rate limited"}},
                },
                {
                    "status": 200,
                    "body": {
                        "choices": [
                            {
                                "message": {
                                    "role": "assistant",
                                    "content": "second-ok",
                                }
                            }
                        ],
                        "usage": {
                            "prompt_tokens": 2,
                            "completion_tokens": 3,
                            "total_tokens": 5,
                        },
                    },
                },
                {
                    "status": 200,
                    "raw_body": b"not-json",
                    "content_type": "application/json",
                },
            ]
        )
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(
                model_config,
                base_url=f"http://127.0.0.1:{server.server_port}/v1",
            )

            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )

            gateway.invoke(self._request("node-usage-1"))
            gateway.invoke(self._request("node-usage-2"))
            with self.assertRaises(InvocationFailedError):
                gateway.invoke(self._request("node-usage-3"))

            summary = gateway.usage()
            self.assertEqual(summary.invocation_count, 3)
            self.assertEqual(summary.success_count, 2)
            self.assertEqual(summary.failure_count, 1)
            self.assertEqual(summary.attempt_count, 4)
            self.assertEqual(summary.retry_count, 1)
            self.assertEqual(summary.input_tokens, 4)
            self.assertEqual(summary.output_tokens, 6)
            self.assertEqual(summary.total_tokens, 10)
            self.assertIsNotNone(summary.avg_latency_ms)
            self.assertIsNotNone(summary.max_latency_ms)

            filtered = gateway.usage(node_id="node-usage-2")
            self.assertEqual(filtered.invocation_count, 1)
            self.assertEqual(filtered.attempt_count, 2)
            self.assertEqual(filtered.retry_count, 1)
            self.assertEqual(filtered.node_id, "node-usage-2")


class _GatewayHandler(BaseHTTPRequestHandler):
    fail = False

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/responses":
            self.send_error(404)
            return
        body = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode("utf-8")
        payload = json.loads(body)
        if self.fail:
            response = json.dumps({"error": {"message": "gateway failed"}}).encode("utf-8")
            self.send_response(500)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(response)))
            self.end_headers()
            self.wfile.write(response)
            return

        response = json.dumps(_response_payload_for_text(f"echo:{_extract_input_text(payload.get('input'))}")).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


class _PlannedGatewayHandler(BaseHTTPRequestHandler):
    response_plan: list[dict[str, Any]] = []
    response_lock = threading.Lock()

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/responses":
            self.send_error(404)
            return
        _ = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        with self.response_lock:
            if self.response_plan:
                current = self.response_plan.pop(0)
            else:
                current = {
                    "status": 200,
                    "body": {
                        "choices": [
                            {
                                "message": {
                                    "role": "assistant",
                                    "content": "default-ok",
                                }
                            }
                        ]
                    },
                }

        status = int(current.get("status", 200))
        content_type = str(current.get("content_type", "application/json"))
        raw_body = current.get("raw_body")
        if raw_body is None:
            body = _normalize_response_body(current.get("body", _response_payload_for_text("default-ok")))
            raw_body = json.dumps(body).encode("utf-8")
        else:
            raw_body = bytes(raw_body)

        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(raw_body)))
        self.end_headers()
        self.wfile.write(raw_body)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


def _start_gateway_server(*, fail: bool = False) -> tuple[ThreadingHTTPServer, threading.Thread]:
    handler = type(
        "ConfiguredGatewayHandler",
        (_GatewayHandler,),
        {"fail": fail},
    )
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


def _start_gateway_server_plan(
    response_plan: list[dict[str, Any]],
) -> tuple[ThreadingHTTPServer, threading.Thread]:
    handler = type(
        "ConfiguredPlannedGatewayHandler",
        (_PlannedGatewayHandler,),
        {
            "response_plan": [dict(item) for item in response_plan],
            "response_lock": threading.Lock(),
        },
    )
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


def _response_payload_for_text(
    text: str,
    *,
    input_tokens: int = 2,
    output_tokens: int = 3,
    total_tokens: int = 5,
    cached_input_tokens: int = 0,
    reasoning_tokens: int = 0,
) -> dict[str, Any]:
    return {
        "id": "resp-test",
        "object": "response",
        "model": "gpt-4.1",
        "status": "completed",
        "output": [
            {
                "type": "message",
                "role": "assistant",
                "status": "completed",
                "content": [
                    {
                        "type": "output_text",
                        "text": text,
                    }
                ],
            }
        ],
        "usage": {
            "input_tokens": input_tokens,
            "input_tokens_details": {
                "cached_tokens": cached_input_tokens,
            },
            "output_tokens": output_tokens,
            "output_tokens_details": {
                "reasoning_tokens": reasoning_tokens,
            },
            "total_tokens": total_tokens,
        },
    }


def _normalize_response_body(body: dict[str, Any]) -> dict[str, Any]:
    if "output" in body or "error" in body:
        return body
    choices = body.get("choices")
    if isinstance(choices, list) and choices:
        first = choices[0] if isinstance(choices[0], dict) else {}
        message = first.get("message", {}) if isinstance(first, dict) else {}
        text = str(message.get("content", ""))
        usage = body.get("usage", {}) if isinstance(body.get("usage"), dict) else {}
        return _response_payload_for_text(
            text,
            input_tokens=int(usage.get("prompt_tokens", 2)),
            output_tokens=int(usage.get("completion_tokens", 3)),
            total_tokens=int(usage.get("total_tokens", 5)),
        )
    return body


def _extract_input_text(value: Any) -> str:
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        parts: list[str] = []
        for item in value:
            if not isinstance(item, dict):
                continue
            if item.get("type") == "message":
                content = item.get("content", [])
                if isinstance(content, list):
                    for content_item in content:
                        if isinstance(content_item, dict) and content_item.get("type") in {"input_text", "text"}:
                            parts.append(str(content_item.get("text", "")))
        return "".join(parts)
    return ""


if __name__ == "__main__":
    unittest.main()
