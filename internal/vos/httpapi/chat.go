package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vos/internal/openmate/observability"
	"vos/internal/schedule"
	"vos/internal/vos/domain"
	"vos/internal/vos/service"
)

const (
	defaultChatTurnTimeout = 2 * time.Minute
	chatPollInterval       = 150 * time.Millisecond
	chatPollLimit          = 200
)

type v1ChatRequest struct {
	NodeID       *string          `json:"node_id"`
	Message      string           `json:"message"`
	History      []map[string]any `json:"history"`
	SystemPrompt *string          `json:"system_prompt"`
	SaveSession  *bool            `json:"save_session"`
}

type v1ChatResponse struct {
	NodeID        string         `json:"node_id"`
	Reply         string         `json:"reply"`
	Model         string         `json:"model"`
	Provider      string         `json:"provider"`
	Usage         map[string]any `json:"usage"`
	MemoryWritten any            `json:"memory_written"`
	MethodTraces  any            `json:"method_traces"`
}

type v1ToolTrace struct {
	CallID string
	Tool   string
	Args   map[string]any
	Result map[string]any
	Error  string
	Call   string
}

type chatTurnState struct {
	NodeID          string
	SessionID       string
	LastSeq         int
	Reply           string
	AssistantBuffer strings.Builder
	Usage           map[string]any
	Model           string
	Provider        string
	ToolTraces      []v1ToolTrace
	toolTraceByCall map[string]int
}

func (state *chatTurnState) ensureToolIndex() {
	if state.toolTraceByCall == nil {
		state.toolTraceByCall = map[string]int{}
	}
}

func (state *chatTurnState) addToolStart(callID string, tool string, args map[string]any) v1ToolTrace {
	state.ensureToolIndex()
	trace := v1ToolTrace{
		CallID: callID,
		Tool:   tool,
		Args:   cloneMapOrEmpty(args),
		Call:   "start",
	}
	state.ToolTraces = append(state.ToolTraces, trace)
	state.toolTraceByCall[callID] = len(state.ToolTraces) - 1
	return trace
}

func (state *chatTurnState) addToolResult(callID string, tool string, result map[string]any, errText string) v1ToolTrace {
	state.ensureToolIndex()
	trace := v1ToolTrace{
		CallID: callID,
		Tool:   tool,
		Result: cloneMapOrEmpty(result),
		Error:  strings.TrimSpace(errText),
		Call:   "result",
	}
	if trace.Error != "" {
		trace.Call = "error"
	}
	if idx, exists := state.toolTraceByCall[callID]; exists && idx >= 0 && idx < len(state.ToolTraces) {
		existing := state.ToolTraces[idx]
		trace.Args = cloneMapOrEmpty(existing.Args)
		state.ToolTraces[idx] = trace
		return trace
	}
	state.ToolTraces = append(state.ToolTraces, trace)
	state.toolTraceByCall[callID] = len(state.ToolTraces) - 1
	return trace
}

func (server *Server) handleV1ChatEntry(writer http.ResponseWriter, request *http.Request) {
	path := strings.TrimPrefix(request.URL.Path, v1Prefix+"/chat")
	switch path {
	case "", "/":
		server.handleV1Chat(writer, request)
	case "/stream":
		server.handleV1ChatStream(writer, request)
	default:
		server.writeV1Error(writer, http.StatusNotFound, "not found")
	}
}

