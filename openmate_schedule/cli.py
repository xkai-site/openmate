"""CLI for the schedule queue module."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

from pydantic import ValidationError

from .models import TopicSnapshot
from .scheduler import plan_topic_dispatch


def _dump(data: Any) -> None:
    def _default(value: Any) -> Any:
        if hasattr(value, "model_dump"):
            return value.model_dump(mode="json")
        raise TypeError(f"not json serializable: {type(value)}")

    print(json.dumps(data, ensure_ascii=False, indent=2, default=_default))


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="schedule",
        description="OpenMate schedule queue CLI. All commands print JSON to stdout.",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    plan = subparsers.add_parser(
        "plan",
        help="Build one topic dispatch plan from a topic snapshot JSON file.",
        description=(
            "Input JSON schema: TopicSnapshot.\n"
            "Output JSON: active priority, current node and dispatch candidate ids."
        ),
        formatter_class=argparse.RawTextHelpFormatter,
    )
    plan.add_argument("--input-file", required=True, help="Path to topic snapshot JSON.")
    plan.add_argument(
        "--available-slots",
        type=int,
        default=1,
        help="Available agent slots for this topic.",
    )

    return parser


def main(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    args = parser.parse_args(argv)

    try:
        if args.command == "plan":
            payload = json.loads(Path(args.input_file).read_text(encoding="utf-8"))
            topic = TopicSnapshot.model_validate(payload)
            plan = plan_topic_dispatch(topic, available_slots=args.available_slots)
            _dump(plan)
            return 0

        parser.print_help()
        return 1
    except (ValidationError, ValueError, FileNotFoundError, json.JSONDecodeError) as exc:
        print(str(exc), file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
