package schedule

import (
	"bytes"
	"encoding/json"
	"path/filepath"
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
	snapshot := makeTopicSnapshot()
	inputFile := filepath.Join(t.TempDir(), "topic.json")
	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := writeFile(inputFile, payload); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"plan", "--input-file", inputFile, "--available-slots", "2"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d, stderr=%s", code, stderr.String())
	}

	var plan DispatchPlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatalf("unmarshal plan output: %v", err)
	}
	if plan.ActivePriority == nil || *plan.ActivePriority != "now" {
		t.Fatalf("unexpected active priority: %+v", plan.ActivePriority)
	}
	if plan.CurrentNodeID == nil || *plan.CurrentNodeID != "node-a" {
		t.Fatalf("unexpected current node: %+v", plan.CurrentNodeID)
	}
	if len(plan.DispatchNodeIDs) != 2 || plan.DispatchNodeIDs[0] != "node-a" || plan.DispatchNodeIDs[1] != "node-b" {
		t.Fatalf("unexpected dispatch list: %+v", plan.DispatchNodeIDs)
	}
}

func TestRunPlanInvalidJSON(t *testing.T) {
	inputFile := filepath.Join(t.TempDir(), "topic.json")
	if err := writeFile(inputFile, []byte("{invalid")); err != nil {
		t.Fatalf("write invalid snapshot: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"plan", "--input-file", inputFile}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "invalid topic snapshot json") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}

func TestRunPlanNegativeSlots(t *testing.T) {
	snapshot := makeTopicSnapshot()
	inputFile := filepath.Join(t.TempDir(), "topic.json")
	payload, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := writeFile(inputFile, payload); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	code := Run([]string{"plan", "--input-file", inputFile, "--available-slots", "-1"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d, stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "available_slots must be >= 0") {
		t.Fatalf("unexpected stderr: %s", stderr.String())
	}
}
