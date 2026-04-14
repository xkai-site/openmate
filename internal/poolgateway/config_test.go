package poolgateway

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadModelConfigUsesDefaultRetryPolicyWhenRetryMissing(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	writeModelConfig(t, configPath, 3)

	config, err := LoadModelConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	policy := config.RetryPolicy()
	if policy.MaxAttempts != defaultRetryPolicy().MaxAttempts {
		t.Fatalf("unexpected max attempts: %d", policy.MaxAttempts)
	}
	if policy.BaseBackoff != defaultRetryPolicy().BaseBackoff {
		t.Fatalf("unexpected base backoff: %s", policy.BaseBackoff)
	}
}

func TestLoadModelConfigUsesConfiguredRetryPolicy(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	maxAttempts := 5
	baseBackoffMS := 750
	writeModelConfigWithRetry(t, configPath, 3, &maxAttempts, &baseBackoffMS)

	config, err := LoadModelConfig(configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	policy := config.RetryPolicy()
	if policy.MaxAttempts != 5 {
		t.Fatalf("unexpected max attempts: %d", policy.MaxAttempts)
	}
	if policy.BaseBackoff != 750*time.Millisecond {
		t.Fatalf("unexpected base backoff: %s", policy.BaseBackoff)
	}
}

func TestLoadModelConfigRejectsInvalidRetryPolicy(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	content := []byte(`{
  "offline_failure_threshold": 3,
  "retry": {
    "max_attempts": 0
  },
  "apis": [
    {
      "api_id": "api-1",
      "model": "gpt-4.1",
      "base_url": "http://unused.local/v1",
      "api_key": "sk-test",
      "max_concurrent": 1,
      "enabled": true
    }
  ]
}`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadModelConfig(configPath)
	if err == nil {
		t.Fatalf("expected config validation error")
	}
	if err.Error() != "retry.max_attempts must be > 0" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadModelConfigRejectsReservedRequestDefaultsFields(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "model.json")
	content := []byte(`{
  "offline_failure_threshold": 3,
  "apis": [
    {
      "api_id": "api-1",
      "model": "gpt-4.1",
      "base_url": "http://unused.local/v1",
      "api_key": "sk-test",
      "max_concurrent": 1,
      "enabled": true,
      "request_defaults": {
        "model": "should-not-be-here"
      }
    }
  ]
}`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadModelConfig(configPath)
	if err == nil {
		t.Fatalf("expected config validation error")
	}
	if err.Error() != "request_defaults.model must not be set for api_id=api-1" {
		t.Fatalf("unexpected error: %v", err)
	}
}
