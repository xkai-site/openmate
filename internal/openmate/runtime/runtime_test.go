package runtime

import (
	"path/filepath"
	"testing"
)

func TestOpenAndClose(t *testing.T) {
	tempDir := t.TempDir()
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
