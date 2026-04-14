package poolgateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestGatewayInvokeSuccessPersistsRecord(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 3)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return successProvider{}, nil
	})

	response, err := gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-1",
		NodeID:    "node-1",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if response.Status != InvocationStatusSuccess {
		t.Fatalf("unexpected status: %s", response.Status)
	}
	if response.Route == nil || response.Route.APIID != "api-1" {
		t.Fatalf("unexpected route: %+v", response.Route)
	}
	if response.OutputText == nil || *response.OutputText != "ok from api-1" {
		t.Fatalf("unexpected output: %+v", response.OutputText)
	}

	records, err := gateway.Records(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if len(records[0].Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(records[0].Attempts))
	}
}

func TestGatewayRoutePolicyPinsAPI(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 3)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return successProvider{}, nil
	})
	apiID := "api-2"
	response, err := gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-2",
		NodeID:    "node-2",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
		RoutePolicy: RoutePolicy{APIID: &apiID},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if response.Route == nil || response.Route.APIID != "api-2" {
		t.Fatalf("unexpected route: %+v", response.Route)
	}
}

func TestGatewayFailureThresholdMovesAPIOffline(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 1)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return failureProvider{}, nil
	})
	apiID := "api-1"
	_, err = gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-fail",
		NodeID:    "node-fail",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
		RoutePolicy: RoutePolicy{APIID: &apiID},
	})
	if err == nil {
		t.Fatalf("expected invocation failure")
	}
	capacity, err := gateway.Capacity(context.Background())
	if err != nil {
		t.Fatalf("capacity: %v", err)
	}
	if capacity.OfflineAPIs != 1 {
		t.Fatalf("expected 1 offline API, got %d", capacity.OfflineAPIs)
	}
	_, err = gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-fail-2",
		NodeID:    "node-fail-2",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
		RoutePolicy: RoutePolicy{APIID: &apiID},
	})
	if err == nil || err.Error() != ErrNoCapacity.Error() {
		t.Fatalf("expected no capacity, got %v", err)
	}
}

func TestGatewayRetryableFailureSucceedsOnSecondAttempt(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 3)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetRetryPolicy(RetryPolicy{MaxAttempts: 2})
	gateway.SetSleepFn(func(ctx context.Context, delay time.Duration) error {
		_ = ctx
		_ = delay
		return nil
	})
	scripted := &scriptedProvider{
		outcomes: []providerOutcome{
			{
				err: &ProviderInvocationError{
					GatewayError: GatewayError{
						Code:      "provider_timeout",
						Message:   "timeout from api-1",
						Retryable: true,
						Details:   map[string]any{},
					},
				},
			},
			{
				result: successfulProviderResult("retry-ok"),
			},
		},
	}
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return scripted, nil
	})

	apiID := "api-1"
	response, err := gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-retry",
		NodeID:    "node-retry",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
		RoutePolicy: RoutePolicy{APIID: &apiID},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if response.Status != InvocationStatusSuccess {
		t.Fatalf("unexpected status: %s", response.Status)
	}

	records, err := gateway.Records(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if len(records[0].Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(records[0].Attempts))
	}
	if records[0].Attempts[0].Error == nil || records[0].Attempts[0].Error.Code != "provider_timeout" {
		t.Fatalf("unexpected first attempt error: %+v", records[0].Attempts[0].Error)
	}
	if records[0].Attempts[1].Status != InvocationStatusSuccess {
		t.Fatalf("unexpected second attempt status: %s", records[0].Attempts[1].Status)
	}

	capacity, err := gateway.Capacity(context.Background())
	if err != nil {
		t.Fatalf("capacity: %v", err)
	}
	if capacity.OfflineAPIs != 0 {
		t.Fatalf("expected 0 offline APIs, got %d", capacity.OfflineAPIs)
	}
}

