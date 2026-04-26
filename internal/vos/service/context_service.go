package service

import (
	"fmt"

	"vos/internal/vos/domain"
)

const (
	topicMetadataUserMemoryKey  = "user_memory"
	topicMetadataTopicMemoryKey = "topic_memory"
	topicMetadataGlobalIndexKey = "global_index"
	contextEventsPageSize       = 200
)

func (service *Service) GetContextSnapshot(nodeID string) (*domain.ContextSnapshot, error) {
	if service.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	if nodeID == "" {
		return nil, domain.ValidationError{Message: "node ID is required"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	node, err := requireNode(state, nodeID)
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, node.TopicID)
	if err != nil {
		return nil, err
	}

	nodeMemory, err := resolveContextNodeMemory(state, node)
	if err != nil {
		return nil, err
	}

	sessionHistory, err := service.buildContextSessionHistory(node)
	if err != nil {
		return nil, err
	}

	processContexts, err := service.buildProcessContexts(state, node)
	if err != nil {
		return nil, err
	}

	return &domain.ContextSnapshot{
		NodeID:          node.ID,
		UserMemory:      resolveUserMemory(state, topic),
		TopicMemory:     readMetadataObject(topic.Metadata, topicMetadataTopicMemoryKey),
		NodeMemory:      nodeMemory,
		GlobalIndex:     readMetadataValue(topic.Metadata, topicMetadataGlobalIndexKey),
		SessionHistory:  sessionHistory,
		ProcessContexts: processContexts,
	}, nil
}

func resolveUserMemory(state domain.VfsState, topic *domain.Topic) map[string]any {
	if state.User != nil && state.User.UserMemory != nil {
		return cloneMap(state.User.UserMemory)
	}
	if topic == nil {
		return nil
	}
	return readMetadataObject(topic.Metadata, topicMetadataUserMemoryKey)
}

func (service *Service) buildContextSessionHistory(node *domain.Node) ([]domain.ContextSessionHistory, error) {
	history := make([]domain.ContextSessionHistory, 0, len(node.Session))
	for _, sessionID := range node.Session {
		session, err := service.sessionStore.GetSession(sessionID)
		if err != nil {
			return nil, err
		}
		events, err := service.listAllSessionEvents(sessionID)
		if err != nil {
			return nil, err
		}
		history = append(history, domain.ContextSessionHistory{
			Session: cloneSession(session),
			Events:  events,
		})
	}
	return history, nil
}

func (service *Service) listAllSessionEvents(sessionID string) ([]*domain.SessionEvent, error) {
	events := make([]*domain.SessionEvent, 0)
	afterSeq := 0

	for {
		batch, err := service.sessionStore.ListEvents(sessionID, afterSeq, contextEventsPageSize)
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			return events, nil
		}

		for _, event := range batch {
			events = append(events, cloneSessionEvent(event))
		}

		lastSeq := batch[len(batch)-1].Seq
		if lastSeq <= afterSeq {
			return nil, fmt.Errorf("session events are not strictly increasing: session=%s seq=%d", sessionID, lastSeq)
		}
		afterSeq = lastSeq
		if len(batch) < contextEventsPageSize {
			return events, nil
		}
	}
}

func resolveContextNodeMemory(state domain.VfsState, node *domain.Node) (map[string]any, error) {
	if node.ParentID != nil {
		parent, err := requireNode(state, *node.ParentID)
		if err != nil {
			return nil, err
		}
		if parent.Memory != nil {
			return cloneMap(parent.Memory), nil
		}
	}
	return cloneMapNil(node.Memory), nil
}

func readMetadataObject(metadata map[string]any, key string) map[string]any {
	value := readMetadataValue(metadata, key)
	if value == nil {
		return nil
	}
	memory, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return cloneMap(memory)
}

func readMetadataValue(metadata map[string]any, key string) any {
	if metadata == nil {
		return nil
	}
	value, exists := metadata[key]
	if !exists {
		return nil
	}
	return value
}

func (service *Service) buildProcessContexts(state domain.VfsState, node *domain.Node) ([]domain.ProcessContext, error) {
	processes := resolveNodeProcesses(state, node)
	if len(processes) == 0 {
		return nil, nil
	}

	ctxs := make([]domain.ProcessContext, 0, len(processes))
	for _, item := range processes {
		pc := domain.ProcessContext{
			Name:   item.Name,
			Status: item.Status,
		}

		if item.Summary != nil {
			pc.Summary = cloneMap(item.Summary)
		} else if item.SessionRange != nil && !item.SessionRange.Closed() {
			events, err := service.loadSessionRangeEvents(item.SessionRange)
			if err != nil {
				return nil, err
			}
			if len(events) > 0 {
				pc.SessionEvents = events
			}
		}

		ctxs = append(ctxs, pc)
	}
	return ctxs, nil
}

func (service *Service) loadSessionRangeEvents(sr *domain.SessionRange) ([]*domain.SessionEvent, error) {
	var events []*domain.SessionEvent

	allEvents, err := service.listAllSessionEvents(sr.StartSessionID)
	if err != nil {
		return nil, err
	}

	for _, evt := range allEvents {
		if sr.StartEventSeq != nil && evt.Seq < *sr.StartEventSeq {
			continue
		}
		if sr.EndEventSeq != nil && evt.Seq > *sr.EndEventSeq {
			break
		}
		events = append(events, cloneSessionEvent(evt))
	}
	return events, nil
}
