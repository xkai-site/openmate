package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"vos/internal/vos/domain"
)

const (
	topicMetadataMemoryProposalsKey = "topic_memory_proposals"
	memoryProposalMinConfidence     = 0.7
)

type MemoryApplyDecision string

const (
	MemoryApplyDecisionConfirm MemoryApplyDecision = "confirm"
	MemoryApplyDecisionReject  MemoryApplyDecision = "reject"
)

type ApplyCompactionResultsInput struct {
	NodeID          string
	ExpectedVersion *int
	Processes       []domain.ProcessItem
	Compacted       []CompactedProcess
}

func (service *Service) ApplyCompactionResults(input ApplyCompactionResultsInput) (*domain.Node, []domain.MemoryProposal, error) {
	if strings.TrimSpace(input.NodeID) == "" {
		return nil, nil, domain.ValidationError{Message: "node ID is required"}
	}
	if input.ExpectedVersion != nil && *input.ExpectedVersion <= 0 {
		return nil, nil, domain.ValidationError{Message: "expected version must be a positive integer"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, nil, err
	}
	node, err := requireNode(state, input.NodeID)
	if err != nil {
		return nil, nil, err
	}
	if input.ExpectedVersion != nil && node.Version != *input.ExpectedVersion {
		return nil, nil, domain.VersionConflictError{Kind: "node", ID: node.ID, Expected: *input.ExpectedVersion, Actual: node.Version}
	}
	topic, err := requireTopic(state, node.TopicID)
	if err != nil {
		return nil, nil, err
	}

	if input.Processes != nil {
		if err := syncNodeProcesses(state, node, input.Processes); err != nil {
			return nil, nil, err
		}
	}

	created := buildPendingMemoryProposals(topic.ID, node.ID, input.Compacted)
	if len(created) > 0 {
		existing, err := readTopicMemoryProposals(topic.Metadata)
		if err != nil {
			return nil, nil, err
		}
		merged := append(existing, created...)
		if topic.Metadata == nil {
			topic.Metadata = map[string]any{}
		}
		topic.Metadata[topicMetadataMemoryProposalsKey] = merged
	}

	touchNode(node)
	touchTopic(topic)
	if err := service.store.Save(state); err != nil {
		return nil, nil, err
	}
	return cloneNode(node), cloneMemoryProposals(created), nil
}

func (service *Service) ListTopicMemoryProposals(topicID string) ([]domain.MemoryProposal, error) {
	if strings.TrimSpace(topicID) == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, topicID)
	if err != nil {
		return nil, err
	}
	items, err := readTopicMemoryProposals(topic.Metadata)
	if err != nil {
		return nil, err
	}
	return cloneMemoryProposals(items), nil
}

func (service *Service) ApplyTopicMemoryProposal(topicID, proposalID string, decision MemoryApplyDecision) (*domain.MemoryProposal, error) {
	topicID = strings.TrimSpace(topicID)
	proposalID = strings.TrimSpace(proposalID)
	if topicID == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}
	if proposalID == "" {
		return nil, domain.ValidationError{Message: "proposal ID is required"}
	}
	if decision != MemoryApplyDecisionConfirm && decision != MemoryApplyDecisionReject {
		return nil, domain.ValidationError{Message: "decision must be confirm or reject"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, topicID)
	if err != nil {
		return nil, err
	}
	proposals, err := readTopicMemoryProposals(topic.Metadata)
	if err != nil {
		return nil, err
	}

	idx := -1
	for i := range proposals {
		if proposals[i].ProposalID == proposalID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, domain.ValidationError{Message: fmt.Sprintf("proposal not found in topic: %s", proposalID)}
	}
	item := proposals[idx]
	if item.TopicID != topicID {
		return nil, domain.ValidationError{Message: fmt.Sprintf("proposal does not belong to topic: %s", proposalID)}
	}
	if item.Status != domain.MemoryProposalStatusPending {
		return nil, domain.ValidationError{Message: fmt.Sprintf("proposal is already finalized: %s", proposalID)}
	}

	if decision == MemoryApplyDecisionConfirm {
		topicMemory := readMetadataObject(topic.Metadata, topicMetadataTopicMemoryKey)
		if topicMemory == nil {
			topicMemory = map[string]any{}
		}
		for _, entry := range item.Entries {
			if strings.TrimSpace(entry.Key) == "" {
				continue
			}
			topicMemory[entry.Key] = entry.Value
		}
		if topic.Metadata == nil {
			topic.Metadata = map[string]any{}
		}
		topic.Metadata[topicMetadataTopicMemoryKey] = topicMemory
		item.Status = domain.MemoryProposalStatusApplied
	} else {
		item.Status = domain.MemoryProposalStatusRejected
	}
	proposals[idx] = item
	if topic.Metadata == nil {
		topic.Metadata = map[string]any{}
	}
	topic.Metadata[topicMetadataMemoryProposalsKey] = proposals

	touchTopic(topic)
	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	cloned := item
	cloned.Entries = cloneMemoryProposalEntries(item.Entries)
	cloned.Evidence = cloneStrings(item.Evidence)
	return &cloned, nil
}

