package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func DefaultUnifiedDBFile() string {
	return filepath.FromSlash(".openmate/runtime/openmate.db")
}

func DefaultVOSStateFile() string {
	return filepath.FromSlash(".openmate/runtime/vos_state.json")
}

func DefaultVOSBinaryPath() string {
	binary := ".openmate/bin/vos"
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	return filepath.FromSlash(binary)
}

func DefaultWorkerCommand(workspaceRoot string) string {
	base := strings.TrimSpace(workspaceRoot)
	if base == "" {
		base = "."
	}
	venvPython := filepath.Join(filepath.Clean(base), filepath.FromSlash(".venv/Scripts/python.exe"))
	if _, err := os.Stat(venvPython); err == nil {
		return venvPython + " -m openmate_agent.cli worker run"
	}
	return "python -m openmate_agent.cli worker run"
}
