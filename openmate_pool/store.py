"""SQLite persistence for OpenMate API pool."""

from __future__ import annotations

import sqlite3
from datetime import datetime, timedelta
from pathlib import Path
from uuid import uuid4

from .errors import InvalidStateError, NoCapacityError, TicketNotFoundError
from .model_config import ApiEndpointConfig, ModelConfig
from .models import CapacitySnapshot, DispatchTicket, ExecutionRequest, ReleaseReceipt, UsageMetrics, UsageRecord


class PoolStateStore:
    """SQLite-backed store for pool acquire/release operations."""

    def __init__(self, path: Path) -> None:
        self.path = path
        self.path.parent.mkdir(parents=True, exist_ok=True)
        self._init_db()

    def sync_from_model_config(self, config: ModelConfig) -> None:
        with self._tx() as conn:
            self._sync_config(conn, config)

    def acquire(
        self,
        *,
        config: ModelConfig,
        request: ExecutionRequest,
        lease_ms: int,
    ) -> tuple[DispatchTicket, ApiEndpointConfig]:
        with self._tx() as conn:
            self._sync_config(conn, config)
            ticket, endpoint = self._acquire_tx(conn, request=request, lease_ms=lease_ms)
            return ticket, endpoint

    def release(
        self,
        *,
        config: ModelConfig,
        ticket_id: str,
        result: str,
        reason: str,
        usage: UsageMetrics | None = None,
        result_summary: str | None = None,
        error_message: str | None = None,
    ) -> ReleaseReceipt:
        with self._tx() as conn:
            self._sync_config(conn, config)
            return self._release_tx(
                conn,
                ticket_id=ticket_id,
                result=result,
                reason=reason,
                usage=usage,
                result_summary=result_summary,
                error_message=error_message,
            )

    def capacity(self, config: ModelConfig) -> CapacitySnapshot:
        with self._tx() as conn:
            self._sync_config(conn, config)
            return self._capacity_tx(conn)

    def list_tickets(self, config: ModelConfig, node_id: str | None = None) -> list[DispatchTicket]:
        with self._tx() as conn:
            self._sync_config(conn, config)
            sql = (
                "SELECT ticket_id, request_id, node_id, api_id, lease_ms, acquired_at, expires_at "
                "FROM tickets"
            )
            params: tuple[object, ...] = ()
            if node_id is not None:
                sql += " WHERE node_id = ?"
                params = (node_id,)
            sql += " ORDER BY acquired_at ASC, ticket_id ASC"
            rows = conn.execute(sql, params).fetchall()
            return [
                DispatchTicket(
                    ticket_id=row["ticket_id"],
                    request_id=row["request_id"],
                    node_id=row["node_id"],
                    api_id=row["api_id"],
                    lease_ms=row["lease_ms"],
                    acquired_at=datetime.fromisoformat(row["acquired_at"]),
                    expires_at=datetime.fromisoformat(row["expires_at"]),
                )
                for row in rows
            ]

    def usage_records(self, config: ModelConfig, node_id: str | None = None) -> list[UsageRecord]:
        with self._tx() as conn:
            self._sync_config(conn, config)
            sql = (
                "SELECT ticket_id, request_id, node_id, api_id, released_at, usage_json, "
                "reason, result_summary FROM usage_records"
            )
            params: tuple[object, ...] = ()
            if node_id is not None:
                sql += " WHERE node_id = ?"
                params = (node_id,)
            sql += " ORDER BY released_at ASC, id ASC"
            rows = conn.execute(sql, params).fetchall()
            records: list[UsageRecord] = []
            for row in rows:
                usage = (
                    UsageMetrics.model_validate_json(row["usage_json"])
                    if row["usage_json"]
                    else None
                )
                records.append(
                    UsageRecord(
                        ticket_id=row["ticket_id"],
                        request_id=row["request_id"],
                        node_id=row["node_id"],
                        api_id=row["api_id"],
                        released_at=datetime.fromisoformat(row["released_at"]),
                        usage=usage,
                        reason=row["reason"],
                        result_summary=row["result_summary"],
                    )
                )
            return records

    def _connect(self) -> sqlite3.Connection:
        conn = sqlite3.connect(self.path, timeout=30.0, isolation_level=None)
        conn.row_factory = sqlite3.Row
        conn.execute("PRAGMA foreign_keys = ON")
        conn.execute("PRAGMA journal_mode = WAL")
        return conn

    def _tx(self):
        class _Tx:
            def __init__(self, outer: PoolStateStore) -> None:
                self.outer = outer
                self.conn: sqlite3.Connection | None = None

            def __enter__(self) -> sqlite3.Connection:
                self.conn = self.outer._connect()
                self.conn.execute("BEGIN IMMEDIATE")
                return self.conn

            def __exit__(self, exc_type, exc, tb) -> None:
                assert self.conn is not None
                if exc is None:
                    self.conn.commit()
                else:
                    self.conn.rollback()
                self.conn.close()

        return _Tx(self)

    def _init_db(self) -> None:
        with self._connect() as conn:
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS meta (
                  key TEXT PRIMARY KEY,
                  value TEXT NOT NULL
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS apis (
                  api_id TEXT PRIMARY KEY,
                  model_class TEXT NOT NULL,
                  base_url TEXT NOT NULL,
                  api_key TEXT NOT NULL,
                  max_concurrent INTEGER NOT NULL CHECK (max_concurrent > 0),
                  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
                  status TEXT NOT NULL CHECK (status IN ('available', 'leased', 'offline')),
                  lease_count INTEGER NOT NULL DEFAULT 0 CHECK (lease_count >= 0),
                  failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0),
                  last_error TEXT
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS tickets (
                  ticket_id TEXT PRIMARY KEY,
                  request_id TEXT NOT NULL,
                  node_id TEXT NOT NULL,
                  api_id TEXT NOT NULL,
                  lease_ms INTEGER NOT NULL CHECK (lease_ms > 0),
                  acquired_at TEXT NOT NULL,
                  expires_at TEXT NOT NULL,
                  FOREIGN KEY (api_id) REFERENCES apis(api_id)
                )
                """
            )
            conn.execute(
                """
                CREATE TABLE IF NOT EXISTS usage_records (
                  id INTEGER PRIMARY KEY AUTOINCREMENT,
                  ticket_id TEXT NOT NULL,
                  request_id TEXT NOT NULL,
                  node_id TEXT NOT NULL,
                  api_id TEXT NOT NULL,
                  released_at TEXT NOT NULL,
                  result TEXT NOT NULL CHECK (result IN ('success', 'failure')),
                  reason TEXT NOT NULL,
                  usage_json TEXT,
                  result_summary TEXT
                )
                """
            )
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_tickets_node_id ON tickets(node_id)"
            )
            conn.execute(
                "CREATE INDEX IF NOT EXISTS idx_usage_node_id ON usage_records(node_id)"
            )

    def _sync_config(self, conn: sqlite3.Connection, config: ModelConfig) -> None:
        self._set_meta(
            conn,
            "global_max_concurrent",
            "" if config.global_max_concurrent is None else str(config.global_max_concurrent),
        )
        self._set_meta(conn, "offline_failure_threshold", str(config.offline_failure_threshold))

        configured_ids: set[str] = set()
        for endpoint in config.apis:
            configured_ids.add(endpoint.api_id)
            row = conn.execute(
                "SELECT lease_count, failure_count FROM apis WHERE api_id = ?",
                (endpoint.api_id,),
            ).fetchone()
            if row is None:
                lease_count = 0
                failure_count = 0
                status = "available" if endpoint.enabled else "offline"
                conn.execute(
                    """
                    INSERT INTO apis (
                      api_id, model_class, base_url, api_key, max_concurrent,
                      enabled, status, lease_count, failure_count, last_error
                    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
                    """,
                    (
                        endpoint.api_id,
                        endpoint.model,
                        endpoint.base_url,
                        endpoint.api_key,
                        endpoint.max_concurrent,
                        1 if endpoint.enabled else 0,
                        status,
                        lease_count,
                        failure_count,
                        None,
                    ),
                )
            else:
                lease_count = int(row["lease_count"])
                failure_count = int(row["failure_count"])
                if not endpoint.enabled:
                    status = "offline"
                elif failure_count >= config.offline_failure_threshold:
                    status = "offline"
                elif lease_count >= endpoint.max_concurrent:
                    status = "leased"
                else:
                    status = "available"
                conn.execute(
                    """
                    UPDATE apis
                    SET model_class = ?, base_url = ?, api_key = ?, max_concurrent = ?,
                        enabled = ?, status = ?
                    WHERE api_id = ?
                    """,
                    (
                        endpoint.model,
                        endpoint.base_url,
                        endpoint.api_key,
                        endpoint.max_concurrent,
                        1 if endpoint.enabled else 0,
                        status,
                        endpoint.api_id,
                    ),
                )

        if configured_ids:
            placeholders = ", ".join("?" for _ in configured_ids)
            conn.execute(
                f"UPDATE apis SET enabled = 0, status = 'offline' WHERE api_id NOT IN ({placeholders})",
                tuple(sorted(configured_ids)),
            )
        else:
            conn.execute("UPDATE apis SET enabled = 0, status = 'offline'")

    def _acquire_tx(
        self,
        conn: sqlite3.Connection,
        *,
        request: ExecutionRequest,
        lease_ms: int,
    ) -> tuple[DispatchTicket, ApiEndpointConfig]:
        limit = self._get_global_limit(conn)
        if limit is not None:
            active_count = int(conn.execute("SELECT COUNT(*) AS c FROM tickets").fetchone()["c"])
            if active_count >= limit:
                raise NoCapacityError("global quota reached")

        row = conn.execute(
            """
            SELECT api_id, model_class, base_url, api_key, max_concurrent, lease_count
            FROM apis
            WHERE enabled = 1 AND status != 'offline' AND lease_count < max_concurrent
            ORDER BY lease_count ASC, api_id ASC
            LIMIT 1
            """
        ).fetchone()
        if row is None:
            raise NoCapacityError("no available API")

        api_id = row["api_id"]
        max_concurrent = int(row["max_concurrent"])
        lease_count = int(row["lease_count"])
        new_lease = lease_count + 1
        next_status = "leased" if new_lease >= max_concurrent else "available"
        conn.execute(
            "UPDATE apis SET lease_count = ?, status = ? WHERE api_id = ?",
            (new_lease, next_status, api_id),
        )

        now = datetime.utcnow()
        expires_at = now + timedelta(milliseconds=lease_ms)
        ticket = DispatchTicket(
            ticket_id=str(uuid4()),
            request_id=request.request_id,
            node_id=request.node_id,
            api_id=api_id,
            lease_ms=lease_ms,
            acquired_at=now,
            expires_at=expires_at,
        )
        conn.execute(
            """
            INSERT INTO tickets (
              ticket_id, request_id, node_id, api_id, lease_ms, acquired_at, expires_at
            ) VALUES (?, ?, ?, ?, ?, ?, ?)
            """,
            (
                ticket.ticket_id,
                ticket.request_id,
                ticket.node_id,
                ticket.api_id,
                ticket.lease_ms,
                ticket.acquired_at.isoformat(),
                ticket.expires_at.isoformat(),
            ),
        )

        endpoint = ApiEndpointConfig(
            api_id=api_id,
            model=row["model_class"],
            base_url=row["base_url"],
            api_key=row["api_key"],
            max_concurrent=max_concurrent,
            enabled=True,
        )
        return ticket, endpoint

    def _release_tx(
        self,
        conn: sqlite3.Connection,
        *,
        ticket_id: str,
        result: str,
        reason: str,
        usage: UsageMetrics | None,
        result_summary: str | None,
        error_message: str | None,
    ) -> ReleaseReceipt:
        row = conn.execute(
            """
            SELECT t.ticket_id, t.request_id, t.node_id, t.api_id,
                   a.lease_count, a.max_concurrent, a.failure_count, a.enabled
            FROM tickets AS t
            JOIN apis AS a ON a.api_id = t.api_id
            WHERE t.ticket_id = ?
            """,
            (ticket_id,),
        ).fetchone()
        if row is None:
            raise TicketNotFoundError(f"ticket not found: {ticket_id}")

        lease_count = int(row["lease_count"])
        if lease_count <= 0:
            raise InvalidStateError(f"lease count underflow on API: {row['api_id']}")

        new_lease = lease_count - 1
        threshold = self._get_offline_failure_threshold(conn)
        current_failure = int(row["failure_count"])
        if result == "success":
            next_failure = 0
            next_error = None
        else:
            next_failure = current_failure + 1
            next_error = error_message or result_summary or "request failed"

        if int(row["enabled"]) == 0:
            next_status = "offline"
        elif result == "failure" and next_failure >= threshold:
            next_status = "offline"
        elif new_lease >= int(row["max_concurrent"]):
            next_status = "leased"
        else:
            next_status = "available"

        conn.execute("DELETE FROM tickets WHERE ticket_id = ?", (ticket_id,))
        conn.execute(
            """
            UPDATE apis
            SET lease_count = ?, failure_count = ?, last_error = ?, status = ?
            WHERE api_id = ?
            """,
            (
                new_lease,
                next_failure,
                next_error,
                next_status,
                row["api_id"],
            ),
        )

        released_at = datetime.utcnow()
        usage_payload = usage.model_dump_json(exclude_none=True) if usage else None
        conn.execute(
            """
            INSERT INTO usage_records (
              ticket_id, request_id, node_id, api_id, released_at,
              result, reason, usage_json, result_summary
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
            """,
            (
                row["ticket_id"],
                row["request_id"],
                row["node_id"],
                row["api_id"],
                released_at.isoformat(),
                result,
                reason,
                usage_payload,
                result_summary,
            ),
        )

        return ReleaseReceipt(
            ticket_id=row["ticket_id"],
            request_id=row["request_id"],
            node_id=row["node_id"],
            api_id=row["api_id"],
            released_at=released_at,
            reason=reason,
            usage=usage,
            result_summary=result_summary,
        )

    def _capacity_tx(self, conn: sqlite3.Connection) -> CapacitySnapshot:
        row = conn.execute(
            """
            SELECT
              COUNT(*) AS total_apis,
              COALESCE(SUM(max_concurrent), 0) AS total_slots,
              COALESCE(SUM(lease_count), 0) AS leased_slots,
              COALESCE(
                SUM(
                  CASE
                    WHEN status != 'offline' THEN
                      CASE WHEN max_concurrent - lease_count > 0
                           THEN max_concurrent - lease_count
                           ELSE 0 END
                    ELSE 0
                  END
                ),
                0
              ) AS available_slots,
              COALESCE(SUM(CASE WHEN status = 'offline' THEN 1 ELSE 0 END), 0) AS offline_apis
            FROM apis
            """
        ).fetchone()
        ticket_count = int(conn.execute("SELECT COUNT(*) AS c FROM tickets").fetchone()["c"])
        global_limit = self._get_global_limit(conn)
        throttled = global_limit is not None and ticket_count >= global_limit
        return CapacitySnapshot(
            total_apis=int(row["total_apis"]),
            total_slots=int(row["total_slots"]),
            available_slots=int(row["available_slots"]),
            leased_slots=int(row["leased_slots"]),
            offline_apis=int(row["offline_apis"]),
            throttled=throttled,
        )

    def _set_meta(self, conn: sqlite3.Connection, key: str, value: str) -> None:
        conn.execute(
            "INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
            (key, value),
        )

    def _get_meta(self, conn: sqlite3.Connection, key: str) -> str | None:
        row = conn.execute("SELECT value FROM meta WHERE key = ?", (key,)).fetchone()
        if row is None:
            return None
        return str(row["value"])

    def _get_global_limit(self, conn: sqlite3.Connection) -> int | None:
        raw = self._get_meta(conn, "global_max_concurrent")
        if raw is None or raw == "":
            return None
        return int(raw)

    def _get_offline_failure_threshold(self, conn: sqlite3.Connection) -> int:
        raw = self._get_meta(conn, "offline_failure_threshold")
        if raw is None or raw == "":
            return 3
        return int(raw)