func TestGatewayRateLimitedFailureRetriesWithoutOffliningAPI(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 1)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetRetryPolicy(RetryPolicy{MaxAttempts: 2})
	gateway.SetSleepFn(func(ctx context.Context, delay time.Duration) error {
		_ = ctx
		_ = delay
		return nil
	})
	scripted := &scriptedProvider{
		outcomes: func() []providerOutcome {
			providerStatusCode := 429
			return []providerOutcome{
				{
					err: &ProviderInvocationError{
						GatewayError: GatewayError{
							Code:               "provider_rate_limited",
							Message:            "provider returned HTTP 429",
							Retryable:          true,
							ProviderStatusCode: &providerStatusCode,
							Details:            map[string]any{},
						},
					},
				},
				{
					result: successfulProviderResult("rate-limit-ok"),
				},
			}
		}(),
	}
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return scripted, nil
	})

	apiID := "api-1"
	response, err := gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-429",
		NodeID:    "node-429",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
		RoutePolicy: RoutePolicy{APIID: &apiID},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if response.Status != InvocationStatusSuccess {
		t.Fatalf("unexpected status: %s", response.Status)
	}

	records, err := gateway.Records(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 || len(records[0].Attempts) != 2 {
		t.Fatalf("unexpected attempts: %+v", records)
	}
	if records[0].Attempts[0].Error == nil || records[0].Attempts[0].Error.Code != "provider_rate_limited" {
		t.Fatalf("unexpected first attempt error: %+v", records[0].Attempts[0].Error)
	}

	capacity, err := gateway.Capacity(context.Background())
	if err != nil {
		t.Fatalf("capacity: %v", err)
	}
	if capacity.OfflineAPIs != 0 {
		t.Fatalf("expected 0 offline APIs, got %d", capacity.OfflineAPIs)
	}
}

func TestGatewayInvalidJSONFailureDoesNotTakeAPIOffline(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 1)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return invalidJSONProvider{}, nil
	})

	apiID := "api-1"
	_, err = gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-json",
		NodeID:    "node-json",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
		RoutePolicy: RoutePolicy{APIID: &apiID},
	})
	if err == nil {
		t.Fatalf("expected invocation failure")
	}
	var invocationErr *InvocationFailedError
	if !errors.As(err, &invocationErr) {
		t.Fatalf("expected invocation failure error, got %T", err)
	}
	if invocationErr.Response.Error == nil || invocationErr.Response.Error.Code != "provider_invalid_json" {
		t.Fatalf("unexpected response error: %+v", invocationErr.Response.Error)
	}

	capacity, err := gateway.Capacity(context.Background())
	if err != nil {
		t.Fatalf("capacity: %v", err)
	}
	if capacity.OfflineAPIs != 0 {
		t.Fatalf("expected 0 offline APIs, got %d", capacity.OfflineAPIs)
	}
}

func TestGatewayModelConfigCanDisableRetries(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	maxAttempts := 1
	baseBackoffMS := 0
	writeModelConfigWithRetry(t, configPath, 3, &maxAttempts, &baseBackoffMS)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetSleepFn(func(ctx context.Context, delay time.Duration) error {
		_ = ctx
		_ = delay
		return nil
	})
	scripted := &scriptedProvider{
		outcomes: []providerOutcome{
			{
				err: &ProviderInvocationError{
					GatewayError: GatewayError{
						Code:      "provider_rate_limited",
						Message:   "provider returned HTTP 429",
						Retryable: true,
						Details:   map[string]any{},
					},
				},
			},
			{
				result: successfulProviderResult("should-not-run"),
			},
		},
	}
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return scripted, nil
	})

	apiID := "api-1"
	_, err = gateway.Invoke(context.Background(), InvokeRequest{
		RequestID: "req-no-retry",
		NodeID:    "node-no-retry",
		Request: OpenAIResponsesRequest{
			"input": "hello",
		},
		RoutePolicy: RoutePolicy{APIID: &apiID},
	})
	if err == nil {
		t.Fatalf("expected invocation failure")
	}

	records, err := gateway.Records(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if len(records[0].Attempts) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(records[0].Attempts))
	}
	if records[0].Error == nil || records[0].Error.Code != "provider_rate_limited" {
		t.Fatalf("unexpected final error: %+v", records[0].Error)
	}
}

