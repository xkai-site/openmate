import json
import unittest
from datetime import UTC, datetime

from openmate_agent.session_writer import SessionWriterError, VosSessionWriter


def _session_payload(session_id: str, node_id: str) -> str:
    now = datetime.now(UTC).isoformat()
    return json.dumps(
        {
            "id": session_id,
            "node_id": node_id,
            "status": "active",
            "created_at": now,
            "updated_at": now,
            "last_seq": 0,
        }
    )


class _StubVosSessionWriter(VosSessionWriter):
    def __init__(self) -> None:
        super().__init__(workspace_root=".")
        self.commands: list[list[str]] = []
        self._sessions: dict[str, str] = {}

    def _run_command(self, command: list[str]) -> str:  # type: ignore[override]
        self.commands.append(command)
        if command[:2] == ["session", "get"]:
            session_id = command[3]
            node_id = self._sessions.get(session_id)
            if node_id is None:
                raise SessionWriterError(f"session not found: {session_id}")
            return _session_payload(session_id, node_id)

        if command[:2] == ["session", "create"]:
            node_id = command[3]
            if "--session-id" in command:
                session_id = command[command.index("--session-id") + 1]
            else:
                session_id = f"auto-{node_id}"
            self._sessions[session_id] = node_id
            return _session_payload(session_id, node_id)

        raise SessionWriterError(f"unsupported command: {command}")


class VosSessionWriterTests(unittest.TestCase):
    def test_ensure_session_reuses_existing_session(self) -> None:
        writer = _StubVosSessionWriter()
        writer._sessions["session-1"] = "node-1"

        resolved = writer.ensure_session(node_id="node-1", session_id="session-1")

        self.assertEqual(resolved, "session-1")
        self.assertEqual(writer.commands[0][:2], ["session", "get"])
        self.assertTrue(all(command[:2] != ["session", "create"] for command in writer.commands))

    def test_ensure_session_creates_when_session_not_found(self) -> None:
        writer = _StubVosSessionWriter()

        resolved = writer.ensure_session(node_id="node-2", session_id="session-2")

        self.assertEqual(resolved, "session-2")
        self.assertEqual(writer.commands[0][:2], ["session", "get"])
        self.assertEqual(writer.commands[1][:2], ["session", "create"])

    def test_ensure_session_rejects_node_mismatch(self) -> None:
        writer = _StubVosSessionWriter()
        writer._sessions["session-3"] = "node-A"

        with self.assertRaises(SessionWriterError):
            writer.ensure_session(node_id="node-B", session_id="session-3")

    def test_ensure_session_creates_when_session_id_not_provided(self) -> None:
        writer = _StubVosSessionWriter()

        resolved = writer.ensure_session(node_id="node-4")

        self.assertEqual(resolved, "auto-node-4")
        self.assertEqual(len(writer.commands), 1)
        self.assertEqual(writer.commands[0][:2], ["session", "create"])


if __name__ == "__main__":
    unittest.main()
