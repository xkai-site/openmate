package poolgateway

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type ProviderKind string

const ProviderKindOpenAICompatible ProviderKind = "openai_compatible"

type PricingConfig struct {
	InputPer1MUSD       *float64 `json:"input_per_1m_usd"`
	OutputPer1MUSD      *float64 `json:"output_per_1m_usd"`
	CachedInputPer1MUSD *float64 `json:"cached_input_per_1m_usd"`
	ReasoningPer1MUSD   *float64 `json:"reasoning_per_1m_usd"`
}

type APIConfig struct {
	APIID           string            `json:"api_id"`
	Provider        ProviderKind      `json:"provider"`
	Model           string            `json:"model"`
	BaseURL         string            `json:"base_url"`
	APIKey          string            `json:"api_key"`
	MaxConcurrent   int               `json:"max_concurrent"`
	Enabled         bool              `json:"enabled"`
	Headers         map[string]string `json:"headers"`
	RequestDefaults map[string]any    `json:"request_defaults"`
	Pricing         *PricingConfig    `json:"pricing"`
}

type RetryConfig struct {
	MaxAttempts   *int `json:"max_attempts"`
	BaseBackoffMS *int `json:"base_backoff_ms"`
}

type ModelConfig struct {
	GlobalMaxConcurrent     *int        `json:"global_max_concurrent"`
	OfflineFailureThreshold int         `json:"offline_failure_threshold"`
	Retry                   RetryConfig `json:"retry"`
	APIs                    []APIConfig `json:"apis"`
}

func LoadModelConfig(path string) (ModelConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return ModelConfig{}, fmt.Errorf("model config not found: %s", path)
	}
	if len(content) == 0 {
		return ModelConfig{}, fmt.Errorf("model config is empty: %s", path)
	}

	var config ModelConfig
	if err := json.Unmarshal(content, &config); err != nil {
		return ModelConfig{}, fmt.Errorf("invalid model config: %w", err)
	}

	if config.OfflineFailureThreshold <= 0 {
		config.OfflineFailureThreshold = 3
	}
	for idx := range config.APIs {
		if config.APIs[idx].Provider == "" {
			config.APIs[idx].Provider = ProviderKindOpenAICompatible
		}
		if err := validateAPIConfig(config.APIs[idx]); err != nil {
			return ModelConfig{}, err
		}
	}
	if err := validateRetryConfig(config.Retry); err != nil {
		return ModelConfig{}, err
	}
	return config, nil
}

func (config ModelConfig) APIByID(apiID string) (APIConfig, bool) {
	for _, api := range config.APIs {
		if api.APIID == apiID {
			return api, true
		}
	}
	return APIConfig{}, false
}

func validateAPIConfig(api APIConfig) error {
	if api.APIID == "" {
		return fmt.Errorf("api_id is required")
	}
	if api.Model == "" {
		return fmt.Errorf("model is required for api_id=%s", api.APIID)
	}
	if api.BaseURL == "" {
		return fmt.Errorf("base_url is required for api_id=%s", api.APIID)
	}
	if api.APIKey == "" {
		return fmt.Errorf("api_key is required for api_id=%s", api.APIID)
	}
	if api.MaxConcurrent <= 0 {
		return fmt.Errorf("max_concurrent must be > 0 for api_id=%s", api.APIID)
	}
	if err := validateRequestDefaults(api.APIID, api.RequestDefaults); err != nil {
		return err
	}
	if err := validatePricingConfig(api.APIID, api.Pricing); err != nil {
		return err
	}
	return nil
}

func validateRetryConfig(retry RetryConfig) error {
	if retry.MaxAttempts != nil && *retry.MaxAttempts <= 0 {
		return fmt.Errorf("retry.max_attempts must be > 0")
	}
	if retry.BaseBackoffMS != nil && *retry.BaseBackoffMS < 0 {
		return fmt.Errorf("retry.base_backoff_ms must be >= 0")
	}
	return nil
}

func validateRequestDefaults(apiID string, defaults map[string]any) error {
	if len(defaults) == 0 {
		return nil
	}
	if _, exists := defaults["model"]; exists {
		return fmt.Errorf("request_defaults.model must not be set for api_id=%s", apiID)
	}
	if _, exists := defaults["input"]; exists {
		return fmt.Errorf("request_defaults.input must not be set for api_id=%s", apiID)
	}
	if stream, ok := defaults["stream"].(bool); ok && stream {
		return fmt.Errorf("request_defaults.stream is not supported for api_id=%s", apiID)
	}
	if value, ok := anyFloat64(defaults["temperature"]); ok && (value < 0 || value > 2) {
		return fmt.Errorf("request_defaults.temperature must be between 0 and 2 for api_id=%s", apiID)
	}
	if value, ok := anyFloat64(defaults["top_p"]); ok && (value < 0 || value > 1) {
		return fmt.Errorf("request_defaults.top_p must be between 0 and 1 for api_id=%s", apiID)
	}
	if value, ok := anyInt(defaults["max_output_tokens"]); ok && value <= 0 {
		return fmt.Errorf("request_defaults.max_output_tokens must be > 0 for api_id=%s", apiID)
	}
	return nil
}

func validatePricingConfig(apiID string, pricing *PricingConfig) error {
	if pricing == nil {
		return nil
	}
	check := func(name string, value *float64) error {
		if value != nil && *value < 0 {
			return fmt.Errorf("pricing.%s must be >= 0 for api_id=%s", name, apiID)
		}
		return nil
	}
	if err := check("input_per_1m_usd", pricing.InputPer1MUSD); err != nil {
		return err
	}
	if err := check("output_per_1m_usd", pricing.OutputPer1MUSD); err != nil {
		return err
	}
	if err := check("cached_input_per_1m_usd", pricing.CachedInputPer1MUSD); err != nil {
		return err
	}
	if err := check("reasoning_per_1m_usd", pricing.ReasoningPer1MUSD); err != nil {
		return err
	}
	return nil
}

func (config ModelConfig) RetryPolicy() RetryPolicy {
	policy := defaultRetryPolicy()
	if config.Retry.MaxAttempts != nil {
		policy.MaxAttempts = *config.Retry.MaxAttempts
	}
	if config.Retry.BaseBackoffMS != nil {
		policy.BaseBackoff = time.Duration(*config.Retry.BaseBackoffMS) * time.Millisecond
	}
	return policy.normalized()
}

func anyFloat64(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func anyInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case float32:
		return int(typed), true
	case int:
		return typed, true
	case int64:
		return int(typed), true
	default:
		return 0, false
	}
}