func TestGatewayUsageSummaryAggregatesRecords(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 3)

	store, err := NewStore(filepath.Join(tempDir, "pool_state.db"))
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	gateway := NewGateway(store, configPath)
	gateway.SetSleepFn(func(ctx context.Context, delay time.Duration) error {
		_ = ctx
		_ = delay
		return nil
	})
	rateLimitStatusCode := 429
	scripted := &scriptedProvider{
		outcomes: []providerOutcome{
			{
				result: successfulProviderResult("first-ok"),
			},
			{
				err: &ProviderInvocationError{
					GatewayError: GatewayError{
						Code:               "provider_rate_limited",
						Message:            "provider returned HTTP 429",
						Retryable:          true,
						ProviderStatusCode: &rateLimitStatusCode,
						Details:            map[string]any{},
					},
				},
			},
			{
				result: successfulProviderResult("second-ok"),
			},
			{
				err: &ProviderInvocationError{
					GatewayError: GatewayError{
						Code:      "provider_invalid_json",
						Message:   "provider returned invalid json",
						Retryable: false,
						Details:   map[string]any{},
					},
				},
			},
		},
	}
	gateway.SetProviderFactory(func(provider string) (ProviderClient, error) {
		return scripted, nil
	})

	invoke := func(nodeID string) error {
		_, err := gateway.Invoke(context.Background(), InvokeRequest{
			RequestID: "req-" + nodeID,
			NodeID:    nodeID,
			Request: OpenAIResponsesRequest{
				"input": "hello",
			},
		})
		return err
	}
	if err := invoke("node-1"); err != nil {
		t.Fatalf("invoke node-1: %v", err)
	}
	if err := invoke("node-2"); err != nil {
		t.Fatalf("invoke node-2: %v", err)
	}
	if err := invoke("node-3"); err == nil {
		t.Fatalf("expected invocation failure for node-3")
	}

	summary, err := gateway.Usage(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("usage: %v", err)
	}
	if summary.InvocationCount != 3 {
		t.Fatalf("expected 3 invocations, got %d", summary.InvocationCount)
	}
	if summary.SuccessCount != 2 || summary.FailureCount != 1 {
		t.Fatalf("unexpected success/failure counts: %+v", summary)
	}
	if summary.AttemptCount != 4 || summary.RetryCount != 1 {
		t.Fatalf("unexpected attempt counts: %+v", summary)
	}
	if summary.InputTokens != 2 || summary.OutputTokens != 4 || summary.TotalTokens != 6 {
		t.Fatalf("unexpected usage totals: %+v", summary)
	}
	if summary.AvgLatencyMS == nil || summary.MaxLatencyMS == nil {
		t.Fatalf("expected latency aggregates: %+v", summary)
	}

	nodeID := "node-2"
	filtered, err := gateway.Usage(context.Background(), &nodeID, nil)
	if err != nil {
		t.Fatalf("filtered usage: %v", err)
	}
	if filtered.InvocationCount != 1 || filtered.AttemptCount != 2 || filtered.RetryCount != 1 {
		t.Fatalf("unexpected filtered summary: %+v", filtered)
	}
	if filtered.NodeID == nil || *filtered.NodeID != "node-2" {
		t.Fatalf("unexpected filtered node id: %+v", filtered.NodeID)
	}
}

type successProvider struct{}

func (successProvider) Invoke(ctx context.Context, reservation InvocationReservation, request InvokeRequest) (ProviderInvokeResult, error) {
	_ = ctx
	_ = request
	text := "ok from " + reservation.APIID
	prompt := 1
	completion := 2
	total := 3
	return ProviderInvokeResult{
		OutputText: &text,
		Response: map[string]any{
			"id":     "resp-" + reservation.APIID,
			"object": "response",
			"model":  reservation.Model,
			"status": "completed",
			"output": []any{
				map[string]any{
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []any{
						map[string]any{
							"type": "output_text",
							"text": text,
						},
					},
				},
			},
		},
		Usage: &UsageMetrics{
			InputTokens:  &prompt,
			OutputTokens: &completion,
			TotalTokens:  &total,
		},
	}, nil
}

