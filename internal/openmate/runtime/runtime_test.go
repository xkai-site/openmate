package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndClose(t *testing.T) {
	tempDir := t.TempDir()
	modelConfig := filepath.Join(tempDir, "model.json")
	if err := os.WriteFile(modelConfig, []byte(`{
  "global_max_concurrent": 1,
  "offline_failure_threshold": 3,
  "apis": [
    {
      "api_id": "api-1",
      "provider": "openai_compatible",
      "model": "gpt-4.1-mini",
      "base_url": "https://example.invalid/v1",
      "api_key": "sk-test",
      "max_concurrent": 1,
      "enabled": true
    }
  ]
}`), 0o644); err != nil {
		t.Fatalf("write model config: %v", err)
	}

	instance, err := Open(Config{
		StateFile:      filepath.Join(tempDir, "vos_state.json"),
		SessionDBFile:  filepath.Join(tempDir, "openmate.db"),
		ScheduleDBFile: filepath.Join(tempDir, "openmate.db"),
		WorkspaceRoot:  tempDir,
		WorkerCommand:  []string{"python", "-m", "openmate_agent.cli", "worker", "run"},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if instance.Service == nil {
		t.Fatalf("runtime service should not be nil")
	}
	if instance.ScheduleEngine == nil {
		t.Fatalf("runtime schedule engine should not be nil")
	}
	if err := instance.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestOpenFailsWhenModelConfigMissing(t *testing.T) {
	tempDir := t.TempDir()
	_, err := Open(Config{
		StateFile:      filepath.Join(tempDir, "vos_state.json"),
		SessionDBFile:  filepath.Join(tempDir, "openmate.db"),
		ScheduleDBFile: filepath.Join(tempDir, "openmate.db"),
		WorkspaceRoot:  tempDir,
		WorkerCommand:  []string{"python", "-m", "openmate_agent.cli", "worker", "run"},
	})
	if err == nil {
		t.Fatalf("Open() expected error when model config is missing")
	}
}
