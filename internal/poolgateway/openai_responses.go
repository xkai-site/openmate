package poolgateway

import (
	"errors"
	"fmt"
	"strings"
)

func normalizeRequest(request InvokeRequest) InvokeRequest {
	if request.Request == nil {
		request.Request = OpenAIResponsesRequest{}
	}
	return request
}

func validateInvokeRequest(request InvokeRequest) error {
	if strings.TrimSpace(request.RequestID) == "" {
		return errors.New("request_id is required")
	}
	if strings.TrimSpace(request.NodeID) == "" {
		return errors.New("node_id is required")
	}
	if len(request.Request) == 0 {
		return errors.New("request is required")
	}
	input, exists := request.Request["input"]
	if !exists || input == nil {
		return errors.New("request.input is required")
	}
	if _, exists := request.Request["model"]; exists {
		return errors.New("request.model must not be set; model comes from model.json")
	}
	legacyChatFields := []string{
		"messages",
		"functions",
		"function_call",
		"tool_calls",
		"max_tokens",
	}
	for _, field := range legacyChatFields {
		if _, exists := request.Request[field]; exists {
			return fmt.Errorf("request.%s is ChatCompletions-only and is not supported; use Responses API fields", field)
		}
	}
	if stream, ok := request.Request["stream"].(bool); ok && stream {
		return errors.New("request.stream is not supported yet")
	}
	return nil
}

func mergeRequestPayload(defaults map[string]any, request OpenAIResponsesRequest, model string) map[string]any {
	merged := cloneMap(defaults)
	merged = mergeMaps(merged, map[string]any(request))
	merged["model"] = model
	return merged
}

func cloneMap(source map[string]any) map[string]any {
	if len(source) == 0 {
		return map[string]any{}
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			result[key] = cloneMap(typed)
		case []any:
			result[key] = cloneSlice(typed)
		default:
			result[key] = typed
		}
	}
	return result
}

func cloneSlice(source []any) []any {
	if len(source) == 0 {
		return []any{}
	}
	result := make([]any, 0, len(source))
	for _, value := range source {
		switch typed := value.(type) {
		case map[string]any:
			result = append(result, cloneMap(typed))
		case []any:
			result = append(result, cloneSlice(typed))
		default:
			result = append(result, typed)
		}
	}
	return result
}

func mergeMaps(base map[string]any, override map[string]any) map[string]any {
	if len(base) == 0 {
		return cloneMap(override)
	}
	if len(override) == 0 {
		return base
	}
	for key, value := range override {
		existing, exists := base[key]
		overrideMap, overrideIsMap := value.(map[string]any)
		existingMap, existingIsMap := existing.(map[string]any)
		if exists && existingIsMap && overrideIsMap {
			base[key] = mergeMaps(existingMap, overrideMap)
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			base[key] = cloneMap(typed)
		case []any:
			base[key] = cloneSlice(typed)
		default:
			base[key] = typed
		}
	}
	return base
}

func gatewayUnsupportedRequest(field string) GatewayError {
	return GatewayError{
		Code:      "gateway_unsupported_request",
		Message:   fmt.Sprintf("%s is not supported yet", field),
		Retryable: false,
		Details: map[string]any{
			"field": field,
		},
	}
}
