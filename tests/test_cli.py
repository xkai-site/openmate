from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

from openmate_pool.cli import main
from openmate_pool.model_config import load_model_config
from openmate_pool.store import PoolStateStore


class CliTestCase(unittest.TestCase):
    @staticmethod
    def _write_model_config(path: Path, *, threshold: int = 3) -> None:
        path.write_text(
            json.dumps(
                {
                    "global_max_concurrent": 2,
                    "offline_failure_threshold": threshold,
                    "apis": [
                        {
                            "api_id": "api-1",
                            "model": "gpt-4.1",
                            "base_url": "https://api.openai.com/v1",
                            "api_key": "sk-test",
                            "max_concurrent": 1,
                            "enabled": True,
                        }
                    ],
                }
            ),
            encoding="utf-8",
        )

    def test_help_available(self) -> None:
        with self.assertRaises(SystemExit) as ctx:
            main(["--help"])
        self.assertEqual(ctx.exception.code, 0)

    def test_config_driven_acquire_release_flow(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(model_config)
            base = [
                "--db-file",
                str(db_file),
                "--model-config",
                str(model_config),
            ]

            self.assertEqual(
                main(
                    base
                    + [
                        "get",
                        "--request-id",
                        "req-1",
                        "--node-id",
                        "node-1",
                    ]
                ),
                0,
            )

            store = PoolStateStore(db_file)
            config = load_model_config(model_config)
            tickets = store.list_tickets(config)
            self.assertEqual(len(tickets), 1)
            ticket_id = tickets[0].ticket_id

            self.assertEqual(
                main(
                    base
                    + [
                        "done",
                        "--ticket-id",
                        ticket_id,
                        "--result",
                        "success",
                        "--result-summary",
                        "ok",
                    ]
                ),
                0,
            )

            self.assertEqual(len(store.list_tickets(config)), 0)
            self.assertEqual(len(store.usage_records(config)), 1)

    def test_missing_model_config_returns_error(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            missing_config = Path(tmpdir) / "missing.json"
            self.assertEqual(
                main(
                    [
                        "--db-file",
                        str(db_file),
                        "--model-config",
                        str(missing_config),
                        "get",
                        "--request-id",
                        "req-1",
                        "--node-id",
                        "node-1",
                    ]
                ),
                2,
            )

    def test_release_failure_triggers_offline_after_threshold(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(model_config, threshold=1)
            base = [
                "--db-file",
                str(db_file),
                "--model-config",
                str(model_config),
            ]

            self.assertEqual(
                main(base + ["get", "--request-id", "req-1", "--node-id", "node-1"]),
                0,
            )
            store = PoolStateStore(db_file)
            config = load_model_config(model_config)
            ticket_id = store.list_tickets(config)[0].ticket_id

            self.assertEqual(
                main(
                    base
                    + [
                        "done",
                        "--ticket-id",
                        ticket_id,
                        "--result",
                        "failure",
                        "--error-message",
                        "timeout",
                    ]
                ),
                0,
            )

            self.assertEqual(
                main(base + ["get", "--request-id", "req-2", "--node-id", "node-2"]),
                2,
            )

    def test_short_commands_for_query(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(model_config)
            base = [
                "--db-file",
                str(db_file),
                "--model-config",
                str(model_config),
            ]

            self.assertEqual(main(base + ["sync"]), 0)
            self.assertEqual(main(base + ["cap"]), 0)
            self.assertEqual(main(base + ["tickets"]), 0)
            self.assertEqual(main(base + ["usage"]), 0)

    def test_old_alias_commands_are_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmpdir:
            db_file = Path(tmpdir) / "pool_state.db"
            model_config = Path(tmpdir) / "model.json"
            self._write_model_config(model_config)
            base = [
                "--db-file",
                str(db_file),
                "--model-config",
                str(model_config),
            ]

            with self.assertRaises(SystemExit):
                main(base + ["acquire", "--request-id", "req-1", "--node-id", "node-1"])


if __name__ == "__main__":
    unittest.main()
