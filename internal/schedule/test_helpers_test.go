package schedule

import (
	"os"
	"time"
)

func makeTopicSnapshot() TopicSnapshot {
	early := time.Date(2026, time.April, 13, 10, 0, 0, 0, time.UTC)
	later := early.Add(time.Minute)
	return TopicSnapshot{
		TopicID:    "topic-1",
		QueueLevel: TopicQueueLevelL0,
		Nodes: []TopicNode{
			{
				NodeID:            "node-a",
				Name:              "collect context",
				Priority:          NodePriority{Label: "now", Rank: 0},
				Status:            NodeStatusReady,
				EnteredPriorityAt: early,
			},
			{
				NodeID:            "node-b",
				Name:              "draft answer",
				Priority:          NodePriority{Label: "now", Rank: 0},
				Status:            NodeStatusReady,
				EnteredPriorityAt: later,
			},
			{
				NodeID:            "node-c",
				Name:              "cleanup",
				Priority:          NodePriority{Label: "later", Rank: 1},
				Status:            NodeStatusReady,
				EnteredPriorityAt: later.Add(time.Minute),
			},
		},
		Runtime: TopicRuntimeState{
			TopicID:          "topic-1",
			CurrentNodeID:    stringPtr("node-a"),
			RunningNodeIDs:   []string{},
			LastWorkedNodeID: stringPtr("node-a"),
			SwitchCount:      1,
		},
	}
}

func writeFile(path string, payload []byte) error {
	return os.WriteFile(path, payload, 0o644)
}
