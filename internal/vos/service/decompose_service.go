package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	openmatepaths "vos/internal/openmate/paths"
	"vos/internal/vos/domain"
)

const (
	DefaultNodeDecomposeMaxItems = 5
	decomposeHintPolicy          = "Decompose by business/domain outcomes first; do not split by technical stack. Keep one-level direct child tasks only."
	defaultDecomposeTimeout      = 60 * time.Second
)

type DecomposeAgentRequest struct {
	RequestID       string         `json:"request_id"`
	TopicID         string         `json:"topic_id"`
	NodeID          string         `json:"node_id"`
	NodeName        string         `json:"node_name"`
	Mode            string         `json:"mode"`
	Hint            string         `json:"hint,omitempty"`
	MaxItems        int            `json:"max_items"`
	SessionID       string         `json:"session_id,omitempty"`
	ContextSnapshot map[string]any `json:"context_snapshot,omitempty"`
}

type DecomposeAgentTask struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

type DecomposeAgentResponse struct {
	RequestID  string               `json:"request_id"`
	TopicID    string               `json:"topic_id"`
	NodeID     string               `json:"node_id"`
	Status     string               `json:"status"`
	Output     string               `json:"output,omitempty"`
	Error      string               `json:"error,omitempty"`
	DurationMS int                  `json:"duration_ms"`
	Tasks      []DecomposeAgentTask `json:"tasks"`
}

type NodeDecomposeInput struct {
	NodeID   string
	Hint     string
	MaxItems int
}

type NodeDecomposeResult struct {
	RequestID    string               `json:"request_id"`
	TopicID      string               `json:"topic_id"`
	NodeID       string               `json:"node_id"`
	Status       string               `json:"status"`
	Output       string               `json:"output,omitempty"`
	Error        string               `json:"error,omitempty"`
	DurationMS   int                  `json:"duration_ms"`
	Tasks        []DecomposeAgentTask `json:"tasks"`
	CreatedNodes []*domain.Node       `json:"created_nodes"`
}

type NodeDecomposeRunner interface {
	Run(ctx context.Context, request DecomposeAgentRequest) (*DecomposeAgentResponse, error)
}

type NodeDecomposeRunnerFunc func(ctx context.Context, request DecomposeAgentRequest) (*DecomposeAgentResponse, error)

func (runner NodeDecomposeRunnerFunc) Run(ctx context.Context, request DecomposeAgentRequest) (*DecomposeAgentResponse, error) {
	if runner == nil {
		return nil, domain.ValidationError{Message: "decompose runner is required"}
	}
	return runner(ctx, request)
}

type CommandDecomposeRunner struct {
	commandRaw string
	timeout    time.Duration
}

func NewCommandDecomposeRunner(commandRaw string) *CommandDecomposeRunner {
	return &CommandDecomposeRunner{
		commandRaw: strings.TrimSpace(commandRaw),
		timeout:    defaultDecomposeTimeout,
	}
}

