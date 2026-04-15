package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"vos/internal/vos/cli"
	"vos/internal/vos/domain"
)

func TestContextSnapshotCLIFlow(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := tempDir + "/vos_state.json"
	sessionDBFile := tempDir + "/vos_sessions.db"
	base := []string{"--state-file", stateFile, "--session-db-file", sessionDBFile}

	if code := cli.Run(
		append(
			base,
			"topic", "create",
			"--topic-id", "topic-1",
			"--name", "Topic One",
			"--metadata-json", `{"user_memory":{"user_id":"u-1"},"topic_memory":{"summary":"topic-s"},"global_index":{"records":["r1"]}}`,
		),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-parent", "--name", "Parent"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node parent create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "node", "update", "--node-id", "node-parent", "--memory-json", `{"shared_summary":"from-parent"}`),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("node parent update memory code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-leaf", "--parent-id", "node-parent", "--name", "Leaf", "--memory-json", `{"leaf_private":"secret"}`),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("node leaf create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "session", "create", "--node-id", "node-leaf", "--session-id", "session-1"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("session create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "session", "append-event", "--session-id", "session-1", "--item-type", "message", "--payload-json", `{"role":"user","text":"hello"}`),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("session append-event code = %d, want 0", code)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(append(base, "context", "snapshot", "--node-id", "node-leaf"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("context snapshot code = %d, want 0, stderr=%q", code, stderr.String())
	}

	var snapshot domain.ContextSnapshot
	if err := json.Unmarshal(stdout.Bytes(), &snapshot); err != nil {
		t.Fatalf("json.Unmarshal(context snapshot) error = %v", err)
	}
	if snapshot.NodeID != "node-leaf" {
		t.Fatalf("NodeID = %s, want node-leaf", snapshot.NodeID)
	}
	if snapshot.UserMemory["user_id"] != "u-1" {
		t.Fatalf("UserMemory = %v, want user_id=u-1", snapshot.UserMemory)
	}
	if snapshot.TopicMemory["summary"] != "topic-s" {
		t.Fatalf("TopicMemory = %v, want summary=topic-s", snapshot.TopicMemory)
	}
	if snapshot.NodeMemory["shared_summary"] != "from-parent" {
		t.Fatalf("NodeMemory = %v, want parent shared_summary", snapshot.NodeMemory)
	}
	if len(snapshot.SessionHistory) != 1 {
		t.Fatalf("len(SessionHistory) = %d, want 1", len(snapshot.SessionHistory))
	}
	if snapshot.SessionHistory[0].Session == nil || snapshot.SessionHistory[0].Session.ID != "session-1" {
		t.Fatalf("SessionHistory[0].Session = %v, want session-1", snapshot.SessionHistory[0].Session)
	}
	if len(snapshot.SessionHistory[0].Events) != 1 {
		t.Fatalf("len(SessionHistory[0].Events) = %d, want 1", len(snapshot.SessionHistory[0].Events))
	}
	if snapshot.SessionHistory[0].Events[0].Seq != 1 {
		t.Fatalf("SessionHistory[0].Events[0].Seq = %d, want 1", snapshot.SessionHistory[0].Events[0].Seq)
	}
}

func TestContextSnapshotCLIRequiresNodeID(t *testing.T) {
	tempDir := t.TempDir()
	stateFile := tempDir + "/vos_state.json"
	sessionDBFile := tempDir + "/vos_sessions.db"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(
		[]string{
			"--state-file", stateFile,
			"--session-db-file", sessionDBFile,
			"context", "snapshot",
		},
		&stdout,
		&stderr,
	)
	if code != 2 {
		t.Fatalf("context snapshot without node-id code = %d, want 2", code)
	}
}
