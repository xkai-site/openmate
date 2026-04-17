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
	switch normalizeAPIMode(reservation.APIMode) {
	case APIModeChatCompletions:
		return provider.invokeChatCompletions(ctx, reservation, request)
	case APIModeResponses:
		fallthrough
	default:
		return provider.invokeResponses(ctx, reservation, request)
	}
}

func (provider OpenAICompatibleProvider) invokeResponses(
	ctx context.Context,
	reservation InvocationReservation,
	request InvokeRequest,
) (ProviderInvokeResult, error) {
	payload := mergeRequestPayload(reservation.RequestDefaults, request.Request, reservation.Model)
	// Compatibility guard: some providers reject these Responses fields.
	delete(payload, "metadata")
	delete(payload, "truncation")
	delete(payload, "user")
	payload["input"] = normalizeResponsesInput(payload["input"])
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
			GatewayError: classifyHTTPError(response.StatusCode, responseBody, httpRequest.URL.Path),
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

func (provider OpenAICompatibleProvider) invokeChatCompletions(
	ctx context.Context,
	reservation InvocationReservation,
	request InvokeRequest,
) (ProviderInvokeResult, error) {
	chatPayload, gatewayError := buildChatCompletionsPayloadForInvoke(reservation, request)
	if gatewayError != nil {
		return ProviderInvokeResult{}, &ProviderInvocationError{GatewayError: *gatewayError}
	}

	body, err := json.Marshal(chatPayload)
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
			GatewayError: classifyHTTPError(response.StatusCode, responseBody, httpRequest.URL.Path),
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

	if gatewayError := classifyChatCompletionsPayload(payloadJSON); gatewayError != nil {
		return ProviderInvokeResult{}, &ProviderInvocationError{GatewayError: *gatewayError}
	}

	normalized, err := normalizeChatCompletionsResponse(payloadJSON)
	if err != nil {
		return ProviderInvokeResult{}, &ProviderInvocationError{
			GatewayError: GatewayError{
				Code:      "provider_invalid_json",
				Message:   fmt.Sprintf("provider returned unexpected chat completion payload: %v", err),
				Retryable: false,
				Details:   map[string]any{},
			},
		}
	}

	return ProviderInvokeResult{
		Response:   normalized,
		OutputText: extractOutputText(normalized),
		Usage:      extractUsage(normalized),
	}, nil
}

func buildChatCompletionsPayloadForInvoke(
	reservation InvocationReservation,
	request InvokeRequest,
) (map[string]any, *GatewayError) {
	if len(request.ChatRequest) > 0 {
		chatRequestPayload := mergeChatCompletionsPayload(reservation.RequestDefaults, request.ChatRequest, reservation.Model)
		return buildChatCompletionsPayload(chatRequestPayload)
	}
	if len(request.Request) > 0 {
		responsesPayload := mergeRequestPayload(reservation.RequestDefaults, request.Request, reservation.Model)
		return buildChatCompletionsPayloadFromResponsesRequest(responsesPayload)
	}
	gatewayError := gatewayUnsupportedRequest("request")
	gatewayError.Message = "either request or chat_request is required for chat_completions mode"
	return nil, &gatewayError
}

func buildChatCompletionsPayload(chatPayload map[string]any) (map[string]any, *GatewayError) {
	messages, ok := chatPayload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return nil, &GatewayError{
			Code:      "gateway_unsupported_request",
			Message:   "chat_request.messages is required for chat_completions mode",
			Retryable: false,
			Details: map[string]any{
				"field": "chat_request.messages",
			},
		}
	}
	if stream, ok := chatPayload["stream"].(bool); ok && stream {
		return nil, &GatewayError{
			Code:      "gateway_unsupported_request",
			Message:   "chat_request.stream is not supported yet",
			Retryable: false,
			Details: map[string]any{
				"field": "chat_request.stream",
			},
		}
	}
	for key := range chatPayload {
		if !isSupportedChatCompletionsField(key) {
			return nil, &GatewayError{
				Code:      "gateway_unsupported_request",
				Message:   fmt.Sprintf("chat_request.%s is not supported", key),
				Retryable: false,
				Details: map[string]any{
					"field": "chat_request." + key,
				},
			}
		}
	}

	payload := map[string]any{
		"model":    chatPayload["model"],
		"messages": messages,
	}
	if tools, exists := chatPayload["tools"]; exists && tools != nil {
		payload["tools"] = tools
	}
	if toolChoice, exists := chatPayload["tool_choice"]; exists && toolChoice != nil {
		payload["tool_choice"] = toolChoice
	}
	if responseFormat, exists := chatPayload["response_format"]; exists && responseFormat != nil {
		payload["response_format"] = responseFormat
	}
	if temperature, exists := chatPayload["temperature"]; exists && temperature != nil {
		payload["temperature"] = temperature
	}
	if topP, exists := chatPayload["top_p"]; exists && topP != nil {
		payload["top_p"] = topP
	}
	if maxTokens, exists := chatPayload["max_tokens"]; exists && maxTokens != nil {
		payload["max_tokens"] = maxTokens
	}
	if store, exists := chatPayload["store"]; exists && store != nil {
		payload["store"] = store
	}
	return payload, nil
}

