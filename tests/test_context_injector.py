from __future__ import annotations

import json
import unittest
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch

from openmate_agent.context_reader import ContextReaderError, ContextSnapshotRecord, VosContextReader
from openmate_agent.context_injector import VosContextInjector
from openmate_agent.models import ContextBundle
from openmate_agent.service import AgentCapabilityService


class VosContextReaderTests(unittest.TestCase):
    def test_snapshot_parses_context_payload(self) -> None:
        payload = _snapshot_payload(node_id="node-1")
        with TemporaryDirectory() as tmp:
            gateway = VosContextReader(
                workspace_root=tmp,
                state_file=Path(tmp, ".vos_state.json"),
                session_db_file=Path(tmp, ".vos_sessions.db"),
                binary_path=Path(tmp, "vos.exe"),
            )
            with patch("openmate_agent.context_reader.ensure_vos_binary", return_value=Path(tmp, "vos.exe")):
                with patch("openmate_agent.context_reader.subprocess.run") as mock_run:
                    mock_run.return_value.returncode = 0
                    mock_run.return_value.stdout = json.dumps(payload)
                    mock_run.return_value.stderr = ""
                    snapshot = gateway.snapshot(node_id="node-1")

        self.assertEqual(snapshot.node_id, "node-1")
        self.assertEqual(snapshot.user_memory, {"uid": "u-1"})
        self.assertEqual(len(snapshot.session_history), 1)
        self.assertEqual(snapshot.session_history[0].events[0].seq, 1)

    def test_snapshot_raises_when_stdout_empty(self) -> None:
        with TemporaryDirectory() as tmp:
            gateway = VosContextReader(
                workspace_root=tmp,
                state_file=Path(tmp, ".vos_state.json"),
                session_db_file=Path(tmp, ".vos_sessions.db"),
                binary_path=Path(tmp, "vos.exe"),
            )
            with patch("openmate_agent.context_reader.ensure_vos_binary", return_value=Path(tmp, "vos.exe")):
                with patch("openmate_agent.context_reader.subprocess.run") as mock_run:
                    mock_run.return_value.returncode = 0
                    mock_run.return_value.stdout = ""
                    mock_run.return_value.stderr = ""
                    with self.assertRaises(ContextReaderError):
                        gateway.snapshot(node_id="node-1")

    def test_snapshot_runs_subprocess_with_utf8_decode(self) -> None:
        payload = _snapshot_payload(node_id="node-encoding")
        with TemporaryDirectory() as tmp:
            gateway = VosContextReader(
                workspace_root=tmp,
                state_file=Path(tmp, ".vos_state.json"),
                session_db_file=Path(tmp, ".vos_sessions.db"),
                binary_path=Path(tmp, "vos.exe"),
            )
            with patch("openmate_agent.context_reader.ensure_vos_binary", return_value=Path(tmp, "vos.exe")):
                with patch("openmate_agent.context_reader.subprocess.run") as mock_run:
                    mock_run.return_value.returncode = 0
                    mock_run.return_value.stdout = json.dumps(payload, ensure_ascii=False)
                    mock_run.return_value.stderr = ""

                    gateway.snapshot(node_id="node-encoding")

                    kwargs = mock_run.call_args.kwargs
                    self.assertTrue(kwargs["text"])
                    self.assertEqual(kwargs["encoding"], "utf-8")
                    self.assertEqual(kwargs["errors"], "replace")


class VosContextInjectorTests(unittest.TestCase):
    def test_inject_builds_single_context_payload(self) -> None:
        snapshot = ContextSnapshotRecord.model_validate(_snapshot_payload(node_id="node-2"))

        class Reader:
            def snapshot(self, node_id: str) -> ContextSnapshotRecord:
                self.called_node_id = node_id
                return snapshot

        reader = Reader()
        injector = VosContextInjector(reader=reader)  # type: ignore[arg-type]
        bundle = injector.inject("node-2")

        self.assertIsInstance(bundle, ContextBundle)
        self.assertEqual(bundle.node_id, "node-2")
        self.assertIn('"user_memory"', bundle.payload)
        self.assertIn('"topic_memory"', bundle.payload)
        self.assertIn('"process_contexts"', bundle.payload)
        self.assertIn('"session_history"', bundle.payload)


class AgentServiceContextSelectionTests(unittest.TestCase):
    def test_service_uses_vos_context_injector_when_vos_paths_present(self) -> None:
        service = AgentCapabilityService(
            gateway=_FakeGateway(),
            workspace_root=".",
            vos_state_file=".vos_state.json",
            vos_session_db_file=".vos_sessions.db",
            vos_binary_path="vos.exe",
        )
        self.assertIsInstance(service._context_injector, VosContextInjector)  # type: ignore[attr-defined]

    def test_service_prefers_explicit_context_injector(self) -> None:
        class ExplicitContextInjector:
            def inject(self, node_id: str) -> ContextBundle:
                return ContextBundle(node_id=node_id, payload='{"custom":"context"}')

        explicit = ExplicitContextInjector()
        service = AgentCapabilityService(
            gateway=_FakeGateway(),
            context_injector=explicit,  # type: ignore[arg-type]
            vos_state_file=".vos_state.json",
            vos_session_db_file=".vos_sessions.db",
            vos_binary_path="vos.exe",
        )
        self.assertIs(service._context_injector, explicit)  # type: ignore[attr-defined]


class _FakeGateway:
    def invoke(self, request):  # type: ignore[no-untyped-def]
        _ = request
        raise RuntimeError("not used in this test")


def _snapshot_payload(node_id: str) -> dict[str, object]:
    return {
        "node_id": node_id,
        "user_memory": {"uid": "u-1"},
        "topic_memory": {"summary": "topic-s"},
        "node_memory": {"summary": "node-s"},
        "global_index": {"records": ["r1"]},
        "session_history": [
            {
                "session": {
                    "id": "session-1",
                    "node_id": node_id,
                    "status": "active",
                    "created_at": "2026-04-15T00:00:00Z",
                    "updated_at": "2026-04-15T00:00:01Z",
                    "last_seq": 1,
                },
                "events": [
                    {
                        "id": "event-1",
                        "session_id": "session-1",
                        "seq": 1,
                        "item_type": "message",
                        "provider_item_id": None,
                        "role": "assistant",
                        "call_id": None,
                        "payload_json": {"text": "hello"},
                        "created_at": "2026-04-15T00:00:01Z",
                    }
                ],
            }
        ],
    }


if __name__ == "__main__":
    unittest.main()

