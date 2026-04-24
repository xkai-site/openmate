from __future__ import annotations

import json
import logging
from typing import Any

from .context_reader import ContextSnapshotRecord, VosContextReader
from .interfaces import ContextInjector
from .models import ContextBundle

_LOGGER = logging.getLogger(__name__)

# Default threshold: ~70% of 180K chars (~45K tokens for Chinese/mixed text)
_DEFAULT_COMPACT_THRESHOLD_CHARS = 180_000


class VosContextInjector(ContextInjector):
    def __init__(
        self,
        reader: VosContextReader,
        *,
        compact_threshold_chars: int = _DEFAULT_COMPACT_THRESHOLD_CHARS,
    ) -> None:
        self._reader = reader
        self._compact_threshold_chars = compact_threshold_chars

    def inject(self, node_id: str) -> ContextBundle:
        # Try to get a compacted snapshot first
        snapshot = self._reader.snapshot(node_id=node_id)
        payload = self._render_payload(snapshot)

        # If payload exceeds threshold, auto-trigger compact and rebuild
        if len(payload) > self._compact_threshold_chars:
            _LOGGER.info(
                "Context payload %d chars exceeds threshold %d, triggering auto-compact for node %s",
                len(payload),
                self._compact_threshold_chars,
                node_id,
            )
            try:
                self._reader.compact_node(node_id)
                # Re-read after compaction
                snapshot = self._reader.snapshot(node_id=node_id)
                payload = self._render_payload(snapshot)
                _LOGGER.info(
                    "Post-compact context payload is %d chars for node %s",
                    len(payload),
                    node_id,
                )
            except Exception as exc:
                _LOGGER.warning("Auto-compact failed for node %s: %s", node_id, exc)

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

        process_contexts: list[dict[str, Any]] = []
        for pc in snapshot.process_contexts:
            entry: dict[str, Any] = {
                "name": pc.name,
                "status": pc.status,
            }
            if pc.summary is not None:
                entry["summary"] = pc.summary
            if pc.session_events:
                entry["session_events"] = [
                    event.model_dump(mode="json") for event in pc.session_events
                ]
            process_contexts.append(entry)

        payload: dict[str, Any] = {
            "node_id": snapshot.node_id,
            "user_memory": snapshot.user_memory,
            "topic_memory": snapshot.topic_memory,
            "process_contexts": process_contexts,
            "session_history": session_history,
        }
        return json.dumps(payload, ensure_ascii=False, indent=2)
