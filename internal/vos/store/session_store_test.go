package store_test

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

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
		Status:    domain.SessionStatusActive,
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

	callID := "call-1"
	event := &domain.SessionEvent{
		ID:        "event-1",
		SessionID: session.ID,
		ItemType:  "function_call",
		CallID:    &callID,
		PayloadJSON: map[string]any{
			"name": "read_file",
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
	if events[0].PayloadJSON["name"] != "read_file" {
		t.Fatalf("PayloadJSON = %v, want name=read_file", events[0].PayloadJSON)
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
		Status:    domain.SessionStatusActive,
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
		ItemType:  "function_call",
		CallID:    stringPtr("call-2"),
		PayloadJSON: map[string]any{
			"name": "read_file",
		},
		CreatedAt: time.Now().UTC(),
	}
	if _, err := sessionStore.AppendEvent(event, nil); err == nil {
		t.Fatalf("AppendEvent() error = nil, want sequence conflict")
	}
}

func TestSQLiteSessionStoreListEventsByCallID(t *testing.T) {
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
		Status:    domain.SessionStatusActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		LastSeq:   0,
	}
	if err := sessionStore.CreateSession(session); err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	callID := "call-1"
	callEvent := &domain.SessionEvent{
		ID:          "event-1",
		SessionID:   session.ID,
		ItemType:    "function_call",
		CallID:      &callID,
		PayloadJSON: map[string]any{"name": "read_file"},
		CreatedAt:   time.Now().UTC(),
	}
	if _, err := sessionStore.AppendEvent(callEvent, nil); err != nil {
		t.Fatalf("AppendEvent(function_call) error = %v", err)
	}

	outputEvent := &domain.SessionEvent{
		ID:          "event-2",
		SessionID:   session.ID,
		ItemType:    "function_call_output",
		CallID:      &callID,
		PayloadJSON: map[string]any{"output": "ok"},
		CreatedAt:   time.Now().UTC(),
	}
	if _, err := sessionStore.AppendEvent(outputEvent, nil); err != nil {
		t.Fatalf("AppendEvent(function_call_output) error = %v", err)
	}

	events, err := sessionStore.ListEventsByCallID(session.ID, callID, 10)
	if err != nil {
		t.Fatalf("ListEventsByCallID() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].ItemType != "function_call" || events[1].ItemType != "function_call_output" {
		t.Fatalf("item types = [%s %s], want [function_call function_call_output]", events[0].ItemType, events[1].ItemType)
	}
}

func TestSQLiteSessionStoreRejectsLegacySchema(t *testing.T) {
	dbPath := t.TempDir() + "/sessions.db"
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("PRAGMA foreign_keys error = %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE sessions (
		   id TEXT PRIMARY KEY,
		   node_id TEXT NOT NULL,
		   status TEXT NOT NULL CHECK (status IN ('open', 'closed', 'failed')),
		   created_at TEXT NOT NULL,
		   updated_at TEXT NOT NULL,
		   last_seq INTEGER NOT NULL CHECK (last_seq >= 0)
		)
	`); err != nil {
		t.Fatalf("create legacy sessions error = %v", err)
	}
	if _, err := db.Exec(`
		CREATE TABLE session_events (
		   id TEXT PRIMARY KEY,
		   session_id TEXT NOT NULL,
		   seq INTEGER NOT NULL CHECK (seq > 0),
		   kind TEXT NOT NULL CHECK (kind IN ('user_message', 'assistant_message', 'tool_call', 'tool_result', 'status', 'error')),
		   role TEXT CHECK (role IN ('user', 'assistant', 'tool', 'system')),
		   call_id TEXT,
		   payload_json TEXT NOT NULL,
		   created_at TEXT NOT NULL,
		   FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
		   UNIQUE (session_id, seq)
		)
	`); err != nil {
		t.Fatalf("create legacy session_events error = %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO sessions (id, node_id, status, created_at, updated_at, last_seq)
		VALUES ('session-1', 'node-1', 'open', '2026-04-14T00:00:00Z', '2026-04-14T00:00:00Z', 1)
	`); err != nil {
		t.Fatalf("insert legacy session error = %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO session_events (id, session_id, seq, kind, role, call_id, payload_json, created_at)
		VALUES ('event-1', 'session-1', 1, 'tool_call', 'assistant', 'call-1', '{"name":"read"}', '2026-04-14T00:00:00Z')
	`); err != nil {
		t.Fatalf("insert legacy event error = %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("db.Close() error = %v", err)
	}

	sessionStore, err := store.NewSQLiteSessionStore(dbPath)
	if err == nil {
		_ = sessionStore.Close()
		t.Fatalf("NewSQLiteSessionStore() error = nil, want legacy schema rejection")
	}
}

func stringPtr(raw string) *string {
	return &raw
}
