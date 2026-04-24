package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"vos/internal/vos/domain"
)

// CompactAgentRequest is sent to the compact agent CLI.
type CompactAgentRequest struct {
	NodeID    string                   `json:"node_id"`
	Processes []CompactProcessInput    `json:"processes"`
	Context   *domain.ContextSnapshot  `json:"context,omitempty"`
}

// CompactProcessInput describes a single process and its uncompacted sessions.
type CompactProcessInput struct {
	Process               domain.ProcessItem `json:"process"`
	UncompactedSessionIDs []string            `json:"uncompacted_session_ids"`
}

// CompactAgentResponse is the expected response from the compact agent CLI.
type CompactAgentResponse struct {
	Status    string              `json:"status"`
	Compacted []CompactedProcess  `json:"compacted"`
	Error     *string             `json:"error,omitempty"`
}

// CompactedProcess is the result of compacting a single process.
type CompactedProcess struct {
	Name               string         `json:"name"`
	Memory             map[string]any `json:"memory"`
	CompactedSessionIDs []string       `json:"compacted_session_ids"`
}

// CompactRunner executes the compact agent.
type CompactRunner interface {
	RunCompact(ctx context.Context, request CompactAgentRequest) (*CompactAgentResponse, error)
}

// CommandCompactRunner calls the compact agent via CLI shell-out.
type CommandCompactRunner struct {
	command string
}

// NewCommandCompactRunner creates a runner with the given agent command.
func NewCommandCompactRunner() *CommandCompactRunner {
	return &CommandCompactRunner{
		command: DefaultCompactAgentCommand(),
	}
}

// DefaultCompactAgentCommand returns the default compact agent CLI command.
func DefaultCompactAgentCommand() string {
	return "python -m openmate_agent.cli compact run"
}

func (runner *CommandCompactRunner) RunCompact(ctx context.Context, request CompactAgentRequest) (*CompactAgentResponse, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal compact request: %w", err)
	}

	cmd := exec.CommandContext(ctx, runner.command, "--request-file", string(requestJSON))
	// Actually, we need to parse the command properly
	parts := strings.Fields(runner.command)
	name := parts[0]
	args := append(parts[1:], "--request-json", string(requestJSON))

	cmd = exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("compact agent failed: %w\noutput: %s", err, string(stdout))
	}

	var response CompactAgentResponse
	if err := json.Unmarshal(stdout, &response); err != nil {
		return nil, fmt.Errorf("failed to parse compact agent response: %w\noutput: %s", err, string(stdout))
	}
	return &response, nil
}

// CompactProcesses orchestrates the compaction of node processes using the given runner.
func (service *Service) CompactProcesses(runner CompactRunner, nodeID string, processes interface{}, snapshot *domain.ContextSnapshot) (*CompactAgentResponse, error) {
	// Build request from the processes
	var compactInputs []CompactProcessInput
	switch p := processes.(type) {
	case []CompactProcessInput:
		compactInputs = p
	default:
		return nil, fmt.Errorf("invalid processes type")
	}

	request := CompactAgentRequest{
		NodeID:    nodeID,
		Processes: compactInputs,
		Context:   snapshot,
	}

	response, err := runner.RunCompact(context.Background(), request)
	if err != nil {
		return nil, err
	}
	if response.Status == "failed" {
		errMsg := "compact agent returned failed status"
		if response.Error != nil {
			errMsg = *response.Error
		}
		return nil, fmt.Errorf("%s", errMsg)
	}
	return response, nil
}
