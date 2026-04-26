import unittest
from pathlib import Path
from unittest import mock

from openmate_agent.vos_cli import resolve_node_tool_context


class VosCliTests(unittest.TestCase):
    def test_resolve_node_tool_context_parses_topic_workspace(self) -> None:
        with mock.patch("openmate_agent.vos_cli.run_vos_cli") as vos_mock:
            vos_mock.return_value = (
                '{"node_id":"node-1","parent_id":"root-1","node_name":"Node One","topic_id":"topic-1","topic_workspace":"D:/workspace/topic-1"}'
            )
            context = resolve_node_tool_context(workspace_root=Path("."), node_id="node-1")

        self.assertEqual(context.parent_id, "root-1")
        self.assertEqual(context.node_name, "Node One")
        self.assertEqual(context.topic_id, "topic-1")
        self.assertEqual(context.topic_workspace, "D:/workspace/topic-1")


if __name__ == "__main__":
    unittest.main()
