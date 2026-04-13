from __future__ import annotations

import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path

from openmate_schedule.cli import main
from openmate_schedule.models import (
    NodePriority,
    NodeStatus,
    TopicNode,
    TopicQueueLevel,
    TopicRuntimeState,
    TopicSnapshot,
)
from openmate_schedule.scheduler import plan_topic_dispatch


class ScheduleCliTests(unittest.TestCase):
    def test_help_available(self) -> None:
        with self.assertRaises(SystemExit) as ctx:
            main(["--help"])
        self.assertEqual(ctx.exception.code, 0)

    def test_plan_command_outputs_dispatch_plan(self) -> None:
        snapshot = _make_topic_snapshot()
        with tempfile.TemporaryDirectory() as tmpdir:
            input_file = Path(tmpdir) / "topic.json"
            input_file.write_text(snapshot.model_dump_json(indent=2), encoding="utf-8")

            stdout = StringIO()
            with redirect_stdout(stdout):
                exit_code = main(
                    [
                        "plan",
                        "--input-file",
                        str(input_file),
                        "--available-slots",
                        "2",
                    ]
                )

        self.assertEqual(exit_code, 0)
        payload = json.loads(stdout.getvalue())
        self.assertEqual(payload["active_priority"], "now")
        self.assertEqual(payload["current_node_id"], "node-a")
        self.assertEqual(payload["dispatch_node_ids"], ["node-a", "node-b"])


class SchedulePlannerTests(unittest.TestCase):
    def test_continuation_first_keeps_current_node(self) -> None:
        plan = plan_topic_dispatch(_make_topic_snapshot(), available_slots=2)
        self.assertEqual(plan.current_node_id, "node-a")
        self.assertEqual(plan.dispatch_node_ids, ["node-a", "node-b"])
        self.assertFalse(plan.stalled)

    def test_fallback_uses_last_worked_node(self) -> None:
        snapshot = _make_topic_snapshot(current_node_id="node-c", last_worked_node_id="node-b")
        plan = plan_topic_dispatch(snapshot, available_slots=1)
        self.assertEqual(plan.current_node_id, "node-b")
        self.assertEqual(plan.dispatch_node_ids, ["node-b"])

    def test_highest_priority_layer_can_stall(self) -> None:
        snapshot = _make_topic_snapshot(
            node_a_status=NodeStatus.BLOCKED,
            node_b_status=NodeStatus.RETRY_COOLDOWN,
        )
        plan = plan_topic_dispatch(snapshot, available_slots=2)
        self.assertTrue(plan.stalled)
        self.assertEqual(plan.active_priority, "now")
        self.assertEqual(plan.dispatch_node_ids, [])


def _make_topic_snapshot(
    *,
    current_node_id: str | None = "node-a",
    last_worked_node_id: str | None = "node-a",
    node_a_status: NodeStatus = NodeStatus.READY,
    node_b_status: NodeStatus = NodeStatus.READY,
) -> TopicSnapshot:
    return TopicSnapshot(
        topic_id="topic-1",
        queue_level=TopicQueueLevel.L0,
        nodes=[
            TopicNode(
                node_id="node-a",
                name="collect context",
                priority=NodePriority(label="now", rank=0),
                status=node_a_status,
            ),
            TopicNode(
                node_id="node-b",
                name="draft answer",
                priority=NodePriority(label="now", rank=0),
                status=node_b_status,
            ),
            TopicNode(
                node_id="node-c",
                name="cleanup",
                priority=NodePriority(label="later", rank=1),
                status=NodeStatus.READY,
            ),
        ],
        runtime=TopicRuntimeState(
            topic_id="topic-1",
            current_node_id=current_node_id,
            running_node_ids=[],
            last_worked_node_id=last_worked_node_id,
            switch_count=1,
        ),
    )


if __name__ == "__main__":
    unittest.main()
