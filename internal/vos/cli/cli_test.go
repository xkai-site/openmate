package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
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
			"--process-json", `[{"name":"created","status":"done"}]`,
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
	if len(updatedNode.Process) != 1 {
		t.Fatalf("Process length = %d, want 1", len(updatedNode.Process))
	}
	if updatedNode.Process[0].Name != "created" || updatedNode.Process[0].Status != domain.ProcessStatusDone {
		t.Fatalf("Process[0] = %+v, want name=created status=done", updatedNode.Process[0])
	}
	if updatedNode.Process[0].Timestamp.IsZero() {
		t.Fatalf("Process[0].Timestamp should not be zero")
	}
	cache, ok := rootNode.Memory["_child_memory_cache"].(map[string]any)
	if !ok {
		t.Fatalf("root memory cache missing: %v", rootNode.Memory)
	}
	if _, ok := cache["node-1"]; !ok {
		t.Fatalf("root memory cache entry missing for node-1: %v", cache)
	}
}

func TestNodeCreateWithoutTopicCreatesIndependentTopic(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	var firstStdout bytes.Buffer
	var stderr bytes.Buffer
	if code := cli.Run(append(base, "node", "create", "--name", "Quick Node A"), &firstStdout, &stderr); code != 0 {
		t.Fatalf("first node create code = %d, want 0, stderr=%q", code, stderr.String())
	}
	var secondStdout bytes.Buffer
	if code := cli.Run(append(base, "node", "create", "--name", "Quick Node B"), &secondStdout, &stderr); code != 0 {
		t.Fatalf("second node create code = %d, want 0, stderr=%q", code, stderr.String())
	}

	firstNode := domain.Node{}
	if err := json.Unmarshal(firstStdout.Bytes(), &firstNode); err != nil {
		t.Fatalf("json.Unmarshal(first node create) error = %v", err)
	}
	secondNode := domain.Node{}
	if err := json.Unmarshal(secondStdout.Bytes(), &secondNode); err != nil {
		t.Fatalf("json.Unmarshal(second node create) error = %v", err)
	}
	if firstNode.TopicID == secondNode.TopicID {
		t.Fatalf("topic IDs should differ, got %q", firstNode.TopicID)
	}

	svc := service.New(store.NewJSONStateStore(stateFile))
	firstTopic, err := svc.GetTopic(firstNode.TopicID)
	if err != nil {
		t.Fatalf("GetTopic(first) error = %v", err)
	}
	secondTopic, err := svc.GetTopic(secondNode.TopicID)
	if err != nil {
		t.Fatalf("GetTopic(second) error = %v", err)
	}
	if firstNode.ParentID == nil || *firstNode.ParentID != firstTopic.RootNodeID {
		t.Fatalf("first node parent_id = %v, want %s", firstNode.ParentID, firstTopic.RootNodeID)
	}
	if secondNode.ParentID == nil || *secondNode.ParentID != secondTopic.RootNodeID {
		t.Fatalf("second node parent_id = %v, want %s", secondNode.ParentID, secondTopic.RootNodeID)
	}
}

