package service_test

import (
	"testing"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

func newTestService(t *testing.T) *service.Service {
	t.Helper()
	stateFile := t.TempDir() + "/vos_state.json"
	return service.New(store.NewJSONStateStore(stateFile))
}

func TestCreateTopicCreatesRootNode(t *testing.T) {
	svc := newTestService(t)

	topic, rootNode, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID:  "topic-1",
		Name:     "Topic One",
		Metadata: map[string]any{"owner": "vos"},
		Tags:     []string{"init"},
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	storedTopic, err := svc.GetTopic(topic.ID)
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	storedRoot, err := svc.GetNode(rootNode.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}

	if storedTopic.RootNodeID != rootNode.ID {
		t.Fatalf("RootNodeID = %s, want %s", storedTopic.RootNodeID, rootNode.ID)
	}
	if storedRoot.ParentID != nil {
		t.Fatalf("root ParentID = %v, want nil", *storedRoot.ParentID)
	}
	if storedRoot.Status != domain.NodeStatusReady {
		t.Fatalf("root Status = %s, want %s", storedRoot.Status, domain.NodeStatusReady)
	}
	if storedTopic.Metadata["owner"] != "vos" {
		t.Fatalf("Metadata[owner] = %v, want vos", storedTopic.Metadata["owner"])
	}
	if len(storedTopic.Tags) != 1 || storedTopic.Tags[0] != "init" {
		t.Fatalf("Tags = %v, want [init]", storedTopic.Tags)
	}
}

func TestCreateMoveAndDeleteLeafNode(t *testing.T) {
	svc := newTestService(t)

	topic, root, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	nodeA, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-a", Name: "Node A"})
	if err != nil {
		t.Fatalf("CreateNode(node-a) error = %v", err)
	}
	nodeB, err := svc.CreateNode(service.CreateNodeInput{
		TopicID:  topic.ID,
		NodeID:   "node-b",
		ParentID: stringPtr(nodeA.ID),
		Name:     "Node B",
	})
	if err != nil {
		t.Fatalf("CreateNode(node-b) error = %v", err)
	}

	operableA, err := svc.IsLeafOperable(nodeA.ID)
	if err != nil {
		t.Fatalf("IsLeafOperable(node-a) error = %v", err)
	}
	if operableA {
		t.Fatalf("node-a should not be leaf operable")
	}

	operableB, err := svc.IsLeafOperable(nodeB.ID)
	if err != nil {
		t.Fatalf("IsLeafOperable(node-b) error = %v", err)
	}
	if !operableB {
		t.Fatalf("node-b should be leaf operable")
	}

	moved, err := svc.MoveNode(nodeB.ID, root.ID)
	if err != nil {
		t.Fatalf("MoveNode() error = %v", err)
	}
	if moved.ParentID == nil || *moved.ParentID != root.ID {
		t.Fatalf("moved ParentID = %v, want %s", moved.ParentID, root.ID)
	}

	children, err := svc.ListChildren(root.ID)
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	if len(children) != 2 || children[0].ID != "node-a" || children[1].ID != "node-b" {
		t.Fatalf("root children = %#v, want [node-a node-b]", children)
	}

	deleted, err := svc.DeleteNode(nodeB.ID)
	if err != nil {
		t.Fatalf("DeleteNode() error = %v", err)
	}
	if deleted.ID != nodeB.ID {
		t.Fatalf("deleted ID = %s, want %s", deleted.ID, nodeB.ID)
	}

	children, err = svc.ListChildren(root.ID)
	if err != nil {
		t.Fatalf("ListChildren(root) error = %v", err)
	}
	if len(children) != 1 || children[0].ID != "node-a" {
		t.Fatalf("root children after delete = %#v, want [node-a]", children)
	}
}

func TestMoveRejectsCycle(t *testing.T) {
	svc := newTestService(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	nodeA, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-a", Name: "Node A"})
	if err != nil {
		t.Fatalf("CreateNode(node-a) error = %v", err)
	}
	nodeB, err := svc.CreateNode(service.CreateNodeInput{
		TopicID:  topic.ID,
		NodeID:   "node-b",
		ParentID: stringPtr(nodeA.ID),
		Name:     "Node B",
	})
	if err != nil {
		t.Fatalf("CreateNode(node-b) error = %v", err)
	}

	if _, err := svc.MoveNode(nodeA.ID, nodeB.ID); err == nil {
		t.Fatalf("MoveNode() error = nil, want cycle rejection")
	}
}

func TestDeleteRejectsNonLeafNode(t *testing.T) {
	svc := newTestService(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	nodeA, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-a", Name: "Node A"})
	if err != nil {
		t.Fatalf("CreateNode(node-a) error = %v", err)
	}
	if _, err := svc.CreateNode(service.CreateNodeInput{
		TopicID:  topic.ID,
		NodeID:   "node-b",
		ParentID: stringPtr(nodeA.ID),
		Name:     "Node B",
	}); err != nil {
		t.Fatalf("CreateNode(node-b) error = %v", err)
	}

	if _, err := svc.DeleteNode(nodeA.ID); err == nil {
		t.Fatalf("DeleteNode() error = nil, want non-leaf rejection")
	}
}

func TestUpdateNodeAppendsRuntimeFields(t *testing.T) {
	svc := newTestService(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-a", Name: "Node A"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	status := domain.NodeStatusActive
	updated, err := svc.UpdateNode(service.UpdateNodeInput{
		NodeID:      node.ID,
		Description: stringPtr("drafted"),
		Status:      &status,
		Memory:      map[string]any{"summary": "leaf memory"},
		Input:       map[string]any{"attachment": "input-1"},
		Output:      map[string]any{"artifact": "output-1"},
		SessionIDs:  []string{"session-1", "session-2"},
		Progress:    []string{"created", "running"},
	})
	if err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}

	if updated.Description == nil || *updated.Description != "drafted" {
		t.Fatalf("Description = %v, want drafted", updated.Description)
	}
	if updated.Status != domain.NodeStatusActive {
		t.Fatalf("Status = %s, want %s", updated.Status, domain.NodeStatusActive)
	}
	if updated.Memory["summary"] != "leaf memory" {
		t.Fatalf("Memory = %v, want summary", updated.Memory)
	}
	if updated.Input["attachment"] != "input-1" {
		t.Fatalf("Input = %v, want attachment", updated.Input)
	}
	if updated.Output["artifact"] != "output-1" {
		t.Fatalf("Output = %v, want artifact", updated.Output)
	}
	if len(updated.Session) != 2 || updated.Session[0] != "session-1" || updated.Session[1] != "session-2" {
		t.Fatalf("Session = %v, want [session-1 session-2]", updated.Session)
	}
	if len(updated.Progress) != 2 || updated.Progress[0] != "created" || updated.Progress[1] != "running" {
		t.Fatalf("Progress = %v, want [created running]", updated.Progress)
	}
}

func stringPtr(raw string) *string {
	return &raw
}
