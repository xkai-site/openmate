package schedule

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseTopicSnapshotJSONAppliesDefaults(t *testing.T) {
	payload := []byte(`{
  "topic_id": "topic-1",
  "runtime": {
    "topic_id": "topic-1"
  },
  "nodes": [
    {
      "node_id": "node-a",
      "name": "collect context",
      "priority": {
        "label": "now",
        "rank": 0
      }
    }
  ]
}`)

	snapshot, err := ParseTopicSnapshotJSON(payload)
	if err != nil {
		t.Fatalf("ParseTopicSnapshotJSON() error = %v", err)
	}
	if snapshot.QueueLevel != TopicQueueLevelL0 {
		t.Fatalf("QueueLevel = %s, want L0", snapshot.QueueLevel)
	}
	if snapshot.Nodes[0].Status != NodeStatusReady {
		t.Fatalf("NodeStatus = %s, want ready", snapshot.Nodes[0].Status)
	}
	if snapshot.Nodes[0].EnteredPriorityAt.IsZero() {
		t.Fatalf("EnteredPriorityAt should be defaulted")
	}
	if snapshot.Runtime.RunningNodeIDs == nil || len(snapshot.Runtime.RunningNodeIDs) != 0 {
		t.Fatalf("RunningNodeIDs = %+v, want empty slice", snapshot.Runtime.RunningNodeIDs)
	}
}

