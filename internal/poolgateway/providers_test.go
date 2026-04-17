package poolgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestOpenAICompatibleProviderClassifies5xxAsNonRetryable(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`upstream unavailable`))
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
	if providerErr.GatewayError.Code != "provider_http_error" {
		t.Fatalf("unexpected code: %s", providerErr.GatewayError.Code)
	}
	if providerErr.GatewayError.ProviderStatusCode == nil || *providerErr.GatewayError.ProviderStatusCode != http.StatusBadGateway {
		t.Fatalf("unexpected provider status code: %+v", providerErr.GatewayError.ProviderStatusCode)
	}
	if providerErr.GatewayError.Retryable {
		t.Fatalf("expected 5xx to be non-retryable")
	}
}

func TestOpenAICompatibleProviderUsesChatCompletionsWhenAPIModeConfigured(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, exists := payload["input"]; exists {
			t.Fatalf("chat completions payload must not contain input: %+v", payload)
		}
		if payload["model"] != "gpt-4.1" {
			t.Fatalf("unexpected model: %+v", payload["model"])
		}
		if payload["tool_choice"] != "auto" {
			t.Fatalf("expected tool_choice=auto: %+v", payload["tool_choice"])
		}
		if _, exists := payload["user"]; exists {
			t.Fatalf("user must be stripped before provider call: %+v", payload["user"])
		}
		tools, ok := payload["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools: %+v", payload["tools"])
		}
		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Fatalf("unexpected messages: %+v", payload["messages"])
		}
		firstMessage, ok := messages[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected first message: %+v", messages[0])
		}
		if firstMessage["role"] != "user" || firstMessage["content"] != "你好" {
			t.Fatalf("unexpected first message payload: %+v", firstMessage)
		}

		_, _ = writer.Write([]byte(`{
			"id":"chatcmpl-1",
			"object":"chat.completion",
			"model":"gpt-4.1",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok from chat"}}],
			"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}
		}`))
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	result, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		APIMode: APIModeChatCompletions,
		Model:   "gpt-4.1",
	}, InvokeRequest{
		ChatRequest: OpenAIChatCompletionsRequest{
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "你好",
				},
			},
			"tool_choice": "auto",
			"user":        "end-user-1",
			"tools": []any{
				map[string]any{"type": "function", "name": "read"},
			},
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.OutputText == nil || *result.OutputText != "ok from chat" {
		t.Fatalf("unexpected output: %+v", result.OutputText)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 2 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestOpenAICompatibleProviderUsesResponsesPayloadWhenChatModeConfigured(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if payload["model"] != "gpt-4.1" {
			t.Fatalf("unexpected model: %+v", payload["model"])
		}
		messages, ok := payload["messages"].([]any)
		if !ok || len(messages) != 2 {
			t.Fatalf("unexpected messages: %+v", payload["messages"])
		}
		systemMessage, ok := messages[0].(map[string]any)
		if !ok || systemMessage["role"] != "system" || systemMessage["content"] != "system instruction" {
			t.Fatalf("unexpected system message payload: %+v", messages[0])
		}
		userMessage, ok := messages[1].(map[string]any)
		if !ok || userMessage["role"] != "user" || userMessage["content"] != "你好" {
			t.Fatalf("unexpected user message payload: %+v", messages[1])
		}
		tools, ok := payload["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Fatalf("unexpected tools payload: %+v", payload["tools"])
		}
		tool, ok := tools[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected tool payload: %+v", tools[0])
		}
		if tool["type"] != "function" {
			t.Fatalf("unexpected tool type: %+v", tool["type"])
		}
		functionDef, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected function payload: %+v", tool["function"])
		}
		if functionDef["name"] != "read" {
			t.Fatalf("unexpected function name: %+v", functionDef["name"])
		}
		if payload["max_tokens"] != float64(32) {
			t.Fatalf("unexpected max_tokens: %+v", payload["max_tokens"])
		}
		if _, exists := payload["user"]; exists {
			t.Fatalf("user must be stripped before provider call: %+v", payload["user"])
		}

		_, _ = writer.Write([]byte(`{
			"id":"chatcmpl-2",
			"object":"chat.completion",
			"model":"gpt-4.1",
			"choices":[{"index":0,"message":{"role":"assistant","content":"ok from responses payload"}}],
			"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}
		}`))
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	result, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		APIMode: APIModeChatCompletions,
		Model:   "gpt-4.1",
	}, InvokeRequest{
		Request: OpenAIResponsesRequest{
			"input":        "你好",
			"instructions": "system instruction",
			"tools": []any{
				map[string]any{
					"type":        "function",
					"name":        "read",
					"description": "read file",
					"parameters": map[string]any{
						"type": "object",
					},
				},
			},
			"max_output_tokens": 32,
			"user":              "end-user-2",
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.OutputText == nil || *result.OutputText != "ok from responses payload" {
		t.Fatalf("unexpected output: %+v", result.OutputText)
	}
}

func TestOpenAICompatibleProviderRejectsUnsupportedFieldInChatCompletionsMode(t *testing.T) {
	t.Parallel()
	provider := OpenAICompatibleProvider{}
	_, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: "http://unused.local/v1",
		APIKey:  "sk-test",
		APIMode: APIModeChatCompletions,
		Model:   "gpt-4.1",
	}, InvokeRequest{
		ChatRequest: OpenAIChatCompletionsRequest{
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "hello",
				},
			},
			"metadata": map[string]any{
				"trace_id": "abc",
			},
		},
	})
	if err == nil {
		t.Fatalf("expected provider error")
	}
	providerErr, ok := err.(*ProviderInvocationError)
	if !ok {
		t.Fatalf("expected ProviderInvocationError, got %T", err)
	}
	if providerErr.GatewayError.Code != "gateway_unsupported_request" {
		t.Fatalf("unexpected code: %s", providerErr.GatewayError.Code)
	}
	if !strings.Contains(providerErr.GatewayError.Message, "chat_request.metadata") {
		t.Fatalf("unexpected message: %s", providerErr.GatewayError.Message)
	}
}