func (server *Server) handleV1Chat(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodPost)
		return
	}
	startedAt := time.Now().UTC()
	requestID := resolveRequestID(request)
	logger := observability.NormalizeLogger(server.logger).With(
		slog.String(observability.FieldOperation, "chat.sync"),
		slog.String(observability.FieldRequestID, requestID),
		slog.String(observability.FieldTraceID, requestID),
	)
	request = request.WithContext(observability.WithLogger(request.Context(), logger))

	chatRequest, nodeID, sessionID, err := server.prepareChatTurn(request)
	if err != nil {
		logger.Error("prepare chat turn failed", slog.Any("error", err))
		server.writeV1ServiceError(writer, err)
		return
	}
	logger = logger.With(
		slog.String(observability.FieldNodeID, nodeID),
		slog.String(observability.FieldSessionID, sessionID),
	)
	request = request.WithContext(observability.WithLogger(request.Context(), logger))
	state := chatTurnState{
		NodeID:    nodeID,
		SessionID: sessionID,
	}
	if err := server.waitChatTurn(request.Context(), &state, nil); err != nil {
		logger.Error("wait chat turn failed", slog.Any("error", err))
		server.writeV1ServiceError(writer, err)
		return
	}
	if strings.TrimSpace(state.Reply) == "" {
		logger.Warn("chat finished without assistant reply")
		server.writeV1Error(writer, http.StatusGatewayTimeout, "chat turn completed without assistant reply")
		return
	}
	if state.Model == "" {
		state.Model = "responses"
	}
	if state.Provider == "" {
		state.Provider = "pool"
	}

	_ = chatRequest
	logger.Info(
		"chat turn completed",
		slog.Int64(observability.FieldDurationMS, int64(time.Now().UTC().Sub(startedAt).Milliseconds())),
	)
	server.writeV1Success(writer, v1ChatResponse{
		NodeID:        nodeID,
		Reply:         state.Reply,
		Model:         state.Model,
		Provider:      state.Provider,
		Usage:         cloneMapOrEmpty(state.Usage),
		MemoryWritten: nil,
		MethodTraces:  nil,
	})
}

func (server *Server) handleV1ChatStream(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		server.writeV1MethodNotAllowed(writer, request.Method, http.MethodPost)
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		server.writeV1Error(writer, http.StatusInternalServerError, "streaming is not supported by current response writer")
		return
	}
	requestID := resolveRequestID(request)
	logger := observability.NormalizeLogger(server.logger).With(
		slog.String(observability.FieldOperation, "chat.stream"),
		slog.String(observability.FieldRequestID, requestID),
		slog.String(observability.FieldTraceID, requestID),
	)
	request = request.WithContext(observability.WithLogger(request.Context(), logger))

	chatRequest, nodeID, sessionID, err := server.prepareChatTurn(request)
	if err != nil {
		logger.Error("prepare chat stream turn failed", slog.Any("error", err))
		server.writeV1ServiceError(writer, err)
		return
	}
	logger = logger.With(
		slog.String(observability.FieldNodeID, nodeID),
		slog.String(observability.FieldSessionID, sessionID),
	)
	request = request.WithContext(observability.WithLogger(request.Context(), logger))
	_ = chatRequest

	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")
	writer.WriteHeader(http.StatusOK)

	emitter := newSSEEmitter(writer, flusher, nodeID, sessionID)
	emitter.phase("reading_node")
	emitter.phase("waiting_schedule")

	state := chatTurnState{
		NodeID:    nodeID,
		SessionID: sessionID,
	}
	if err := server.waitChatTurn(request.Context(), &state, emitter); err != nil {
		logger.Error("wait chat stream turn failed", slog.Any("error", err))
		emitter.fatal(err)
		return
	}

	reply := state.Reply
	if strings.TrimSpace(reply) == "" {
		reply = state.AssistantBuffer.String()
	}
	if strings.TrimSpace(reply) != "" {
		emitter.assistantDone(reply)
	}
	emitter.phase("finalizing")
	emitter.summary(state)
	logger.Info("chat stream turn completed")
}

