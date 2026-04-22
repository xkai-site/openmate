package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vos/internal/poolgateway"
	"vos/internal/vos/domain"
	"vos/internal/vos/service"
)

type apiEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func TestServerV1TopicAndNodeLifecycle(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	createTopicEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/topics", map[string]any{
		"name":        "Frontend Preview",
		"description": "UI validation topic",
	}, http.StatusOK)
	if createTopicEnv.Code != http.StatusOK || createTopicEnv.Message != "ok" {
		t.Fatalf("create topic envelope = %+v, want code=200 message=ok", createTopicEnv)
	}

	createTopicResp := struct {
		Topic struct {
			ID string `json:"id"`
		} `json:"topic"`
		RootNode struct {
			ID string `json:"id"`
		} `json:"root_node"`
	}{}
	mustDecodeEnvelopeData(t, createTopicEnv, &createTopicResp)
	if createTopicResp.Topic.ID == "" {
		t.Fatalf("create topic id should not be empty")
	}
	if createTopicResp.RootNode.ID == "" {
		t.Fatalf("create topic root node id should not be empty")
	}

	topicsEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodGet, testServer.URL+"/api/v1/topics", nil, http.StatusOK)
	topicsResp := []map[string]any{}
	mustDecodeEnvelopeData(t, topicsEnv, &topicsResp)
	if len(topicsResp) != 1 {
		t.Fatalf("topics len = %d, want 1", len(topicsResp))
	}

	createNodeResp := struct {
		ID      string `json:"id"`
		TopicID string `json:"topic_id"`
		Status  string `json:"status"`
	}{}
	createNodeEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/nodes", map[string]any{
		"topic_id": createTopicResp.Topic.ID,
		"name":     "Build UI draft",
		"status":   "waiting",
	}, http.StatusOK)
	mustDecodeEnvelopeData(t, createNodeEnv, &createNodeResp)
	if createNodeResp.ID == "" {
		t.Fatalf("create node id should not be empty")
	}
	if createNodeResp.TopicID != createTopicResp.Topic.ID {
		t.Fatalf("create node topic_id = %q, want %q", createNodeResp.TopicID, createTopicResp.Topic.ID)
	}
	if createNodeResp.Status != "waiting" {
		t.Fatalf("create node status = %q, want waiting", createNodeResp.Status)
	}

	nodesEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodGet, testServer.URL+"/api/v1/topics/"+createTopicResp.Topic.ID+"/nodes?leaf_only=true", nil, http.StatusOK)
	nodesResp := []map[string]any{}
	mustDecodeEnvelopeData(t, nodesEnv, &nodesResp)
	if len(nodesResp) != 1 {
		t.Fatalf("leaf nodes len = %d, want 1", len(nodesResp))
	}
	if nodesResp[0]["id"] != createNodeResp.ID {
		t.Fatalf("leaf node id = %v, want %s", nodesResp[0]["id"], createNodeResp.ID)
	}
}

func TestServerV1NodeCreateDefaultsToDefaultTopic(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	firstEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/nodes", map[string]any{
		"name": "Quick Node 1",
	}, http.StatusOK)
	secondEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/nodes", map[string]any{
		"name": "Quick Node 2",
	}, http.StatusOK)

	firstNode := struct {
		ID      string `json:"id"`
		TopicID string `json:"topic_id"`
	}{}
	secondNode := struct {
		ID      string `json:"id"`
		TopicID string `json:"topic_id"`
	}{}
	mustDecodeEnvelopeData(t, firstEnv, &firstNode)
	mustDecodeEnvelopeData(t, secondEnv, &secondNode)

	if firstNode.TopicID != service.DefaultTopicID {
		t.Fatalf("first node topic_id = %q, want %q", firstNode.TopicID, service.DefaultTopicID)
	}
	if secondNode.TopicID != service.DefaultTopicID {
		t.Fatalf("second node topic_id = %q, want %q", secondNode.TopicID, service.DefaultTopicID)
	}
	if firstNode.ID == secondNode.ID {
		t.Fatalf("node IDs should differ, got %q", firstNode.ID)
	}
}

