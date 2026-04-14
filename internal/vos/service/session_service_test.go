package service_test

import (
	"testing"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

func newTestServiceWithSessions(t *testing.T) *service.Service {
	t.Helper()
	tempDir := t.TempDir()
	stateStore := store.NewJSONStateStore(tempDir + "/vos_state.json")
	sessionStore, err := store.NewSQLiteSessionStore(tempDir + "/vos_sessions.db")
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = sessionStore.Close()
	})
	return service.NewWithSessionStore(stateStore, sessionStore)
}

func TestCreateSessionAppendsNodeReference(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-1", Name: "Node One"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	session, err := svc.CreateSession(service.CreateSessionInput{NodeID: node.ID})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.NodeID != node.ID {
		t.Fatalf("NodeID = %s, want %s", session.NodeID, node.ID)
	}
	if session.Status != domain.SessionStatusActive {
		t.Fatalf("Status = %s, want active", session.Status)
	}
	if session.LastSeq != 0 {
		t.Fatalf("LastSeq = %d, want 0", session.LastSeq)
	}

	storedNode, err := svc.GetNode(node.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if len(storedNode.Session) != 1 || storedNode.Session[0] != session.ID {
		t.Fatalf("Node.Session = %v, want [%s]", storedNode.Session, session.ID)
	}
}

func TestAppendSessionEventStoresOrderedEvents(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-1", Name: "Node One"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	session, err := svc.CreateSession(service.CreateSessionInput{NodeID: node.ID})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	userEvent, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call",
		CallID:    stringPtr("call-0"),
		PayloadJSON: map[string]any{
			"role": "user",
			"name": "prepare",
		},
	})
	if err != nil {
		t.Fatalf("AppendSessionEvent(user) error = %v", err)
	}
	if userEvent.Seq != 1 {
		t.Fatalf("user event seq = %d, want 1", userEvent.Seq)
	}
	if userEvent.Role == nil || *userEvent.Role != domain.SessionRoleUser {
		t.Fatalf("user event role = %v, want user", userEvent.Role)
	}

	callID := "call-1"
	toolCall, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call_output",
		CallID:    &callID,
		PayloadJSON: map[string]any{
			"output": "ok",
		},
	})
	if err != nil {
		t.Fatalf("AppendSessionEvent(function_call) error = %v", err)
	}
	if toolCall.Seq != 2 {
		t.Fatalf("tool call seq = %d, want 2", toolCall.Seq)
	}

	events, err := svc.ListSessionEvents(session.ID, 1, 10)
	if err != nil {
		t.Fatalf("ListSessionEvents() error = %v", err)
	}
	if len(events) != 1 || events[0].Seq != 2 {
		t.Fatalf("events after seq 1 = %#v, want only seq 2", events)
	}

	storedSession, err := svc.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if storedSession.LastSeq != 2 {
		t.Fatalf("LastSeq = %d, want 2", storedSession.LastSeq)
	}
}

func TestAppendSessionEventRequiresCallIDForToolEvents(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-1", Name: "Node One"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	session, err := svc.CreateSession(service.CreateSessionInput{NodeID: node.ID})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call_output",
		PayloadJSON: map[string]any{
			"success": true,
		},
	}); err == nil {
		t.Fatalf("AppendSessionEvent() error = nil, want call ID validation")
	}
}

func TestAppendSessionEventUpdatesStatusOnlyViaNextStatus(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-1", Name: "Node One"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	session, err := svc.CreateSession(service.CreateSessionInput{NodeID: node.ID})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	waiting := domain.SessionStatusWaiting
	callID := "call-1"
	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call",
		CallID:    &callID,
		PayloadJSON: map[string]any{
			"name": "read",
		},
		NextStatus: &waiting,
	}); err != nil {
		t.Fatalf("AppendSessionEvent(waiting) error = %v", err)
	}
	stored, err := svc.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if stored.Status != domain.SessionStatusWaiting {
		t.Fatalf("Status = %s, want waiting", stored.Status)
	}

	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call_output",
		CallID:    &callID,
		PayloadJSON: map[string]any{
			"output": "done",
		},
	}); err != nil {
		t.Fatalf("AppendSessionEvent(function_call_output) error = %v", err)
	}
	stored, err = svc.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if stored.Status != domain.SessionStatusWaiting {
		t.Fatalf("Status = %s, want waiting unchanged", stored.Status)
	}

	completed := domain.SessionStatusCompleted
	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call",
		CallID:    stringPtr("call-2"),
		PayloadJSON: map[string]any{
			"name": "finalize",
		},
		NextStatus: &completed,
	}); err != nil {
		t.Fatalf("AppendSessionEvent(completed) error = %v", err)
	}
	stored, err = svc.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if stored.Status != domain.SessionStatusCompleted {
		t.Fatalf("Status = %s, want completed", stored.Status)
	}
}

func TestAppendSessionEventRejectsUnsupportedItemType(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-1", Name: "Node One"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	session, err := svc.CreateSession(service.CreateSessionInput{NodeID: node.ID})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "message",
		PayloadJSON: map[string]any{
			"text": "not allowed",
		},
	}); err == nil {
		t.Fatalf("AppendSessionEvent() error = nil, want unsupported item type validation")
	}
}

func TestListSessionEventsByCallID(t *testing.T) {
	svc := newTestServiceWithSessions(t)

	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-1", Name: "Node One"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	session, err := svc.CreateSession(service.CreateSessionInput{NodeID: node.ID})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	callID := "call-42"
	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call",
		CallID:    &callID,
		PayloadJSON: map[string]any{
			"name": "read_file",
		},
	}); err != nil {
		t.Fatalf("AppendSessionEvent(function_call) error = %v", err)
	}
	if _, err := svc.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: session.ID,
		ItemType:  "function_call_output",
		CallID:    &callID,
		PayloadJSON: map[string]any{
			"output": "ok",
		},
	}); err != nil {
		t.Fatalf("AppendSessionEvent(function_call_output) error = %v", err)
	}

	events, err := svc.ListSessionEventsByCallID(session.ID, callID, 10)
	if err != nil {
		t.Fatalf("ListSessionEventsByCallID() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].ItemType != "function_call" || events[1].ItemType != "function_call_output" {
		t.Fatalf("item types = [%s %s], want [function_call function_call_output]", events[0].ItemType, events[1].ItemType)
	}
}