func TestOpenAICompatibleProviderNormalizesChatToolCalls(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		_, _ = writer.Write([]byte(`{
			"id":"chatcmpl-tool-1",
			"object":"chat.completion",
			"model":"gpt-4.1",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[
						{
							"id":"call-1",
							"type":"function",
							"function":{
								"name":"get_weather",
								"arguments":"{\"city\":\"Beijing\"}"
							}
						}
					]
				}
			}],
			"usage":{"prompt_tokens":2,"completion_tokens":3,"total_tokens":5}
		}`))
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	result, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		APIMode: APIModeChatCompletions,
		Model:   "gpt-4.1",
	}, InvokeRequest{
		ChatRequest: OpenAIChatCompletionsRequest{
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "北京天气",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.OutputText != nil {
		t.Fatalf("expected nil output_text for tool call only response: %+v", result.OutputText)
	}
	items, ok := result.Response["output"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected output items: %+v", result.Response["output"])
	}
	functionCall, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected function_call payload: %+v", items[0])
	}
	if functionCall["type"] != "function_call" {
		t.Fatalf("unexpected output type: %+v", functionCall["type"])
	}
	if functionCall["call_id"] != "call-1" || functionCall["name"] != "get_weather" {
		t.Fatalf("unexpected function_call payload: %+v", functionCall)
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
		if _, exists := payload["metadata"]; exists {
			t.Fatalf("metadata must be stripped before provider call: %+v", payload["metadata"])
		}
		if _, exists := payload["truncation"]; exists {
			t.Fatalf("truncation must be stripped before provider call: %+v", payload["truncation"])
		}
		if _, exists := payload["user"]; exists {
			t.Fatalf("user must be stripped before provider call: %+v", payload["user"])
		}
		inputItems, ok := payload["input"].([]any)
		if !ok || len(inputItems) != 1 {
			t.Fatalf("unexpected input payload: %+v", payload["input"])
		}
		firstInput, ok := inputItems[0].(map[string]any)
		if !ok {
			t.Fatalf("unexpected first input item: %+v", inputItems[0])
		}
		if firstInput["role"] != "user" || firstInput["content"] != "hello" {
			t.Fatalf("unexpected input item: %+v", firstInput)
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
		Request: OpenAIResponsesRequest{
			"input":      "hello",
			"metadata":   map[string]any{"trace_id": "req-1"},
			"truncation": "auto",
			"user":       "end-user-3",
		},
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

func TestOpenAICompatibleProviderSupportsResponsesStream(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/responses" {
			http.NotFound(writer, request)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if stream, ok := payload["stream"].(bool); !ok || !stream {
			t.Fatalf("expected stream=true payload, got: %+v", payload["stream"])
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(
			writer,
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"hello \"}\n\n"+
				"data: {\"type\":\"response.output_text.delta\",\"delta\":\"world\"}\n\n"+
				"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-stream-1\",\"object\":\"response\",\"model\":\"gpt-4.1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"hello world\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":3,\"total_tokens\":5}}}\n\n"+
				"data: [DONE]\n\n",
		)
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	result, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "gpt-4.1",
	}, InvokeRequest{
		Request: OpenAIResponsesRequest{
			"input":  "hello",
			"stream": true,
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.OutputText == nil || *result.OutputText != "hello world" {
		t.Fatalf("unexpected output: %+v", result.OutputText)
	}
	if result.Usage == nil || result.Usage.TotalTokens == nil || *result.Usage.TotalTokens != 5 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestOpenAICompatibleProviderSupportsChatCompletionsStream(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if stream, ok := payload["stream"].(bool); !ok || !stream {
			t.Fatalf("expected stream=true payload, got: %+v", payload["stream"])
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(
			writer,
			"data: {\"id\":\"chatcmpl-stream-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\"}}]}\n\n"+
				"data: {\"id\":\"chatcmpl-stream-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello \"}}]}\n\n"+
				"data: {\"id\":\"chatcmpl-stream-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"world\"}}]}\n\n"+
				"data: {\"id\":\"chatcmpl-stream-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3,\"total_tokens\":5}}\n\n"+
				"data: [DONE]\n\n",
		)
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	result, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		APIMode: APIModeChatCompletions,
		Model:   "gpt-4.1",
	}, InvokeRequest{
		ChatRequest: OpenAIChatCompletionsRequest{
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "hello",
				},
			},
			"stream": true,
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.OutputText == nil || *result.OutputText != "hello world" {
		t.Fatalf("unexpected output: %+v", result.OutputText)
	}
	if result.Usage == nil || result.Usage.InputTokens == nil || *result.Usage.InputTokens != 2 {
		t.Fatalf("unexpected usage: %+v", result.Usage)
	}
}

func TestOpenAICompatibleProviderSupportsChatCompletionsToolStream(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/chat/completions" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(
			writer,
			"data: {\"id\":\"chatcmpl-stream-tool-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"{\\\"city\\\":\\\"\"}}]}}]}\n\n"+
				"data: {\"id\":\"chatcmpl-stream-tool-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"Beijing\\\"}\"}}]}}]}\n\n"+
				"data: {\"id\":\"chatcmpl-stream-tool-1\",\"object\":\"chat.completion.chunk\",\"model\":\"gpt-4.1\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"+
				"data: [DONE]\n\n",
		)
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	result, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		APIMode: APIModeChatCompletions,
		Model:   "gpt-4.1",
	}, InvokeRequest{
		ChatRequest: OpenAIChatCompletionsRequest{
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "weather",
				},
			},
			"stream": true,
		},
	})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if result.OutputText != nil {
		t.Fatalf("expected nil output_text for tool stream response: %+v", result.OutputText)
	}
	items, ok := result.Response["output"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("unexpected output items: %+v", result.Response["output"])
	}
	functionCall, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("unexpected function_call payload: %+v", items[0])
	}
	if functionCall["call_id"] != "call-1" || functionCall["name"] != "get_weather" {
		t.Fatalf("unexpected function_call payload: %+v", functionCall)
	}
	if functionCall["arguments"] != "{\"city\":\"Beijing\"}" {
		t.Fatalf("unexpected function_call arguments: %+v", functionCall["arguments"])
	}
}

func TestGetProviderClientSupportsOpenAIAlias(t *testing.T) {
	t.Parallel()
	provider, err := GetProviderClient("openai")
	if err != nil {
		t.Fatalf("expected alias to be supported: %v", err)
	}
	if _, ok := provider.(OpenAICompatibleProvider); !ok {
		t.Fatalf("unexpected provider type: %T", provider)
	}
}

func TestGetProviderClientAcceptsArbitraryProviderLabel(t *testing.T) {
	t.Parallel()
	provider, err := GetProviderClient("Xcode")
	if err != nil {
		t.Fatalf("expected provider label to not block invocation: %v", err)
	}
	if _, ok := provider.(OpenAICompatibleProvider); !ok {
		t.Fatalf("unexpected provider type: %T", provider)
	}
}

func TestOpenAICompatibleProvider404OnResponsesContainsHint(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.NotFound(writer, request)
	}))
	defer server.Close()

	provider := OpenAICompatibleProvider{HTTPClient: server.Client()}
	_, err := provider.Invoke(context.Background(), InvocationReservation{
		BaseURL: server.URL + "/v1",
		APIKey:  "sk-test",
		Model:   "deepseek-chat",
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
	if providerErr.GatewayError.Code != "provider_http_error" {
		t.Fatalf("unexpected code: %s", providerErr.GatewayError.Code)
	}
	if !strings.Contains(providerErr.GatewayError.Message, "chat/completions") {
		t.Fatalf("unexpected message: %s", providerErr.GatewayError.Message)
	}
	path, ok := providerErr.GatewayError.Details["path"].(string)
	if !ok || path != "/v1/responses" {
		t.Fatalf("unexpected request path detail: %+v", providerErr.GatewayError.Details)
	}
}
