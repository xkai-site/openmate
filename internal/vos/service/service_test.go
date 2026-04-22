package service_test

import (
	"errors"
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

func TestCreateNodeWithoutTopicCreatesIndependentTopic(t *testing.T) {
	svc := newTestService(t)

	firstNode, err := svc.CreateNode(service.CreateNodeInput{
		Name: "Quick Chat A",
	})
	if err != nil {
		t.Fatalf("CreateNode(first) error = %v", err)
	}
	secondNode, err := svc.CreateNode(service.CreateNodeInput{
		Name: "Quick Chat B",
	})
	if err != nil {
		t.Fatalf("CreateNode(second) error = %v", err)
	}

	if firstNode.TopicID == secondNode.TopicID {
		t.Fatalf("topic ids should differ, got %q", firstNode.TopicID)
	}

	firstTopic, err := svc.GetTopic(firstNode.TopicID)
	if err != nil {
		t.Fatalf("GetTopic(first) error = %v", err)
	}
	secondTopic, err := svc.GetTopic(secondNode.TopicID)
	if err != nil {
		t.Fatalf("GetTopic(second) error = %v", err)
	}

	if firstNode.ParentID == nil || *firstNode.ParentID != firstTopic.RootNodeID {
		t.Fatalf("first node parent id = %v, want %s", firstNode.ParentID, firstTopic.RootNodeID)
	}
	if secondNode.ParentID == nil || *secondNode.ParentID != secondTopic.RootNodeID {
		t.Fatalf("second node parent id = %v, want %s", secondNode.ParentID, secondTopic.RootNodeID)
	}
}

func TestListDisplayRootNodesReturnsTopicRootsOnly(t *testing.T) {
	svc := newTestService(t)

	firstNode, err := svc.CreateNode(service.CreateNodeInput{Name: "Temp A"})
	if err != nil {
		t.Fatalf("CreateNode(first) error = %v", err)
	}
	firstTopic, err := svc.GetTopic(firstNode.TopicID)
	if err != nil {
		t.Fatalf("GetTopic(first) error = %v", err)
	}
	_, topicRoot, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-project",
		Name:    "Project Topic",
	})
	if err != nil {
		t.Fatalf("CreateTopic(project) error = %v", err)
	}
	secondNode, err := svc.CreateNode(service.CreateNodeInput{Name: "Temp B"})
	if err != nil {
		t.Fatalf("CreateNode(second) error = %v", err)
	}
	secondTopic, err := svc.GetTopic(secondNode.TopicID)
	if err != nil {
		t.Fatalf("GetTopic(second) error = %v", err)
	}

	roots, err := svc.ListDisplayRootNodes()
	if err != nil {
		t.Fatalf("ListDisplayRootNodes() error = %v", err)
	}
	if len(roots) != 3 {
		t.Fatalf("roots len = %d, want 3", len(roots))
	}

	foundIDs := map[string]bool{}
	for _, node := range roots {
		foundIDs[node.ID] = true
	}
	if !foundIDs[firstTopic.RootNodeID] {
		t.Fatalf("display roots missing first topic root: %s", firstTopic.RootNodeID)
	}
	if !foundIDs[secondTopic.RootNodeID] {
		t.Fatalf("display roots missing second topic root: %s", secondTopic.RootNodeID)
	}
	if !foundIDs[topicRoot.ID] {
		t.Fatalf("display roots missing topic root: %s", topicRoot.ID)
	}
	if foundIDs[firstNode.ID] {
		t.Fatalf("display roots should not include first chat node: %s", firstNode.ID)
	}
	if foundIDs[secondNode.ID] {
		t.Fatalf("display roots should not include second chat node: %s", secondNode.ID)
	}
}

