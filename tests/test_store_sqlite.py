from __future__ import annotations

import tempfile
import unittest
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path

from openmate_pool.errors import NoCapacityError
from openmate_pool.model_config import ApiEndpointConfig, ModelConfig
from openmate_pool.models import ExecutionRequest
from openmate_pool.store import PoolStateStore


class SqliteConcurrencyTestCase(unittest.TestCase):
    def test_concurrent_acquire_respects_max_concurrent(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            store = PoolStateStore(db_file)
            config = ModelConfig(
                global_max_concurrent=10,
                offline_failure_threshold=3,
                apis=[
                    ApiEndpointConfig(
                        api_id="api-1",
                        model="gpt-4.1",
                        base_url="https://api.openai.com/v1",
                        api_key="sk-test",
                        max_concurrent=2,
                        enabled=True,
                    )
                ],
            )

            def _acquire(idx: int) -> str | None:
                request = ExecutionRequest(
                    request_id=f"req-{idx}",
                    node_id=f"node-{idx}",
                )
                try:
                    ticket, _ = store.acquire(
                        config=config,
                        request=request,
                        lease_ms=30_000,
                    )
                    return ticket.ticket_id
                except NoCapacityError:
                    return None

            with ThreadPoolExecutor(max_workers=8) as pool:
                results = list(pool.map(_acquire, range(12)))

            success_ids = [item for item in results if item is not None]
            self.assertEqual(len(success_ids), 2)

            tickets = store.list_tickets(config)
            self.assertEqual(len(tickets), 2)


if __name__ == "__main__":
    unittest.main()
