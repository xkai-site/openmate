import unittest
from pathlib import Path
from unittest import mock

from openmate_agent.permission_store import VosPermissionStore


class PermissionStoreTests(unittest.TestCase):
    def test_record_topic_policy_audit_passes_requested_capabilities_and_rule(self) -> None:
        store = VosPermissionStore(workspace_root=Path("."))
        with mock.patch("openmate_agent.permission_store.run_vos_cli") as vos_mock:
            vos_mock.return_value = "{}"
            store.record_topic_policy_audit(
                topic_id="topic-1",
                node_id="node-1",
                tool_name="write",
                decision="allow",
                risk_tags=["path"],
                requested_capabilities={"write_file": {"path": "D:/workspace/file.txt"}},
                matched_rule_id="rule-1",
            )
        command = vos_mock.call_args.kwargs["command"]
        self.assertIn("--requested-capabilities-json", command)
        self.assertIn("--matched-rule-id", command)
        self.assertIn("rule-1", command)


if __name__ == "__main__":
    unittest.main()

