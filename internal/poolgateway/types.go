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

type RoutePolicy struct {
	APIID *string `json:"api_id"`
}

type OpenAIResponsesRequest map[string]any

type OpenAIResponsesResponse map[string]any

type InvokeRequest struct {
	RequestID   string                 `json:"request_id"`
	NodeID      string                 `json:"node_id"`
	Request     OpenAIResponsesRequest `json:"request"`
	TimeoutMS   *int                   `json:"timeout_ms"`
	RoutePolicy RoutePolicy            `json:"route_policy"`
}

type UsageMetrics struct {
	InputTokens       *int     `json:"input_tokens"`
	OutputTokens      *int     `json:"output_tokens"`
	TotalTokens       *int     `json:"total_tokens"`
	CachedInputTokens *int     `json:"cached_input_tokens"`
	ReasoningTokens   *int     `json:"reasoning_tokens"`
	LatencyMS         *int     `json:"latency_ms"`
	CostUSD           *float64 `json:"cost_usd"`
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
	InvocationID string                  `json:"invocation_id"`
	RequestID    string                  `json:"request_id"`
	NodeID       string                  `json:"node_id"`
	Status       InvocationStatus        `json:"status"`
	Route        *RouteDecision          `json:"route"`
	Response     OpenAIResponsesResponse `json:"response"`
	OutputText   *string                 `json:"output_text"`
	Usage        *UsageMetrics           `json:"usage"`
	Timing       InvocationTiming        `json:"timing"`
	Error        *GatewayError           `json:"error"`
}

type InvocationRecord struct {
	InvocationID string                  `json:"invocation_id"`
	Request      InvokeRequest           `json:"request"`
	Status       InvocationStatus        `json:"status"`
	Route        *RouteDecision          `json:"route"`
	Response     OpenAIResponsesResponse `json:"response"`
	OutputText   *string                 `json:"output_text"`
	Usage        *UsageMetrics           `json:"usage"`
	Timing       InvocationTiming        `json:"timing"`
	Error        *GatewayError           `json:"error"`
	Attempts     []InvocationAttempt     `json:"attempts"`
}

type UsageSummary struct {
	NodeID            *string   `json:"node_id"`
	Limit             *int      `json:"limit"`
	InvocationCount   int       `json:"invocation_count"`
	SuccessCount      int       `json:"success_count"`
	FailureCount      int       `json:"failure_count"`
	AttemptCount      int       `json:"attempt_count"`
	RetryCount        int       `json:"retry_count"`
	InputTokens       int       `json:"input_tokens"`
	OutputTokens      int       `json:"output_tokens"`
	TotalTokens       int       `json:"total_tokens"`
	CachedInputTokens int       `json:"cached_input_tokens"`
	ReasoningTokens   int       `json:"reasoning_tokens"`
	TotalCostUSD      *float64  `json:"total_cost_usd"`
	AvgLatencyMS      *int      `json:"avg_latency_ms"`
	MaxLatencyMS      *int      `json:"max_latency_ms"`
	GeneratedAt       time.Time `json:"generated_at"`
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
	InvocationID    string            `json:"invocation_id"`
	AttemptID       string            `json:"attempt_id"`
	RequestID       string            `json:"request_id"`
	NodeID          string            `json:"node_id"`
	APIID           string            `json:"api_id"`
	Provider        string            `json:"provider"`
	Model           string            `json:"model"`
	BaseURL         string            `json:"base_url"`
	APIKey          string            `json:"api_key"`
	Headers         map[string]string `json:"headers"`
	RequestDefaults map[string]any    `json:"request_defaults"`
	Pricing         *PricingConfig    `json:"pricing"`
	StartedAt       time.Time         `json:"started_at"`
}

func (reservation InvocationReservation) Route() RouteDecision {
	return RouteDecision{
		APIID:    reservation.APIID,
		Provider: reservation.Provider,
		Model:    reservation.Model,
	}
}

type ProviderInvokeResult struct {
	Response   OpenAIResponsesResponse `json:"response"`
	OutputText *string                 `json:"output_text"`
	Usage      *UsageMetrics           `json:"usage"`
}
