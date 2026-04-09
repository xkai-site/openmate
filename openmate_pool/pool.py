"""Core state machine for OpenMate API pool."""

from __future__ import annotations

from datetime import datetime, timedelta
from uuid import uuid4

from .errors import (
    ApiExistsError,
    ApiNotFoundError,
    InvalidStateError,
    NoCapacityError,
    TicketNotFoundError,
)
from .models import (
    ApiDescriptor,
    ApiRecord,
    ApiRuntimeState,
    ApiStatus,
    CapacitySnapshot,
    DispatchTicket,
    ExecutionRequest,
    PoolState,
    ReleaseReceipt,
    UsageMetrics,
    UsageRecord,
)


class ApiPool:
    """In-memory API pool with Pydantic-typed state."""

    def __init__(self, state: PoolState | None = None) -> None:
        self.state = state or PoolState()

    def register_api(self, descriptor: ApiDescriptor) -> ApiRecord:
        if descriptor.api_id in self.state.apis:
            raise ApiExistsError(f"api already exists: {descriptor.api_id}")
        if descriptor.status == ApiStatus.LEASED:
            raise InvalidStateError("cannot register API with leased status")

        record = ApiRecord(
            descriptor=descriptor,
            runtime=ApiRuntimeState(api_id=descriptor.api_id),
        )
        self.state.apis[descriptor.api_id] = record
        return record

    def upsert_api(self, descriptor: ApiDescriptor) -> ApiRecord:
        record = self.state.apis.get(descriptor.api_id)
        if record is None:
            return self.register_api(descriptor)

        record.descriptor.model_class = descriptor.model_class
        record.descriptor.max_concurrent = descriptor.max_concurrent
        if record.descriptor.status != ApiStatus.OFFLINE:
            if record.runtime.lease_count >= record.descriptor.max_concurrent:
                record.descriptor.status = ApiStatus.LEASED
            else:
                record.descriptor.status = ApiStatus.AVAILABLE
        return record

    def deactivate_missing_apis(self, configured_ids: set[str]) -> None:
        for api_id, record in self.state.apis.items():
            if api_id not in configured_ids:
                record.descriptor.status = ApiStatus.OFFLINE

    def list_apis(self) -> list[ApiRecord]:
        return [self.state.apis[key] for key in sorted(self.state.apis)]

    def list_tickets(self, node_id: str | None = None) -> list[DispatchTicket]:
        tickets = [self.state.tickets[key] for key in sorted(self.state.tickets)]
        if node_id is None:
            return tickets
        return [ticket for ticket in tickets if ticket.node_id == node_id]

    def get_usage_records(self, node_id: str | None = None) -> list[UsageRecord]:
        if node_id is None:
            return list(self.state.usage_records)
        return [item for item in self.state.usage_records if item.node_id == node_id]

    def acquire(self, request: ExecutionRequest, lease_ms: int = 30_000) -> DispatchTicket:
        if self._is_global_throttled():
            raise NoCapacityError("global quota reached")

        candidates: list[ApiRecord] = []
        for record in self.state.apis.values():
            if record.descriptor.status == ApiStatus.OFFLINE:
                continue
            if record.runtime.lease_count < record.descriptor.max_concurrent:
                candidates.append(record)

        if not candidates:
            raise NoCapacityError("no available API")

        selected = min(
            candidates,
            key=lambda item: (item.runtime.lease_count, item.descriptor.api_id),
        )

        selected.runtime.lease_count += 1
        if selected.runtime.lease_count >= selected.descriptor.max_concurrent:
            selected.descriptor.status = ApiStatus.LEASED
        else:
            selected.descriptor.status = ApiStatus.AVAILABLE

        now = datetime.utcnow()
        ticket = DispatchTicket(
            ticket_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            api_id=selected.descriptor.api_id,
            lease_ms=lease_ms,
            acquired_at=now,
            expires_at=now + timedelta(milliseconds=lease_ms),
        )
        self.state.tickets[ticket.ticket_id] = ticket
        return ticket

    def release(
        self,
        ticket_id: str,
        usage: UsageMetrics | None = None,
        result_summary: str | None = None,
        reason: str = "completed",
    ) -> ReleaseReceipt:
        ticket = self.state.tickets.pop(ticket_id, None)
        if ticket is None:
            raise TicketNotFoundError(f"ticket not found: {ticket_id}")

        record = self._get_api(ticket.api_id)
        if record.runtime.lease_count <= 0:
            raise InvalidStateError(f"lease count underflow on API: {ticket.api_id}")

        record.runtime.lease_count -= 1
        if record.descriptor.status != ApiStatus.OFFLINE:
            if record.runtime.lease_count >= record.descriptor.max_concurrent:
                record.descriptor.status = ApiStatus.LEASED
            else:
                record.descriptor.status = ApiStatus.AVAILABLE

        released_at = datetime.utcnow()
        usage_record = UsageRecord(
            ticket_id=ticket.ticket_id,
            request_id=ticket.request_id,
            node_id=ticket.node_id,
            api_id=ticket.api_id,
            released_at=released_at,
            usage=usage,
            result_summary=result_summary,
            reason=reason,
        )
        self.state.usage_records.append(usage_record)

        return ReleaseReceipt(
            ticket_id=ticket.ticket_id,
            request_id=ticket.request_id,
            node_id=ticket.node_id,
            api_id=ticket.api_id,
            released_at=released_at,
            reason=reason,
            usage=usage,
            result_summary=result_summary,
        )

    def fail_api(self, api_id: str, error_message: str | None = None) -> ApiRecord:
        record = self._get_api(api_id)
        record.runtime.failure_count += 1
        record.runtime.last_error = error_message
        if record.runtime.failure_count >= self.state.offline_failure_threshold:
            record.descriptor.status = ApiStatus.OFFLINE
        return record

    def record_success(self, api_id: str) -> ApiRecord:
        record = self._get_api(api_id)
        record.runtime.failure_count = 0
        record.runtime.last_error = None
        if record.descriptor.status != ApiStatus.OFFLINE:
            if record.runtime.lease_count >= record.descriptor.max_concurrent:
                record.descriptor.status = ApiStatus.LEASED
            else:
                record.descriptor.status = ApiStatus.AVAILABLE
        return record

    def recover_api(self, api_id: str) -> ApiRecord:
        record = self._get_api(api_id)
        record.runtime.failure_count = 0
        record.runtime.last_error = None
        if record.runtime.lease_count >= record.descriptor.max_concurrent:
            record.descriptor.status = ApiStatus.LEASED
        else:
            record.descriptor.status = ApiStatus.AVAILABLE
        return record

    def set_offline(self, api_id: str) -> ApiRecord:
        record = self._get_api(api_id)
        record.descriptor.status = ApiStatus.OFFLINE
        return record

    def capacity_snapshot(self) -> CapacitySnapshot:
        total_apis = len(self.state.apis)
        total_slots = sum(item.descriptor.max_concurrent for item in self.state.apis.values())
        leased_slots = sum(item.runtime.lease_count for item in self.state.apis.values())
        available_slots = sum(
            max(item.descriptor.max_concurrent - item.runtime.lease_count, 0)
            for item in self.state.apis.values()
            if item.descriptor.status != ApiStatus.OFFLINE
        )
        offline_apis = sum(
            1 for item in self.state.apis.values() if item.descriptor.status == ApiStatus.OFFLINE
        )
        return CapacitySnapshot(
            total_apis=total_apis,
            total_slots=total_slots,
            available_slots=available_slots,
            leased_slots=leased_slots,
            offline_apis=offline_apis,
            throttled=self._is_global_throttled(),
        )

    def sweep_timeout(self, now: datetime | None = None) -> list[ReleaseReceipt]:
        now = now or datetime.utcnow()
        expired_ids = [
            ticket_id
            for ticket_id, ticket in self.state.tickets.items()
            if ticket.expires_at <= now
        ]
        receipts: list[ReleaseReceipt] = []
        for ticket_id in sorted(expired_ids):
            receipts.append(
                self.release(
                    ticket_id=ticket_id,
                    reason="timeout_sweep",
                    result_summary="lease expired and reclaimed",
                )
            )
        return receipts

    def set_global_limit(self, max_concurrent: int | None) -> None:
        self.state.global_max_concurrent = max_concurrent

    def _get_api(self, api_id: str) -> ApiRecord:
        record = self.state.apis.get(api_id)
        if record is None:
            raise ApiNotFoundError(f"api not found: {api_id}")
        return record

    def _is_global_throttled(self) -> bool:
        limit = self.state.global_max_concurrent
        if limit is None:
            return False
        return len(self.state.tickets) >= limit
