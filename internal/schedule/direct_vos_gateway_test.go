package schedule

import (
	"context"
	"path/filepath"
	"testing"

	"vos/internal/vos/domain"
	vosservice "vos/internal/vos/service"
	vosstore "vos/internal/vos/store"
)

func TestDirectVOSGatewayEnsurePriorityNode(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "vos_state.json")
	sessionDBFile := filepath.Join(tempDir, "openmate.db")

	sessionStore, err := vosstore.NewSQLiteSessionStore(sessionDBFile)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore() error = %v", err)
	}
	defer func() {
		_ = sessionStore.Close()
	}()

	service := vosservice.NewWithSessionStore(
		vosstore.NewJSONStateStore(stateFile),
		sessionStore,
	)
	gateway, err := NewDirectVOSGateway(service)
	if err != nil {
		t.Fatalf("NewDirectVOSGateway() error = %v", err)
	}

	_, _, err = service.CreateTopic(vosservice.CreateTopicInput{
		TopicID: "topic-a",
		Name:    "topic-a",
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	firstID, err := gateway.EnsurePriorityNode(context.Background(), "topic-a")
	if err != nil {
		t.Fatalf("EnsurePriorityNode() first error = %v", err)
	}
	secondID, err := gateway.EnsurePriorityNode(context.Background(), "topic-a")
	if err != nil {
		t.Fatalf("EnsurePriorityNode() second error = %v", err)
	}
	if firstID == "" {
		t.Fatalf("first priority node id should not be empty")
	}
	if firstID != secondID {
		t.Fatalf("priority node id mismatch: first=%s second=%s", firstID, secondID)
	}
}

func TestDirectVOSGatewaySessionAndEvents(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := filepath.Join(tempDir, "vos_state.json")
	sessionDBFile := filepath.Join(tempDir, "openmate.db")

	sessionStore, err := vosstore.NewSQLiteSessionStore(sessionDBFile)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore() error = %v", err)
	}
	defer func() {
		_ = sessionStore.Close()
	}()

	service := vosservice.NewWithSessionStore(
		vosstore.NewJSONStateStore(stateFile),
		sessionStore,
	)
	gateway, err := NewDirectVOSGateway(service)
	if err != nil {
		t.Fatalf("NewDirectVOSGateway() error = %v", err)
	}

	_, rootNode, err := service.CreateTopic(vosservice.CreateTopicInput{Name: "topic-b"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	if rootNode == nil {
		t.Fatalf("root node should not be nil")
	}

	sessionID, err := gateway.EnsureSession(context.Background(), rootNode.ID, nil)
	if err != nil {
		t.Fatalf("EnsureSession() error = %v", err)
	}
	if sessionID == "" {
		t.Fatalf("session id should not be empty")
	}

	startEvent, err := gateway.AppendDispatchAuthorizedEvent(context.Background(), sessionID, map[string]any{
		"kind": "dispatch_authorized",
	})
	if err != nil {
		t.Fatalf("AppendDispatchAuthorizedEvent() error = %v", err)
	}
	if startEvent.Seq != 1 {
		t.Fatalf("start event seq = %d, want 1", startEvent.Seq)
	}

	if err := gateway.AppendDispatchResultEvent(context.Background(), sessionID, map[string]any{
		"kind":   "dispatch_result",
		"status": "succeeded",
	}); err != nil {
		t.Fatalf("AppendDispatchResultEvent() error = %v", err)
	}

	sessionRecord, err := service.GetSession(sessionID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if sessionRecord.Status != domain.SessionStatusCompleted {
		t.Fatalf("session status = %s, want completed", sessionRecord.Status)
	}

	events, err := service.ListSessionEvents(sessionID, 0, 100)
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
}