func (server *Server) prepareChatTurn(request *http.Request) (v1ChatRequest, string, string, error) {
	logger := observability.LoggerFromContext(request.Context(), server.logger).With(
		slog.String(observability.FieldOperation, "chat.prepare"),
	)
	var chatRequest v1ChatRequest
	if err := decodeJSON(request.Body, &chatRequest); err != nil {
		logger.Warn("decode chat request failed", slog.Any("error", err))
		return v1ChatRequest{}, "", "", domain.ValidationError{Message: err.Error()}
	}
	if strings.TrimSpace(chatRequest.Message) == "" {
		logger.Warn("chat message is empty")
		return v1ChatRequest{}, "", "", domain.ValidationError{Message: "message is required"}
	}

	node, err := server.resolveChatNode(chatRequest)
	if err != nil {
		logger.Error("resolve chat node failed", slog.Any("error", err))
		return v1ChatRequest{}, "", "", err
	}
	nodeID := node.ID
	logger = logger.With(
		slog.String(observability.FieldTopicID, node.TopicID),
		slog.String(observability.FieldNodeID, nodeID),
	)
	sessionRecord, err := server.service.CreateSession(service.CreateSessionInput{
		NodeID: nodeID,
		Status: domain.SessionStatusActive,
	})
	if err != nil {
		logger.Error("create session failed", slog.Any("error", err))
		return v1ChatRequest{}, "", "", err
	}
	sessionID := sessionRecord.ID
	logger = logger.With(slog.String(observability.FieldSessionID, sessionID))

	_, err = server.service.AppendSessionEvent(service.AppendSessionEventInput{
		SessionID: sessionID,
		ItemType:  domain.SessionItemTypeMessage,
		Role:      sessionRolePtr(domain.SessionRoleUser),
		PayloadJSON: map[string]any{
			"role":    domain.SessionRoleUser,
			"content": strings.TrimSpace(chatRequest.Message),
		},
		NextStatus: sessionStatusPtr(domain.SessionStatusActive),
	})
	if err != nil {
		logger.Error("append user message event failed", slog.Any("error", err))
		return v1ChatRequest{}, "", "", err
	}

	if err := server.enqueueChatNode(request.Context(), *node, sessionID); err != nil {
		logger.Error("enqueue chat node failed", slog.Any("error", err))
		_, _ = server.service.AppendSessionEvent(service.AppendSessionEventInput{
			SessionID: sessionID,
			ItemType:  domain.SessionItemTypeMessage,
			Role:      sessionRolePtr(domain.SessionRoleSystem),
			PayloadJSON: map[string]any{
				"role": domain.SessionRoleSystem,
				"error": map[string]any{
					"code":      "SCHEDULE_ENQUEUE_FAILED",
					"message":   err.Error(),
					"retryable": true,
				},
			},
			NextStatus: sessionStatusPtr(domain.SessionStatusFailed),
		})
		return v1ChatRequest{}, "", "", err
	}
	logger.Info("chat turn enqueued")

	return chatRequest, nodeID, sessionID, nil
}

func (server *Server) resolveChatNode(request v1ChatRequest) (*domain.Node, error) {
	if request.NodeID != nil {
		nodeID := strings.TrimSpace(*request.NodeID)
		if nodeID != "" {
			return server.service.GetNode(nodeID)
		}
	}

	topicName := buildAutoTopicName(request.Message)
	rootName := "Conversation"
	topic, rootNode, err := server.service.CreateTopic(service.CreateTopicInput{
		Name:         topicName,
		RootNodeName: &rootName,
	})
	if err != nil {
		return nil, err
	}
	_ = topic
	return rootNode, nil
}

func buildAutoTopicName(message string) string {
	trimmed := strings.TrimSpace(message)
	if trimmed == "" {
		return "Chat Topic"
	}
	runes := []rune(trimmed)
	if len(runes) > 24 {
		trimmed = string(runes[:24]) + "..."
	}
	return "Chat: " + trimmed
}

