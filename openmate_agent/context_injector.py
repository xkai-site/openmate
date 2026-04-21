from __future__ import annotations

import json
from typing import Any

from .context_reader import ContextSnapshotRecord, VosContextReader
from .interfaces import ContextInjector
from .models import ContextBundle


class VosContextInjector(ContextInjector):
    def __init__(self, reader: VosContextReader) -> None:
        self._reader = reader

    def inject(self, node_id: str) -> ContextBundle:
        snapshot = self._reader.snapshot(node_id=node_id)
        payload = self._render_payload(snapshot)
        return ContextBundle(node_id=node_id, payload=payload)

    @staticmethod
    def _render_payload(snapshot: ContextSnapshotRecord) -> str:
        session_history: list[dict[str, Any]] = []
        for item in snapshot.session_history:
            session_history.append(
                {
                    "session": item.session.model_dump(mode="json"),
                    "events": [event.model_dump(mode="json") for event in item.events],
                }
            )

        payload: dict[str, Any] = {
            "SystemPrompt": {
                "memory": {
                    "user_memory": snapshot.user_memory,
                    "topic_memory": snapshot.topic_memory,
                    "node_memory": snapshot.node_memory,
                    "global_index": snapshot.global_index,
                }
            },
            "UserPrompt": {
                "session": session_history,
            },
        }
        return json.dumps(payload, ensure_ascii=False, indent=2)
