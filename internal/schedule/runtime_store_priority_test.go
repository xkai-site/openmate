package schedule

import (
	"strings"
	"testing"
)

func TestRuntimeStoreRejectsPriorityRankLabelConflictOnEnqueue(t *testing.T) {
	store := openTestRuntimeStore(t)
	defer func() {
		_ = store.Close()
	}()

	if err := store.UpsertPriorityNode("topic-1", "topic-1-priority", AgentSpec{Mode: "priority"}); err != nil {
		t.Fatalf("UpsertPriorityNode() error = %v", err)
	}

	_, err := store.UpsertEnqueueNode(EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-a",
		NodeName: "node a",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
		Priority: NodePriority{
			Label: "interactive",
			Rank:  0,
		},
	})
	if err == nil {
		t.Fatalf("UpsertEnqueueNode() error = nil, want conflict error")
	}
	if !strings.Contains(err.Error(), "priority rank 0 already mapped to label") {
		t.Fatalf("UpsertEnqueueNode() error = %v, want rank mapping conflict", err)
	}
}

func TestRuntimeStoreAllowsSameRankSameLabel(t *testing.T) {
	store := openTestRuntimeStore(t)
	defer func() {
		_ = store.Close()
	}()

	_, err := store.UpsertEnqueueNode(EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-a",
		NodeName: "node a",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
		Priority: NodePriority{
			Label: BusinessNodePriorityLabel,
			Rank:  BusinessNodePriorityRank,
		},
	})
	if err != nil {
		t.Fatalf("first UpsertEnqueueNode() error = %v", err)
	}

	_, err = store.UpsertEnqueueNode(EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-b",
		NodeName: "node b",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
		Priority: NodePriority{
			Label: BusinessNodePriorityLabel,
			Rank:  BusinessNodePriorityRank,
		},
	})
	if err != nil {
		t.Fatalf("second UpsertEnqueueNode() error = %v", err)
	}
}
