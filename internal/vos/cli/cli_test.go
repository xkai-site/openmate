package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"vos/internal/vos/cli"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

func TestHelpAvailable(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := cli.Run([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("Run(--help) code = %d, want 0", code)
	}
	if !strings.Contains(stderr.String(), "vos") {
		t.Fatalf("help output missing command name: %q", stderr.String())
	}
}

func TestTopicAndNodeFlow(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One", "--metadata-json", `{"owner":"vos"}`), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-1", "--name", "Node One", "--status", "active"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "update", "--node-id", "node-1", "--session-id", "session-1", "--progress", "created", "--memory-json", `{"summary":"ready"}`), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node update code = %d, want 0", code)
	}

	svc := service.New(store.NewJSONStateStore(stateFile))
	topic, err := svc.GetTopic("topic-1")
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	node, err := svc.GetNode("node-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}

	if topic.Metadata["owner"] != "vos" {
		t.Fatalf("Metadata[owner] = %v, want vos", topic.Metadata["owner"])
	}
	if node.ParentID == nil || *node.ParentID != topic.RootNodeID {
		t.Fatalf("ParentID = %v, want %s", node.ParentID, topic.RootNodeID)
	}
	if len(node.Session) != 1 || node.Session[0] != "session-1" {
		t.Fatalf("Session = %v, want [session-1]", node.Session)
	}
	if len(node.Progress) != 1 || node.Progress[0] != "created" {
		t.Fatalf("Progress = %v, want [created]", node.Progress)
	}
	if node.Memory["summary"] != "ready" {
		t.Fatalf("Memory = %v, want summary=ready", node.Memory)
	}

	if code := cli.Run(append(base, "node", "leaf", "--node-id", "node-1"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node leaf code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "children", "--node-id", topic.RootNodeID), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node children code = %d, want 0", code)
	}
}

func TestInvalidJSONReturnsError(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := cli.Run(
		[]string{
			"--state-file", stateFile,
			"topic", "create",
			"--name", "Topic One",
			"--metadata-json", "[]",
		},
		&stdout,
		&stderr,
	)
	if code != 2 {
		t.Fatalf("Run(invalid json) code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "metadata-json must be a JSON object") {
		t.Fatalf("stderr = %q, want metadata-json error", stderr.String())
	}
}
