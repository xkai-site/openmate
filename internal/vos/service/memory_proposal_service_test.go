package service_test

import (
	"strings"
	"testing"

	"vos/internal/vos/domain"
	"vos/internal/vos/service"
)

func TestApplyCompactionResultsWritesSummaryAndPendingProposal(t *testing.T) {
	svc := newTestService(t)
	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-1", Name: "Topic One"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-1", Name: "Node 1"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if _, err := svc.UpdateNode(service.UpdateNodeInput{NodeID: node.ID, Process: []domain.ProcessItem{{ID: "proc-1", Name: "Implement", Status: domain.ProcessStatusDone}}}); err != nil {
		t.Fatalf("UpdateNode(process) error = %v", err)
	}

	node, err = svc.GetNode(node.ID)
	if err != nil {
		t.Fatalf("GetNode() error = %v", err)
	}
	processes, err := svc.ListNodeProcesses(node.ID)
	if err != nil {
		t.Fatalf("ListNodeProcesses() error = %v", err)
	}
	if len(processes) != 1 {
		t.Fatalf("len(processes) = %d, want 1", len(processes))
	}

	compacted := []service.CompactedProcess{
		{
			ProcessID:           "proc-1",
			Name:                "Implement",
			Summary:             map[string]any{"key_findings": "done"},
			CompactedSessionIDs: []string{"session-1"},
			MemoryProposals: []service.MemoryProposalCandidate{
				{
					ProposeUpdate: true,
					Entries: []service.MemoryProposalEntry{
						{Key: "working_style", Value: "strict"},
					},
					Evidence:   []string{"user repeatedly requested strict style"},
					Confidence: 0.92,
					Reason:     "stable cross-turn consensus",
				},
			},
		},
	}
	processes[0].Summary = compacted[0].Summary
	processes[0].CompactedSessionIDs = compacted[0].CompactedSessionIDs

	_, created, err := svc.ApplyCompactionResults(service.ApplyCompactionResultsInput{
		NodeID:          node.ID,
		ExpectedVersion: &node.Version,
		Processes:       processes,
		Compacted:       compacted,
	})
	if err != nil {
		t.Fatalf("ApplyCompactionResults() error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created proposals) = %d, want 1", len(created))
	}
	if created[0].Status != domain.MemoryProposalStatusPending {
		t.Fatalf("proposal status = %s, want pending", created[0].Status)
	}

	updatedProcesses, err := svc.ListNodeProcesses(node.ID)
	if err != nil {
		t.Fatalf("ListNodeProcesses(updated) error = %v", err)
	}
	if updatedProcesses[0].Summary["key_findings"] != "done" {
		t.Fatalf("summary = %v, want key_findings=done", updatedProcesses[0].Summary)
	}

	storedTopic, err := svc.GetTopic(topic.ID)
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	if _, ok := storedTopic.Metadata["topic_memory"]; ok {
		t.Fatalf("topic_memory should not be auto-written before confirm")
	}

	proposal, err := svc.ApplyTopicMemoryProposal(topic.ID, created[0].ProposalID, service.MemoryApplyDecisionConfirm)
	if err != nil {
		t.Fatalf("ApplyTopicMemoryProposal(confirm) error = %v", err)
	}
	if proposal.Status != domain.MemoryProposalStatusApplied {
		t.Fatalf("proposal status = %s, want applied", proposal.Status)
	}
	storedTopic, err = svc.GetTopic(topic.ID)
	if err != nil {
		t.Fatalf("GetTopic(after confirm) error = %v", err)
	}
	topicMemory, ok := storedTopic.Metadata["topic_memory"].(map[string]any)
	if !ok {
		t.Fatalf("topic_memory = %T, want map[string]any", storedTopic.Metadata["topic_memory"])
	}
	if topicMemory["working_style"] != "strict" {
		t.Fatalf("topic_memory = %v, want working_style=strict", topicMemory)
	}
}