func TestTopicSnapshotValidateErrors(t *testing.T) {
	testCases := []struct {
		name    string
		mutate  func(TopicSnapshot) TopicSnapshot
		wantErr string
	}{
		{
			name: "runtime topic mismatch",
			mutate: func(snapshot TopicSnapshot) TopicSnapshot {
				snapshot.Runtime.TopicID = "topic-2"
				return snapshot
			},
			wantErr: "runtime.topic_id must match topic_id",
		},
		{
			name: "duplicate node id",
			mutate: func(snapshot TopicSnapshot) TopicSnapshot {
				snapshot.Nodes[1].NodeID = "node-a"
				return snapshot
			},
			wantErr: "duplicate node_id: node-a",
		},
		{
			name: "unknown current node",
			mutate: func(snapshot TopicSnapshot) TopicSnapshot {
				snapshot.Runtime.CurrentNodeID = stringPtr("node-x")
				return snapshot
			},
			wantErr: "unknown current_node_id: node-x",
		},
		{
			name: "duplicate running node",
			mutate: func(snapshot TopicSnapshot) TopicSnapshot {
				snapshot.Runtime.RunningNodeIDs = []string{"node-a", "node-a"}
				return snapshot
			},
			wantErr: "duplicate running node id: node-a",
		},
		{
			name: "rank label mismatch",
			mutate: func(snapshot TopicSnapshot) TopicSnapshot {
				snapshot.Nodes[1].Priority.Label = "urgent"
				return snapshot
			},
			wantErr: "priority ranks must map to exactly one label within one topic snapshot",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			snapshot := testCase.mutate(makeTopicSnapshot())
			if err := snapshot.Validate(); err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("Validate() error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestPlanTopicDispatchContinuationFirst(t *testing.T) {
	plan, err := planTopicDispatch(makeTopicSnapshot(), 2)
	if err != nil {
		t.Fatalf("planTopicDispatch() error = %v", err)
	}
	if plan.CurrentNodeID == nil || *plan.CurrentNodeID != "node-a" {
		t.Fatalf("CurrentNodeID = %+v, want node-a", plan.CurrentNodeID)
	}
	if len(plan.DispatchNodeIDs) != 2 || plan.DispatchNodeIDs[0] != "node-a" || plan.DispatchNodeIDs[1] != "node-b" {
		t.Fatalf("DispatchNodeIDs = %+v, want [node-a node-b]", plan.DispatchNodeIDs)
	}
}

func TestPlanTopicDispatchFallsBackToLastWorked(t *testing.T) {
	snapshot := makeTopicSnapshot()
	snapshot.Runtime.CurrentNodeID = stringPtr("node-c")
	snapshot.Runtime.LastWorkedNodeID = stringPtr("node-b")

	plan, err := planTopicDispatch(snapshot, 1)
	if err != nil {
		t.Fatalf("planTopicDispatch() error = %v", err)
	}
	if plan.CurrentNodeID == nil || *plan.CurrentNodeID != "node-b" {
		t.Fatalf("CurrentNodeID = %+v, want node-b", plan.CurrentNodeID)
	}
	if len(plan.DispatchNodeIDs) != 1 || plan.DispatchNodeIDs[0] != "node-b" {
		t.Fatalf("DispatchNodeIDs = %+v, want [node-b]", plan.DispatchNodeIDs)
	}
}

func TestPlanTopicDispatchReturnsStalled(t *testing.T) {
	snapshot := makeTopicSnapshot()
	snapshot.Nodes[0].Status = NodeStatusBlocked
	snapshot.Nodes[1].Status = NodeStatusRetryCooldown

	plan, err := planTopicDispatch(snapshot, 2)
	if err != nil {
		t.Fatalf("planTopicDispatch() error = %v", err)
	}
	if !plan.Stalled {
		t.Fatalf("expected stalled plan")
	}
	if len(plan.DispatchNodeIDs) != 0 {
		t.Fatalf("DispatchNodeIDs = %+v, want empty", plan.DispatchNodeIDs)
	}
}

func TestPlanTopicDispatchWithZeroSlots(t *testing.T) {
	plan, err := planTopicDispatch(makeTopicSnapshot(), 0)
	if err != nil {
		t.Fatalf("planTopicDispatch() error = %v", err)
	}
	if plan.CurrentNodeID == nil || *plan.CurrentNodeID != "node-a" {
		t.Fatalf("CurrentNodeID = %+v, want node-a", plan.CurrentNodeID)
	}
	if len(plan.DispatchNodeIDs) != 0 {
		t.Fatalf("DispatchNodeIDs = %+v, want empty", plan.DispatchNodeIDs)
	}
}

func TestPlanTopicDispatchSkipsRunningNodes(t *testing.T) {
	snapshot := makeTopicSnapshot()
	snapshot.Runtime.RunningNodeIDs = []string{"node-a"}

	plan, err := planTopicDispatch(snapshot, 2)
	if err != nil {
		t.Fatalf("planTopicDispatch() error = %v", err)
	}
	if len(plan.DispatchNodeIDs) != 1 || plan.DispatchNodeIDs[0] != "node-b" {
		t.Fatalf("DispatchNodeIDs = %+v, want [node-b]", plan.DispatchNodeIDs)
	}
}

func TestPlanTopicDispatchWithoutNonTerminalNodes(t *testing.T) {
	snapshot := makeTopicSnapshot()
	for index := range snapshot.Nodes {
		snapshot.Nodes[index].Status = NodeStatusSucceeded
	}

	plan, err := planTopicDispatch(snapshot, 2)
	if err != nil {
		t.Fatalf("planTopicDispatch() error = %v", err)
	}
	if plan.ActivePriority != nil || plan.CurrentNodeID != nil {
		t.Fatalf("expected nil active/current values, got %+v", plan)
	}
	if len(plan.DispatchNodeIDs) != 0 {
		t.Fatalf("DispatchNodeIDs = %+v, want empty", plan.DispatchNodeIDs)
	}
}

func TestDispatchPlanJSONUsesEmptyArrays(t *testing.T) {
	plan := DispatchPlan{TopicID: "topic-1"}
	plan.Normalize()

	payload, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	content := string(payload)
	if !strings.Contains(content, `"dispatch_node_ids":[]`) {
		t.Fatalf("expected empty dispatch_node_ids array, got %s", content)
	}
	if !strings.Contains(content, `"reasons":[]`) {
		t.Fatalf("expected empty reasons array, got %s", content)
	}
}