func (server *Server) enqueueChatNode(ctx context.Context, node domain.Node, sessionID string) error {
	logger := observability.LoggerFromContext(ctx, server.logger).With(
		slog.String(observability.FieldOperation, "chat.enqueue"),
		slog.String(observability.FieldTopicID, node.TopicID),
		slog.String(observability.FieldNodeID, node.ID),
		slog.String(observability.FieldSessionID, sessionID),
	)
	if server.scheduleMode == "inproc" {
		if server.runtime == nil || server.runtime.ScheduleEngine == nil {
			logger.Error("schedule runtime is not configured")
			return fmt.Errorf("schedule runtime is not configured")
		}
		_, err := server.runtime.ScheduleEngine.Enqueue(ctx, schedule.EnqueueRequest{
			TopicID:   node.TopicID,
			NodeID:    node.ID,
			NodeName:  node.Name,
			SessionID: sessionID,
			AgentSpec: schedule.AgentSpec{
				Mode:            "",
				WorkspaceRoot:   server.workspace,
				PoolDBFile:      server.sessionDB,
				PoolModelConfig: server.modelConfig,
				VOSStateFile:    server.stateFile,
				VOSSessionDB:    server.sessionDB,
				UseSessionEvent: true,
			},
			Priority: schedule.NodePriority{
				Label: schedule.BusinessNodePriorityLabel,
				Rank:  schedule.BusinessNodePriorityRank,
			},
			IdempotencyKey: "chat:" + sessionID,
		})
		if err != nil {
			logger.Error("inproc enqueue failed", slog.Any("error", err))
			return err
		}
		logger.Info("inproc enqueue succeeded")
		return err
	}

	spec := map[string]any{
		"mode":              "",
		"workspace_root":    server.workspace,
		"pool_db_file":      server.sessionDB,
		"pool_model_config": server.modelConfig,
		"vos_state_file":    server.stateFile,
		"vos_session_db":    server.sessionDB,
		"use_session_event": true,
	}

	enqueueRequest := map[string]any{
		"topic_id":   node.TopicID,
		"node_id":    node.ID,
		"node_name":  node.Name,
		"session_id": sessionID,
		"agent_spec": spec,
		"priority": map[string]any{
			"label": schedule.BusinessNodePriorityLabel,
			"rank":  schedule.BusinessNodePriorityRank,
		},
		"idempotency_key": "chat:" + sessionID,
	}

	enqueueRaw, err := json.Marshal(enqueueRequest)
	if err != nil {
		return fmt.Errorf("marshal enqueue request: %w", err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	requestFile, err := os.CreateTemp("", "openmate-chat-enqueue-*.json")
	if err != nil {
		return fmt.Errorf("create enqueue request file: %w", err)
	}
	requestFilePath := requestFile.Name()
	_ = requestFile.Close()
	defer func() {
		_ = os.Remove(requestFilePath)
	}()
	if err := os.WriteFile(requestFilePath, enqueueRaw, 0o644); err != nil {
		return fmt.Errorf("write enqueue request file: %w", err)
	}

	command := append([]string{}, server.scheduleCmd...)
	command = append(
		command,
		"--db-file", server.scheduleDB,
		"--workdir", server.workspace,
		"enqueue",
		"--request-file", requestFilePath,
	)
	output, err := runCommand(timeoutCtx, server.workspace, command)
	if err != nil {
		logger.Error("shell enqueue failed", slog.Any("error", err))
		return err
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		logger.Error("decode schedule enqueue response failed", slog.Any("error", err))
		return fmt.Errorf("decode schedule enqueue response: %w", err)
	}
	logger.Info("shell enqueue succeeded")
	return nil
}

func (server *Server) waitChatTurn(ctx context.Context, state *chatTurnState, emitter *sseEmitter) error {
	if state == nil {
		return domain.ValidationError{Message: "chat state is required"}
	}
	logger := observability.LoggerFromContext(ctx, server.logger).With(
		slog.String(observability.FieldOperation, "chat.wait"),
		slog.String(observability.FieldNodeID, state.NodeID),
		slog.String(observability.FieldSessionID, state.SessionID),
	)
	deadline := time.Now().UTC().Add(defaultChatTurnTimeout)
	responsePhaseEmitted := false
	for {
		if server.scheduleMode == "inproc" {
			if err := server.tickSchedule(ctx); err != nil {
				logger.Error("tick schedule failed", slog.Any("error", err))
				return err
			}
		}

		events, err := server.service.ListSessionEvents(state.SessionID, state.LastSeq, chatPollLimit)
		if err != nil {
			logger.Error("list session events failed", slog.Any("error", err))
			return err
		}
		for _, event := range events {
			if event == nil {
				continue
			}
			state.LastSeq = event.Seq
			switch event.ItemType {
			case domain.SessionItemTypeAssistantDelta:
				delta := readOptionalString(event.PayloadJSON, "delta")
				if delta == "" {
					continue
				}
				state.AssistantBuffer.WriteString(delta)
				if emitter != nil {
					if !responsePhaseEmitted {
						emitter.phase("responding")
						responsePhaseEmitted = true
					}
					emitter.assistantDelta(delta)
				}
			case domain.SessionItemTypeFunctionCall:
				tool := readOptionalString(event.PayloadJSON, "name")
				callID := ""
				if event.CallID != nil {
					callID = strings.TrimSpace(*event.CallID)
				}
				args := mapFromAny(event.PayloadJSON["arguments"])
				trace := state.addToolStart(callID, tool, args)
				if emitter != nil {
					emitter.toolCall(trace)
				}
			case domain.SessionItemTypeFunctionCallOutput:
				callID := ""
				if event.CallID != nil {
					callID = strings.TrimSpace(*event.CallID)
				}
				okValue, _ := event.PayloadJSON["ok"].(bool)
				result := mapFromAny(event.PayloadJSON["output"])
				errText := ""
				if !okValue {
					errText = readErrorMessage(event.PayloadJSON["error"])
				}
				toolName := ""
				if idx, exists := state.toolTraceByCall[callID]; exists && idx >= 0 && idx < len(state.ToolTraces) {
					toolName = state.ToolTraces[idx].Tool
				}
				trace := state.addToolResult(callID, toolName, result, errText)
				if emitter != nil {
					emitter.toolCall(trace)
				}
			case domain.SessionItemTypeMessage:
				role := ""
				if event.Role != nil {
					role = string(*event.Role)
				}
				if role == "" {
					role = readOptionalString(event.PayloadJSON, "role")
				}
				if role != string(domain.SessionRoleAssistant) {
					continue
				}
				content := extractV1MessageContent(event.PayloadJSON)
				if content == "" {
					content = readOptionalString(event.PayloadJSON, "output_text")
				}
				if content != "" {
					state.Reply = content
				}
				if usage := mapFromAny(event.PayloadJSON["usage"]); len(usage) > 0 {
					state.Usage = usage
				}
				if model := readOptionalString(event.PayloadJSON, "model"); model != "" {
					state.Model = model
				}
				if provider := readOptionalString(event.PayloadJSON, "provider"); provider != "" {
					state.Provider = provider
				}
			}
		}

		sessionRecord, err := server.service.GetSession(state.SessionID)
		if err != nil {
			logger.Error("get session failed", slog.Any("error", err))
			return err
		}
		switch sessionRecord.Status {
		case domain.SessionStatusCompleted:
			if strings.TrimSpace(state.Reply) == "" {
				state.Reply = state.AssistantBuffer.String()
			}
			logger.Info("chat wait completed")
			return nil
		case domain.SessionStatusFailed:
			logger.Warn("chat session failed")
			return domain.ValidationError{Message: "chat session failed"}
		}

		if time.Now().UTC().After(deadline) {
			logger.Warn("chat session timed out")
			return domain.ValidationError{Message: "chat session timed out"}
		}
		if err := waitWithContext(ctx, server.scheduleTickInterval); err != nil {
			return err
		}
	}
}

func waitWithContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (server *Server) tickSchedule(ctx context.Context) error {
	if server.runtime == nil || server.runtime.ScheduleEngine == nil {
		return fmt.Errorf("schedule runtime is not configured")
	}
	server.scheduleMu.Lock()
	defer server.scheduleMu.Unlock()
	_, err := server.runtime.ScheduleEngine.Tick(ctx, 1)
	return err
}

type sseEmitter struct {
	writer   http.ResponseWriter
	flusher  http.Flusher
	nodeID   string
	turnID   string
	sequence int64
}

func newSSEEmitter(writer http.ResponseWriter, flusher http.Flusher, nodeID, turnID string) *sseEmitter {
	return &sseEmitter{
		writer:  writer,
		flusher: flusher,
		nodeID:  nodeID,
		turnID:  turnID,
	}
}

func (emitter *sseEmitter) nextEventID() string {
	emitter.sequence++
	return fmt.Sprintf("%s:%d", emitter.turnID, emitter.sequence)
}

func (emitter *sseEmitter) withBase(payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["event_id"] = emitter.nextEventID()
	payload["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	payload["turn_id"] = emitter.turnID
	payload["node_id"] = emitter.nodeID
	return payload
}

func (emitter *sseEmitter) emit(eventType string, payload map[string]any) {
	if emitter == nil {
		return
	}
	fullPayload := emitter.withBase(payload)
	raw, err := json.Marshal(fullPayload)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(emitter.writer, "event: %s\n", eventType)
	_, _ = fmt.Fprintf(emitter.writer, "data: %s\n\n", string(raw))
	emitter.flusher.Flush()
}

func (emitter *sseEmitter) phase(phase string) {
	emitter.emit("phase", map[string]any{
		"phase": phase,
	})
}

func (emitter *sseEmitter) assistantDelta(delta string) {
	emitter.emit("assistant_delta", map[string]any{
		"delta": delta,
	})
}

func (emitter *sseEmitter) assistantDone(reply string) {
	emitter.emit("assistant_done", map[string]any{
		"reply": reply,
	})
}

func (emitter *sseEmitter) toolCall(trace v1ToolTrace) {
	payload := map[string]any{
		"call": trace.Call,
		"tool": trace.Tool,
	}
	if len(trace.Args) > 0 {
		payload["args"] = trace.Args
	}
	if len(trace.Result) > 0 {
		payload["result"] = trace.Result
	}
	if trace.Error != "" {
		payload["error"] = trace.Error
	}
	emitter.emit("tool_call", payload)
}

func (emitter *sseEmitter) fatal(err error) {
	message := "chat stream failed"
	if err != nil {
		message = err.Error()
	}
	emitter.emit("fatal", map[string]any{
		"message": message,
	})
}

func (emitter *sseEmitter) summary(state chatTurnState) {
	toolTraces := make([]map[string]any, 0, len(state.ToolTraces))
	for _, trace := range state.ToolTraces {
		entry := map[string]any{
			"tool": trace.Tool,
			"call": trace.Call,
			"args": cloneMapOrEmpty(trace.Args),
		}
		if len(trace.Result) > 0 {
			entry["result"] = cloneMapOrEmpty(trace.Result)
		}
		if trace.Error != "" {
			entry["error"] = trace.Error
		}
		toolTraces = append(toolTraces, entry)
	}
	payload := map[string]any{
		"usage":          cloneMapOrEmpty(state.Usage),
		"memory_written": nil,
		"method_traces":  nil,
		"tool_traces":    toolTraces,
	}
	if state.Model != "" {
		payload["model"] = state.Model
	}
	if state.Provider != "" {
		payload["provider"] = state.Provider
	}
	emitter.emit("summary", payload)
}

func mapFromAny(value any) map[string]any {
	raw, ok := value.(map[string]any)
	if !ok || raw == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(raw))
	for key, entry := range raw {
		cloned[key] = entry
	}
	return cloned
}

func readErrorMessage(value any) string {
	entry, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return readOptionalString(entry, "message")
}

func sessionRolePtr(role domain.SessionRole) *domain.SessionRole {
	value := role
	return &value
}

func sessionStatusPtr(status domain.SessionStatus) *domain.SessionStatus {
	value := status
	return &value
}

func runCommand(ctx context.Context, workdir string, command []string) (string, error) {
	if len(command) == 0 {
		return "", domain.ValidationError{Message: "empty command"}
	}
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	if strings.TrimSpace(workdir) != "" {
		cmd.Dir = filepath.Clean(workdir)
	}
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", fmt.Errorf("%s failed: %w", strings.Join(command, " "), err)
		}
		return "", fmt.Errorf("%s failed: %w (%s)", strings.Join(command, " "), err, text)
	}
	if text == "" {
		return "", fmt.Errorf("%s returned empty stdout", strings.Join(command, " "))
	}
	return text, nil
}

