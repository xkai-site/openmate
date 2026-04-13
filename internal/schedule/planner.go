package schedule

import "sort"

func planTopicDispatch(topic TopicSnapshot, availableSlots int) (DispatchPlan, error) {
	if availableSlots < 0 {
		return DispatchPlan{}, ValidationError{Message: "available_slots must be >= 0"}
	}

	nonTerminal := nonTerminalNodes(topic)
	if len(nonTerminal) == 0 {
		plan := DispatchPlan{
			TopicID:                topic.TopicID,
			ActiveCandidateNodeIDs: []string{},
			DispatchNodeIDs:        []string{},
			Reasons:                []string{"topic has no non-terminal leaf nodes"},
		}
		plan.Normalize()
		return plan, nil
	}

	highestRank := nonTerminal[0].Priority.Rank
	for _, node := range nonTerminal[1:] {
		if node.Priority.Rank < highestRank {
			highestRank = node.Priority.Rank
		}
	}

	activeLayer := make([]TopicNode, 0, len(nonTerminal))
	for _, node := range nonTerminal {
		if node.Priority.Rank == highestRank {
			activeLayer = append(activeLayer, node)
		}
	}
	activePriority := activeLayer[0].Priority.Label

	activeCandidates := make([]TopicNode, 0, len(activeLayer))
	for _, node := range activeLayer {
		if node.Status.statusBlocksDispatch() {
			continue
		}
		activeCandidates = append(activeCandidates, node)
	}
	orderedCandidates := sortActiveCandidates(activeCandidates)

	if len(orderedCandidates) == 0 {
		plan := DispatchPlan{
			TopicID:                topic.TopicID,
			ActivePriority:         stringPtr(activePriority),
			ActiveCandidateNodeIDs: []string{},
			DispatchNodeIDs:        []string{},
			Stalled:                true,
			Reasons: []string{
				"highest priority layer has no runnable nodes",
				"lower priority layers stay blocked until the active layer is cleared or reprioritized",
			},
		}
		plan.Normalize()
		return plan, nil
	}

	currentNodeID := chooseCurrentNodeID(orderedCandidates, topic.Runtime)
	runningNodeIDs := make(map[string]struct{}, len(topic.Runtime.RunningNodeIDs))
	for _, nodeID := range topic.Runtime.RunningNodeIDs {
		runningNodeIDs[nodeID] = struct{}{}
	}

	dispatchNodeIDs := make([]string, 0, availableSlots)
	if currentNodeID != nil && availableSlots > 0 {
		if _, exists := runningNodeIDs[*currentNodeID]; !exists {
			dispatchNodeIDs = append(dispatchNodeIDs, *currentNodeID)
		}
	}

	for _, node := range orderedCandidates {
		if len(dispatchNodeIDs) >= availableSlots {
			break
		}
		if currentNodeID != nil && node.NodeID == *currentNodeID {
			continue
		}
		if _, exists := runningNodeIDs[node.NodeID]; exists {
			continue
		}
		dispatchNodeIDs = append(dispatchNodeIDs, node.NodeID)
	}

	reasons := []string{"active priority resolved to " + activePriority}
	switch {
	case currentNodeID != nil && topic.Runtime.CurrentNodeID != nil && *currentNodeID == *topic.Runtime.CurrentNodeID:
		reasons = append(reasons, "continuation-first kept the current node")
	case currentNodeID != nil && topic.Runtime.LastWorkedNodeID != nil && *currentNodeID == *topic.Runtime.LastWorkedNodeID:
		reasons = append(reasons, "current node was reset to last_worked_node_id")
	default:
		reasons = append(reasons, "current node was selected by stable queue order")
	}

	plan := DispatchPlan{
		TopicID:                topic.TopicID,
		ActivePriority:         stringPtr(activePriority),
		CurrentNodeID:          currentNodeID,
		ActiveCandidateNodeIDs: candidateNodeIDs(orderedCandidates),
		DispatchNodeIDs:        dispatchNodeIDs,
		Reasons:                reasons,
	}
	plan.Normalize()
	return plan, nil
}

func nonTerminalNodes(topic TopicSnapshot) []TopicNode {
	nodes := make([]TopicNode, 0, len(topic.Nodes))
	for _, node := range topic.Nodes {
		if node.Status.statusIsTerminal() {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func sortActiveCandidates(nodes []TopicNode) []TopicNode {
	sorted := make([]TopicNode, len(nodes))
	copy(sorted, nodes)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].EnteredPriorityAt.Equal(sorted[j].EnteredPriorityAt) {
			return sorted[i].NodeID < sorted[j].NodeID
		}
		return sorted[i].EnteredPriorityAt.Before(sorted[j].EnteredPriorityAt)
	})
	return sorted
}

func chooseCurrentNodeID(activeCandidates []TopicNode, runtime TopicRuntimeState) *string {
	candidateIDs := make(map[string]struct{}, len(activeCandidates))
	for _, node := range activeCandidates {
		candidateIDs[node.NodeID] = struct{}{}
	}
	if runtime.CurrentNodeID != nil {
		if _, exists := candidateIDs[*runtime.CurrentNodeID]; exists {
			return stringPtr(*runtime.CurrentNodeID)
		}
	}
	if runtime.LastWorkedNodeID != nil {
		if _, exists := candidateIDs[*runtime.LastWorkedNodeID]; exists {
			return stringPtr(*runtime.LastWorkedNodeID)
		}
	}
	if len(activeCandidates) == 0 {
		return nil
	}
	return stringPtr(activeCandidates[0].NodeID)
}

func candidateNodeIDs(nodes []TopicNode) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		ids = append(ids, node.NodeID)
	}
	return ids
}

func (status NodeStatus) statusIsTerminal() bool {
	switch status {
	case NodeStatusSucceeded, NodeStatusFailed, NodeStatusCancelled:
		return true
	default:
		return false
	}
}

func (status NodeStatus) statusBlocksDispatch() bool {
	switch status {
	case NodeStatusBlocked, NodeStatusRetryCooldown, NodeStatusWaitingExternal:
		return true
	default:
		return false
	}
}

func stringPtr(value string) *string {
	return &value
}