func buildChatCompletionsPayloadFromResponsesRequest(responsesPayload map[string]any) (map[string]any, *GatewayError) {
	messages := convertInputToChatMessages(responsesPayload["input"])
	if instructions := anyString(responsesPayload["instructions"]); instructions != "" {
		messages = append(
			[]map[string]any{
				{
					"role":    "system",
					"content": instructions,
				},
			},
			messages...,
		)
	}
	if len(messages) == 0 {
		return nil, &GatewayError{
			Code:      "gateway_unsupported_request",
			Message:   "request.input is empty after conversion for chat_completions mode",
			Retryable: false,
			Details: map[string]any{
				"field": "request.input",
			},
		}
	}

	payload := map[string]any{
		"model":    responsesPayload["model"],
		"messages": normalizeChatMessages(messages),
	}
	if tools, exists := responsesPayload["tools"]; exists && tools != nil {
		payload["tools"] = normalizeResponsesToolsForChat(tools)
	}
	if toolChoice, exists := responsesPayload["tool_choice"]; exists && toolChoice != nil {
		payload["tool_choice"] = toolChoice
	}
	if temperature, exists := responsesPayload["temperature"]; exists && temperature != nil {
		payload["temperature"] = temperature
	}
	if topP, exists := responsesPayload["top_p"]; exists && topP != nil {
		payload["top_p"] = topP
	}
	if maxOutputTokens, exists := responsesPayload["max_output_tokens"]; exists && maxOutputTokens != nil {
		payload["max_tokens"] = maxOutputTokens
	}
	if store, exists := responsesPayload["store"]; exists && store != nil {
		payload["store"] = store
	}
	return payload, nil
}

func normalizeResponsesToolsForChat(rawTools any) any {
	tools, ok := rawTools.([]any)
	if !ok {
		return rawTools
	}
	normalized := make([]any, 0, len(tools))
	for _, toolRaw := range tools {
		tool, ok := toolRaw.(map[string]any)
		if !ok {
			normalized = append(normalized, toolRaw)
			continue
		}
		if anyString(tool["type"]) != "function" {
			normalized = append(normalized, tool)
			continue
		}
		if _, hasFunction := tool["function"]; hasFunction {
			normalized = append(normalized, tool)
			continue
		}
		functionDef := map[string]any{
			"name":        tool["name"],
			"description": tool["description"],
			"parameters":  tool["parameters"],
		}
		normalized = append(normalized, map[string]any{
			"type":     "function",
			"function": functionDef,
		})
	}
	return normalized
}

func normalizeChatMessages(messages []map[string]any) []any {
	normalized := make([]any, 0, len(messages))
	for _, message := range messages {
		normalized = append(normalized, message)
	}
	return normalized
}

func isSupportedChatCompletionsField(field string) bool {
	switch field {
	case "model", "messages", "tools", "tool_choice", "response_format", "temperature", "top_p", "max_tokens", "user", "store", "stream":
		return true
	default:
		return false
	}
}

func containsFunctionCallOutput(input any) bool {
	inputItems, ok := input.([]any)
	if !ok {
		return false
	}
	for _, item := range inputItems {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if anyString(itemMap["type"]) == "function_call_output" {
			return true
		}
	}
	return false
}

func normalizeResponsesInput(input any) []any {
	switch typed := input.(type) {
	case nil:
		return []any{}
	case []any:
		return typed
	case []map[string]any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		return items
	case string:
		if strings.TrimSpace(typed) == "" {
			return []any{}
		}
		return []any{
			map[string]any{
				"role":    "user",
				"content": typed,
			},
		}
	case map[string]any:
		return []any{typed}
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return []any{}
		}
		return []any{
			map[string]any{
				"role":    "user",
				"content": string(raw),
			},
		}
	}
}