func TestServerV1TreeRootsReturnsDisplayRoots(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	defaultNodeA, err := server.service.CreateNode(service.CreateNodeInput{
		Name: "Temp Root A",
	})
	if err != nil {
		t.Fatalf("CreateNode(default A) error = %v", err)
	}
	defaultNodeB, err := server.service.CreateNode(service.CreateNodeInput{
		Name: "Temp Root B",
	})
	if err != nil {
		t.Fatalf("CreateNode(default B) error = %v", err)
	}
	_, topicRoot, err := server.service.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-visible",
		Name:    "Visible Topic",
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	defaultTopic, err := server.service.GetTopic(service.DefaultTopicID)
	if err != nil {
		t.Fatalf("GetTopic(default) error = %v", err)
	}

	rootsEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodGet, testServer.URL+"/api/v1/tree/roots", nil, http.StatusOK)
	rootsResp := []map[string]any{}
	mustDecodeEnvelopeData(t, rootsEnv, &rootsResp)
	if len(rootsResp) != 3 {
		t.Fatalf("roots len = %d, want 3", len(rootsResp))
	}

	found := map[string]bool{}
	for _, item := range rootsResp {
		id, _ := item["id"].(string)
		found[id] = true
		if id == defaultTopic.RootNodeID {
			t.Fatalf("roots should not include default structural root: %s", id)
		}
	}
	if !found[defaultNodeA.ID] {
		t.Fatalf("roots missing default entry: %s", defaultNodeA.ID)
	}
	if !found[defaultNodeB.ID] {
		t.Fatalf("roots missing default entry: %s", defaultNodeB.ID)
	}
	if !found[topicRoot.ID] {
		t.Fatalf("roots missing topic root: %s", topicRoot.ID)
	}
}

func TestServerV1NodeDecomposeCreatesChildren(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	if _, _, err := server.service.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-decompose",
		Name:    "Decompose Topic",
	}); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parentNode, err := server.service.CreateNode(service.CreateNodeInput{
		TopicID: "topic-decompose",
		NodeID:  "node-parent",
		Name:    "Parent Node",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	gotRequest := service.DecomposeAgentRequest{}
	server.decomposeRunner = service.NodeDecomposeRunnerFunc(func(ctx context.Context, request service.DecomposeAgentRequest) (*service.DecomposeAgentResponse, error) {
		gotRequest = request
		return &service.DecomposeAgentResponse{
			RequestID:  request.RequestID,
			TopicID:    request.TopicID,
			NodeID:     request.NodeID,
			Status:     "succeeded",
			Output:     "ok",
			DurationMS: 7,
			Tasks: []service.DecomposeAgentTask{
				{
					Title:       "Task A",
					Description: "first child",
					Status:      "ready",
				},
				{
					Title:       "Task B",
					Description: "second child",
					Status:      "ready",
				},
			},
		}, nil
	})

	envelope := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/nodes/"+parentNode.ID+"/decompose", map[string]any{
		"hint":      "focus MVP",
		"max_items": 2,
	}, http.StatusOK)

	var result service.NodeDecomposeResult
	mustDecodeEnvelopeData(t, envelope, &result)
	if result.Status != "succeeded" {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if len(result.Tasks) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(result.Tasks))
	}
	if len(result.CreatedNodes) != 2 {
		t.Fatalf("len(created_nodes) = %d, want 2", len(result.CreatedNodes))
	}
	for _, created := range result.CreatedNodes {
		if created.ParentID == nil || *created.ParentID != parentNode.ID {
			t.Fatalf("created node parent_id = %v, want %s", created.ParentID, parentNode.ID)
		}
		if created.Status != domain.NodeStatusReady {
			t.Fatalf("created node status = %s, want ready", created.Status)
		}
	}

	if gotRequest.NodeID != parentNode.ID {
		t.Fatalf("runner request node_id = %q, want %q", gotRequest.NodeID, parentNode.ID)
	}
	if gotRequest.MaxItems != 2 {
		t.Fatalf("runner request max_items = %d, want 2", gotRequest.MaxItems)
	}
	if !strings.Contains(gotRequest.Hint, "focus MVP") {
		t.Fatalf("runner request hint = %q, want contains user hint", gotRequest.Hint)
	}

	children, err := server.service.ListChildren(parentNode.ID)
	if err != nil {
		t.Fatalf("ListChildren() error = %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("len(children) = %d, want 2", len(children))
	}
}