type failureProvider struct{}

func (failureProvider) Invoke(ctx context.Context, reservation InvocationReservation, request InvokeRequest) (ProviderInvokeResult, error) {
	_ = ctx
	_ = request
	return ProviderInvokeResult{}, &ProviderInvocationError{
		GatewayError: GatewayError{
			Code:      "provider_timeout",
			Message:   "timeout from " + reservation.APIID,
			Retryable: true,
			Details:   map[string]any{},
		},
	}
}

type invalidJSONProvider struct{}

func (invalidJSONProvider) Invoke(ctx context.Context, reservation InvocationReservation, request InvokeRequest) (ProviderInvokeResult, error) {
	_ = ctx
	_ = reservation
	_ = request
	return ProviderInvokeResult{}, &ProviderInvocationError{
		GatewayError: GatewayError{
			Code:      "provider_invalid_json",
			Message:   "provider returned invalid json",
			Retryable: false,
			Details:   map[string]any{},
		},
	}
}

type providerOutcome struct {
	result ProviderInvokeResult
	err    error
}

type scriptedProvider struct {
	mu       sync.Mutex
	outcomes []providerOutcome
}

func (provider *scriptedProvider) Invoke(ctx context.Context, reservation InvocationReservation, request InvokeRequest) (ProviderInvokeResult, error) {
	_ = ctx
	_ = reservation
	_ = request

	provider.mu.Lock()
	defer provider.mu.Unlock()

	if len(provider.outcomes) == 0 {
		return ProviderInvokeResult{}, errors.New("unexpected invoke after scripted outcomes exhausted")
	}
	outcome := provider.outcomes[0]
	provider.outcomes = provider.outcomes[1:]
	return outcome.result, outcome.err
}

func successfulProviderResult(text string) ProviderInvokeResult {
	prompt := 1
	completion := 2
	total := 3
	return ProviderInvokeResult{
		OutputText: &text,
		Response: map[string]any{
			"id":     "resp-test",
			"object": "response",
			"model":  "gpt-4.1",
			"status": "completed",
			"output": []any{
				map[string]any{
					"type":   "message",
					"role":   "assistant",
					"status": "completed",
					"content": []any{
						map[string]any{
							"type": "output_text",
							"text": text,
						},
					},
				},
			},
		},
		Usage: &UsageMetrics{
			InputTokens:  &prompt,
			OutputTokens: &completion,
			TotalTokens:  &total,
		},
	}
}

func writeModelConfig(t *testing.T, path string, threshold int) {
	t.Helper()
	writeModelConfigWithRetry(t, path, threshold, nil, nil)
}

func writeModelConfigWithRetry(
	t *testing.T,
	path string,
	threshold int,
	maxAttempts *int,
	baseBackoffMS *int,
) {
	t.Helper()

	payload := map[string]any{
		"global_max_concurrent":     2,
		"offline_failure_threshold": threshold,
		"apis": []map[string]any{
			{
				"api_id":         "api-1",
				"model":          "gpt-4.1",
				"base_url":       "http://unused.local/v1",
				"api_key":        "sk-test-1",
				"max_concurrent": 1,
				"enabled":        true,
			},
			{
				"api_id":         "api-2",
				"model":          "gpt-4.1-mini",
				"base_url":       "http://unused.local/v1",
				"api_key":        "sk-test-2",
				"max_concurrent": 1,
				"enabled":        true,
			},
		},
	}
	if maxAttempts != nil || baseBackoffMS != nil {
		retry := map[string]any{}
		if maxAttempts != nil {
			retry["max_attempts"] = *maxAttempts
		}
		if baseBackoffMS != nil {
			retry["base_backoff_ms"] = *baseBackoffMS
		}
		payload["retry"] = retry
	}

	content, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
