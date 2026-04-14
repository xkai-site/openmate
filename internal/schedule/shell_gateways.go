package schedule

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ShellGatewayConfig struct {
	Workdir string

	VOSCommand       []string
	VOSStateFile     string
	VOSSessionDBFile string

	WorkerCommand []string
}

type ShellVOSGateway struct {
	config ShellGatewayConfig
}

type ShellWorkerGateway struct {
	config ShellGatewayConfig
}

func NewShellVOSGateway(config ShellGatewayConfig) (*ShellVOSGateway, error) {
	if len(config.VOSCommand) == 0 {
		return nil, ValidationError{Message: "vos command is required"}
	}
	return &ShellVOSGateway{config: config}, nil
}

func NewShellWorkerGateway(config ShellGatewayConfig) (*ShellWorkerGateway, error) {
	if len(config.WorkerCommand) == 0 {
		return nil, ValidationError{Message: "worker command is required"}
	}
	return &ShellWorkerGateway{config: config}, nil
}

func (gateway *ShellVOSGateway) EnsurePriorityNode(ctx context.Context, topicID string) (string, error) {
	nodesPayload, err := gateway.runVOS(ctx, []string{
		"node", "list",
		"--topic-id", topicID,
		"--leaf-only",
	})
	if err != nil {
		return "", err
	}
	var nodes []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(nodesPayload, &nodes); err != nil {
		return "", fmt.Errorf("decode vos node list: %w", err)
	}
	for _, node := range nodes {
		if node.Name == PriorityNodeName {
			return node.ID, nil
		}
	}
	createdPayload, err := gateway.runVOS(ctx, []string{
		"node", "create",
		"--topic-id", topicID,
		"--name", PriorityNodeName,
		"--description", "schedule priority node",
		"--status", "ready",
	})
	if err != nil {
		return "", err
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createdPayload, &created); err != nil {
		return "", fmt.Errorf("decode priority node create response: %w", err)
	}
	if created.ID == "" {
		return "", fmt.Errorf("vos priority node create returned empty id")
	}
	return created.ID, nil
}

func (gateway *ShellVOSGateway) EnsureSession(ctx context.Context, nodeID string, knownSessionID *string) (string, error) {
	if knownSessionID != nil && strings.TrimSpace(*knownSessionID) != "" {
		return strings.TrimSpace(*knownSessionID), nil
	}
	payload, err := gateway.runVOS(ctx, []string{
		"session", "create",
		"--node-id", nodeID,
		"--status", "active",
	})
	if err != nil {
		return "", err
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &session); err != nil {
		return "", fmt.Errorf("decode session create response: %w", err)
	}
	if session.ID == "" {
		return "", fmt.Errorf("session create returned empty id")
	}
	return session.ID, nil
}

func (gateway *ShellVOSGateway) AppendDispatchAuthorizedEvent(ctx context.Context, sessionID string, payload map[string]any) (SessionEventRecord, error) {
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return SessionEventRecord{}, fmt.Errorf("marshal dispatch authorized payload: %w", err)
	}
	response, err := gateway.runVOS(ctx, []string{
		"session", "append-event",
		"--session-id", sessionID,
		"--item-type", "message",
		"--role", "system",
		"--payload-json", string(payloadRaw),
		"--next-status", "active",
	})
	if err != nil {
		return SessionEventRecord{}, err
	}
	var event struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		Seq       int    `json:"seq"`
	}
	if err := json.Unmarshal(response, &event); err != nil {
		return SessionEventRecord{}, fmt.Errorf("decode dispatch authorized event: %w", err)
	}
	return SessionEventRecord{ID: event.ID, SessionID: event.SessionID, Seq: event.Seq}, nil
}

func (gateway *ShellVOSGateway) AppendDispatchResultEvent(ctx context.Context, sessionID string, payload map[string]any) error {
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal dispatch result payload: %w", err)
	}
	nextStatus := "active"
	if status, ok := payload["status"].(string); ok && status != "" {
		if status == "succeeded" {
			nextStatus = "completed"
		}
		if status == "failed" {
			nextStatus = "failed"
		}
	}
	_, err = gateway.runVOS(ctx, []string{
		"session", "append-event",
		"--session-id", sessionID,
		"--item-type", "message",
		"--role", "system",
		"--payload-json", string(payloadRaw),
		"--next-status", nextStatus,
	})
	return err
}

func (gateway *ShellVOSGateway) runVOS(ctx context.Context, args []string) ([]byte, error) {
	full := make([]string, 0, len(gateway.config.VOSCommand)+6+len(args))
	full = append(full, gateway.config.VOSCommand...)
	if gateway.config.VOSStateFile != "" {
		full = append(full, "--state-file", gateway.config.VOSStateFile)
	}
	if gateway.config.VOSSessionDBFile != "" {
		full = append(full, "--session-db-file", gateway.config.VOSSessionDBFile)
	}
	full = append(full, args...)
	return runCommandJSON(ctx, gateway.config.Workdir, full)
}

func (gateway *ShellWorkerGateway) Execute(ctx context.Context, request WorkerExecuteRequest) (WorkerExecuteResponse, error) {
	tempDir := os.TempDir()
	if gateway.config.Workdir != "" {
		tempDir = gateway.config.Workdir
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return WorkerExecuteResponse{}, fmt.Errorf("ensure worker temp dir: %w", err)
	}
	requestFile, err := os.CreateTemp(tempDir, "schedule-worker-request-*.json")
	if err != nil {
		return WorkerExecuteResponse{}, fmt.Errorf("create worker request file: %w", err)
	}
	requestPath := requestFile.Name()
	_ = requestFile.Close()
	defer func() {
		_ = os.Remove(requestPath)
	}()

	raw, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return WorkerExecuteResponse{}, fmt.Errorf("marshal worker request: %w", err)
	}
	if err := os.WriteFile(requestPath, raw, 0o644); err != nil {
		return WorkerExecuteResponse{}, fmt.Errorf("write worker request file: %w", err)
	}

	command := append([]string{}, gateway.config.WorkerCommand...)
	command = append(command, "--request-file", requestPath)
	responseRaw, err := runCommandJSON(ctx, gateway.config.Workdir, command)
	if err != nil {
		return WorkerExecuteResponse{}, err
	}
	var response WorkerExecuteResponse
	if err := json.Unmarshal(responseRaw, &response); err != nil {
		return WorkerExecuteResponse{}, fmt.Errorf("decode worker response: %w", err)
	}
	if response.RequestID == "" {
		response.RequestID = request.RequestID
	}
	if response.TopicID == "" {
		response.TopicID = request.TopicID
	}
	if response.NodeID == "" {
		response.NodeID = request.NodeID
	}
	if response.SessionID == "" {
		response.SessionID = request.SessionID
	}
	if response.EventID == "" {
		response.EventID = request.EventID
	}
	return response, nil
}

func runCommandJSON(ctx context.Context, workdir string, command []string) ([]byte, error) {
	if len(command) == 0 {
		return nil, ValidationError{Message: "empty command"}
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if workdir != "" {
		cmd.Dir = filepath.Clean(workdir)
	}
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return nil, fmt.Errorf("%s failed: %w", strings.Join(command, " "), err)
		}
		return nil, fmt.Errorf("%s failed: %w (%s)", strings.Join(command, " "), err, text)
	}
	if text == "" {
		return nil, fmt.Errorf("%s returned empty stdout", strings.Join(command, " "))
	}
	return []byte(text), nil
}
