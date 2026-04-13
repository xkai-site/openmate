package poolgateway

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type ProviderKind string

const ProviderKindOpenAICompatible ProviderKind = "openai_compatible"

type APIConfig struct {
	APIID         string       `json:"api_id"`
	Provider      ProviderKind `json:"provider"`
	Model         string       `json:"model"`
	BaseURL       string       `json:"base_url"`
	APIKey        string       `json:"api_key"`
	MaxConcurrent int          `json:"max_concurrent"`
	Enabled       bool         `json:"enabled"`
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