func TestNodeDecomposeHelpAvailable(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	sessionDBFile := t.TempDir() + "/openmate.db"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(
		[]string{
			"--state-file", stateFile,
			"--session-db-file", sessionDBFile,
			"node", "decompose", "--help",
		},
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("node decompose --help code = %d, want 0, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "vos node decompose") {
		t.Fatalf("help output = %q, want vos node decompose usage", stderr.String())
	}
}

func TestNodeDecomposeCreatesDirectChildren(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	sessionDBFile := t.TempDir() + "/openmate.db"
	base := []string{"--state-file", stateFile, "--session-db-file", sessionDBFile}

	if code := cli.Run(
		append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-parent", "--name", "Parent Node"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "session", "create", "--node-id", "node-parent", "--session-id", "session-parent"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("session create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "session", "append-event", "--session-id", "session-parent", "--item-type", "message", "--payload-json", `{"role":"user","content":"split task"}`),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("session append-event code = %d, want 0", code)
	}

	t.Setenv("OPENMATE_DECOMPOSE_HELPER", "1")
	t.Setenv("OPENMATE_DECOMPOSE_HELPER_MODE", "success")
	helperCommand := os.Args[0] + " -test.run TestNodeDecomposeAgentHelper --"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(
		append(
			base,
			"node", "decompose",
			"--node-id", "node-parent",
			"--hint", "focus first runnable tasks",
			"--max-items", "2",
			"--agent-command", helperCommand,
		),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("node decompose code = %d, want 0, stderr=%q", code, stderr.String())
	}

	var output struct {
		Status       string        `json:"status"`
		Tasks        []any         `json:"tasks"`
		CreatedNodes []domain.Node `json:"created_nodes"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("json.Unmarshal(node decompose) error = %v", err)
	}
	if output.Status != "succeeded" {
		t.Fatalf("status = %s, want succeeded", output.Status)
	}
	if len(output.Tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(output.Tasks))
	}
	if len(output.CreatedNodes) != 2 {
		t.Fatalf("len(created_nodes) = %d, want 2", len(output.CreatedNodes))
	}
	for _, node := range output.CreatedNodes {
		if node.ParentID == nil || *node.ParentID != "node-parent" {
			t.Fatalf("created node parent_id = %v, want node-parent", node.ParentID)
		}
		if node.Status != domain.NodeStatusReady {
			t.Fatalf("created node status = %s, want ready", node.Status)
		}
	}

	svc := service.New(store.NewJSONStateStore(stateFile))
	children, err := svc.ListChildren("node-parent")
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("len(children) = %d, want 2", len(children))
	}
}

func TestNodeDecomposeReturnsErrorWhenTargetMissing(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	sessionDBFile := t.TempDir() + "/openmate.db"
	base := []string{"--state-file", stateFile, "--session-db-file", sessionDBFile}

	t.Setenv("OPENMATE_DECOMPOSE_HELPER", "1")
	t.Setenv("OPENMATE_DECOMPOSE_HELPER_MODE", "success")
	helperCommand := os.Args[0] + " -test.run TestNodeDecomposeAgentHelper --"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(
		append(base, "node", "decompose", "--node-id", "missing-node", "--agent-command", helperCommand),
		&stdout,
		&stderr,
	)
	if code != 2 {
		t.Fatalf("node decompose missing target code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "node not found") {
		t.Fatalf("stderr = %q, want node not found", stderr.String())
	}
}

func TestNodeDecomposeReturnsErrorWhenAgentFailed(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	sessionDBFile := t.TempDir() + "/openmate.db"
	base := []string{"--state-file", stateFile, "--session-db-file", sessionDBFile}

	if code := cli.Run(
		append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-parent", "--name", "Parent Node"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}

	t.Setenv("OPENMATE_DECOMPOSE_HELPER", "1")
	t.Setenv("OPENMATE_DECOMPOSE_HELPER_MODE", "failed")
	helperCommand := os.Args[0] + " -test.run TestNodeDecomposeAgentHelper --"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(
		append(base, "node", "decompose", "--node-id", "node-parent", "--agent-command", helperCommand),
		&stdout,
		&stderr,
	)
	if code != 2 {
		t.Fatalf("node decompose agent failed code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "helper failed") {
		t.Fatalf("stderr = %q, want helper failed", stderr.String())
	}
}

func TestNodeDecomposeReturnsErrorWhenAgentReturnedEmptyTasks(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	sessionDBFile := t.TempDir() + "/openmate.db"
	base := []string{"--state-file", stateFile, "--session-db-file", sessionDBFile}

	if code := cli.Run(
		append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(
		append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-parent", "--name", "Parent Node"),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}

	t.Setenv("OPENMATE_DECOMPOSE_HELPER", "1")
	t.Setenv("OPENMATE_DECOMPOSE_HELPER_MODE", "empty")
	helperCommand := os.Args[0] + " -test.run TestNodeDecomposeAgentHelper --"

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := cli.Run(
		append(base, "node", "decompose", "--node-id", "node-parent", "--agent-command", helperCommand),
		&stdout,
		&stderr,
	)
	if code != 2 {
		t.Fatalf("node decompose empty tasks code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "empty tasks") {
		t.Fatalf("stderr = %q, want empty tasks", stderr.String())
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
		append(base, "node", "update", "--node-id", "node-1", "--expected-version", "99", "--process-json", `[{"name":"created","status":"done"}]`),
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

func TestNodeUpdateWithProcessJSON(t *testing.T) {
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
	code := cli.Run(
		append(
			base,
			"node", "update",
			"--node-id", "node-1",
			"--process-json", `[{"name":"queued","status":"todo"},{"name":"running","status":"done"}]`,
		),
		&stdout,
		&stderr,
	)
	if code != 0 {
		t.Fatalf("node update code = %d, want 0, stderr=%q", code, stderr.String())
	}

	updated := domain.Node{}
	if err := json.Unmarshal(stdout.Bytes(), &updated); err != nil {
		t.Fatalf("json.Unmarshal(node update) error = %v", err)
	}
	if len(updated.Process) != 2 {
		t.Fatalf("Process length = %d, want 2", len(updated.Process))
	}
	if updated.Process[0].Name != "queued" || updated.Process[0].Status != domain.ProcessStatusTodo {
		t.Fatalf("Process[0] = %+v, want queued/todo", updated.Process[0])
	}
	if updated.Process[1].Name != "running" || updated.Process[1].Status != domain.ProcessStatusDone {
		t.Fatalf("Process[1] = %+v, want running/done", updated.Process[1])
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

func TestNodeDecomposeAgentHelper(t *testing.T) {
	if os.Getenv("OPENMATE_DECOMPOSE_HELPER") != "1" {
		return
	}

	requestFile := ""
	for index := 0; index < len(os.Args)-1; index++ {
		if os.Args[index] == "--request-file" {
			requestFile = os.Args[index+1]
			break
		}
	}

	response := map[string]any{
		"request_id":  "helper-req",
		"topic_id":    "helper-topic",
		"node_id":     "helper-node",
		"duration_ms": 3,
	}
	switch os.Getenv("OPENMATE_DECOMPOSE_HELPER_MODE") {
	case "failed":
		response["status"] = "failed"
		response["error"] = "helper failed"
		response["tasks"] = []any{}
	case "empty":
		response["status"] = "succeeded"
		response["output"] = "helper empty"
		response["tasks"] = []any{}
	default:
		if requestFile != "" {
			if raw, err := os.ReadFile(requestFile); err == nil {
				var requestPayload map[string]any
				if jsonErr := json.Unmarshal(raw, &requestPayload); jsonErr == nil {
					if value, ok := requestPayload["request_id"].(string); ok && value != "" {
						response["request_id"] = value
					}
					if value, ok := requestPayload["topic_id"].(string); ok && value != "" {
						response["topic_id"] = value
					}
					if value, ok := requestPayload["node_id"].(string); ok && value != "" {
						response["node_id"] = value
					}
				}
			}
		}
		response["status"] = "succeeded"
		response["output"] = "helper success"
		response["tasks"] = []map[string]any{
			{
				"title":       "Business scope alignment",
				"description": "Align user value and acceptance.",
				"status":      "ready",
			},
			{
				"title":       "First delivery slice",
				"description": "Define first executable child task.",
				"status":      "pending",
			},
		}
	}

	raw, _ := json.Marshal(response)
	_, _ = os.Stdout.Write(raw)
	os.Exit(0)
}
