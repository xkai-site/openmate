package poolgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAICompatibleProviderClassifiesRateLimitAsRetryable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
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
		Messages: []LlmMessage{{Role: MessageRoleUser, Content: "hello"}},
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
		if request.URL.Path != "/v1/chat/completions" {
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
		Messages: []LlmMessage{{Role: MessageRoleUser, Content: "hello"}},
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
