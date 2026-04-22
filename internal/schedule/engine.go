package schedule

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"vos/internal/openmate/observability"
)

type SessionEventRecord struct {
	ID        string
	SessionID string
	Seq       int
}

type VOSGateway interface {
	EnsurePriorityNode(ctx context.Context, topicID string) (string, error)
	EnsureSession(ctx context.Context, nodeID string, knownSessionID *string) (string, error)
	AppendDispatchAuthorizedEvent(ctx context.Context, sessionID string, payload map[string]any) (SessionEventRecord, error)
	AppendDispatchResultEvent(ctx context.Context, sessionID string, payload map[string]any) error
}

type WorkerGateway interface {
	Execute(ctx context.Context, request WorkerExecuteRequest) (WorkerExecuteResponse, error)
}

type EngineConfig struct {
	MaxDispatchPerTick int
	DefaultTimeoutMS   int
	AgingThreshold     time.Duration
	Logger             *slog.Logger
}

type Engine struct {
	store  *RuntimeStore
	vos    VOSGateway
	worker WorkerGateway
	now    func() time.Time
	config EngineConfig
	logger *slog.Logger
}

func NewEngine(store *RuntimeStore, vos VOSGateway, worker WorkerGateway, config EngineConfig, now func() time.Time) (*Engine, error) {
	if store == nil {
		return nil, ValidationError{Message: "store is required"}
	}
	if vos == nil {
		return nil, ValidationError{Message: "vos gateway is required"}
	}
	if worker == nil {
		return nil, ValidationError{Message: "worker gateway is required"}
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if config.MaxDispatchPerTick <= 0 {
		config.MaxDispatchPerTick = 1
	}
	if config.DefaultTimeoutMS <= 0 {
		config.DefaultTimeoutMS = 120000
	}
	if config.AgingThreshold <= 0 {
		config.AgingThreshold = 10 * time.Minute
	}
	return &Engine{
		store:  store,
		vos:    vos,
		worker: worker,
		now:    now,
		config: config,
		logger: observability.NormalizeLogger(config.Logger),
	}, nil
}

func (engine *Engine) Enqueue(ctx context.Context, request EnqueueRequest) (EnqueueResult, error) {
	request, err := normalizeBusinessEnqueueRequest(request)
	if err != nil {
		return EnqueueResult{}, err
	}
	logger := observability.LoggerFromContext(ctx, engine.logger).With(
		slog.String(observability.FieldOperation, "schedule.enqueue"),
		slog.String(observability.FieldTopicID, request.TopicID),
		slog.String(observability.FieldNodeID, request.NodeID),
	)
	logger.Debug("enqueue requested")
	created, err := engine.store.UpsertEnqueueNode(request)
	if err != nil {
		logger.Error("enqueue failed", slog.Any("error", err))
		return EnqueueResult{}, err
	}
	topic, err := engine.store.GetTopic(request.TopicID)
	if err != nil {
		logger.Error("load topic after enqueue failed", slog.Any("error", err))
		return EnqueueResult{}, err
	}
	result := EnqueueResult{
		TopicID:        request.TopicID,
		NodeID:         request.NodeID,
		Created:        created,
		PriorityDirty:  topic.PriorityDirty,
		QueueLevel:     string(topic.QueueLevel),
		IdempotencyKey: request.IdempotencyKey,
	}
	if topic.PriorityNodeID != nil {
		result.PriorityNodeID = *topic.PriorityNodeID
	}
	logger.Info(
		"enqueue succeeded",
		slog.Bool("created", result.Created),
		slog.Bool("priority_dirty", result.PriorityDirty),
		slog.String("queue_level", result.QueueLevel),
	)
	return result, nil
}

func normalizeBusinessEnqueueRequest(request EnqueueRequest) (EnqueueRequest, error) {
	if request.TopicID == "" {
		return EnqueueRequest{}, ValidationError{Message: "topic_id must not be empty"}
	}
	if request.NodeID == "" {
		return EnqueueRequest{}, ValidationError{Message: "node_id must not be empty"}
	}
	if request.NodeName == "" {
		request.NodeName = request.NodeID
	}
	if request.Priority.Label == "" {
		request.Priority = NodePriority{
			Label: BusinessNodePriorityLabel,
			Rank:  BusinessNodePriorityRank,
		}
	}
	if err := request.Priority.Validate(); err != nil {
		return EnqueueRequest{}, err
	}
	if request.Priority.Label != BusinessNodePriorityLabel || request.Priority.Rank != BusinessNodePriorityRank {
		return EnqueueRequest{}, ValidationError{
			Message: fmt.Sprintf(
				"business node priority must be fixed to label=%q rank=%d",
				BusinessNodePriorityLabel,
				BusinessNodePriorityRank,
			),
		}
	}
	request.MarkPriorityDirty = true
	return request, nil
}

func (engine *Engine) Tick(ctx context.Context, maxDispatch int) (TickResult, error) {
	logger := observability.LoggerFromContext(ctx, engine.logger).With(
		slog.String(observability.FieldOperation, "schedule.tick"),
	)
	startedAt := engine.now()
	result := TickResult{}
	result.Normalize()

	limit := maxDispatch
	if limit <= 0 {
		limit = engine.config.MaxDispatchPerTick
	}
	if limit <= 0 {
		return result, ValidationError{Message: "max dispatch must be positive"}
	}

	if err := engine.store.PromoteAgedTopics(engine.config.AgingThreshold); err != nil {
		logger.Error("promote aged topics failed", slog.Any("error", err))
		return result, err
	}

	topic, err := engine.selectTopic()
	if err != nil {
		logger.Error("select topic failed", slog.Any("error", err))
		return result, err
	}
	if topic == nil {
		result.Reasons = append(result.Reasons, "no runnable topic found")
		logger.Debug("tick finished without runnable topic")
		return result, nil
	}
	result.SelectedTopicID = topic.TopicID
	result.QueueLevel = string(topic.QueueLevel)
	logger = logger.With(
		slog.String(observability.FieldTopicID, topic.TopicID),
		slog.String("queue_level", string(topic.QueueLevel)),
	)

	if topic.PriorityDirty {
		if len(topic.RunningNodeIDs) > 0 {
			result.Reasons = append(result.Reasons, "priority_node waits until current sessionevent calls complete")
			logger.Debug("priority node skipped because topic has running nodes")
			return result, nil
		}
		if err := engine.ensurePriorityNode(ctx, topic.TopicID); err != nil {
			logger.Error("ensure priority node failed", slog.Any("error", err))
			return result, err
		}
		if err := engine.store.MarkPriorityNodeReady(topic.TopicID); err != nil {
			logger.Error("mark priority node ready failed", slog.Any("error", err))
			return result, err
		}
	}

	snapshot, err := engine.store.BuildTopicSnapshot(topic.TopicID)
	if err != nil {
		logger.Error("build topic snapshot failed", slog.Any("error", err))
		return result, err
	}
	plan, err := planTopicDispatch(snapshot, limit)
	if err != nil {
		logger.Error("build dispatch plan failed", slog.Any("error", err))
		return result, err
	}
	if len(plan.Reasons) > 0 {
		result.Reasons = append(result.Reasons, plan.Reasons...)
	}
	if len(plan.DispatchNodeIDs) == 0 {
		logger.Debug("dispatch plan has no runnable nodes")
		return result, nil
	}

	for _, nodeID := range plan.DispatchNodeIDs {
		record, dispatchErr := engine.dispatchOne(ctx, topic.TopicID, nodeID)
		if dispatchErr != nil {
			logger.Error(
				"dispatch node failed",
				slog.String(observability.FieldNodeID, nodeID),
				slog.Any("error", dispatchErr),
			)
			return result, dispatchErr
		}
		result.Dispatched = append(result.Dispatched, record)
	}

	if err := engine.store.TouchTopicServed(topic.TopicID); err != nil {
		logger.Error("touch topic served failed", slog.Any("error", err))
		return result, err
	}
	hasRunnable, err := engine.store.HasRunnableNodes(topic.TopicID)
	if err != nil {
		logger.Error("check runnable nodes failed", slog.Any("error", err))
		return result, err
	}
	if hasRunnable {
		if err := engine.store.DemoteTopic(topic.TopicID); err != nil {
			logger.Error("demote topic failed", slog.Any("error", err))
			return result, err
		}
	}
	logger.Info(
		"tick completed",
		slog.Int("dispatched_count", len(result.Dispatched)),
		slog.Int64(observability.FieldDurationMS, int64(engine.now().Sub(startedAt).Milliseconds())),
	)

	return result, nil
}

func (engine *Engine) ensurePriorityNode(ctx context.Context, topicID string) error {
	topic, err := engine.store.GetTopic(topicID)
	if err != nil {
		return err
	}
	if topic.PriorityNodeID != nil {
		return nil
	}
	priorityNodeID, err := engine.vos.EnsurePriorityNode(ctx, topicID)
	if err != nil {
		return fmt.Errorf("ensure priority node in vos: %w", err)
	}
	if err := engine.store.UpsertPriorityNode(topicID, priorityNodeID, AgentSpec{Mode: "priority"}); err != nil {
		return err
	}
	return nil
}

func (engine *Engine) dispatchOne(ctx context.Context, topicID, nodeID string) (DispatchRecord, error) {
	baseLogger := observability.LoggerFromContext(ctx, engine.logger).With(
		slog.String(observability.FieldOperation, "schedule.dispatch"),
		slog.String(observability.FieldTopicID, topicID),
		slog.String(observability.FieldNodeID, nodeID),
	)
	startedAt := engine.now()

	node, err := engine.store.GetNode(topicID, nodeID)
	if err != nil {
		baseLogger.Error("load node failed", slog.Any("error", err))
		return DispatchRecord{}, err
	}
	if err := engine.store.MarkNodeRunning(topicID, nodeID); err != nil {
		baseLogger.Error("mark node running failed", slog.Any("error", err))
		return DispatchRecord{}, err
	}

	sessionID, err := engine.vos.EnsureSession(ctx, nodeID, node.SessionID)
	if err != nil {
		baseLogger.Error("ensure session failed", slog.Any("error", err))
		return DispatchRecord{}, fmt.Errorf("ensure session for node %s: %w", nodeID, err)
	}
	dispatchLogger := baseLogger.With(slog.String(observability.FieldSessionID, sessionID))
	if node.SessionID == nil || *node.SessionID != sessionID {
		if err := engine.store.SetNodeSessionID(topicID, nodeID, sessionID); err != nil {
			dispatchLogger.Error("persist node session id failed", slog.Any("error", err))
			return DispatchRecord{}, err
		}
	}

	requestID := fmt.Sprintf("%s-%d", nodeID, engine.now().UnixNano())
	dispatchLogger = dispatchLogger.With(
		slog.String(observability.FieldTraceID, requestID),
		slog.String(observability.FieldRequestID, requestID),
	)
	ctx = observability.WithLogger(ctx, dispatchLogger)
	startEvent, err := engine.vos.AppendDispatchAuthorizedEvent(ctx, sessionID, map[string]any{
		"kind":       "dispatch_authorized",
		"request_id": requestID,
		"topic_id":   topicID,
		"node_id":    nodeID,
	})
	if err != nil {
		dispatchLogger.Error("append dispatch authorized event failed", slog.Any("error", err))
		return DispatchRecord{}, fmt.Errorf("append dispatch authorized event: %w", err)
	}
	dispatchLogger = dispatchLogger.With(slog.String(observability.FieldEventID, startEvent.ID))
	dispatchLogger.Info("dispatch started")

	workerRequest := WorkerExecuteRequest{
		RequestID: requestID,
		TopicID:   topicID,
		NodeID:    nodeID,
		NodeName:  node.Name,
		NodeKind:  "normal",
		AgentSpec: node.AgentSpec,
		SessionID: sessionID,
		EventID:   startEvent.ID,
		TimeoutMS: engine.config.DefaultTimeoutMS,
	}
	if node.IsPriorityNode {
		workerRequest.NodeKind = "priority"
		candidates, err := engine.loadPriorityCandidates(topicID)
		if err != nil {
			return DispatchRecord{}, err
		}
		workerRequest.PriorityCandidates = candidates
	}

	response, err := engine.worker.Execute(ctx, workerRequest)
	if err != nil {
		_ = engine.store.MarkNodeFinished(topicID, nodeID, NodeStatusFailed)
		dispatchLogger.Error("worker execute failed", slog.Any("error", err))
		return DispatchRecord{}, fmt.Errorf("worker execute failed: %w", err)
	}

	finishedStatus := resolveNodeStatus(node, response)
	if err := engine.store.MarkNodeFinished(topicID, nodeID, finishedStatus); err != nil {
		dispatchLogger.Error("mark node finished failed", slog.Any("error", err))
		return DispatchRecord{}, err
	}
	if node.IsPriorityNode {
		if response.Status == "succeeded" {
			if err := engine.store.ApplyPriorityPlan(topicID, response.PriorityPlan); err != nil {
				dispatchLogger.Error("apply priority plan failed", slog.Any("error", err))
				return DispatchRecord{}, err
			}
		} else {
			message := response.Error
			if message == "" {
				message = "priority node execution failed"
			}
			if err := engine.store.ClearPriorityDirty(topicID, &message); err != nil {
				dispatchLogger.Error("clear priority dirty failed", slog.Any("error", err))
				return DispatchRecord{}, err
			}
		}
	}

	_ = engine.vos.AppendDispatchResultEvent(ctx, sessionID, map[string]any{
		"kind":        "dispatch_result",
		"request_id":  requestID,
		"topic_id":    topicID,
		"node_id":     nodeID,
		"status":      response.Status,
		"event_id":    response.EventID,
		"output":      response.Output,
		"error":       response.Error,
		"retryable":   response.Retryable,
		"duration_ms": response.DurationMS,
	})

	record := DispatchRecord{
		RequestID:  requestID,
		TopicID:    topicID,
		NodeID:     nodeID,
		NodeKind:   workerRequest.NodeKind,
		SessionID:  sessionID,
		EventID:    startEvent.ID,
		Status:     response.Status,
		Retryable:  response.Retryable,
		Error:      response.Error,
		DurationMS: response.DurationMS,
	}
	if err := engine.store.RecordDispatch(record); err != nil {
		dispatchLogger.Error("record dispatch failed", slog.Any("error", err))
		return DispatchRecord{}, err
	}
	dispatchLogger.Info(
		"dispatch finished",
		slog.String("status", record.Status),
		slog.Bool("retryable", record.Retryable),
		slog.Int64(observability.FieldDurationMS, int64(engine.now().Sub(startedAt).Milliseconds())),
	)
	return record, nil
}

func resolveNodeStatus(node NodeQueueState, response WorkerExecuteResponse) NodeStatus {
	if response.NextNodeStatus != nil {
		return *response.NextNodeStatus
	}
	if node.IsPriorityNode {
		if response.Status == "succeeded" {
			return NodeStatusSucceeded
		}
		return NodeStatusFailed
	}
	if response.Status == "succeeded" {
		return NodeStatusReady
	}
	if response.Retryable {
		return NodeStatusRetryCooldown
	}
	return NodeStatusFailed
}

func (engine *Engine) loadPriorityCandidates(topicID string) ([]WorkerCandidateNode, error) {
	nodes, err := engine.store.ListNodes(topicID)
	if err != nil {
		return nil, err
	}
	candidates := make([]WorkerCandidateNode, 0, len(nodes))
	for _, node := range nodes {
		if node.IsPriorityNode {
			continue
		}
		if node.Status.statusIsTerminal() {
			continue
		}
		candidates = append(candidates, WorkerCandidateNode{
			NodeID:            node.NodeID,
			Name:              node.Name,
			Status:            node.Status,
			CurrentPriority:   NodePriority{Label: node.PriorityLabel, Rank: node.PriorityRank},
			EnteredPriorityAt: node.EnteredPriorityAt,
			LastWorkedAt:      cloneTime(node.LastWorkedAt),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CurrentPriority.Rank == candidates[j].CurrentPriority.Rank {
			if candidates[i].EnteredPriorityAt.Equal(candidates[j].EnteredPriorityAt) {
				return candidates[i].NodeID < candidates[j].NodeID
			}
			return candidates[i].EnteredPriorityAt.Before(candidates[j].EnteredPriorityAt)
		}
		return candidates[i].CurrentPriority.Rank < candidates[j].CurrentPriority.Rank
	})
	return candidates, nil
}

func (engine *Engine) selectTopic() (*TopicControlState, error) {
	topics, err := engine.store.ListTopics()
	if err != nil {
		return nil, err
	}
	if len(topics) == 0 {
		return nil, nil
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].QueueLevel == topics[j].QueueLevel {
			left := time.Time{}
			right := time.Time{}
			if topics[i].LastServedAt != nil {
				left = *topics[i].LastServedAt
			}
			if topics[j].LastServedAt != nil {
				right = *topics[j].LastServedAt
			}
			if left.Equal(right) {
				return topics[i].TopicID < topics[j].TopicID
			}
			return left.Before(right)
		}
		return queueLevelOrder(topics[i].QueueLevel) < queueLevelOrder(topics[j].QueueLevel)
	})
	for index := range topics {
		hasRunnable, err := engine.store.HasRunnableNodes(topics[index].TopicID)
		if err != nil {
			return nil, err
		}
		if !hasRunnable {
			continue
		}
		return &topics[index], nil
	}
	return nil, nil
}

func queueLevelOrder(level TopicQueueLevel) int {
	switch level {
	case TopicQueueLevelL0:
		return 0
	case TopicQueueLevelL1:
		return 1
	case TopicQueueLevelL2:
		return 2
	default:
		return 3
	}
}
