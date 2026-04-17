package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
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
		append(base, "session", "append-event", "--session-id", "session-1", "--item-type", "function_call", "--call-id", "call-0", "--payload-json", `{"name":"prepare"}`),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("session append-event function_call code = %d, want 0, stderr=%q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(
		append(base, "session", "append-event", "--session-id", "session-1", "--item-type", "function_call_output", "--call-id", "call-1", "--payload-json", `{"output":"ok"}`),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("session append-event tool result code = %d, want 0, stderr=%q", code, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(
		append(base, "session", "append-event", "--session-id", "session-1", "--item-type", "message", "--payload-json", `{"role":"assistant","text":"done"}`),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("session append-event message code = %d, want 0, stderr=%q", code, stderr.String())
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
	if len(events) != 3 {
		t.Fatalf("len(events) = %d, want 3", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 || events[2].Seq != 3 {
		t.Fatalf("event seqs = [%d %d %d], want [1 2 3]", events[0].Seq, events[1].Seq, events[2].Seq)
	}
	if events[0].CallID == nil || *events[0].CallID != "call-0" {
		t.Fatalf("events[0].CallID = %v, want call-0", events[0].CallID)
	}
	if events[1].CallID == nil || *events[1].CallID != "call-1" {
		t.Fatalf("events[1].CallID = %v, want call-1", events[1].CallID)
	}
	if events[2].CallID != nil {
		t.Fatalf("events[2].CallID = %v, want nil", events[2].CallID)
	}
}

func TestSessionCLIRejectsLegacyFlagsAndStatuses(t *testing.T) {
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
	code := cli.Run(append(base, "session", "create", "--node-id", "node-1", "--status", "open"), &stdout, &stderr)
	if code == 0 {
		t.Fatalf("session create with legacy status should fail")
	}

	stdout.Reset()
	stderr.Reset()
	code = cli.Run(
		append(base, "session", "append-event", "--session-id", "session-1", "--kind", "tool_call", "--payload-json", `{"name":"legacy"}`),
		&stdout,
		&stderr,
	)
	if code == 0 {
		t.Fatalf("session append-event with --kind should fail")
	}
}

func TestSessionCLIUsesUnifiedDBFileByDefault(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := tempDir + "/vos_state.json"
	dbFile := tempDir + "/openmate.db"
	base := []string{"--state-file", stateFile, "--db-file", dbFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-1", "--name", "Node One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(append(base, "session", "create", "--node-id", "node-1", "--session-id", "session-unified"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("session create code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"id": "session-unified"`) {
		t.Fatalf("session create output = %q, want session-unified id", stdout.String())
	}
}
