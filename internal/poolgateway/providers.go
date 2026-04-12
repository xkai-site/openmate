package poolgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type ProviderClient interface {
	Invoke(ctx context.Context, reservation InvocationReservation, request InvokeRequest) (ProviderInvokeResult, error)
}

type OpenAICompatibleProvider struct {
	HTTPClient *http.Client
}

func (provider OpenAICompatibleProvider) Invoke(
	ctx context.Context,
	reservation InvocationReservation,
	request InvokeRequest,
) (ProviderInvokeResult, error) {
	payload := map[string]any{
		"model":    reservation.Model,
		"messages": buildProviderMessages(request.Messages),
	}
	if request.Temperature != nil {
		payload["temperature"] = *request.Temperature
	}
	if request.MaxOutputTokens != nil {
		payload["max_completion_tokens"] = *request.MaxOutputTokens
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ProviderInvokeResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(reservation.BaseURL, "/")+"/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return ProviderInvokeResult{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+reservation.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")

	client := provider.HTTPClient
	if client == nil {
		timeout := 30 * time.Second
		if request.TimeoutMS != nil {
			timeout = time.Duration(*request.TimeoutMS) * time.Millisecond
		}
		client = &http.Client{Timeout: timeout}
	}

	response, err := client.Do(httpRequest)
	if err != nil {
		return ProviderInvokeResult{}, &ProviderInvocationError{
			GatewayError: classifyTransportError(err),
		}
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return ProviderInvokeResult{}, err
	}
	if response.StatusCode >= 400 {
		return ProviderInvokeResult{}, &ProviderInvocationError{
			GatewayError: classifyHTTPError(response.StatusCode, responseBody),
		}
	}

	var payloadJSON map[string]any
	if err := json.Unmarshal(responseBody, &payloadJSON); err != nil {
		return ProviderInvokeResult{}, &ProviderInvocationError{
			GatewayError: GatewayError{
				Code:      "provider_invalid_json",
				Message:   fmt.Sprintf("provider returned invalid json: %v", err),
				Retryable: false,
				Details:   map[string]any{},
			},
		}
	}

	return ProviderInvokeResult{
		OutputText:  extractOutputText(payloadJSON),
		RawResponse: payloadJSON,
		Usage:       extractUsage(payloadJSON),
	}, nil
}

func GetProviderClient(provider string) (ProviderClient, error) {
	switch ProviderKind(provider) {
	case ProviderKindOpenAICompatible:
		return OpenAICompatibleProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

func buildProviderMessages(messages []LlmMessage) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		result = append(result, map[string]any{
			"role":    string(message.Role),
			"content": message.Content,
		})
	}
	return result
}

func extractOutputText(payload map[string]any) *string {
	choicesRaw, ok := payload["choices"].([]any)
	if !ok || len(choicesRaw) == 0 {
		return nil
	}
	firstChoice, ok := choicesRaw[0].(map[string]any)
	if !ok {
		return nil
	}
	message, ok := firstChoice["message"].(map[string]any)
	if !ok {
		return nil
	}
	content, exists := message["content"]
	if !exists {
		return nil
	}
	switch value := content.(type) {
	case string:
		result := value
		return &result
	case []any:
		builder := strings.Builder{}
		for _, item := range value {
			part, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if part["type"] == "text" {
				if text, ok := part["text"].(string); ok {
					builder.WriteString(text)
				}
			}
		}
		if builder.Len() == 0 {
			return nil
		}
		result := builder.String()
		return &result
	default:
		return nil
	}
}

func extractUsage(payload map[string]any) *UsageMetrics {
	usageRaw, ok := payload["usage"].(map[string]any)
	if !ok {
		return nil
	}
	return &UsageMetrics{
		PromptTokens:     anyIntPointer(usageRaw["prompt_tokens"]),
		CompletionTokens: anyIntPointer(usageRaw["completion_tokens"]),
		TotalTokens:      anyIntPointer(usageRaw["total_tokens"]),
	}
}

func anyIntPointer(value any) *int {
	switch typed := value.(type) {
	case float64:
		result := int(typed)
		return &result
	case int:
		result := typed
		return &result
	default:
		return nil
	}
}

func classifyTransportError(err error) GatewayError {
	if errors.Is(err, context.DeadlineExceeded) {
		return GatewayError{
			Code:      "provider_timeout",
			Message:   err.Error(),
			Retryable: true,
			Details:   map[string]any{},
		}
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return GatewayError{
			Code:      "provider_timeout",
			Message:   err.Error(),
			Retryable: true,
			Details:   map[string]any{},
		}
	}

	return GatewayError{
		Code:      "provider_unreachable",
		Message:   err.Error(),
		Retryable: true,
		Details:   map[string]any{},
	}
}

func classifyHTTPError(statusCode int, responseBody []byte) GatewayError {
	code := "provider_http_error"
	retryable := false
	switch {
	case statusCode == http.StatusTooManyRequests:
		code = "provider_rate_limited"
		retryable = true
	case statusCode == http.StatusRequestTimeout:
		code = "provider_timeout"
		retryable = true
	case statusCode >= 500:
		retryable = true
	}

	return GatewayError{
		Code:               code,
		Message:            fmt.Sprintf("provider returned HTTP %d", statusCode),
		Retryable:          retryable,
		ProviderStatusCode: &statusCode,
		Details: map[string]any{
			"body": string(responseBody),
		},
	}
}
