from __future__ import annotations

import unittest
from datetime import UTC, datetime
from pathlib import Path
from tempfile import TemporaryDirectory

from openmate_agent.skill_monitor import SkillMonitorService, SkillMonitorStore


class SkillMonitorTests(unittest.TestCase):
    def test_record_and_query(self) -> None:
        with TemporaryDirectory() as tmp:
            store = SkillMonitorStore(Path(tmp) / "skill_monitor.jsonl")
            service = SkillMonitorService(workspace_root=tmp, store=store)
            now = datetime.now(UTC)
            service.record_before(
                node_id="node-1",
                source="model",
                skill_name="alpha",
                skill_path="/tmp/alpha/SKILL.md",
                ts=now,
            )
            service.record_after(
                node_id="node-1",
                source="model",
                skill_name="alpha",
                skill_path="/tmp/alpha/SKILL.md",
                success=False,
                error_code="SKILL_READ_FAILED",
                error="missing",
                duration_ms=11,
                ts=now,
            )
            events = service.list_events(skill_name="alpha", success=False, limit=10)
            self.assertEqual(len(events), 1)
            self.assertEqual(events[0].phase, "after")
            self.assertEqual(events[0].error_code, "SKILL_READ_FAILED")
            summary = service.summarize(skill_name="alpha", limit=10)
            self.assertEqual(len(summary), 1)
            self.assertEqual(summary[0]["skill_name"], "alpha")


if __name__ == "__main__":
    unittest.main()
