from __future__ import annotations

import json
import tempfile
import threading
import time
import unittest
from concurrent.futures import ThreadPoolExecutor
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from openmate_pool.errors import NoCapacityError
from openmate_pool.models import InvokeRequest, LlmMessage
from openmate_pool.pool import PoolGateway


class SqliteConcurrencyTestCase(unittest.TestCase):
    def test_concurrent_invoke_respects_max_concurrent(self) -> None:
        server, thread = _start_gateway_server(delay_seconds=0.2)
        self.addCleanup(server.server_close)
        self.addCleanup(server.shutdown)
        self.addCleanup(thread.join, 1)

        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            model_config.write_text(
                json.dumps(
                    {
                        "global_max_concurrent": 10,
                        "offline_failure_threshold": 3,
                        "apis": [
                            {
                                "api_id": "api-1",
                                "model": "gpt-4.1",
                                "base_url": f"http://127.0.0.1:{server.server_port}/v1",
                                "api_key": "sk-test",
                                "max_concurrent": 2,
                                "enabled": True,
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            gateway = PoolGateway(
                workspace_root=tmpdir,
                db_path=db_file,
                model_config_path=model_config,
            )
            barrier = threading.Barrier(12)

            def _invoke(idx: int) -> str | None:
                barrier.wait()
                request = InvokeRequest(
                    request_id=f"req-{idx}",
                    node_id=f"node-{idx}",
                    messages=[LlmMessage(role="user", content=f"hello-{idx}")],
                    timeout_ms=3_000,
                )
                try:
                    response = gateway.invoke(request)
                    return response.invocation_id
                except NoCapacityError:
                    return None

            with ThreadPoolExecutor(max_workers=12) as pool:
                results = list(pool.map(_invoke, range(12)))

            success_ids = [item for item in results if item is not None]
            self.assertEqual(len(success_ids), 2)

            records = gateway.records()
            self.assertEqual(len(records), 2)
            self.assertTrue(all(record.status.value == "success" for record in records))


class _GatewayHandler(BaseHTTPRequestHandler):
    delay_seconds = 0.0

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/chat/completions":
            self.send_error(404)
            return
        time.sleep(self.delay_seconds)
        body = self.rfile.read(int(self.headers.get("Content-Length", "0"))).decode("utf-8")
        payload = json.loads(body)
        messages = payload.get("messages", [])
        last_content = ""
        if messages and isinstance(messages, list) and isinstance(messages[-1], dict):
            last_content = str(messages[-1].get("content", ""))
        response = json.dumps(
            {
                "choices": [
                    {
                        "message": {
                            "role": "assistant",
                            "content": f"echo:{last_content}",
                        }
                    }
                ]
            }
        ).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(response)))
        self.end_headers()
        self.wfile.write(response)

    def log_message(self, format: str, *args: object) -> None:  # noqa: A003
        _ = (format, args)


def _start_gateway_server(*, delay_seconds: float) -> tuple[ThreadingHTTPServer, threading.Thread]:
    handler = type(
        "ConfiguredGatewayHandler",
        (_GatewayHandler,),
        {"delay_seconds": delay_seconds},
    )
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, thread


if __name__ == "__main__":
    unittest.main()