func TestServerV1NodeDecomposeReturnsNotFoundWhenNodeMissing(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	envelope := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/nodes/missing-node/decompose", map[string]any{}, http.StatusNotFound)
	if envelope.Code != http.StatusNotFound {
		t.Fatalf("code = %d, want %d", envelope.Code, http.StatusNotFound)
	}
	if !strings.Contains(envelope.Message, "node not found") {
		t.Fatalf("message = %q, want node not found", envelope.Message)
	}
}

func TestServerV1NodeDecomposeReturnsUserFacingErrors(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	if _, _, err := server.service.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-decompose-errors",
		Name:    "Decompose Topic Errors",
	}); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	parentNode, err := server.service.CreateNode(service.CreateNodeInput{
		TopicID: "topic-decompose-errors",
		NodeID:  "node-parent-errors",
		Name:    "Parent Node",
	})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}

	testCases := []struct {
		name        string
		runner      service.NodeDecomposeRunner
		wantStatus  int
		wantMessage string
	}{
		{
			name: "agent failed",
			runner: service.NodeDecomposeRunnerFunc(func(ctx context.Context, request service.DecomposeAgentRequest) (*service.DecomposeAgentResponse, error) {
				return &service.DecomposeAgentResponse{
					RequestID: request.RequestID,
					TopicID:   request.TopicID,
					NodeID:    request.NodeID,
					Status:    "failed",
					Error:     "agent failed",
				}, nil
			}),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "agent failed",
		},
		{
			name: "agent empty tasks",
			runner: service.NodeDecomposeRunnerFunc(func(ctx context.Context, request service.DecomposeAgentRequest) (*service.DecomposeAgentResponse, error) {
				return &service.DecomposeAgentResponse{
					RequestID: request.RequestID,
					TopicID:   request.TopicID,
					NodeID:    request.NodeID,
					Status:    "succeeded",
					Tasks:     []service.DecomposeAgentTask{},
				}, nil
			}),
			wantStatus:  http.StatusBadRequest,
			wantMessage: "empty tasks",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			server.decomposeRunner = testCase.runner
			envelope := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/nodes/"+parentNode.ID+"/decompose", map[string]any{}, testCase.wantStatus)
			if envelope.Code != testCase.wantStatus {
				t.Fatalf("code = %d, want %d", envelope.Code, testCase.wantStatus)
			}
			if !strings.Contains(envelope.Message, testCase.wantMessage) {
				t.Fatalf("message = %q, want contains %q", envelope.Message, testCase.wantMessage)
			}
		})
	}
}

func TestServerV1ReturnsValidationError(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	errorEnv := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/topics", map[string]any{
		"description": "name missing",
	}, http.StatusBadRequest)
	if errorEnv.Code != http.StatusBadRequest {
		t.Fatalf("error code = %d, want %d", errorEnv.Code, http.StatusBadRequest)
	}
	if errorEnv.Message == "" {
		t.Fatalf("error message should not be empty")
	}
}

func TestServerV1UnimplementedEndpoint(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	response := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/tree/generate", map[string]any{"demand": "build app"}, http.StatusNotImplemented)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusNotImplemented)
	}
	if response.Message != notImplementedV1Msg {
		t.Fatalf("response message = %q, want %q", response.Message, notImplementedV1Msg)
	}
}