func TestUpdateAndDeleteTopic(t *testing.T) {
	svc := newTestService(t)

	topic, rootNode, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID:     "topic-1",
		Name:        "Topic One",
		Description: stringPtr("legacy"),
		Metadata:    map[string]any{"owner": "vos"},
		Tags:        []string{"init"},
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if _, err := svc.CreateNode(service.CreateNodeInput{
		TopicID: topic.ID,
		NodeID:  "node-1",
		Name:    "Node One",
	}); err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	updated, err := svc.UpdateTopic(service.UpdateTopicInput{
		TopicID:          topic.ID,
		Name:             stringPtr("Topic Two"),
		ClearDescription: true,
		Metadata:         map[string]any{"owner": "schedule"},
		ReplaceMetadata:  true,
		Tags:             []string{"go", "vos"},
		ReplaceTags:      true,
	})
	if err != nil {
		t.Fatalf("UpdateTopic() error = %v", err)
	}

	if updated.Name != "Topic Two" {
		t.Fatalf("Name = %s, want Topic Two", updated.Name)
	}
	if updated.Description != nil {
		t.Fatalf("Description = %v, want nil", updated.Description)
	}
	if updated.Metadata["owner"] != "schedule" {
		t.Fatalf("Metadata = %v, want owner=schedule", updated.Metadata)
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "go" || updated.Tags[1] != "vos" {
		t.Fatalf("Tags = %v, want [go vos]", updated.Tags)
	}

	deleted, err := svc.DeleteTopic(topic.ID)
	if err != nil {
		t.Fatalf("DeleteTopic() error = %v", err)
	}
	if deleted.Topic.ID != topic.ID {
		t.Fatalf("deleted topic ID = %s, want %s", deleted.Topic.ID, topic.ID)
	}
	if len(deleted.DeletedNodeIDs) != 2 || deleted.DeletedNodeIDs[0] != rootNode.ID || deleted.DeletedNodeIDs[1] != "node-1" {
		t.Fatalf("DeletedNodeIDs = %v, want [%s node-1]", deleted.DeletedNodeIDs, rootNode.ID)
	}
	if _, err := svc.GetTopic(topic.ID); err == nil {
		t.Fatalf("GetTopic() error = nil after delete")
	}
	if _, err := svc.GetNode(rootNode.ID); err == nil {
		t.Fatalf("GetNode(root) error = nil after topic delete")
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

func TestUpdateNodeAppendsRuntimeFieldsAndAggregatesParentMemory(t *testing.T) {
	svc := newTestService(t)

	topic, root, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-a", Name: "Node A"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	expectedVersion := node.Version
	status := domain.NodeStatusActive
	updated, err := svc.UpdateNode(service.UpdateNodeInput{
		NodeID:          node.ID,
		ExpectedVersion: &expectedVersion,
		Name:            stringPtr("Node A Prime"),
		Description:     stringPtr("drafted"),
		Status:          &status,
		Memory:          map[string]any{"summary": "leaf memory"},
		Input:           map[string]any{"attachment": "input-1"},
		Output:          map[string]any{"artifact": "output-1"},
		SessionIDs:      []string{"session-1", "session-2"},
		Progress:        []string{"created", "running"},
	})
	if err != nil {
		t.Fatalf("UpdateNode() error = %v", err)
	}

	if updated.Version != expectedVersion+1 {
		t.Fatalf("Version = %d, want %d", updated.Version, expectedVersion+1)
	}
	if updated.Name != "Node A Prime" {
		t.Fatalf("Name = %s, want Node A Prime", updated.Name)
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

	parent, err := svc.GetNode(root.ID)
	if err != nil {
		t.Fatalf("GetNode(root) error = %v", err)
	}
	cache, ok := parent.Memory["_child_memory_cache"].(map[string]any)
	if !ok {
		t.Fatalf("root memory cache missing: %v", parent.Memory)
	}
	entry, ok := cache[node.ID].(map[string]any)
	if !ok {
		t.Fatalf("root memory entry missing for %s: %v", node.ID, cache)
	}
	if entry["name"] != "Node A Prime" {
		t.Fatalf("cached name = %v, want Node A Prime", entry["name"])
	}
	cachedMemory, ok := entry["memory"].(map[string]any)
	if !ok {
		t.Fatalf("cached memory = %T, want map[string]any", entry["memory"])
	}
	if cachedMemory["summary"] != "leaf memory" {
		t.Fatalf("cached memory = %v, want summary=leaf memory", cachedMemory)
	}
}

func TestUpdateNodeRejectsVersionConflict(t *testing.T) {
	svc := newTestService(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-a", Name: "Node A"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	staleVersion := node.Version + 1
	_, err = svc.UpdateNode(service.UpdateNodeInput{
		NodeID:          node.ID,
		ExpectedVersion: &staleVersion,
		Progress:        []string{"should-fail"},
	})
	if err == nil {
		t.Fatalf("UpdateNode() error = nil, want version conflict")
	}

	var conflict domain.VersionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("UpdateNode() error = %T, want VersionConflictError", err)
	}
}

func TestListNodesByFilterReturnsSchedulableLeaves(t *testing.T) {
	svc := newTestService(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parent, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-parent", Name: "Parent"})
	if err != nil {
		t.Fatalf("CreateNode(parent) error = %v", err)
	}
	if _, err := svc.CreateNode(service.CreateNodeInput{
		TopicID:  topic.ID,
		NodeID:   "node-active",
		ParentID: stringPtr(parent.ID),
		Name:     "Active Leaf",
		Status:   domain.NodeStatusActive,
	}); err != nil {
		t.Fatalf("CreateNode(active) error = %v", err)
	}
	if _, err := svc.CreateNode(service.CreateNodeInput{
		TopicID: topic.ID,
		NodeID:  "node-done",
		Name:    "Done Leaf",
		Status:  domain.NodeStatusDone,
	}); err != nil {
		t.Fatalf("CreateNode(done) error = %v", err)
	}

	nodes, err := svc.ListNodesByFilter(topic.ID, service.NodeListFilter{
		LeafOnly:        true,
		ExcludeStatuses: []domain.NodeStatus{domain.NodeStatusDone},
	})
	if err != nil {
		t.Fatalf("ListNodesByFilter() error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-active" {
		t.Fatalf("filtered nodes = %#v, want [node-active]", nodes)
	}
}

func TestMoveDoesNotRecomputeParentMemory(t *testing.T) {
	svc := newTestService(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parentA, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "parent-a", Name: "Parent A"})
	if err != nil {
		t.Fatalf("CreateNode(parent-a) error = %v", err)
	}
	parentB, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "parent-b", Name: "Parent B"})
	if err != nil {
		t.Fatalf("CreateNode(parent-b) error = %v", err)
	}
	leaf, err := svc.CreateNode(service.CreateNodeInput{
		TopicID:  topic.ID,
		NodeID:   "leaf-1",
		ParentID: stringPtr(parentA.ID),
		Name:     "Leaf",
		Memory:   map[string]any{"summary": "persist"},
	})
	if err != nil {
		t.Fatalf("CreateNode(leaf) error = %v", err)
	}

	if _, err := svc.MoveNode(leaf.ID, parentB.ID); err != nil {
		t.Fatalf("MoveNode() error = %v", err)
	}

	storedParentA, err := svc.GetNode(parentA.ID)
	if err != nil {
		t.Fatalf("GetNode(parent-a) error = %v", err)
	}
	storedParentB, err := svc.GetNode(parentB.ID)
	if err != nil {
		t.Fatalf("GetNode(parent-b) error = %v", err)
	}

	cacheA, ok := storedParentA.Memory["_child_memory_cache"].(map[string]any)
	if !ok {
		t.Fatalf("parent-a cache missing: %v", storedParentA.Memory)
	}
	if _, ok := cacheA[leaf.ID]; !ok {
		t.Fatalf("parent-a cache entry missing for %s", leaf.ID)
	}
	if storedParentB.Memory != nil {
		if cacheB, ok := storedParentB.Memory["_child_memory_cache"]; ok && cacheB != nil {
			t.Fatalf("parent-b cache = %v, want no recompute on move", storedParentB.Memory)
		}
	}
}

func stringPtr(raw string) *string {
	return &raw
}
