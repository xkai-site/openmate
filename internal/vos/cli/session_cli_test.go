package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"vos/internal/vos/cli"
	"vos/internal/vos/domain"
)

func TestSessionCLIFlow(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := tempDir + "/vos_state.json"
	sessionDBFile := tempDir + "/vos_sessions.db"
	base := []string{"--state-file", stateFile, "--session-db-file", sessionDBFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-1", "--name", "Node One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(append(base, "session", "create", "--node-id", "node-1", "--session-id", "session-1"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("session create code = %d, want 0, stderr=%q", code, stderr.String())
	}

	var session domain.Session
	if err := json.Unmarshal(stdout.Bytes(), &session); err != nil {
		t.Fatalf("json.Unmarshal(session) error = %v", err)
	}
	if session.ID != "session-1" {
		t.Fatalf("session ID = %s, want session-1", session.ID)
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(
		append(base, "session", "append-event", "--session-id", "session-1", "--kind", "user_message", "--payload-json", `{"text":"hello"}`),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("session append-event user code = %d, want 0, stderr=%q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(
		append(base, "session", "append-event", "--session-id", "session-1", "--kind", "tool_result", "--call-id", "call-1", "--payload-json", `{"success":true}`),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("session append-event tool result code = %d, want 0, stderr=%q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(append(base, "session", "events", "--session-id", "session-1"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("session events code = %d, want 0, stderr=%q", code, stderr.String())
	}

	var events []domain.SessionEvent
	if err := json.Unmarshal(stdout.Bytes(), &events); err != nil {
		t.Fatalf("json.Unmarshal(events) error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("len(events) = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("event seqs = [%d %d], want [1 2]", events[0].Seq, events[1].Seq)
	}
	if events[0].Role == nil || *events[0].Role != domain.SessionRoleUser {
		t.Fatalf("events[0].Role = %v, want user", events[0].Role)
	}
	if events[1].CallID == nil || *events[1].CallID != "call-1" {
		t.Fatalf("events[1].CallID = %v, want call-1", events[1].CallID)
	}
}
