package poolgateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleProviderClassifiesRateLimitAsRetryable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"message":"slow down"}}`))
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	_, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "gpt-4.1",
	}, InvokeRequest{
		Request: OpenAIResponsesRequest{"input": "hello"},
	})
	if err == nil {
		t.Fatalf("expected provider error")
	}
	providerErr, ok := err.(*ProviderInvocationError)
	if !ok {
		t.Fatalf("expected ProviderInvocationError, got %T", err)
	}
	if providerErr.GatewayError.Code != "provider_rate_limited" {
		t.Fatalf("unexpected code: %s", providerErr.GatewayError.Code)
	}
	if !providerErr.GatewayError.Retryable {
		t.Fatalf("expected retryable error")
	}
}

func TestOpenAICompatibleProviderClassifiesInvalidJSONAsNonRetryable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte(`not-json`))
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	_, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "gpt-4.1",
	}, InvokeRequest{
		Request: OpenAIResponsesRequest{"input": "hello"},
	})
	if err == nil {
		t.Fatalf("expected provider error")
	}
	providerErr, ok := err.(*ProviderInvocationError)
	if !ok {
		t.Fatalf("expected ProviderInvocationError, got %T", err)
	}
	if providerErr.GatewayError.Code != "provider_invalid_json" {
		t.Fatalf("unexpected code: %s", providerErr.GatewayError.Code)
	}
	if providerErr.GatewayError.Retryable {
		t.Fatalf("expected non-retryable error")
	}
}

func TestOpenAICompatibleProviderUsesResponsesPayloadAndDefaults(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			http.NotFound(writer, request)
			return
		}
		if got := request.Header.Get("X-Test"); got != "configured" {
			t.Fatalf("unexpected header: %q", got)
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "gpt-4.1" {
			t.Fatalf("unexpected model: %+v", payload["model"])
		}
		if payload["instructions"] != "configured-default" {
			t.Fatalf("unexpected instructions: %+v", payload["instructions"])
		}
		if payload["input"] != "hello" {
			t.Fatalf("unexpected input: %+v", payload["input"])
		}
		usage := `{"input_tokens":2,"input_tokens_details":{"cached_tokens":1},"output_tokens":3,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":5}`
		_, _ = writer.Write([]byte(`{"id":"resp-1","object":"response","model":"gpt-4.1","status":"completed","output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"ok"}]}],"usage":` + usage + `}`))
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	result, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "gpt-4.1",
		Headers: map[string]string{
			"X-Test": "configured",
		},
		RequestDefaults: map[string]any{
			"instructions": "configured-default",
		},
	}, InvokeRequest{
		Request: OpenAIResponsesRequest{"input": "hello"},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.OutputText == nil || *result.OutputText != "ok" {
		t.Fatalf("unexpected output: %+v", result.OutputText)
	}
	if result.Usage == nil || result.Usage.CachedInputTokens == nil || *result.Usage.CachedInputTokens != 1 {
		t.Fatalf("unexpected cached input tokens: %+v", result.Usage)
	}
	if result.Usage.ReasoningTokens == nil || *result.Usage.ReasoningTokens != 1 {
		t.Fatalf("unexpected reasoning tokens: %+v", result.Usage)
	}
}
