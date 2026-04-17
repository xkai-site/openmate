package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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

	response := mustRequestEnvelope(t, testServer.Client(), http.MethodGet, testServer.URL+"/api/v1/planlist", nil, http.StatusNotImplemented)
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
	server, err := NewServer(Config{
		StateFile:     filepath.Join(tempDir, "vos_state.json"),
		SessionDBFile: filepath.Join(tempDir, "openmate.db"),
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
