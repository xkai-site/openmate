package service_test

import (
	"testing"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
)

func TestGetContextSnapshotIncludesMemoriesAndOrderedSessionHistory(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-1",
		Name:    "Topic One",
		Metadata: map[string]any{
			"user_memory":  map[string]any{"user_id": "u-1"},
			"topic_memory": map[string]any{"summary": "topic summary"},
			"global_index": map[string]any{"docs": []any{"doc-a"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parent, err := svc.CreateNode(service.CreateNodeInput{
		TopicID: topic.ID,
		NodeID:  "node-parent",
		Name:    "Parent Node",
	})
	if err != nil {
		t.Fatalf("CreateNode(parent) error = %v", err)
	}
	if _, err := svc.UpdateNode(service.UpdateNodeInput{
		NodeID: parent.ID,
		Memory: map[string]any{"shared_summary": "from-parent"},
	}); err != nil {
		t.Fatalf("UpdateNode(parent memory) error = %v", err)
	}
	leaf, err := svc.CreateNode(service.CreateNodeInput{
		TopicID:  topic.ID,
		NodeID:   "node-leaf",
		ParentID: stringPtr(parent.ID),
		Name:     "Leaf Node",
		Memory:   map[string]any{"leaf_private": "from-leaf"},
	})
	if err != nil {
		t.Fatalf("CreateNode(leaf) error = %v", err)
	}

	sessionA, err := svc.CreateSession(service.CreateSessionInput{
		NodeID:    leaf.ID,
		SessionID: "session-a",
	})
	if err != nil {
		t.Fatalf("CreateSession(session-a) error = %v", err)
	}
	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: sessionA.ID,
		ItemType:  "message",
		PayloadJSON: map[string]any{
			"role": "user",
			"text": "hello",
		},
	}); err != nil {
		t.Fatalf("AppendSessionEvent(session-a:1) error = %v", err)
	}
	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: sessionA.ID,
		ItemType:  "reasoning",
		PayloadJSON: map[string]any{
			"text": "think",
		},
	}); err != nil {
		t.Fatalf("AppendSessionEvent(session-a:2) error = %v", err)
	}

	sessionB, err := svc.CreateSession(service.CreateSessionInput{
		NodeID:    leaf.ID,
		SessionID: "session-b",
	})
	if err != nil {
		t.Fatalf("CreateSession(session-b) error = %v", err)
	}
	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: sessionB.ID,
		ItemType:  "message",
		PayloadJSON: map[string]any{
			"role": "assistant",
			"text": "done",
		},
	}); err != nil {
		t.Fatalf("AppendSessionEvent(session-b:1) error = %v", err)
	}

	snapshot, err := svc.GetContextSnapshot(leaf.ID)
	if err != nil {
		t.Fatalf("GetContextSnapshot() error = %v", err)
	}

	if snapshot.NodeID != leaf.ID {
		t.Fatalf("NodeID = %s, want %s", snapshot.NodeID, leaf.ID)
	}
	if snapshot.UserMemory["user_id"] != "u-1" {
		t.Fatalf("UserMemory = %v, want user_id=u-1", snapshot.UserMemory)
	}
	storedTopic, err := svc.GetTopic(topic.ID)
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	if _, exists := storedTopic.Metadata["user_memory"]; exists {
		t.Fatalf("topic metadata should not contain legacy user_memory after extraction: %v", storedTopic.Metadata)
	}
	if snapshot.TopicMemory["summary"] != "topic summary" {
		t.Fatalf("TopicMemory = %v, want summary=topic summary", snapshot.TopicMemory)
	}
	globalIndex, ok := snapshot.GlobalIndex.(map[string]any)
	if !ok {
		t.Fatalf("GlobalIndex = %T, want map[string]any", snapshot.GlobalIndex)
	}
	if len(globalIndex) != 1 {
		t.Fatalf("GlobalIndex = %v, want 1 entry", globalIndex)
	}
	if snapshot.NodeMemory["shared_summary"] != "from-parent" {
		t.Fatalf("NodeMemory = %v, want parent shared_summary", snapshot.NodeMemory)
	}
	if _, exists := snapshot.NodeMemory["leaf_private"]; exists {
		t.Fatalf("NodeMemory should come from parent; got leaf_private in %v", snapshot.NodeMemory)
	}
	if len(snapshot.SessionHistory) != 2 {
		t.Fatalf("len(SessionHistory) = %d, want 2", len(snapshot.SessionHistory))
	}
	if snapshot.SessionHistory[0].Session == nil || snapshot.SessionHistory[0].Session.ID != "session-a" {
		t.Fatalf("SessionHistory[0].Session = %v, want session-a", snapshot.SessionHistory[0].Session)
	}
	if snapshot.SessionHistory[1].Session == nil || snapshot.SessionHistory[1].Session.ID != "session-b" {
		t.Fatalf("SessionHistory[1].Session = %v, want session-b", snapshot.SessionHistory[1].Session)
	}
	if len(snapshot.SessionHistory[0].Events) != 2 {
		t.Fatalf("len(SessionHistory[0].Events) = %d, want 2", len(snapshot.SessionHistory[0].Events))
	}
	if snapshot.SessionHistory[0].Events[0].Seq != 1 || snapshot.SessionHistory[0].Events[1].Seq != 2 {
		t.Fatalf("SessionHistory[0] seq = [%d %d], want [1 2]",
			snapshot.SessionHistory[0].Events[0].Seq,
			snapshot.SessionHistory[0].Events[1].Seq,
		)
	}
	if len(snapshot.SessionHistory[1].Events) != 1 || snapshot.SessionHistory[1].Events[0].Seq != 1 {
		t.Fatalf("SessionHistory[1].Events = %v, want one seq=1", snapshot.SessionHistory[1].Events)
	}
}

