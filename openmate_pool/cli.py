"""CLI for OpenMate API pool."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from .errors import PoolError
from .model_config import load_model_config
from .models import ExecutionRequest, UsageMetrics
from .store import PoolStateStore


def _dump(data: Any) -> None:
    def _default(value: Any) -> Any:
        if hasattr(value, "model_dump"):
            return value.model_dump(mode="json")
        raise TypeError(f"not json serializable: {type(value)}")

    print(json.dumps(data, ensure_ascii=False, indent=2, default=_default))


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="pool",
        description="OpenMate API pool CLI. All commands print JSON to stdout.",
    )
    parser.add_argument(
        "--db-file",
        default=".pool_state.db",
        help="SQLite state database path",
    )
    parser.add_argument(
        "--model-config",
        default="model.json",
        help="Model config JSON path",
    )

    subparsers = parser.add_subparsers(dest="command", required=True)

    acquire = subparsers.add_parser(
        "get",
        help="Get one API lease",
        description=(
            "Input: request_id, node_id, optional timeout_ms/lease_ms.\n"
            "Output JSON: { ticket, endpoint }."
        ),
        formatter_class=argparse.RawTextHelpFormatter,
    )
    acquire.add_argument("--request-id", required=True, help="Unique request ID from caller")
    acquire.add_argument("--node-id", required=True, help="Node ID for tracing")
    acquire.add_argument("--timeout-ms", type=int, help="Caller timeout hint (ms)")
    acquire.add_argument("--lease-ms", type=int, default=30_000, help="Lease TTL in milliseconds")

    release = subparsers.add_parser(
        "done",
        help="Release lease by ticket",
        description=(
            "Input: ticket_id + result(success|failure), optional usage fields.\n"
            "Output JSON: release receipt."
        ),
        formatter_class=argparse.RawTextHelpFormatter,
    )
    release.add_argument("--ticket-id", required=True, help="Ticket ID returned by `get`")
    release.add_argument(
        "--result",
        choices=["success", "failure"],
        required=True,
        help="Execution result for health update",
    )
    release.add_argument("--result-summary", help="Optional short summary")
    release.add_argument("--error-message", help="Error detail when result=failure")
    release.add_argument("--reason", default="completed", help="Release reason label")
    release.add_argument("--prompt-tokens", type=int, help="Prompt token count")
    release.add_argument("--completion-tokens", type=int, help="Completion token count")
    release.add_argument("--total-tokens", type=int, help="Total token count")
    release.add_argument("--latency-ms", type=int, help="Latency in milliseconds")
    release.add_argument("--cost-usd", type=float, help="Cost in USD")

    subparsers.add_parser(
        "cap",
        help="Show capacity snapshot",
        description="Output JSON: total_apis, total_slots, available_slots, leased_slots, offline_apis, throttled.",
    )

    list_tickets = subparsers.add_parser(
        "tickets",
        help="List active leases",
        description="Input: optional node_id filter. Output JSON: active ticket list.",
    )
    list_tickets.add_argument("--node-id", help="Filter by node ID")

    usage_records = subparsers.add_parser(
        "usage",
        help="List usage records",
        description="Input: optional node_id/limit. Output JSON: usage record list.",
    )
    usage_records.add_argument("--node-id", help="Filter by node ID")
    usage_records.add_argument("--limit", type=int, help="Return only last N records")

    subparsers.add_parser(
        "sync",
        help="Sync resources from model config",
        description="Output JSON: { synced: true, capacity: ... }",
    )

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    args = parser.parse_args(argv)
    command = args.command

    try:
        store = PoolStateStore(path=Path(args.db_file))
        config = load_model_config(Path(args.model_config))

        if command == "get":
            request = ExecutionRequest(
                request_id=args.request_id,
                node_id=args.node_id,
                timeout_ms=args.timeout_ms,
            )
            ticket, endpoint = store.acquire(
                config=config,
                request=request,
                lease_ms=args.lease_ms,
            )
            _dump(
                {
                    "ticket": ticket,
                    "endpoint": endpoint,
                }
            )
            return 0

        if command == "done":
            usage_data = {
                "prompt_tokens": args.prompt_tokens,
                "completion_tokens": args.completion_tokens,
                "total_tokens": args.total_tokens,
                "latency_ms": args.latency_ms,
                "cost_usd": args.cost_usd,
            }
            compact_usage = {key: value for key, value in usage_data.items() if value is not None}
            usage = UsageMetrics(**compact_usage) if compact_usage else None
            receipt = store.release(
                config=config,
                ticket_id=args.ticket_id,
                result=args.result,
                error_message=args.error_message,
                usage=usage,
                result_summary=args.result_summary,
                reason=args.reason,
            )
            _dump(receipt)
            return 0

        if command == "cap":
            _dump(store.capacity(config))
            return 0

        if command == "tickets":
            _dump(store.list_tickets(config, node_id=args.node_id))
            return 0

        if command == "usage":
            records = store.usage_records(config, node_id=args.node_id)
            if args.limit is not None:
                records = records[-args.limit :]
            _dump(records)
            return 0

        if command == "sync":
            store.sync_from_model_config(config)
            _dump(
                {
                    "synced": True,
                    "capacity": store.capacity(config),
                }
            )
            return 0

        parser.print_help()
        return 1
    except (PoolError, ValidationError, ValueError, FileNotFoundError) as exc:
        print(str(exc), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