func buildPendingMemoryProposals(topicID, nodeID string, compacted []CompactedProcess) []domain.MemoryProposal {
	if len(compacted) == 0 {
		return nil
	}
	created := make([]domain.MemoryProposal, 0)
	now := time.Now().UTC()
	for _, cp := range compacted {
		for _, candidate := range cp.MemoryProposals {
			entries := sanitizeMemoryProposalEntries(candidate.Entries)
			if !candidate.ProposeUpdate || len(entries) == 0 {
				continue
			}
			if candidate.Confidence < memoryProposalMinConfidence {
				continue
			}
			if len(candidate.Evidence) == 0 {
				continue
			}
			created = append(created, domain.MemoryProposal{
				ProposalID: domain.NewID(),
				TopicID:    topicID,
				NodeID:     nodeID,
				ProcessID:  strings.TrimSpace(cp.ProcessID),
				Status:     domain.MemoryProposalStatusPending,
				CreatedAt:  now,
				Entries:    entries,
				Evidence:   cloneStrings(candidate.Evidence),
				Confidence: candidate.Confidence,
				Reason:     strings.TrimSpace(candidate.Reason),
			})
		}
	}
	return created
}

func sanitizeMemoryProposalEntries(entries []MemoryProposalEntry) []domain.MemoryProposalEntry {
	if len(entries) == 0 {
		return nil
	}
	filtered := make([]domain.MemoryProposalEntry, 0, len(entries))
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" || isSensitiveMemoryKey(key) {
			continue
		}
		filtered = append(filtered, domain.MemoryProposalEntry{Key: key, Value: entry.Value})
	}
	return filtered
}

func isSensitiveMemoryKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return true
	}
	blocked := []string{"password", "secret", "token", "apikey", "api_key", "credential", "private_key"}
	for _, part := range blocked {
		if strings.Contains(lower, part) {
			return true
		}
	}
	return false
}

func readTopicMemoryProposals(metadata map[string]any) ([]domain.MemoryProposal, error) {
	if metadata == nil {
		return []domain.MemoryProposal{}, nil
	}
	raw, ok := metadata[topicMetadataMemoryProposalsKey]
	if !ok || raw == nil {
		return []domain.MemoryProposal{}, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal topic memory proposals: %w", err)
	}
	parsed := []domain.MemoryProposal{}
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal topic memory proposals: %w", err)
	}
	for i := range parsed {
		if parsed[i].Status == "" {
			parsed[i].Status = domain.MemoryProposalStatusPending
		}
		parsed[i].Evidence = cloneStrings(parsed[i].Evidence)
		parsed[i].Entries = cloneMemoryProposalEntries(parsed[i].Entries)
	}
	return parsed, nil
}

func cloneMemoryProposalEntries(entries []domain.MemoryProposalEntry) []domain.MemoryProposalEntry {
	if entries == nil {
		return []domain.MemoryProposalEntry{}
	}
	cloned := make([]domain.MemoryProposalEntry, len(entries))
	copy(cloned, entries)
	return cloned
}

func cloneMemoryProposals(raw []domain.MemoryProposal) []domain.MemoryProposal {
	if raw == nil {
		return []domain.MemoryProposal{}
	}
	cloned := make([]domain.MemoryProposal, len(raw))
	for i, item := range raw {
		cloned[i] = item
		cloned[i].Entries = cloneMemoryProposalEntries(item.Entries)
		cloned[i].Evidence = cloneStrings(item.Evidence)
	}
	return cloned
}
