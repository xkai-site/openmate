"""Dispatch planning helpers for schedule queue MVP."""

from __future__ import annotations

from .models import DispatchPlan, NodeStatus, TopicNode, TopicSnapshot

TERMINAL_STATUSES = {
    NodeStatus.SUCCEEDED,
    NodeStatus.FAILED,
    NodeStatus.CANCELLED,
}

BLOCKING_STATUSES = {
    NodeStatus.BLOCKED,
    NodeStatus.RETRY_COOLDOWN,
    NodeStatus.WAITING_EXTERNAL,
}


def _non_terminal_nodes(topic: TopicSnapshot) -> list[TopicNode]:
    return [node for node in topic.nodes if node.status not in TERMINAL_STATUSES]


def _sort_active_candidates(nodes: list[TopicNode]) -> list[TopicNode]:
    return sorted(nodes, key=lambda node: (node.entered_priority_at, node.node_id))


def _choose_current_node(active_candidates: list[TopicNode], topic: TopicSnapshot) -> str | None:
    candidate_ids = {node.node_id for node in active_candidates}
    current_node_id = topic.runtime.current_node_id
    if current_node_id in candidate_ids:
        return current_node_id

    last_worked_node_id = topic.runtime.last_worked_node_id
    if last_worked_node_id in candidate_ids:
        return last_worked_node_id

    ordered = _sort_active_candidates(active_candidates)
    if not ordered:
        return None
    return ordered[0].node_id


def plan_topic_dispatch(topic: TopicSnapshot, *, available_slots: int = 1) -> DispatchPlan:
    if available_slots < 0:
        raise ValueError("available_slots must be >= 0")

    non_terminal_nodes = _non_terminal_nodes(topic)
    if not non_terminal_nodes:
        return DispatchPlan(
            topic_id=topic.topic_id,
            active_priority=None,
            current_node_id=None,
            active_candidate_node_ids=[],
            dispatch_node_ids=[],
            stalled=False,
            reasons=["topic has no non-terminal leaf nodes"],
        )

    highest_rank = min(node.priority.rank for node in non_terminal_nodes)
    active_layer_nodes = [node for node in non_terminal_nodes if node.priority.rank == highest_rank]
    active_priority = active_layer_nodes[0].priority.label

    active_candidates = [node for node in active_layer_nodes if node.status not in BLOCKING_STATUSES]
    ordered_candidates = _sort_active_candidates(active_candidates)

    if not ordered_candidates:
        return DispatchPlan(
            topic_id=topic.topic_id,
            active_priority=active_priority,
            current_node_id=None,
            active_candidate_node_ids=[],
            dispatch_node_ids=[],
            stalled=True,
            reasons=[
                "highest priority layer has no runnable nodes",
                "lower priority layers stay blocked until the active layer is cleared or reprioritized",
            ],
        )

    current_node_id = _choose_current_node(ordered_candidates, topic)
    running_node_ids = set(topic.runtime.running_node_ids)
    dispatch_node_ids: list[str] = []

    if current_node_id is not None and current_node_id not in running_node_ids and available_slots > 0:
        dispatch_node_ids.append(current_node_id)

    for node in ordered_candidates:
        if len(dispatch_node_ids) >= available_slots:
            break
        if node.node_id == current_node_id:
            continue
        if node.node_id in running_node_ids:
            continue
        dispatch_node_ids.append(node.node_id)

    reasons = [f"active priority resolved to {active_priority}"]
    if current_node_id == topic.runtime.current_node_id and current_node_id is not None:
        reasons.append("continuation-first kept the current node")
    elif current_node_id == topic.runtime.last_worked_node_id and current_node_id is not None:
        reasons.append("current node was reset to last_worked_node_id")
    else:
        reasons.append("current node was selected by stable queue order")

    return DispatchPlan(
        topic_id=topic.topic_id,
        active_priority=active_priority,
        current_node_id=current_node_id,
        active_candidate_node_ids=[node.node_id for node in ordered_candidates],
        dispatch_node_ids=dispatch_node_ids,
        stalled=False,
        reasons=reasons,
    )
