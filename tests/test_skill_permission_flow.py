from __future__ import annotations

import os
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest import mock

from openmate_agent.agent_services import ExecutionAgentService
from openmate_agent.models import (
    AgentInput,
    ApprovalDecision,
    Build,
    ContextBundle,
    SkillBundle,
    SkillSpec,
    ToolBundle,
    UserSkillAllow,
)


class _NoopPermissionStore:
    def __init__(self, records: list[UserSkillAllow]) -> None:
        self._records = records
        self.upserts: list[UserSkillAllow] = []

    def list_user_skill_allows(self) -> list[UserSkillAllow]:
        return list(self._records)

    def upsert_user_skill_allow(self, *, skill_name: str, skill_path: str, skill_mtime: str, allowed_roots: list[str]) -> None:
        self.upserts.append(
            UserSkillAllow(
                skill_name=skill_name,
                skill_path=skill_path,
                skill_mtime=skill_mtime,
                allowed_roots=allowed_roots,
            )
        )


class SkillPermissionTests(unittest.TestCase):
    def _service(self, tmp: str, store: _NoopPermissionStore, resolver=None) -> ExecutionAgentService:
        return ExecutionAgentService(
            build_pipeline=mock.Mock(),
            execution_orchestrator=mock.Mock(),
            approval_resolver=resolver,
            permission_store=store,  # type: ignore[arg-type]
            workspace_root=tmp,
        )

    def _agent_input(self, skill: SkillSpec) -> AgentInput:
        return AgentInput(
            node_id="node-1",
            context=ContextBundle(node_id="node-1", payload="{}"),
            tools=ToolBundle(node_id="node-1", tools=[]),
            skills=SkillBundle(node_id="node-1", skills=[skill]),
            prompt="p",
        )

    def test_relative_path_resolves_with_workspace_and_allow_record(self) -> None:
        with TemporaryDirectory() as tmp:
            skill_file = Path(tmp, "skills", "a.md")
            skill_file.parent.mkdir(parents=True, exist_ok=True)
            skill_file.write_text("hello", encoding="utf-8")
            mtime = os.stat(skill_file).st_mtime
            mtime_text = __import__("datetime").datetime.fromtimestamp(mtime, tz=__import__("datetime").timezone.utc).isoformat().replace("+00:00", "Z")
            store = _NoopPermissionStore([
                UserSkillAllow(
                    skill_name="skill.alpha",
                    skill_path=str(skill_file.resolve()),
                    skill_mtime=mtime_text,
                    allowed_roots=[str(Path(tmp).resolve())],
                )
            ])
            service = self._service(tmp, store)
            out = service._apply_skill_permissions(build=Build(node_id="node-1"), agent_input=self._agent_input(SkillSpec(name="skill.alpha", config={"path": "skills/a.md"})))
            self.assertEqual(len(out.skills.skills), 1)
            self.assertEqual(out.skills.skills[0].config.get("content"), "hello")

    def test_mtime_mismatch_triggers_approval_and_allow_once(self) -> None:
        with TemporaryDirectory() as tmp:
            skill_file = Path(tmp, "skills", "a.md")
            skill_file.parent.mkdir(parents=True, exist_ok=True)
            skill_file.write_text("hello", encoding="utf-8")
            store = _NoopPermissionStore([
                UserSkillAllow(
                    skill_name="skill.alpha",
                    skill_path=str(skill_file.resolve()),
                    skill_mtime="2000-01-01T00:00:00Z",
                    allowed_roots=[str(Path(tmp).resolve())],
                )
            ])
            calls: list[object] = []

            def resolver(req):
                calls.append(req)
                return ApprovalDecision(choice="allow_once")

            service = self._service(tmp, store, resolver=resolver)
            out = service._apply_skill_permissions(build=Build(node_id="node-1"), agent_input=self._agent_input(SkillSpec(name="skill.alpha", config={"path": str(skill_file)})))
            self.assertEqual(len(out.skills.skills), 1)
            self.assertEqual(len(calls), 1)
            self.assertEqual(calls[0].payload.get("reason_code"), "MTIME_MISMATCH")

    def test_no_path_keeps_original_behavior(self) -> None:
        with TemporaryDirectory() as tmp:
            store = _NoopPermissionStore([])
            service = self._service(tmp, store)
            skill = SkillSpec(name="skill.alpha", config={})
            out = service._apply_skill_permissions(build=Build(node_id="node-1"), agent_input=self._agent_input(skill))
            self.assertEqual(len(out.skills.skills), 1)


if __name__ == "__main__":
    unittest.main()
