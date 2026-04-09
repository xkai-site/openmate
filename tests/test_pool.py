from __future__ import annotations

import unittest
from datetime import datetime, timedelta

from openmate_pool.errors import NoCapacityError, TicketNotFoundError
from openmate_pool.models import ApiDescriptor, ExecutionRequest, PoolState
from openmate_pool.pool import ApiPool


class ApiPoolTestCase(unittest.TestCase):
    def test_acquire_release_state_transition(self) -> None:
        pool = ApiPool(PoolState())
        pool.register_api(
            ApiDescriptor(api_id="api-1", model_class="gpt-4.1", max_concurrent=1)
        )

        ticket = pool.acquire(ExecutionRequest(request_id="req-1", node_id="node-1"))
        api = pool.state.apis["api-1"]
        self.assertEqual(api.runtime.lease_count, 1)
        self.assertEqual(api.descriptor.status.value, "leased")

        receipt = pool.release(ticket.ticket_id, result_summary="ok")
        self.assertEqual(receipt.ticket_id, ticket.ticket_id)
        self.assertEqual(pool.state.apis["api-1"].runtime.lease_count, 0)
        self.assertEqual(pool.state.apis["api-1"].descriptor.status.value, "available")

    def test_no_capacity_when_slots_full(self) -> None:
        pool = ApiPool(PoolState())
        pool.register_api(
            ApiDescriptor(api_id="api-1", model_class="gpt-4.1", max_concurrent=1)
        )
        pool.acquire(ExecutionRequest(request_id="req-1", node_id="node-1"))

        with self.assertRaises(NoCapacityError):
            pool.acquire(ExecutionRequest(request_id="req-2", node_id="node-2"))

    def test_retry_for_same_node_creates_multiple_tickets(self) -> None:
        pool = ApiPool(PoolState())
        pool.register_api(
            ApiDescriptor(api_id="api-1", model_class="gpt-4.1", max_concurrent=2)
        )

        t1 = pool.acquire(ExecutionRequest(request_id="req-1", node_id="node-x"))
        t2 = pool.acquire(ExecutionRequest(request_id="req-2", node_id="node-x"))

        self.assertNotEqual(t1.ticket_id, t2.ticket_id)
        self.assertEqual(t1.node_id, "node-x")
        self.assertEqual(t2.node_id, "node-x")
        self.assertEqual(t1.api_id, "api-1")
        self.assertEqual(t2.api_id, "api-1")

    def test_failure_threshold_moves_api_offline_and_recover(self) -> None:
        pool = ApiPool(PoolState(offline_failure_threshold=2))
        pool.register_api(
            ApiDescriptor(api_id="api-1", model_class="gpt-4.1", max_concurrent=1)
        )

        pool.fail_api("api-1", error_message="timeout-1")
        pool.fail_api("api-1", error_message="timeout-2")
        self.assertEqual(pool.state.apis["api-1"].descriptor.status.value, "offline")

        with self.assertRaises(NoCapacityError):
            pool.acquire(ExecutionRequest(request_id="req-1", node_id="node-1"))

        pool.recover_api("api-1")
        self.assertEqual(pool.state.apis["api-1"].descriptor.status.value, "available")

    def test_sweep_timeout_reclaims_expired_ticket(self) -> None:
        pool = ApiPool(PoolState())
        pool.register_api(
            ApiDescriptor(api_id="api-1", model_class="gpt-4.1", max_concurrent=1)
        )
        ticket = pool.acquire(
            ExecutionRequest(request_id="req-1", node_id="node-1"),
            lease_ms=10,
        )

        now = ticket.acquired_at + timedelta(milliseconds=50)
        receipts = pool.sweep_timeout(now=now)

        self.assertEqual(len(receipts), 1)
        self.assertEqual(receipts[0].ticket_id, ticket.ticket_id)
        self.assertEqual(pool.state.apis["api-1"].runtime.lease_count, 0)
        self.assertEqual(len(pool.state.tickets), 0)

    def test_global_limit_overrides_total_slots(self) -> None:
        pool = ApiPool(PoolState(global_max_concurrent=1))
        pool.register_api(
            ApiDescriptor(api_id="api-1", model_class="gpt-4.1", max_concurrent=2)
        )
        pool.register_api(
            ApiDescriptor(api_id="api-2", model_class="gpt-4.1", max_concurrent=2)
        )

        pool.acquire(ExecutionRequest(request_id="req-1", node_id="node-1"))
        with self.assertRaises(NoCapacityError):
            pool.acquire(ExecutionRequest(request_id="req-2", node_id="node-2"))

    def test_release_unknown_ticket_raises(self) -> None:
        pool = ApiPool(PoolState())
        with self.assertRaises(TicketNotFoundError):
            pool.release("not-exist")


if __name__ == "__main__":
    unittest.main()