func (runner *CommandDecomposeRunner) Run(ctx context.Context, request DecomposeAgentRequest) (*DecomposeAgentResponse, error) {
	if runner == nil {
		return nil, domain.ValidationError{Message: "decompose runner is required"}
	}
	command := splitCommand(runner.commandRaw)
	if len(command) == 0 {
		return nil, fmt.Errorf("agent-command must not be empty")
	}

	requestFile, err := os.CreateTemp("", "openmate-node-decompose-request-*.json")
	if err != nil {
		return nil, fmt.Errorf("create decompose request file: %w", err)
	}
	requestFilePath := requestFile.Name()
	_ = requestFile.Close()
	defer func() {
		_ = os.Remove(requestFilePath)
	}()

	requestRaw, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal decompose request: %w", err)
	}
	if err := os.WriteFile(requestFilePath, requestRaw, 0o644); err != nil {
		return nil, fmt.Errorf("write decompose request file: %w", err)
	}

	fullCommand := append([]string{}, command...)
	fullCommand = append(fullCommand, "--request-file", requestFilePath)

	if ctx == nil {
		ctx = context.Background()
	}
	commandCtx := ctx
	cancel := func() {}
	if runner.timeout > 0 {
		commandCtx, cancel = context.WithTimeout(ctx, runner.timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(commandCtx, fullCommand[0], fullCommand[1:]...)
	output, runErr := cmd.CombinedOutput()
	outputText := strings.TrimSpace(string(output))
	if outputText == "" {
		if runErr != nil {
			return nil, fmt.Errorf("run decompose agent command failed: %w", runErr)
		}
		return nil, fmt.Errorf("run decompose agent command returned empty output")
	}

	response := &DecomposeAgentResponse{}
	if err := json.Unmarshal([]byte(outputText), response); err != nil {
		if runErr != nil {
			return nil, fmt.Errorf("run decompose agent command failed: %w (%s)", runErr, outputText)
		}
		return nil, fmt.Errorf("decode decompose agent response: %w", err)
	}
	return response, nil
}

func (service *Service) DecomposeNode(ctx context.Context, input NodeDecomposeInput, runner NodeDecomposeRunner) (*NodeDecomposeResult, error) {
	if runner == nil {
		return nil, domain.ValidationError{Message: "decompose runner is required"}
	}
	nodeID := strings.TrimSpace(input.NodeID)
	if nodeID == "" {
		return nil, domain.ValidationError{Message: "node ID is required"}
	}
	maxItems := input.MaxItems
	if maxItems <= 0 {
		return nil, domain.ValidationError{Message: "max-items must be > 0"}
	}

	targetNode, err := service.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	snapshot, err := service.GetContextSnapshot(targetNode.ID)
	if err != nil {
		return nil, err
	}
	contextSnapshot, err := contextSnapshotToMap(snapshot)
	if err != nil {
		return nil, err
	}

	requestID := fmt.Sprintf("req-vos-decompose-%s", domain.NewID())
	request := DecomposeAgentRequest{
		RequestID:       requestID,
		TopicID:         targetNode.TopicID,
		NodeID:          targetNode.ID,
		NodeName:        targetNode.Name,
		Mode:            "decompose",
		Hint:            BuildDecomposeHint(input.Hint),
		MaxItems:        maxItems,
		ContextSnapshot: contextSnapshot,
	}
	if len(targetNode.Session) > 0 {
		request.SessionID = targetNode.Session[len(targetNode.Session)-1]
	}

	agentResponse, err := runner.Run(ctx, request)
	if err != nil {
		return nil, domain.ValidationError{Message: err.Error()}
	}
	if agentResponse == nil {
		return nil, domain.ValidationError{Message: "decompose agent returned empty response"}
	}
	if strings.TrimSpace(agentResponse.Status) != "succeeded" {
		message := strings.TrimSpace(agentResponse.Error)
		if message == "" {
			message = "decompose agent returned failed status"
		}
		return nil, domain.ValidationError{Message: message}
	}
	if len(agentResponse.Tasks) == 0 {
		return nil, domain.ValidationError{Message: "decompose agent returned empty tasks"}
	}

	createdNodes := make([]*domain.Node, 0, len(agentResponse.Tasks))
	for index, task := range agentResponse.Tasks {
		title := strings.TrimSpace(task.Title)
		if title == "" {
			title = fmt.Sprintf("Decompose Task %d", index+1)
		}
		description := strings.TrimSpace(task.Description)
		createInput := CreateNodeInput{
			TopicID:  targetNode.TopicID,
			Name:     title,
			ParentID: stringPtr(targetNode.ID),
			Status:   domain.NodeStatusReady,
		}
		if description != "" {
			createInput.Description = stringPtr(description)
		}
		createdNode, createErr := service.CreateNode(createInput)
		if createErr != nil {
			return nil, createErr
		}
		createdNodes = append(createdNodes, createdNode)
	}

	resultRequestID := strings.TrimSpace(agentResponse.RequestID)
	if resultRequestID == "" {
		resultRequestID = request.RequestID
	}
	resultTopicID := strings.TrimSpace(agentResponse.TopicID)
	if resultTopicID == "" {
		resultTopicID = targetNode.TopicID
	}
	resultNodeID := strings.TrimSpace(agentResponse.NodeID)
	if resultNodeID == "" {
		resultNodeID = targetNode.ID
	}

	return &NodeDecomposeResult{
		RequestID:    resultRequestID,
		TopicID:      resultTopicID,
		NodeID:       resultNodeID,
		Status:       agentResponse.Status,
		Output:       agentResponse.Output,
		Error:        agentResponse.Error,
		DurationMS:   agentResponse.DurationMS,
		Tasks:        cloneDecomposeTasks(agentResponse.Tasks),
		CreatedNodes: createdNodes,
	}, nil
}

func BuildDecomposeHint(userHint string) string {
	trimmed := strings.TrimSpace(userHint)
	if trimmed == "" {
		return decomposeHintPolicy
	}
	return decomposeHintPolicy + " User hint: " + trimmed
}

func DefaultDecomposeAgentCommand() string {
	workerCommand := openmatepaths.DefaultWorkerCommand(".")
	if strings.HasSuffix(workerCommand, " worker run") {
		return strings.TrimSuffix(workerCommand, " worker run") + " decompose run"
	}
	venvPython := filepath.FromSlash(".venv/Scripts/python.exe")
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython + " -m openmate_agent.cli decompose run"
	}
	return "python -m openmate_agent.cli decompose run"
}

func contextSnapshotToMap(snapshot *domain.ContextSnapshot) (map[string]any, error) {
	if snapshot == nil {
		return map[string]any{}, nil
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func cloneDecomposeTasks(tasks []DecomposeAgentTask) []DecomposeAgentTask {
	if tasks == nil {
		return []DecomposeAgentTask{}
	}
	cloned := make([]DecomposeAgentTask, len(tasks))
	copy(cloned, tasks)
	return cloned
}

func splitCommand(raw string) []string {
	parts := strings.Fields(strings.TrimSpace(raw))
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}
