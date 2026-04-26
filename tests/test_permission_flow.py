from __future__ import annotations

import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

from openmate_agent.models import ApprovalDecision, PermissionRule
from openmate_agent.service import AgentCapabilityService


class _FakePermissionStore:
    def __init__(self) -> None:
        self.topic_rules: dict[str, list[PermissionRule]] = {}
        self.skill_allows: list[str] = []
        self.add_calls: list[tuple[str, str, str]] = []

    def list_topic_tool_allows(self, *, topic_id: str) -> list[PermissionRule]:
        return list(self.topic_rules.get(topic_id, []))

    def add_topic_tool_allow(self, *, topic_id: str, tool_name: str, dir_prefix: str) -> None:
        self.add_calls.append((topic_id, tool_name, dir_prefix))
        self.topic_rules.setdefault(topic_id, []).append(
            PermissionRule(tool_name=tool_name, normalized_dir_prefix=dir_prefix)
        )

    def list_user_skill_allows(self) -> list[str]:
        return list(self.skill_allows)

    def add_user_skill_allow(self, *, skill_name: str) -> None:
        if skill_name not in self.skill_allows:
            self.skill_allows.append(skill_name)


class PermissionFlowTests(unittest.TestCase):
    def test_allow_and_remember_executes_and_persists_topic_rule(self) -> None:
        store = _FakePermissionStore()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                workspace_root=tmp,
                permission_store=store,  # type: ignore[arg-type]
                approval_resolver=lambda _req: ApprovalDecision(choice="allow_and_remember"),
            )
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Permission",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                result = service.run_tool(
                    node_id="node-1",
                    tool_name="write",
                    payload={"path": "notes/a.txt", "content": "ok"},
                    source="model",
                )
                self.assertTrue(Path(tmp, "notes", "a.txt").exists())

        self.assertTrue(result.success)
        self.assertGreaterEqual(len(store.add_calls), 1)
        self.assertEqual(store.add_calls[0][0], "topic-1")
        self.assertEqual(store.add_calls[0][1], "write")

    def test_deny_does_not_execute(self) -> None:
        store = _FakePermissionStore()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                workspace_root=tmp,
                permission_store=store,  # type: ignore[arg-type]
                approval_resolver=lambda _req: ApprovalDecision(choice="deny"),
            )
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Permission",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                result = service.run_tool(
                    node_id="node-1",
                    tool_name="write",
                    payload={"path": "notes/deny.txt", "content": "blocked"},
                    source="model",
                )
                self.assertFalse(Path(tmp, "notes", "deny.txt").exists())
        self.assertFalse(result.success)
        self.assertEqual(result.error_code, "TOOL_ACTION_BLOCKED")

    def test_supplement_does_not_execute(self) -> None:
        store = _FakePermissionStore()
        with TemporaryDirectory() as tmp:
            service = AgentCapabilityService(
                workspace_root=tmp,
                permission_store=store,  # type: ignore[arg-type]
                approval_resolver=lambda _req: ApprovalDecision(choice="supplement", supplement_text="use safer path"),
            )
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Permission",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                result = service.run_tool(
                    node_id="node-1",
                    tool_name="write",
                    payload={"path": "notes/supplement.txt", "content": "blocked"},
                    source="model",
                )
                self.assertFalse(Path(tmp, "notes", "supplement.txt").exists())
        self.assertFalse(result.success)
        self.assertEqual(result.error_code, "TOOL_ACTION_SUPPLEMENT")
        self.assertIn("use safer path", result.output)

    def test_topic_rule_prefix_match_allows_without_new_confirmation(self) -> None:
        store = _FakePermissionStore()
        with TemporaryDirectory() as tmp:
            allowed_dir = str(Path(tmp, "notes").resolve()).replace("\\", "/")
            store.topic_rules["topic-1"] = [PermissionRule(tool_name="write", normalized_dir_prefix=allowed_dir)]
            resolver_calls: list[str] = []

            def resolver(_req: object) -> ApprovalDecision:
                resolver_calls.append("called")
                return ApprovalDecision(choice="deny")

            service = AgentCapabilityService(
                workspace_root=tmp,
                permission_store=store,  # type: ignore[arg-type]
                approval_resolver=resolver,
            )
            with mock.patch("openmate_agent.tool_runtime.resolve_node_tool_context") as context_mock:
                context_mock.return_value = mock.Mock(
                    parent_id=None,
                    node_name="Node Permission",
                    topic_id="topic-1",
                    topic_workspace=tmp,
                )
                result = service.run_tool(
                    node_id="node-1",
                    tool_name="write",
                    payload={"path": "notes/already-allowed.txt", "content": "ok"},
                    source="model",
                )
        self.assertTrue(result.success)
        self.assertEqual(resolver_calls, [])


if __name__ == "__main__":
    unittest.main()