func TestApplyTopicMemoryProposalRejectAndFinalizeProtection(t *testing.T) {
	svc := newTestService(t)
	topic, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-r", Name: "Topic Reject"})
	if err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topic.ID, NodeID: "node-r", Name: "Node"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if _, err := svc.UpdateNode(service.UpdateNodeInput{NodeID: node.ID, Process: []domain.ProcessItem{{ID: "proc-r", Name: "P", Status: domain.ProcessStatusDone}}}); err != nil {
		t.Fatalf("UpdateNode(process) error = %v", err)
	}
	node, _ = svc.GetNode(node.ID)
	processes, _ := svc.ListNodeProcesses(node.ID)
	compacted := []service.CompactedProcess{{
		ProcessID: "proc-r",
		Name:      "P",
		Summary:   map[string]any{"k": "v"},
		MemoryProposals: []service.MemoryProposalCandidate{{
			ProposeUpdate: true,
			Entries:       []service.MemoryProposalEntry{{Key: "long_term_constraint", Value: "must test before next step"}},
			Evidence:      []string{"rule stated multiple times"},
			Confidence:    0.95,
			Reason:        "explicit constraint",
		}},
	}}
	processes[0].Summary = compacted[0].Summary
	_, created, err := svc.ApplyCompactionResults(service.ApplyCompactionResultsInput{
		NodeID:          node.ID,
		ExpectedVersion: &node.Version,
		Processes:       processes,
		Compacted:       compacted,
	})
	if err != nil {
		t.Fatalf("ApplyCompactionResults() error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}

	proposalID := created[0].ProposalID
	proposal, err := svc.ApplyTopicMemoryProposal(topic.ID, proposalID, service.MemoryApplyDecisionReject)
	if err != nil {
		t.Fatalf("ApplyTopicMemoryProposal(reject) error = %v", err)
	}
	if proposal.Status != domain.MemoryProposalStatusRejected {
		t.Fatalf("proposal status = %s, want rejected", proposal.Status)
	}

	storedTopic, err := svc.GetTopic(topic.ID)
	if err != nil {
		t.Fatalf("GetTopic() error = %v", err)
	}
	if _, ok := storedTopic.Metadata["topic_memory"]; ok {
		t.Fatalf("topic_memory should remain unchanged after reject")
	}

	_, err = svc.ApplyTopicMemoryProposal(topic.ID, proposalID, service.MemoryApplyDecisionConfirm)
	if err == nil {
		t.Fatalf("ApplyTopicMemoryProposal(confirm twice) error = nil, want finalized error")
	}
	if !strings.Contains(err.Error(), "finalized") {
		t.Fatalf("error = %v, want finalized", err)
	}
}

func TestApplyTopicMemoryProposalRejectsCrossTopicAndLowQualityCandidates(t *testing.T) {
	svc := newTestService(t)
	topicA, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-a", Name: "A"})
	if err != nil {
		t.Fatalf("CreateTopic(A) error = %v", err)
	}
	topicB, _, err := svc.CreateTopic(service.CreateTopicInput{TopicID: "topic-b", Name: "B"})
	if err != nil {
		t.Fatalf("CreateTopic(B) error = %v", err)
	}
	node, err := svc.CreateNode(service.CreateNodeInput{TopicID: topicA.ID, NodeID: "node-a", Name: "Node"})
	if err != nil {
		t.Fatalf("CreateNode() error = %v", err)
	}
	if _, err := svc.UpdateNode(service.UpdateNodeInput{NodeID: node.ID, Process: []domain.ProcessItem{{ID: "proc-a", Name: "P", Status: domain.ProcessStatusDone}}}); err != nil {
		t.Fatalf("UpdateNode(process) error = %v", err)
	}
	node, _ = svc.GetNode(node.ID)
	processes, _ := svc.ListNodeProcesses(node.ID)

	compacted := []service.CompactedProcess{{
		ProcessID: "proc-a",
		Name:      "P",
		Summary:   map[string]any{"k": "v"},
		MemoryProposals: []service.MemoryProposalCandidate{
			{ProposeUpdate: true, Entries: []service.MemoryProposalEntry{{Key: "tmp", Value: "x"}}, Evidence: []string{"e"}, Confidence: 0.1, Reason: "low confidence"},
			{ProposeUpdate: true, Entries: []service.MemoryProposalEntry{{Key: "api_key", Value: "secret"}}, Evidence: []string{"e"}, Confidence: 0.9, Reason: "sensitive"},
			{ProposeUpdate: true, Entries: []service.MemoryProposalEntry{{Key: "team_norm", Value: "always confirm before write"}}, Evidence: []string{"stated in multiple turns"}, Confidence: 0.88, Reason: "stable"},
		},
	}}
	processes[0].Summary = compacted[0].Summary
	_, created, err := svc.ApplyCompactionResults(service.ApplyCompactionResultsInput{
		NodeID:          node.ID,
		ExpectedVersion: &node.Version,
		Processes:       processes,
		Compacted:       compacted,
	})
	if err != nil {
		t.Fatalf("ApplyCompactionResults() error = %v", err)
	}
	if len(created) != 1 {
		t.Fatalf("len(created) = %d, want 1", len(created))
	}

	_, err = svc.ApplyTopicMemoryProposal(topicB.ID, created[0].ProposalID, service.MemoryApplyDecisionConfirm)
	if err == nil {
		t.Fatalf("cross-topic apply error = nil, want error")
	}
	if !strings.Contains(err.Error(), "not found in topic") {
		t.Fatalf("error = %v, want not found in topic", err)
	}
}
