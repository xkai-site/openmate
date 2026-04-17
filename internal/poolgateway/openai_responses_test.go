package poolgateway

import (
	"strings"
	"testing"
)

func TestValidateInvokeRequestRequiresExactlyOneRequestBody(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		request InvokeRequest
	}{
		{
			name: "none",
			request: InvokeRequest{
				RequestID: "req-1",
				NodeID:    "node-1",
			},
		},
		{
			name: "both",
			request: InvokeRequest{
				RequestID: "req-1",
				NodeID:    "node-1",
				Request: OpenAIResponsesRequest{
					"input": "hello",
				},
				ChatRequest: OpenAIChatCompletionsRequest{
					"messages": []any{
						map[string]any{
							"role":    "user",
							"content": "hello",
						},
					},
				},
			},
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateInvokeRequest(testCase.request)
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(err.Error(), "exactly one of request or chat_request") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateInvokeRequestRejectsLegacyChatCompletionsFields(t *testing.T) {
	t.Parallel()

	fields := []string{
		"messages",
		"functions",
		"function_call",
		"tool_calls",
		"max_tokens",
	}
	for _, field := range fields {
		field := field
		t.Run(field, func(t *testing.T) {
			t.Parallel()

			err := validateInvokeRequest(
				InvokeRequest{
					RequestID: "req-1",
					NodeID:    "node-1",
					Request: OpenAIResponsesRequest{
						"input": "hello",
						field:   "legacy",
					},
				},
			)
			if err == nil {
				t.Fatalf("expected error for field %q", field)
			}
			if !strings.Contains(err.Error(), "request."+field) {
				t.Fatalf("unexpected error for field %q: %v", field, err)
			}
		})
	}
}

func TestValidateInvokeRequestAcceptsChatRequest(t *testing.T) {
	t.Parallel()

	err := validateInvokeRequest(InvokeRequest{
		RequestID: "req-1",
		NodeID:    "node-1",
		ChatRequest: OpenAIChatCompletionsRequest{
			"messages": []any{
				map[string]any{
					"role":    "user",
					"content": "hello",
				},
			},
			"tool_choice": "auto",
			"tools": []any{
				map[string]any{
					"type": "function",
					"name": "echo",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected chat request to pass validation: %v", err)
	}
}

func TestValidateInvokeRequestAcceptsResponsesStream(t *testing.T) {
	t.Parallel()

	err := validateInvokeRequest(InvokeRequest{
		RequestID: "req-1",
		NodeID:    "node-1",
		Request: OpenAIResponsesRequest{
			"input":  "hello",
			"stream": true,
		},
	})
	if err != nil {
		t.Fatalf("expected responses stream request to pass validation: %v", err)
	}
}

func TestValidateInvokeRequestAcceptsChatStream(t *testing.T) {
	t.Parallel()

	err := validateInvokeRequest(InvokeRequest{
		RequestID: "req-1",
		NodeID:    "node-1",
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
		t.Fatalf("expected chat stream request to pass validation: %v", err)
	}
}

func TestValidateInvokeRequestForModeAllowsResponsesPayloadForChatMode(t *testing.T) {
	t.Parallel()

	err := validateInvokeRequestForMode(
		InvokeRequest{
			RequestID: "req-1",
			NodeID:    "node-1",
			Request: OpenAIResponsesRequest{
				"input": "hello",
			},
		},
		APIModeChatCompletions,
	)
	if err != nil {
		t.Fatalf("expected responses payload to be accepted for chat mode: %v", err)
	}
}