func TestGetContextSnapshotFallsBackToCurrentNodeMemory(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	_, root, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-1",
		Name:    "Topic One",
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if _, err := svc.UpdateNode(service.UpdateNodeInput{
		NodeID: root.ID,
		Memory: map[string]any{"root_summary": "from-root"},
	}); err != nil {
		t.Fatalf("UpdateNode(root memory) error = %v", err)
	}
	if _, err := svc.CreateSession(service.CreateSessionInput{
		NodeID:    root.ID,
		SessionID: "session-root",
	}); err != nil {
		t.Fatalf("CreateSession(root) error = %v", err)
	}

	snapshot, err := svc.GetContextSnapshot(root.ID)
	if err != nil {
		t.Fatalf("GetContextSnapshot() error = %v", err)
	}
	if snapshot.NodeID != root.ID {
		t.Fatalf("NodeID = %s, want %s", snapshot.NodeID, root.ID)
	}
	if snapshot.NodeMemory["root_summary"] != "from-root" {
		t.Fatalf("NodeMemory = %v, want root_summary=from-root", snapshot.NodeMemory)
	}
	if len(snapshot.SessionHistory) != 1 {
		t.Fatalf("len(SessionHistory) = %d, want 1", len(snapshot.SessionHistory))
	}
	if snapshot.SessionHistory[0].Session == nil || snapshot.SessionHistory[0].Session.NodeID != root.ID {
		t.Fatalf("SessionHistory[0].Session = %v, want node_id=%s", snapshot.SessionHistory[0].Session, root.ID)
	}
}

func TestGetContextSnapshotRequiresSessionStore(t *testing.T) {
	svc := newTestService(t)
	if _, err := svc.GetContextSnapshot("node-1"); err == nil {
		t.Fatalf("GetContextSnapshot() error = nil, want session store error")
	}
}

func TestGetContextSnapshotIncludesProcessSummaryContext(t *testing.T) {
	svc := newTestServiceWithSessions(t)
	_, _, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-ps",
		Name:    "Topic Process Summary",
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{
		TopicID: "topic-ps",
		NodeID:  "node-ps",
		Name:    "Node PS",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if _, err := svc.UpdateNode(service.UpdateNodeInput{
		NodeID: node.ID,
		Process: []domain.ProcessItem{
			{
				ID:      "proc-ps",
				Name:    "Summarized Process",
				Status:  domain.ProcessStatusDone,
				Summary: map[string]any{"key_findings": "aligned"},
			},
		},
	}); err != nil {
		t.Fatalf("UpdateNode(process) error = %v", err)
	}
	if _, err := svc.CreateSession(service.CreateSessionInput{
		NodeID:    node.ID,
		SessionID: "session-ps",
	}); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	snapshot, err := svc.GetContextSnapshot(node.ID)
	if err != nil {
		t.Fatalf("GetContextSnapshot() error = %v", err)
	}
	if len(snapshot.ProcessContexts) != 1 {
		t.Fatalf("len(ProcessContexts) = %d, want 1", len(snapshot.ProcessContexts))
	}
	if snapshot.ProcessContexts[0].Summary["key_findings"] != "aligned" {
		t.Fatalf("ProcessContexts[0].Summary = %v, want key_findings=aligned", snapshot.ProcessContexts[0].Summary)
	}
}