func TestServerV1ChatRequiresPost(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	response := mustRequestEnvelope(t, testServer.Client(), http.MethodGet, testServer.URL+"/api/v1/chat", nil, http.StatusMethodNotAllowed)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}

func TestServerV1ChatValidatesMessage(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	response := mustRequestEnvelope(t, testServer.Client(), http.MethodPost, testServer.URL+"/api/v1/chat", map[string]any{
		"node_id": "node-1",
	}, http.StatusBadRequest)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if response.Message == "" {
		t.Fatalf("response message should not be empty")
	}
}

func TestResolveChatNodeUsesDefaultTopicWithoutNodeAndTopic(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	firstNode, err := server.resolveChatNode(v1ChatRequest{Message: "hello one"})
	if err != nil {
		t.Fatalf("resolveChatNode(first) error = %v", err)
	}
	secondNode, err := server.resolveChatNode(v1ChatRequest{Message: "hello two"})
	if err != nil {
		t.Fatalf("resolveChatNode(second) error = %v", err)
	}
	if firstNode.TopicID != service.DefaultTopicID {
		t.Fatalf("first node topic_id = %q, want %q", firstNode.TopicID, service.DefaultTopicID)
	}
	if secondNode.TopicID != service.DefaultTopicID {
		t.Fatalf("second node topic_id = %q, want %q", secondNode.TopicID, service.DefaultTopicID)
	}
	if firstNode.ID == secondNode.ID {
		t.Fatalf("chat nodes should differ, got %q", firstNode.ID)
	}

	topics, err := server.service.ListTopics()
	if err != nil {
		t.Fatalf("ListTopics() error = %v", err)
	}
	if len(topics) != 1 || topics[0].ID != service.DefaultTopicID {
		t.Fatalf("topics = %#v, want single default topic", topics)
	}
}

func TestResolveChatNodeUsesExplicitTopicWhenProvided(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	topic, _, err := server.service.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-explicit",
		Name:    "Explicit Topic",
	})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	topicID := topic.ID

	node, err := server.resolveChatNode(v1ChatRequest{
		TopicID: &topicID,
		Message: "hello explicit",
	})
	if err != nil {
		t.Fatalf("resolveChatNode() error = %v", err)
	}
	if node.TopicID != topic.ID {
		t.Fatalf("node topic_id = %q, want %q", node.TopicID, topic.ID)
	}
}

