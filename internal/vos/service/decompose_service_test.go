package service_test

import (
	"context"
	"strings"
	"testing"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

func newTestServiceWithSession(t *testing.T) *service.Service {
	t.Helper()
	stateFile := t.TempDir() + "/vos_state.json"
	sessionDBFile := t.TempDir() + "/openmate.db"
	sessionStore, err := store.NewSQLiteSessionStore(sessionDBFile)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = sessionStore.Close()
	})
	return service.NewWithSessionStore(store.NewJSONStateStore(stateFile), sessionStore)
}

func TestDecomposeNodeCreatesReadyChildren(t *testing.T) {
	svc := newTestServiceWithSession(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-decompose",
		Name:    "Topic Decompose",
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parentNode, err := svc.CreateNode(service.CreateNodeInput{
		TopicID: topic.ID,
		NodeID:  "node-parent",
		Name:    "Parent Node",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	capturedRequest := service.DecomposeAgentRequest{}
	runner := service.NodeDecomposeRunnerFunc(func(ctx context.Context, request service.DecomposeAgentRequest) (*service.DecomposeAgentResponse, error) {
		capturedRequest = request
		return &service.DecomposeAgentResponse{
			RequestID:  request.RequestID,
			TopicID:    request.TopicID,
			NodeID:     request.NodeID,
			Status:     "succeeded",
			DurationMS: 8,
			Tasks: []service.DecomposeAgentTask{
				{
					Title:       "Task A",
					Description: "first",
					Status:      "ready",
				},
				{
					Title:       "Task B",
					Description: "second",
					Status:      "pending",
				},
			},
		}, nil
	})

	result, err := svc.DecomposeNode(context.Background(), service.NodeDecomposeInput{
		NodeID:   parentNode.ID,
		Hint:     "focus MVP",
		MaxItems: 2,
	}, runner)
	if err != nil {
		t.Fatalf("DecomposeNode() error = %v", err)
	}

	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(result.Tasks))
	}
	if len(result.CreatedNodes) != 2 {
		t.Fatalf("len(created_nodes) = %d, want 2", len(result.CreatedNodes))
	}
	for _, child := range result.CreatedNodes {
		if child.ParentID == nil || *child.ParentID != parentNode.ID {
			t.Fatalf("child parent_id = %v, want %s", child.ParentID, parentNode.ID)
		}
		if child.Status != domain.NodeStatusReady {
			t.Fatalf("child status = %q, want ready", child.Status)
		}
	}
	if capturedRequest.TopicID != topic.ID {
		t.Fatalf("runner request topic_id = %q, want %q", capturedRequest.TopicID, topic.ID)
	}
	if capturedRequest.NodeID != parentNode.ID {
		t.Fatalf("runner request node_id = %q, want %q", capturedRequest.NodeID, parentNode.ID)
	}
	if capturedRequest.MaxItems != 2 {
		t.Fatalf("runner request max_items = %d, want 2", capturedRequest.MaxItems)
	}
	if !strings.Contains(capturedRequest.Hint, "focus MVP") {
		t.Fatalf("runner request hint = %q, want contains focus MVP", capturedRequest.Hint)
	}

	children, err := svc.ListChildren(parentNode.ID)
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("len(children) = %d, want 2", len(children))
	}
}

func TestDecomposeNodeReturnsErrorWhenAgentFailed(t *testing.T) {
	svc := newTestServiceWithSession(t)

	if _, _, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-decompose-error",
		Name:    "Topic Decompose Error",
	}); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parentNode, err := svc.CreateNode(service.CreateNodeInput{
		TopicID: "topic-decompose-error",
		NodeID:  "node-parent-error",
		Name:    "Parent Node",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	_, err = svc.DecomposeNode(context.Background(), service.NodeDecomposeInput{
		NodeID:   parentNode.ID,
		MaxItems: service.DefaultNodeDecomposeMaxItems,
	}, service.NodeDecomposeRunnerFunc(func(ctx context.Context, request service.DecomposeAgentRequest) (*service.DecomposeAgentResponse, error) {
		return &service.DecomposeAgentResponse{
			RequestID: request.RequestID,
			TopicID:   request.TopicID,
			NodeID:    request.NodeID,
			Status:    "failed",
			Error:     "agent failed",
		}, nil
	}))
	if err == nil {
		t.Fatalf("DecomposeNode() error = nil, want failed status error")
	}
	if !strings.Contains(err.Error(), "agent failed") {
		t.Fatalf("error = %v, want contains agent failed", err)
	}
}

func TestDecomposeNodeReturnsErrorWhenAgentTasksEmpty(t *testing.T) {
	svc := newTestServiceWithSession(t)

	if _, _, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-decompose-empty",
		Name:    "Topic Decompose Empty",
	}); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parentNode, err := svc.CreateNode(service.CreateNodeInput{
		TopicID: "topic-decompose-empty",
		NodeID:  "node-parent-empty",
		Name:    "Parent Node",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	_, err = svc.DecomposeNode(context.Background(), service.NodeDecomposeInput{
		NodeID:   parentNode.ID,
		MaxItems: service.DefaultNodeDecomposeMaxItems,
	}, service.NodeDecomposeRunnerFunc(func(ctx context.Context, request service.DecomposeAgentRequest) (*service.DecomposeAgentResponse, error) {
		return &service.DecomposeAgentResponse{
			RequestID: request.RequestID,
			TopicID:   request.TopicID,
			NodeID:    request.NodeID,
			Status:    "succeeded",
			Tasks:     []service.DecomposeAgentTask{},
		}, nil
	}))
	if err == nil {
		t.Fatalf("DecomposeNode() error = nil, want empty tasks error")
	}
	if !strings.Contains(err.Error(), "empty tasks") {
		t.Fatalf("error = %v, want contains empty tasks", err)
	}
}