func defaultScheduleCommand(workspaceRoot string) []string {
	if value := strings.TrimSpace(os.Getenv("OPENMATE_SCHEDULE_COMMAND")); value != "" {
		parts := strings.Fields(value)
		if len(parts) > 0 {
			return parts
		}
	}
	exePath := filepath.Join(workspaceRoot, filepath.FromSlash(".openmate/bin/openmate-schedule.exe"))
	if stat, err := os.Stat(exePath); err == nil && !stat.IsDir() {
		return []string{exePath}
	}
	binaryPath := filepath.Join(workspaceRoot, filepath.FromSlash(".openmate/bin/openmate-schedule"))
	if stat, err := os.Stat(binaryPath); err == nil && !stat.IsDir() {
		return []string{binaryPath}
	}
	return []string{"go", "run", "./cmd/openmate-schedule"}
}

func resolveRequestID(request *http.Request) string {
	if request != nil {
		candidates := []string{
			request.Header.Get("X-Request-ID"),
			request.Header.Get("X-Trace-ID"),
			request.Header.Get("X-Correlation-ID"),
		}
		for _, candidate := range candidates {
			value := strings.TrimSpace(candidate)
			if value != "" {
				return value
			}
		}
	}
	return fmt.Sprintf("req-%d", time.Now().UTC().UnixNano())
}
