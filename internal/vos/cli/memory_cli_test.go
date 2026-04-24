package cli_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"vos/internal/vos/cli"
	"vos/internal/vos/domain"
	"vos/internal/vos/service"
	"vos/internal/vos/store"
)

func TestMemoryProposalListAndApplyCLI(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "create", "--topic-id", "topic-1", "--node-id", "node-1", "--name", "Node One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node create code = %d, want 0", code)
	}
	if code := cli.Run(append(base, "node", "update", "--node-id", "node-1", "--process-json", `[{"id":"proc-1","name":"P1","status":"done"}]`), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("node update process code = %d, want 0", code)
	}

	svc := service.New(store.NewJSONStateStore(stateFile))
	node, err := svc.GetNode("node-1")
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	processes, err := svc.ListNodeProcesses(node.ID)
	if err != nil {
		t.Fatalf("ListNodeProcesses() error = %v", err)
	}
	processes[0].Summary = map[string]any{"key_findings": "done"}
	_, created, err := svc.ApplyCompactionResults(service.ApplyCompactionResultsInput{
		NodeID:          node.ID,
		ExpectedVersion: &node.Version,
		Processes:       processes,
		Compacted: []service.CompactedProcess{{
			ProcessID: "proc-1",
			Name:      "P1",
			Summary:   map[string]any{"key_findings": "done"},
			MemoryProposals: []service.MemoryProposalCandidate{{
				ProposeUpdate: true,
				Entries:       []service.MemoryProposalEntry{{Key: "team_norm", Value: "confirm before memory write"}},
				Evidence:      []string{"stated repeatedly"},
				Confidence:    0.9,
				Reason:        "stable consensus",
			}},
		}},
	})
	if err != nil {
		t.Fatalf("ApplyCompactionResults() error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}

	var listOut bytes.Buffer
	var listErr bytes.Buffer
	if code := cli.Run(append(base, "memory", "proposal", "list", "--topic-id", "topic-1"), &listOut, &listErr); code != 0 {
		t.Fatalf("memory proposal list code = %d, want 0, stderr=%q", code, listErr.String())
	}
	listed := []domain.MemoryProposal{}
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("json.Unmarshal(list proposals) error = %v", err)
	}
	if len(listed) != 1 || listed[0].Status != domain.MemoryProposalStatusPending {
		t.Fatalf("listed proposals = %+v, want one pending", listed)
	}

	var applyOut bytes.Buffer
	var applyErr bytes.Buffer
	if code := cli.Run(append(base, "memory", "apply", "--topic-id", "topic-1", "--proposal-id", created[0].ProposalID, "--decision", "confirm"), &applyOut, &applyErr); code != 0 {
		t.Fatalf("memory apply code = %d, want 0, stderr=%q", code, applyErr.String())
	}
	applied := domain.MemoryProposal{}
	if err := json.Unmarshal(applyOut.Bytes(), &applied); err != nil {
		t.Fatalf("json.Unmarshal(apply) error = %v", err)
	}
	if applied.Status != domain.MemoryProposalStatusApplied {
		t.Fatalf("apply status = %s, want applied", applied.Status)
	}

	topic, err := svc.GetTopic("topic-1")
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	topicMemory, ok := topic.Metadata["topic_memory"].(map[string]any)
	if !ok || topicMemory["team_norm"] != "confirm before memory write" {
		t.Fatalf("topic_memory = %v, want merged entry", topic.Metadata["topic_memory"])
	}
}

func TestMemoryApplyCLIRejectsDuplicateFinalize(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-1", "--name", "Topic One"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}
	svc := service.New(store.NewJSONStateStore(stateFile))
	topic, err := svc.GetTopic("topic-1")
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	topic.Metadata["topic_memory_proposals"] = []domain.MemoryProposal{{
		ProposalID: "proposal-1",
		TopicID:    topic.ID,
		NodeID:     "node-1",
		ProcessID:  "proc-1",
		Status:     domain.MemoryProposalStatusPending,
		Entries:    []domain.MemoryProposalEntry{{Key: "k", Value: "v"}},
		Evidence:   []string{"e"},
		Confidence: 0.9,
	}}
	if _, err := svc.UpdateTopic(service.UpdateTopicInput{TopicID: topic.ID, Metadata: topic.Metadata, ReplaceMetadata: true}); err != nil {
		t.Fatalf("UpdateTopic(seed proposal) error = %v", err)
	}

	if code := cli.Run(append(base, "memory", "apply", "--topic-id", "topic-1", "--proposal-id", "proposal-1", "--decision", "reject"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("first reject code = %d, want 0", code)
	}
	var stderr bytes.Buffer
	code := cli.Run(append(base, "memory", "apply", "--topic-id", "topic-1", "--proposal-id", "proposal-1", "--decision", "confirm"), &bytes.Buffer{}, &stderr)
	if code != 2 {
		t.Fatalf("second apply code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "finalized") {
		t.Fatalf("stderr = %q, want finalized", stderr.String())
	}
}
