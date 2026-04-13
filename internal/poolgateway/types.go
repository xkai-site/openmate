package poolgateway

import "time"

type ApiStatus string

const (
	ApiStatusAvailable ApiStatus = "available"
	ApiStatusLeased    ApiStatus = "leased"
	ApiStatusOffline   ApiStatus = "offline"
)

type InvocationStatus string

const (
	InvocationStatusRunning InvocationStatus = "running"
	InvocationStatusSuccess InvocationStatus = "success"
	InvocationStatusFailure InvocationStatus = "failure"
)

type MessageRole string

const (
	MessageRoleSystem    MessageRole = "system"
	MessageRoleUser      MessageRole = "user"
	MessageRoleAssistant MessageRole = "assistant"
	MessageRoleTool      MessageRole = "tool"
)

type ResponseMode string

const ResponseModeText ResponseMode = "text"

type LlmMessage struct {
	Role    MessageRole `json:"role"`
	Content string      `json:"content"`
}

type RoutePolicy struct {
	APIID *string `json:"api_id"`
}

type InvokeRequest struct {
	RequestID       string                 `json:"request_id"`
	NodeID          string                 `json:"node_id"`
	Messages        []LlmMessage           `json:"messages"`
	ResponseMode    ResponseMode           `json:"response_mode"`
	Temperature     *float64               `json:"temperature"`
	MaxOutputTokens *int                   `json:"max_output_tokens"`
	TimeoutMS       *int                   `json:"timeout_ms"`
	RoutePolicy     RoutePolicy            `json:"route_policy"`
	Metadata        map[string]interface{} `json:"metadata"`
}

type UsageMetrics struct {
	PromptTokens     *int     `json:"prompt_tokens"`
	CompletionTokens *int     `json:"completion_tokens"`
	TotalTokens      *int     `json:"total_tokens"`
	LatencyMS        *int     `json:"latency_ms"`
	CostUSD          *float64 `json:"cost_usd"`
}

type RouteDecision struct {
	APIID    string `json:"api_id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type GatewayError struct {
	Code               string                 `json:"code"`
	Message            string                 `json:"message"`
	Retryable          bool                   `json:"retryable"`
	ProviderStatusCode *int                   `json:"provider_status_code"`
	Details            map[string]interface{} `json:"details"`
}

type InvocationTiming struct {
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	LatencyMS  *int       `json:"latency_ms"`
}

type InvocationAttempt struct {
	AttemptID string           `json:"attempt_id"`
	Route     RouteDecision    `json:"route"`
	Status    InvocationStatus `json:"status"`
	Timing    InvocationTiming `json:"timing"`
	Usage     *UsageMetrics    `json:"usage"`
	Error     *GatewayError    `json:"error"`
}

type InvokeResponse struct {
	InvocationID string           `json:"invocation_id"`
	RequestID    string           `json:"request_id"`
	NodeID       string           `json:"node_id"`
	Status       InvocationStatus `json:"status"`
	Route        *RouteDecision   `json:"route"`
	OutputText   *string          `json:"output_text"`
	RawResponse  map[string]any   `json:"raw_response"`
	Usage        *UsageMetrics    `json:"usage"`
	Timing       InvocationTiming `json:"timing"`
	Error        *GatewayError    `json:"error"`
}

type InvocationRecord struct {
	InvocationID string              `json:"invocation_id"`
	Request      InvokeRequest       `json:"request"`
	Status       InvocationStatus    `json:"status"`
	Route        *RouteDecision      `json:"route"`
	OutputText   *string             `json:"output_text"`
	RawResponse  map[string]any      `json:"raw_response"`
	Usage        *UsageMetrics       `json:"usage"`
	Timing       InvocationTiming    `json:"timing"`
	Error        *GatewayError       `json:"error"`
	Attempts     []InvocationAttempt `json:"attempts"`
}

type UsageSummary struct {
	NodeID           *string   `json:"node_id"`
	Limit            *int      `json:"limit"`
	InvocationCount  int       `json:"invocation_count"`
	SuccessCount     int       `json:"success_count"`
	FailureCount     int       `json:"failure_count"`
	AttemptCount     int       `json:"attempt_count"`
	RetryCount       int       `json:"retry_count"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	TotalCostUSD     *float64  `json:"total_cost_usd"`
	AvgLatencyMS     *int      `json:"avg_latency_ms"`
	MaxLatencyMS     *int      `json:"max_latency_ms"`
	GeneratedAt      time.Time `json:"generated_at"`
}

type CapacitySnapshot struct {
	TotalAPIs      int       `json:"total_apis"`
	TotalSlots     int       `json:"total_slots"`
	AvailableSlots int       `json:"available_slots"`
	LeasedSlots    int       `json:"leased_slots"`
	OfflineAPIs    int       `json:"offline_apis"`
	Throttled      bool      `json:"throttled"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type SyncResult struct {
	Synced   bool             `json:"synced"`
	Capacity CapacitySnapshot `json:"capacity"`
}

type InvocationReservation struct {
	InvocationID string    `json:"invocation_id"`
	AttemptID    string    `json:"attempt_id"`
	RequestID    string    `json:"request_id"`
	NodeID       string    `json:"node_id"`
	APIID        string    `json:"api_id"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	BaseURL      string    `json:"base_url"`
	APIKey       string    `json:"api_key"`
	StartedAt    time.Time `json:"started_at"`
}

func (reservation InvocationReservation) Route() RouteDecision {
	return RouteDecision{
		APIID:    reservation.APIID,
		Provider: reservation.Provider,
		Model:    reservation.Model,
	}
}

type ProviderInvokeResult struct {
	OutputText  *string        `json:"output_text"`
	RawResponse map[string]any `json:"raw_response"`
	Usage       *UsageMetrics  `json:"usage"`
}