func convertInputToChatMessages(input any) []map[string]any {
	switch typed := input.(type) {
	case string:
		if strings.TrimSpace(typed) == "" {
			return []map[string]any{}
		}
		return []map[string]any{
			{
				"role":    "user",
				"content": typed,
			},
		}
	case []any:
		messages := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			role := anyString(itemMap["role"])
			content := chatMessageContent(itemMap["content"])
			if role != "" && content != "" {
				messages = append(messages, map[string]any{
					"role":    role,
					"content": content,
				})
				continue
			}
			if text := anyString(itemMap["text"]); text != "" {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": text,
				})
				continue
			}
			if raw, err := json.Marshal(itemMap); err == nil {
				messages = append(messages, map[string]any{
					"role":    "user",
					"content": string(raw),
				})
			}
		}
		return messages
	case map[string]any:
		role := anyString(typed["role"])
		content := chatMessageContent(typed["content"])
		if role != "" && content != "" {
			return []map[string]any{
				{
					"role":    role,
					"content": content,
				},
			}
		}
		if raw, err := json.Marshal(typed); err == nil {
			return []map[string]any{
				{
					"role":    "user",
					"content": string(raw),
				},
			}
		}
	}
	return []map[string]any{}
}

func chatMessageContent(content any) string {
	switch typed := content.(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		builder := strings.Builder{}
		for _, item := range typed {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if anyString(itemMap["type"]) == "text" || anyString(itemMap["type"]) == "output_text" {
				text := anyString(itemMap["text"])
				if text != "" {
					builder.WriteString(text)
				}
			}
		}
		return strings.TrimSpace(builder.String())
	default:
		return ""
	}
}

func classifyChatCompletionsPayload(payload map[string]any) *GatewayError {
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
	return nil
}

func normalizeChatCompletionsResponse(payload map[string]any) (map[string]any, error) {
	choicesRaw, ok := payload["choices"].([]any)
	if !ok || len(choicesRaw) == 0 {
		return nil, fmt.Errorf("choices is required")
	}
	firstChoice, ok := choicesRaw[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("choices[0] is invalid")
	}
	messageRaw, ok := firstChoice["message"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("choices[0].message is required")
	}
	content := chatMessageContent(messageRaw["content"])

	output := make([]any, 0, 1)
	if content != "" {
		output = append(output, map[string]any{
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []any{
				map[string]any{
					"type": "output_text",
					"text": content,
				},
			},
		})
	}
	output = append(output, normalizeChatToolCalls(messageRaw["tool_calls"])...)

	usage := map[string]any{}
	if usageRaw, ok := payload["usage"].(map[string]any); ok {
		usage["input_tokens"] = usageRaw["prompt_tokens"]
		usage["output_tokens"] = usageRaw["completion_tokens"]
		usage["total_tokens"] = usageRaw["total_tokens"]
	}

	normalized := map[string]any{
		"id":     payload["id"],
		"object": "response",
		"model":  payload["model"],
		"status": "completed",
		"output": output,
		"usage":  usage,
	}
	return normalized, nil
}

func normalizeChatToolCalls(raw any) []any {
	toolCalls, ok := raw.([]any)
	if !ok || len(toolCalls) == 0 {
		return []any{}
	}
	items := make([]any, 0, len(toolCalls))
	for index, toolCallRaw := range toolCalls {
		toolCall, ok := toolCallRaw.(map[string]any)
		if !ok {
			continue
		}
		functionRaw, ok := toolCall["function"].(map[string]any)
		if !ok {
			continue
		}
		name := anyString(functionRaw["name"])
		if name == "" {
			continue
		}
		callID := anyString(toolCall["id"])
		if callID == "" {
			callID = fmt.Sprintf("chat_tool_call_%d", index+1)
		}
		arguments := functionRaw["arguments"]
		if arguments == nil {
			arguments = "{}"
		}
		items = append(items, map[string]any{
			"type":      "function_call",
			"id":        callID,
			"call_id":   callID,
			"name":      name,
			"arguments": arguments,
		})
	}
	return items
}

func GetProviderClient(provider string) (ProviderClient, error) {
	_ = provider
	// provider is currently treated as an observability label.
	// Runtime invocation uses the unified OpenAI-compatible transport path.
	return OpenAICompatibleProvider{}, nil
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

func classifyHTTPError(statusCode int, responseBody []byte, requestPath string) GatewayError {
	code := "provider_http_error"
	retryable := false
	message := fmt.Sprintf("provider returned HTTP %d", statusCode)
	switch {
	case statusCode == http.StatusTooManyRequests:
		code = "provider_rate_limited"
		retryable = true
	case statusCode == http.StatusRequestTimeout:
		code = "provider_timeout"
		retryable = true
	case statusCode == http.StatusNotFound && strings.HasSuffix(strings.TrimSpace(requestPath), "/responses"):
		message = "provider returned HTTP 404 on /responses; endpoint may only support chat/completions"
	case statusCode >= 500:
		// Fast-fail on upstream 5xx to avoid duplicate billable calls.
		retryable = false
	}

	return GatewayError{
		Code:               code,
		Message:            message,
		Retryable:          retryable,
		ProviderStatusCode: &statusCode,
		Details: map[string]any{
			"path": requestPath,
			"body": string(responseBody),
		},
	}
}
