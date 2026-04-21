package cli_test

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"vos/internal/vos/cli"
	"vos/internal/vos/domain"
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
	if code := cli.Run(append(base, "topic", "update", "--topic-id", "topic-1", "--name", "Topic Prime", "--tags-json", `["go","vos"]`), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic update code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-1", "--name", "Node One", "--status", "active"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-done", "--name", "Done Node", "--status", "done"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create done code = %d, want 0", code)
	}

	svc := service.New(store.NewJSONStateStore(stateFile))
	node, err := svc.GetNode("node-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}

	if code := cli.Run(
		append(
			base,
			"node", "update",
			"--node-id", "node-1",
			"--expected-version", strconv.Itoa(node.Version),
			"--name", "Node Prime",
			"--session-id", "session-1",
			"--progress", "created",
			"--memory-json", `{"summary":"ready"}`,
		),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("node update code = %d, want 0", code)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(append(base, "node", "list", "--topic-id", "topic-1", "--leaf-only", "--exclude-status", "done"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("node list code = %d, want 0, stderr=%q", code, stderr.String())
	}

	var nodes []domain.Node
	if err := json.Unmarshal(stdout.Bytes(), &nodes); err != nil {
		t.Fatalf("json.Unmarshal(node list) error = %v", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-1" {
		t.Fatalf("node list result = %#v, want [node-1]", nodes)
	}

	topic, err := svc.GetTopic("topic-1")
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	updatedNode, err := svc.GetNode("node-1")
	if err != nil {
		t.Fatalf("GetNode(updated) error = %v", err)
	}
	rootNode, err := svc.GetNode(topic.RootNodeID)
	if err != nil {
		t.Fatalf("GetNode(root) error = %v", err)
	}

	if topic.Name != "Topic Prime" {
		t.Fatalf("topic name = %s, want Topic Prime", topic.Name)
	}
	if len(topic.Tags) != 2 || topic.Tags[0] != "go" || topic.Tags[1] != "vos" {
		t.Fatalf("topic tags = %v, want [go vos]", topic.Tags)
	}
	if updatedNode.Name != "Node Prime" {
		t.Fatalf("node name = %s, want Node Prime", updatedNode.Name)
	}
	if len(updatedNode.Session) != 1 || updatedNode.Session[0] != "session-1" {
		t.Fatalf("Session = %v, want [session-1]", updatedNode.Session)
	}
	if len(updatedNode.Progress) != 1 || updatedNode.Progress[0] != "created" {
		t.Fatalf("Progress = %v, want [created]", updatedNode.Progress)
	}
	cache, ok := rootNode.Memory["_child_memory_cache"].(map[string]any)
	if !ok {
		t.Fatalf("root memory cache missing: %v", rootNode.Memory)
	}
	if _, ok := cache["node-1"]; !ok {
		t.Fatalf("root memory cache entry missing for node-1: %v", cache)
	}
}

func TestNodeCreateDefaultsToDefaultTopic(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(append(base, "node", "create", "--name", "Quick Node"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("node create code = %d, want 0, stderr=%q", code, stderr.String())
	}

	createdNode := domain.Node{}
	if err := json.Unmarshal(stdout.Bytes(), &createdNode); err != nil {
		t.Fatalf("json.Unmarshal(node create) error = %v", err)
	}
	if createdNode.TopicID != service.DefaultTopicID {
		t.Fatalf("node topic_id = %q, want %q", createdNode.TopicID, service.DefaultTopicID)
	}

	svc := service.New(store.NewJSONStateStore(stateFile))
	defaultTopic, err := svc.GetTopic(service.DefaultTopicID)
	if err != nil {
		t.Fatalf("GetTopic(default) error = %v", err)
	}
	if createdNode.ParentID == nil || *createdNode.ParentID != defaultTopic.RootNodeID {
		t.Fatalf("node parent_id = %v, want %s", createdNode.ParentID, defaultTopic.RootNodeID)
	}
}

func TestTopicDeleteCommandRemovesTopicTree(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-1", "--name", "Node One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(append(base, "topic", "delete", "--topic-id", "topic-1"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("topic delete code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"deleted_node_ids"`) {
		t.Fatalf("topic delete output = %q, want deleted_node_ids", stdout.String())
	}

	code = cli.Run(append(base, "topic", "get", "--topic-id", "topic-1"), &bytes.Buffer{}, &stderr)
	if code != 2 {
		t.Fatalf("topic get after delete code = %d, want 2", code)
	}
}

func TestNodeUpdateVersionConflictReturnsUserError(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-1", "--name", "Node One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}

	var stderr bytes.Buffer
	code := cli.Run(
		append(base, "node", "update", "--node-id", "node-1", "--expected-version", "99", "--progress", "created"),
		&bytes.Buffer{},
		&stderr,
	)
	if code != 2 {
		t.Fatalf("node update conflict code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "version conflict") {
		t.Fatalf("stderr = %q, want version conflict", stderr.String())
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