func TestNewServerRejectsInvalidScheduleMode(t *testing.T) {
	tempDir := t.TempDir()
	_, err := NewServer(Config{
		StateFile:     filepath.Join(tempDir, "vos_state.json"),
		SessionDBFile: filepath.Join(tempDir, "openmate.db"),
		ScheduleMode:  "invalid",
	})
	if err == nil {
		t.Fatalf("NewServer() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "schedule_mode") {
		t.Fatalf("error = %v, want schedule_mode validation", err)
	}
}

func TestServerIsAPIOnly(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	response, err := testServer.Client().Get(testServer.URL + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("GET / status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
}

func TestServerCORSPreflight(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	request, err := http.NewRequest(http.MethodOptions, testServer.URL+"/api/v1/topics", http.NoBody)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set("Origin", "http://127.0.0.1:8081")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if got := response.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if got := response.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatalf("Access-Control-Allow-Methods should not be empty")
	}
}

func TestServerOldAPIRoutesNotExposed(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	response, err := testServer.Client().Get(testServer.URL + "/api/topics")
	if err != nil {
		t.Fatalf("GET /api/topics error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /api/topics status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
}

func openTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	tempDir := t.TempDir()
	modelConfig := filepath.Join(tempDir, "model.json")
	if err := os.WriteFile(modelConfig, []byte(`{
  "global_max_concurrent": 1,
  "offline_failure_threshold": 3,
  "apis": [
    {
      "api_id": "api-1",
      "provider": "openai_compatible",
      "model": "gpt-4.1-mini",
      "base_url": "https://example.invalid/v1",
      "api_key": "sk-test",
      "max_concurrent": 1,
      "enabled": true
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write model config error = %v", err)
	}
	server, err := NewServer(Config{
		StateFile:        filepath.Join(tempDir, "vos_state.json"),
		SessionDBFile:    filepath.Join(tempDir, "openmate.db"),
		WorkspaceRoot:    tempDir,
		ModelConfig:      modelConfig,
		ScheduleDB:       filepath.Join(tempDir, "openmate.db"),
		ScheduleMode:     "inproc",
		DefaultTimeoutMS: 120000,
		AgingSeconds:     600,
	})
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	testServer := httptest.NewServer(server.Handler())
	return server, testServer
}

func mustRequestEnvelope(
	t *testing.T,
	client *http.Client,
	method string,
	url string,
	payload any,
	expectedStatus int,
) apiEnvelope {
	t.Helper()
	var bodyBytes []byte
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		bodyBytes = encoded
	} else {
		bodyBytes = []byte{}
	}

	request, err := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != expectedStatus {
		t.Fatalf("status = %d, want %d", response.StatusCode, expectedStatus)
	}

	envelope := apiEnvelope{}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response envelope error = %v", err)
	}
	return envelope
}

func mustDecodeEnvelopeData(t *testing.T, envelope apiEnvelope, target any) {
	t.Helper()
	if target == nil {
		return
	}
	if len(envelope.Data) == 0 {
		t.Fatalf("response data is empty")
	}
	if string(envelope.Data) == "null" {
		t.Fatalf("response data is null, target=%T", target)
	}
	if err := json.Unmarshal(envelope.Data, target); err != nil {
		t.Fatalf("decode envelope data into %T error = %v, raw=%s", target, err, string(envelope.Data))
	}
}

func (envelope apiEnvelope) String() string {
	return fmt.Sprintf("{Code:%d Message:%q Data:%s}", envelope.Code, envelope.Message, string(envelope.Data))
}

func TestServerV1Health(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	envelope := mustRequestEnvelope(t, testServer.Client(), http.MethodGet, testServer.URL+"/api/v1/health", nil, http.StatusOK)
	if envelope.Code != http.StatusOK {
		t.Fatalf("health code = %d, want %d", envelope.Code, http.StatusOK)
	}
	payload := map[string]any{}
	mustDecodeEnvelopeData(t, envelope, &payload)
	if payload["status"] != "ok" {
		t.Fatalf("health status = %v, want ok", payload["status"])
	}
}

type testProvider struct {
	invoke func(ctx context.Context, reservation poolgateway.InvocationReservation, request poolgateway.InvokeRequest) (poolgateway.ProviderInvokeResult, error)
}

func (provider testProvider) Invoke(
	ctx context.Context,
	reservation poolgateway.InvocationReservation,
	request poolgateway.InvokeRequest,
) (poolgateway.ProviderInvokeResult, error) {
	if provider.invoke == nil {
		return poolgateway.ProviderInvokeResult{}, fmt.Errorf("test provider invoke is nil")
	}
	return provider.invoke(ctx, reservation, request)
}

func TestServerV1ChatResultRequiresInvocationID(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	response := mustRequestEnvelope(t, testServer.Client(), http.MethodGet, testServer.URL+"/api/v1/chat/result", nil, http.StatusBadRequest)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusBadRequest)
	}
}

func TestServerV1ChatResultReturnsNotFound(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	response := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodGet,
		testServer.URL+"/api/v1/chat/result?invocation_id=inv-missing",
		nil,
		http.StatusNotFound,
	)
	if response.Code != http.StatusNotFound {
		t.Fatalf("response code = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestServerV1ChatResultReturnsInvocationRecord(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	server.runtime.PoolGateway.SetProviderFactory(func(provider string) (poolgateway.ProviderClient, error) {
		return testProvider{
			invoke: func(ctx context.Context, reservation poolgateway.InvocationReservation, request poolgateway.InvokeRequest) (poolgateway.ProviderInvokeResult, error) {
				reply := "hello from test"
				return poolgateway.ProviderInvokeResult{
					Response: map[string]any{
						"object": "response",
						"status": "completed",
						"output": []any{
							map[string]any{
								"type":   "message",
								"role":   "assistant",
								"status": "completed",
								"content": []any{
									map[string]any{
										"type": "output_text",
										"text": reply,
									},
								},
							},
						},
						"usage": map[string]any{
							"input_tokens":  1,
							"output_tokens": 2,
							"total_tokens":  3,
						},
					},
					OutputText: &reply,
					Usage: &poolgateway.UsageMetrics{
						InputTokens:  intPtr(1),
						OutputTokens: intPtr(2),
						TotalTokens:  intPtr(3),
					},
				}, nil
			},
		}, nil
	})

	invokeResponse, err := server.runtime.PoolGateway.Invoke(context.Background(), poolgateway.InvokeRequest{
		RequestID: "req-test-chat-result",
		NodeID:    "node-test-chat-result",
		Request: poolgateway.OpenAIResponsesRequest{
			"input": []any{
				map[string]any{
					"role":    "user",
					"content": "hi",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("invoke error = %v", err)
	}

	envelope := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodGet,
		testServer.URL+"/api/v1/chat/result?invocation_id="+invokeResponse.InvocationID,
		nil,
		http.StatusOK,
	)
	payload := map[string]any{}
	mustDecodeEnvelopeData(t, envelope, &payload)
	if payload["invocation_id"] != invokeResponse.InvocationID {
		t.Fatalf("invocation_id = %v, want %s", payload["invocation_id"], invokeResponse.InvocationID)
	}
	if payload["status"] != "success" {
		t.Fatalf("status = %v, want success", payload["status"])
	}
	if payload["reply"] != "hello from test" {
		t.Fatalf("reply = %v, want hello from test", payload["reply"])
	}
}

func TestServerV1ChatStreamAttachNotRunning(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	requestBody := bytes.NewBufferString(`{"invocation_id":"inv-missing"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/api/v1/chat/stream", requestBody)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusNotFound)
	}
}

func TestServerV1ChatStreamAttachExistingInvocation(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	run := newChatRun("inv-tail", "node-tail", "session-tail")
	server.setChatRun(run)

	go func() {
		time.Sleep(20 * time.Millisecond)
		run.publish(chatRunEvent{
			Type: "assistant_delta",
			Payload: map[string]any{
				"delta": "hello",
			},
		})
		run.publish(chatRunEvent{
			Type: "summary",
			Payload: map[string]any{
				"invocation_id": "inv-tail",
				"status":        "success",
			},
		})
		run.close()
		server.removeChatRun("inv-tail")
	}()

	requestBody := bytes.NewBufferString(`{"invocation_id":"inv-tail"}`)
	request, err := http.NewRequest(http.MethodPost, testServer.URL+"/api/v1/chat/stream", requestBody)
	if err != nil {
		t.Fatalf("http.NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := testServer.Client().Do(request)
	if err != nil {
		t.Fatalf("client.Do() error = %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}
	text := string(body)
	if !strings.Contains(text, "event: invocation") {
		t.Fatalf("stream should include invocation event, got: %s", text)
	}
	if !strings.Contains(text, "\"invocation_id\":\"inv-tail\"") {
		t.Fatalf("stream should include invocation_id, got: %s", text)
	}
	if !strings.Contains(text, "event: assistant_delta") {
		t.Fatalf("stream should include assistant_delta event, got: %s", text)
	}
	if !strings.Contains(text, "event: summary") {
		t.Fatalf("stream should include summary event, got: %s", text)
	}
}

func intPtr(value int) *int {
	result := value
	return &result
}
