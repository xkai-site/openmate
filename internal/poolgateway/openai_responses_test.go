package poolgateway

import (
	"strings"
	"testing"
)

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
