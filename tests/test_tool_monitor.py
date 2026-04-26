from __future__ import annotations

import json
import unittest
from datetime import UTC, datetime, timedelta
from pathlib import Path
from tempfile import TemporaryDirectory

from openmate_agent.service import AgentCapabilityService
from openmate_agent.tool_monitor import ToolMonitorEvent, ToolMonitorService, ToolMonitorStore


class ToolMonitorTests(unittest.TestCase):
    def test_run_tool_records_before_and_after_for_all_outcomes(self) -> None:
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(workspace_root=tmp)

            success_result = service.run_tool(
                node_id="node-monitor",
                tool_name="write",
                payload={"path": "notes.txt", "content": "ok"},
                is_safe=True,
                is_read_only=True,
            )
            self.assertTrue(success_result.success)

            failure_result = service.run_tool(
                node_id="node-monitor",
                tool_name="read",
                payload={"path": "missing.txt"},
                is_safe=True,
                is_read_only=True,
            )
            self.assertFalse(failure_result.success)

            blocked_result = service.run_tool(
                node_id="node-monitor",
                tool_name="read",
                payload={"path": "notes.txt"},
                is_safe=False,
                is_read_only=False,
            )
            self.assertFalse(blocked_result.success)
            self.assertEqual(blocked_result.error_code, "TOOL_ACTION_BLOCKED")

            invalid_result = service.run_tool(
                node_id="",
                tool_name="read",
                payload={"path": "notes.txt"},
                is_safe=True,
                is_read_only=True,
            )
            self.assertFalse(invalid_result.success)
            self.assertEqual(invalid_result.error_code, "TOOL_ACTION_INVALID")

            monitor_path = Path(tmp) / ".openmate" / "runtime" / "tool_monitor.jsonl"
            rows = [
                json.loads(line)
                for line in monitor_path.read_text(encoding="utf-8").splitlines()
                if line.strip()
            ]
            self.assertEqual(len(rows), 8)
            phases = [row["phase"] for row in rows]
            self.assertEqual(phases.count("before"), 4)
            self.assertEqual(phases.count("after"), 4)

            after_rows = [row for row in rows if row["phase"] == "after"]
            self.assertEqual(after_rows[0]["success"], True)
            self.assertEqual(after_rows[1]["success"], False)
            self.assertEqual(after_rows[2]["error_code"], "TOOL_ACTION_BLOCKED")
            self.assertEqual(after_rows[3]["error_code"], "TOOL_ACTION_INVALID")

    def test_store_and_service_filters_and_summary(self) -> None:
        with TemporaryDirectory() as tmp:
            store = ToolMonitorStore(Path(tmp) / "tool_monitor.jsonl")
            service = ToolMonitorService(workspace_root=tmp, store=store)
            now = datetime.now(UTC)
            durations = [10, 20, 30, 40, 100]
            for idx, duration in enumerate(durations):
                store.append(
                    ToolMonitorEvent(
                        event_id=f"before-{idx}",
                        phase="before",
                        ts=now - timedelta(minutes=1),
                        node_id="node-1",
                        tool_name="read",
                        source="cli",
                        is_safe=True,
                        is_read_only=True,
                    )
                )
                store.append(
                    ToolMonitorEvent(
                        event_id=f"after-{idx}",
                        phase="after",
                        ts=now - timedelta(minutes=1),
                        node_id="node-1",
                        tool_name="read",
                        source="cli",
                        is_safe=True,
                        is_read_only=True,
                        success=(idx % 2 == 0),
                        duration_ms=duration,
                    )
                )
            store.append(
                ToolMonitorEvent(
                    event_id="after-http",
                    phase="after",
                    ts=now - timedelta(minutes=1),
                    node_id="node-2",
                    tool_name="write",
                    source="http",
                    is_safe=True,
                    is_read_only=False,
                    success=True,
                    duration_ms=8,
                )
            )

            listed = service.list_events(tool_name="read", source="cli", success=True, limit=10)
            self.assertEqual(len(listed), 3)
            self.assertTrue(all(item.phase == "after" for item in listed))
            self.assertTrue(all(item.success for item in listed))

            summary = service.summarize(tool_name="read", source="cli", window_minutes=10)
            self.assertEqual(len(summary), 1)
            row = summary[0]
            self.assertEqual(row["tool_name"], "read")
            self.assertEqual(row["count"], 5)
            self.assertAlmostEqual(float(row["success_rate"]), 0.6, places=6)
            self.assertAlmostEqual(float(row["avg_duration_ms"]), 40.0, places=6)
            self.assertEqual(float(row["p95_duration_ms"]), 100.0)


if __name__ == "__main__":
    unittest.main()
