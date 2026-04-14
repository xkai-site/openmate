package schedule

import (
	"encoding/json"
	"fmt"
	"time"
)

type ValidationError struct {
	Message string
}

func (err ValidationError) Error() string {
	return err.Message
}

type TopicQueueLevel string

const (
	TopicQueueLevelL0 TopicQueueLevel = "L0"
	TopicQueueLevelL1 TopicQueueLevel = "L1"
	TopicQueueLevelL2 TopicQueueLevel = "L2"
	TopicQueueLevelL3 TopicQueueLevel = "L3"
)

type NodeStatus string

const (
	NodeStatusPending         NodeStatus = "pending"
	NodeStatusReady           NodeStatus = "ready"
	NodeStatusRunning         NodeStatus = "running"
	NodeStatusBlocked         NodeStatus = "blocked"
	NodeStatusRetryCooldown   NodeStatus = "retry_cooldown"
	NodeStatusWaitingExternal NodeStatus = "waiting_external"
	NodeStatusSucceeded       NodeStatus = "succeeded"
	NodeStatusFailed          NodeStatus = "failed"
	NodeStatusCancelled       NodeStatus = "cancelled"
)

type NodePriority struct {
	Label string `json:"label"`
	Rank  int    `json:"rank"`
}

type TopicNode struct {
	NodeID            string       `json:"node_id"`
	Name              string       `json:"name"`
	Priority          NodePriority `json:"priority"`
	Status            NodeStatus   `json:"status"`
	EnteredPriorityAt time.Time    `json:"entered_priority_at"`
	LastWorkedAt      *time.Time   `json:"last_worked_at"`
}

type TopicRuntimeState struct {
	TopicID          string     `json:"topic_id"`
	ActivePriority   *string    `json:"active_priority"`
	CurrentNodeID    *string    `json:"current_node_id"`
	RunningNodeIDs   []string   `json:"running_node_ids"`
	LastWorkedNodeID *string    `json:"last_worked_node_id"`
	LastWorkedAt     *time.Time `json:"last_worked_at"`
	SwitchCount      int        `json:"switch_count"`
	PriorityDirty    bool       `json:"priority_dirty"`
}

type TopicSnapshot struct {
	TopicID    string            `json:"topic_id"`
	QueueLevel TopicQueueLevel   `json:"queue_level"`
	Nodes      []TopicNode       `json:"nodes"`
	Runtime    TopicRuntimeState `json:"runtime"`
}

type DispatchPlan struct {
	TopicID                string   `json:"topic_id"`
	ActivePriority         *string  `json:"active_priority"`
	CurrentNodeID          *string  `json:"current_node_id"`
	ActiveCandidateNodeIDs []string `json:"active_candidate_node_ids"`
	DispatchNodeIDs        []string `json:"dispatch_node_ids"`
	Stalled                bool     `json:"stalled"`
	Reasons                []string `json:"reasons"`
}

func ParseTopicSnapshotJSON(payload []byte) (TopicSnapshot, error) {
	var snapshot TopicSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return TopicSnapshot{}, ValidationError{Message: fmt.Sprintf("invalid topic snapshot json: %v", err)}
	}
	snapshot.Normalize()
	if err := snapshot.Validate(); err != nil {
		return TopicSnapshot{}, err
	}
	return snapshot, nil
}

func (level *TopicQueueLevel) Normalize() {
	if *level == "" {
		*level = TopicQueueLevelL0
	}
}

func (level TopicQueueLevel) Validate() error {
	switch level {
	case TopicQueueLevelL0, TopicQueueLevelL1, TopicQueueLevelL2, TopicQueueLevelL3:
		return nil
	default:
		return ValidationError{Message: fmt.Sprintf("invalid queue_level: %q", string(level))}
	}
}

func (status NodeStatus) isKnown() bool {
	switch status {
	case NodeStatusPending,
		NodeStatusReady,
		NodeStatusRunning,
		NodeStatusBlocked,
		NodeStatusRetryCooldown,
		NodeStatusWaitingExternal,
		NodeStatusSucceeded,
		NodeStatusFailed,
		NodeStatusCancelled:
		return true
	default:
		return false
	}
}

func (priority NodePriority) Validate() error {
	if priority.Label == "" {
		return ValidationError{Message: "priority.label must not be empty"}
	}
	if priority.Rank < 0 {
		return ValidationError{Message: "priority.rank must be >= 0"}
	}
	return nil
}

func (node *TopicNode) Normalize() {
	if node.Status == "" {
		node.Status = NodeStatusReady
	}
	if node.EnteredPriorityAt.IsZero() {
		node.EnteredPriorityAt = time.Now().UTC()
	}
}

