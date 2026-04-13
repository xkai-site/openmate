package schedule

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OpenMate schedule control-plane CLI.") {
		t.Fatalf("expected help output, got %s", stdout.String())
	}
}

func TestRunPlan(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"plan"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, `"module": "schedule"`) {
		t.Fatalf("expected schedule module marker, got %s", output)
	}
	if !strings.Contains(output, `"module_boundary": "cli+json"`) {
		t.Fatalf("expected module boundary marker, got %s", output)
	}
}
