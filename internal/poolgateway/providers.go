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
	payload := mergeRequestPayload(reservation.RequestDefaults, request.Request, reservation.Model)
	if stream, ok := payload["stream"].(bool); ok && stream {
		return ProviderInvokeResult{}, &ProviderInvocationError{
			GatewayError: gatewayUnsupportedRequest("request.stream"),
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ProviderInvokeResult{}, err
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(reservation.BaseURL, "/")+"/responses",
		bytes.NewReader(body),
	)
	if err != nil {
		return ProviderInvokeResult{}, err
	}
	httpRequest.Header.Set("Authorization", "Bearer "+reservation.APIKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	for key, value := range reservation.Headers {
		httpRequest.Header.Set(key, value)
	}

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

	if gatewayError := classifyProviderPayload(payloadJSON); gatewayError != nil {
		return ProviderInvokeResult{}, &ProviderInvocationError{GatewayError: *gatewayError}
	}

	return ProviderInvokeResult{
		Response:   payloadJSON,
		OutputText: extractOutputText(payloadJSON),
		Usage:      extractUsage(payloadJSON),
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

func classifyProviderPayload(payload map[string]any) *GatewayError {
	if errorRaw, ok := payload["error"].(map[string]any); ok && len(errorRaw) > 0 {
		message := anyString(errorRaw["message"])
		if message == "" {
			message = "provider returned error payload"
		}
		return &GatewayError{
			Code:      "provider_response_error",
			Message:   message,
			Retryable: false,
			Details: map[string]any{
				"error": errorRaw,
			},
		}
	}

	status, _ := payload["status"].(string)
	if status == "incomplete" {
		details, _ := payload["incomplete_details"].(map[string]any)
		reason := ""
		if details != nil {
			reason = anyString(details["reason"])
		}
		message := "provider response incomplete"
		if reason != "" {
			message = fmt.Sprintf("provider response incomplete: %s", reason)
		}
		return &GatewayError{
			Code:      "provider_incomplete_response",
			Message:   message,
			Retryable: false,
			Details: map[string]any{
				"status":             status,
				"incomplete_details": details,
			},
		}
	}

	return nil
}

func extractOutputText(payload map[string]any) *string {
	outputRaw, ok := payload["output"].([]any)
	if !ok || len(outputRaw) == 0 {
		return nil
	}
	builder := strings.Builder{}
	for _, item := range outputRaw {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if anyString(message["type"]) != "message" {
			continue
		}
		contentRaw, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for _, contentItem := range contentRaw {
			content, ok := contentItem.(map[string]any)
			if !ok {
				continue
			}
			switch anyString(content["type"]) {
			case "output_text", "text":
				if text := anyString(content["text"]); text != "" {
					builder.WriteString(text)
				}
			}
		}
	}
	if builder.Len() == 0 {
		return nil
	}
	result := builder.String()
	return &result
}

func extractUsage(payload map[string]any) *UsageMetrics {
	usageRaw, ok := payload["usage"].(map[string]any)
	if !ok {
		return nil
	}
	inputDetails, _ := usageRaw["input_tokens_details"].(map[string]any)
	outputDetails, _ := usageRaw["output_tokens_details"].(map[string]any)
	return &UsageMetrics{
		InputTokens:       anyIntPointer(usageRaw["input_tokens"]),
		OutputTokens:      anyIntPointer(usageRaw["output_tokens"]),
		TotalTokens:       anyIntPointer(usageRaw["total_tokens"]),
		CachedInputTokens: anyIntPointer(mapValue(inputDetails, "cached_tokens")),
		ReasoningTokens:   anyIntPointer(mapValue(outputDetails, "reasoning_tokens")),
	}
}

func anyIntPointer(value any) *int {
	switch typed := value.(type) {
	case float64:
		result := int(typed)
		return &result
	case float32:
		result := int(typed)
		return &result
	case int:
		result := typed
		return &result
	case int64:
		result := int(typed)
		return &result
	default:
		return nil
	}
}

func anyString(value any) string {
	typed, ok := value.(string)
	if !ok {
		return ""
	}
	return typed
}

func mapValue(value map[string]any, key string) any {
	if value == nil {
		return nil
	}
	return value[key]
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
