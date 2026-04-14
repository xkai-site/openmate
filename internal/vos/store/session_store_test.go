package store_test

import (
	"testing"
	"time"

	"vos/internal/vos/domain"
	"vos/internal/vos/store"
)

func TestSQLiteSessionStoreCreateGetAndAppendEvent(t *testing.T) {
	dbPath := t.TempDir() + "/sessions.db"
	sessionStore, err := store.NewSQLiteSessionStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = sessionStore.Close()
	})

	session := &domain.Session{
		ID:        "session-1",
		NodeID:    "node-1",
		Status:    domain.SessionStatusOpen,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		LastSeq:   0,
	}
	if err := sessionStore.CreateSession(session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	stored, err := sessionStore.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if stored.NodeID != "node-1" {
		t.Fatalf("NodeID = %s, want node-1", stored.NodeID)
	}
	if stored.LastSeq != 0 {
		t.Fatalf("LastSeq = %d, want 0", stored.LastSeq)
	}

	role := domain.SessionRoleUser
	event := &domain.SessionEvent{
		ID:        "event-1",
		SessionID: session.ID,
		Kind:      domain.SessionEventKindUserMessage,
		Role:      &role,
		PayloadJSON: map[string]any{
			"text": "hello",
		},
		CreatedAt: time.Now().UTC(),
	}
	updated, err := sessionStore.AppendEvent(event, nil)
	if err != nil {
		t.Fatalf("AppendEvent() error = %v", err)
	}
	if event.Seq != 1 {
		t.Fatalf("Seq = %d, want 1", event.Seq)
	}
	if updated.LastSeq != 1 {
		t.Fatalf("LastSeq = %d, want 1", updated.LastSeq)
	}

	events, err := sessionStore.ListEvents(session.ID, 0, 10)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if events[0].PayloadJSON["text"] != "hello" {
		t.Fatalf("PayloadJSON = %v, want text=hello", events[0].PayloadJSON)
	}
}

func TestSQLiteSessionStoreAppendEventRejectsWrongSeq(t *testing.T) {
	dbPath := t.TempDir() + "/sessions.db"
	sessionStore, err := store.NewSQLiteSessionStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteSessionStore() error = %v", err)
	}
	t.Cleanup(func() {
		_ = sessionStore.Close()
	})

	session := &domain.Session{
		ID:        "session-1",
		NodeID:    "node-1",
		Status:    domain.SessionStatusOpen,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		LastSeq:   0,
	}
	if err := sessionStore.CreateSession(session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	event := &domain.SessionEvent{
		ID:        "event-1",
		SessionID: session.ID,
		Seq:       3,
		Kind:      domain.SessionEventKindStatus,
		PayloadJSON: map[string]any{
			"status": "closed",
		},
		CreatedAt: time.Now().UTC(),
	}
	if _, err := sessionStore.AppendEvent(event, nil); err == nil {
		t.Fatalf("AppendEvent() error = nil, want sequence conflict")
	}
}
