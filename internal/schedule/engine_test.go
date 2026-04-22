package schedule

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEnginePriorityNodeRunsBeforeNormalNodes(t *testing.T) {
	store := openTestRuntimeStore(t)
	defer func() {
		_ = store.Close()
	}()

	vos := &fakeVOSGateway{}
	worker := &fakeWorkerGateway{}
	now := fixedNow(time.Date(2026, time.April, 14, 9, 0, 0, 0, time.UTC))
	engine, err := NewEngine(
		store,
		vos,
		worker,
		EngineConfig{
			MaxDispatchPerTick: 1,
			DefaultTimeoutMS:   120000,
			AgingThreshold:     10 * time.Minute,
		},
		now,
	)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	_, err = engine.Enqueue(context.Background(), EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-a",
		NodeName: "node a",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
		Priority: NodePriority{
			Label: BusinessNodePriorityLabel,
			Rank:  BusinessNodePriorityRank,
		},
		MarkPriorityDirty: true,
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	firstTick, err := engine.Tick(context.Background(), 1)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(firstTick.Dispatched) != 1 {
		t.Fatalf("first tick dispatched = %+v, want one dispatch", firstTick.Dispatched)
	}
	if firstTick.Dispatched[0].NodeKind != "priority" {
		t.Fatalf("first dispatch kind = %s, want priority", firstTick.Dispatched[0].NodeKind)
	}

	secondTick, err := engine.Tick(context.Background(), 1)
	if err != nil {
		t.Fatalf("Tick() second error = %v", err)
	}
	if len(secondTick.Dispatched) != 1 {
		t.Fatalf("second tick dispatched = %+v, want one dispatch", secondTick.Dispatched)
	}
	if secondTick.Dispatched[0].NodeID != "node-a" {
		t.Fatalf("second dispatch node = %s, want node-a", secondTick.Dispatched[0].NodeID)
	}
}

func TestEnginePriorityWaitsForRunningNodes(t *testing.T) {
	store := openTestRuntimeStore(t)
	defer func() {
		_ = store.Close()
	}()

	vos := &fakeVOSGateway{}
	worker := &fakeWorkerGateway{}
	now := fixedNow(time.Date(2026, time.April, 14, 9, 0, 0, 0, time.UTC))
	engine, err := NewEngine(
		store,
		vos,
		worker,
		EngineConfig{
			MaxDispatchPerTick: 1,
			DefaultTimeoutMS:   120000,
			AgingThreshold:     10 * time.Minute,
		},
		now,
	)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	_, err = engine.Enqueue(context.Background(), EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-a",
		NodeName: "node a",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
		Priority: NodePriority{
			Label: BusinessNodePriorityLabel,
			Rank:  BusinessNodePriorityRank,
		},
		MarkPriorityDirty: true,
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if err := store.MarkNodeRunning("topic-1", "node-a"); err != nil {
		t.Fatalf("MarkNodeRunning() error = %v", err)
	}
	if err := store.EnsurePriorityDirty("topic-1", "preemption"); err != nil {
		t.Fatalf("EnsurePriorityDirty() error = %v", err)
	}

	result, err := engine.Tick(context.Background(), 1)
	if err != nil {
		t.Fatalf("Tick() error = %v", err)
	}
	if len(result.Dispatched) != 0 {
		t.Fatalf("dispatched = %+v, want none", result.Dispatched)
	}
	if len(result.Reasons) == 0 || !strings.Contains(strings.Join(result.Reasons, " "), "waits") {
		t.Fatalf("reasons = %+v, want wait reason", result.Reasons)
	}
}

func openTestRuntimeStore(t *testing.T) *RuntimeStore {
	t.Helper()
	dbFile := filepath.Join(t.TempDir(), "schedule.db")
	store, err := OpenRuntimeStore(dbFile, fixedNow(time.Date(2026, time.April, 14, 9, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("OpenRuntimeStore() error = %v", err)
	}
	return store
}

func fixedNow(value time.Time) func() time.Time {
	return func() time.Time {
		return value
	}
}

func TestEngineEnqueueDefaultsBusinessPriority(t *testing.T) {
	store := openTestRuntimeStore(t)
	defer func() {
		_ = store.Close()
	}()

	vos := &fakeVOSGateway{}
	worker := &fakeWorkerGateway{}
	engine, err := NewEngine(
		store,
		vos,
		worker,
		EngineConfig{
			MaxDispatchPerTick: 1,
			DefaultTimeoutMS:   120000,
			AgingThreshold:     10 * time.Minute,
		},
		fixedNow(time.Date(2026, time.April, 14, 9, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	_, err = engine.Enqueue(context.Background(), EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-a",
		NodeName: "node a",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
	})
	if err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	node, err := store.GetNode("topic-1", "node-a")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	if node.PriorityLabel != BusinessNodePriorityLabel || node.PriorityRank != BusinessNodePriorityRank {
		t.Fatalf(
			"stored priority = (%s,%d), want (%s,%d)",
			node.PriorityLabel,
			node.PriorityRank,
			BusinessNodePriorityLabel,
			BusinessNodePriorityRank,
		)
	}
}

func TestEngineEnqueueAlwaysMarksPriorityDirty(t *testing.T) {
	store := openTestRuntimeStore(t)
	defer func() {
		_ = store.Close()
	}()

	vos := &fakeVOSGateway{}
	worker := &fakeWorkerGateway{}
	engine, err := NewEngine(
		store,
		vos,
		worker,
		EngineConfig{
			MaxDispatchPerTick: 1,
			DefaultTimeoutMS:   120000,
			AgingThreshold:     10 * time.Minute,
		},
		fixedNow(time.Date(2026, time.April, 14, 9, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	_, err = engine.Enqueue(context.Background(), EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-a",
		NodeName: "node a",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
	})
	if err != nil {
		t.Fatalf("first Enqueue() error = %v", err)
	}
	if err := store.ClearPriorityDirty("topic-1", nil); err != nil {
		t.Fatalf("ClearPriorityDirty() error = %v", err)
	}

	result, err := engine.Enqueue(context.Background(), EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-b",
		NodeName: "node b",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
	})
	if err != nil {
		t.Fatalf("second Enqueue() error = %v", err)
	}
	if !result.PriorityDirty {
		t.Fatalf("result.PriorityDirty = false, want true")
	}

	topic, err := store.GetTopic("topic-1")
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	if !topic.PriorityDirty {
		t.Fatalf("topic.PriorityDirty = false, want true")
	}
}

func TestEngineEnqueueRejectsNonBusinessPriority(t *testing.T) {
	store := openTestRuntimeStore(t)
	defer func() {
		_ = store.Close()
	}()

	vos := &fakeVOSGateway{}
	worker := &fakeWorkerGateway{}
	engine, err := NewEngine(
		store,
		vos,
		worker,
		EngineConfig{
			MaxDispatchPerTick: 1,
			DefaultTimeoutMS:   120000,
			AgingThreshold:     10 * time.Minute,
		},
		fixedNow(time.Date(2026, time.April, 14, 9, 0, 0, 0, time.UTC)),
	)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	_, err = engine.Enqueue(context.Background(), EnqueueRequest{
		TopicID:  "topic-1",
		NodeID:   "node-a",
		NodeName: "node a",
		AgentSpec: AgentSpec{
			Mode: "simulate_success",
		},
		Priority: NodePriority{
			Label: "normal",
			Rank:  2,
		},
	})
	if err == nil {
		t.Fatalf("Enqueue() error = nil, want validation error")
	}
	if !strings.Contains(err.Error(), "business node priority must be fixed") {
		t.Fatalf("Enqueue() error = %v, want business priority validation", err)
	}
}

type fakeVOSGateway struct {
	eventSeq int
}

func (gateway *fakeVOSGateway) EnsurePriorityNode(_ context.Context, topicID string) (string, error) {
	return topicID + "-priority-node", nil
}

func (gateway *fakeVOSGateway) EnsureSession(_ context.Context, nodeID string, knownSessionID *string) (string, error) {
	if knownSessionID != nil && *knownSessionID != "" {
		return *knownSessionID, nil
	}
	return "session-" + nodeID, nil
}

func (gateway *fakeVOSGateway) AppendDispatchAuthorizedEvent(_ context.Context, sessionID string, _ map[string]any) (SessionEventRecord, error) {
	gateway.eventSeq++
	return SessionEventRecord{
		ID:        "event-" + sessionID,
		SessionID: sessionID,
		Seq:       gateway.eventSeq,
	}, nil
}

func (gateway *fakeVOSGateway) AppendDispatchResultEvent(_ context.Context, _ string, _ map[string]any) error {
	return nil
}

type fakeWorkerGateway struct{}

func (gateway *fakeWorkerGateway) Execute(_ context.Context, request WorkerExecuteRequest) (WorkerExecuteResponse, error) {
	if request.NodeKind == "priority" {
		plan := []WorkerPriorityAssignment{}
		for index, candidate := range request.PriorityCandidates {
			plan = append(plan, WorkerPriorityAssignment{
				NodeID: candidate.NodeID,
				Label:  "now",
				Rank:   index + 1,
			})
		}
		return WorkerExecuteResponse{
			RequestID:    request.RequestID,
			TopicID:      request.TopicID,
			NodeID:       request.NodeID,
			SessionID:    request.SessionID,
			EventID:      request.EventID,
			Status:       "succeeded",
			DurationMS:   10,
			PriorityPlan: plan,
		}, nil
	}
	return WorkerExecuteResponse{
		RequestID:  request.RequestID,
		TopicID:    request.TopicID,
		NodeID:     request.NodeID,
		SessionID:  request.SessionID,
		EventID:    request.EventID,
		Status:     "succeeded",
		Output:     "ok",
		DurationMS: 12,
	}, nil
}