func (node TopicNode) Validate() error {
	if node.NodeID == "" {
		return ValidationError{Message: "node_id must not be empty"}
	}
	if node.Name == "" {
		return ValidationError{Message: fmt.Sprintf("node %q name must not be empty", node.NodeID)}
	}
	if err := node.Priority.Validate(); err != nil {
		return err
	}
	if !node.Status.isKnown() {
		return ValidationError{Message: fmt.Sprintf("node %q has invalid status %q", node.NodeID, node.Status)}
	}
	return nil
}

func (runtime *TopicRuntimeState) Normalize() {
	if runtime.RunningNodeIDs == nil {
		runtime.RunningNodeIDs = []string{}
	}
	runtime.ActivePriority = normalizeOptionalString(runtime.ActivePriority)
	runtime.CurrentNodeID = normalizeOptionalString(runtime.CurrentNodeID)
	runtime.LastWorkedNodeID = normalizeOptionalString(runtime.LastWorkedNodeID)
}

func (runtime TopicRuntimeState) Validate() error {
	if runtime.TopicID == "" {
		return ValidationError{Message: "runtime.topic_id must not be empty"}
	}
	if runtime.SwitchCount < 0 {
		return ValidationError{Message: "runtime.switch_count must be >= 0"}
	}
	seen := map[string]struct{}{}
	for _, nodeID := range runtime.RunningNodeIDs {
		if nodeID == "" {
			return ValidationError{Message: "runtime.running_node_ids must not contain empty values"}
		}
		if _, exists := seen[nodeID]; exists {
			return ValidationError{Message: fmt.Sprintf("duplicate running node id: %s", nodeID)}
		}
		seen[nodeID] = struct{}{}
	}
	return nil
}

func (snapshot *TopicSnapshot) Normalize() {
	snapshot.QueueLevel.Normalize()
	if snapshot.Nodes == nil {
		snapshot.Nodes = []TopicNode{}
	}
	for index := range snapshot.Nodes {
		snapshot.Nodes[index].Normalize()
	}
	snapshot.Runtime.Normalize()
}

func (snapshot TopicSnapshot) Validate() error {
	if snapshot.TopicID == "" {
		return ValidationError{Message: "topic_id must not be empty"}
	}
	if err := snapshot.QueueLevel.Validate(); err != nil {
		return err
	}
	if err := snapshot.Runtime.Validate(); err != nil {
		return err
	}
	if snapshot.Runtime.TopicID != snapshot.TopicID {
		return ValidationError{Message: "runtime.topic_id must match topic_id"}
	}

	knownNodeIDs := map[string]struct{}{}
	rankToLabel := map[int]string{}
	for _, node := range snapshot.Nodes {
		if err := node.Validate(); err != nil {
			return err
		}
		if _, exists := knownNodeIDs[node.NodeID]; exists {
			return ValidationError{Message: fmt.Sprintf("duplicate node_id: %s", node.NodeID)}
		}
		knownNodeIDs[node.NodeID] = struct{}{}

		label, exists := rankToLabel[node.Priority.Rank]
		if !exists {
			rankToLabel[node.Priority.Rank] = node.Priority.Label
			continue
		}
		if label != node.Priority.Label {
			return ValidationError{Message: "priority ranks must map to exactly one label within one topic snapshot"}
		}
	}

	if err := validateKnownReference("current_node_id", snapshot.Runtime.CurrentNodeID, knownNodeIDs); err != nil {
		return err
	}
	if err := validateKnownReference("last_worked_node_id", snapshot.Runtime.LastWorkedNodeID, knownNodeIDs); err != nil {
		return err
	}
	for _, nodeID := range snapshot.Runtime.RunningNodeIDs {
		if _, exists := knownNodeIDs[nodeID]; !exists {
			return ValidationError{Message: fmt.Sprintf("unknown running node id: %s", nodeID)}
		}
	}
	return nil
}

func (plan *DispatchPlan) Normalize() {
	if plan.ActiveCandidateNodeIDs == nil {
		plan.ActiveCandidateNodeIDs = []string{}
	}
	if plan.DispatchNodeIDs == nil {
		plan.DispatchNodeIDs = []string{}
	}
	if plan.Reasons == nil {
		plan.Reasons = []string{}
	}
}

func validateKnownReference(field string, value *string, knownNodeIDs map[string]struct{}) error {
	if value == nil {
		return nil
	}
	if _, exists := knownNodeIDs[*value]; exists {
		return nil
	}
	return ValidationError{Message: fmt.Sprintf("unknown %s: %s", field, *value)}
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	if *value == "" {
		return nil
	}
	return value
}
