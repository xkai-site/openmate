package poolgateway

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGatewayInvokeWithRealProvider(t *testing.T) {
	if os.Getenv("OPENMATE_POOL_RUN_INTEGRATION") != "1" {
		t.Skip("set OPENMATE_POOL_RUN_INTEGRATION=1 to run real-provider integration tests")
	}

	configPath := os.Getenv("OPENMATE_POOL_MODEL_CONFIG")
	if configPath == "" {
		t.Skip("set OPENMATE_POOL_MODEL_CONFIG to a real model.json path")
	}

	store, err := NewStore(filepath.Join(t.TempDir(), "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	response, err := gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "integration-req",
		NodeID:    "integration-node",
		Request: OpenAIResponsesRequest{
			"input": "reply with the single word pong",
		},
		TimeoutMS: intValue(30_000),
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if response.Status != InvocationStatusSuccess {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Route == nil {
		t.Fatalf("expected route in response")
	}
	if response.OutputText == nil || *response.OutputText == "" {
		t.Fatalf("expected non-empty output")
	}
	if response.Usage == nil || response.Usage.TotalTokens == nil {
		t.Fatalf("expected usage totals")
	}
	if response.Timing.LatencyMS == nil {
		t.Fatalf("expected latency in response")
	}

	records, err := gateway.Records(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if len(records[0].Attempts) == 0 {
		t.Fatalf("expected at least 1 attempt")
	}
}

func intValue(value int) *int {
	return &value
}
