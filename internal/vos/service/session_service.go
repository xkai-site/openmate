package service

import (
	"fmt"
	"time"

	"vos/internal/vos/domain"
)

type CreateSessionInput struct {
	NodeID    string
	SessionID string
	Status    domain.SessionStatus
}

type AppendSessionEventInput struct {
	SessionID   string
	Kind        domain.SessionEventKind
	Role        *domain.SessionRole
	CallID      *string
	PayloadJSON map[string]any
}

func (service *Service) CreateSession(input CreateSessionInput) (*domain.Session, error) {
	if service.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	if input.NodeID == "" {
		return nil, domain.ValidationError{Message: "node ID is required"}
	}
	if input.Status == "" {
		input.Status = domain.SessionStatusOpen
	}
	if _, err := domain.ParseSessionStatus(string(input.Status)); err != nil {
		return nil, err
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	node, err := requireNode(state, input.NodeID)
	if err != nil {
		return nil, err
	}

	sessionID := input.SessionID
	if sessionID == "" {
		sessionID = domain.NewID()
	}

	for _, existing := range node.Session {
		if existing == sessionID {
			return nil, domain.DuplicateEntityError{Kind: "session", ID: sessionID}
		}
	}

	now := time.Now().UTC()
	session := &domain.Session{
		ID:        sessionID,
		NodeID:    input.NodeID,
		Status:    input.Status,
		CreatedAt: now,
		UpdatedAt: now,
		LastSeq:   0,
	}
	session.Normalize()

	if err := service.sessionStore.CreateSession(session); err != nil {
		return nil, err
	}

	node.Session = append(node.Session, session.ID)
	touchNode(node)

	topic, err := requireTopic(state, node.TopicID)
	if err != nil {
		rollbackErr := service.sessionStore.DeleteSession(session.ID)
		if rollbackErr != nil {
			return nil, fmt.Errorf("load topic after session create: %w (rollback failed: %v)", err, rollbackErr)
		}
		return nil, err
	}
	touchTopic(topic)

	if err := service.store.Save(state); err != nil {
		rollbackErr := service.sessionStore.DeleteSession(session.ID)
		if rollbackErr != nil {
			return nil, fmt.Errorf("save node session reference: %w (rollback failed: %v)", err, rollbackErr)
		}
		return nil, err
	}

	return cloneSession(session), nil
}

func (service *Service) GetSession(sessionID string) (*domain.Session, error) {
	if service.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	if sessionID == "" {
		return nil, domain.ValidationError{Message: "session ID is required"}
	}
	return service.sessionStore.GetSession(sessionID)
}

func (service *Service) AppendSessionEvent(input AppendSessionEventInput) (*domain.SessionEvent, error) {
	if service.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	if input.SessionID == "" {
		return nil, domain.ValidationError{Message: "session ID is required"}
	}
	if _, err := domain.ParseSessionEventKind(string(input.Kind)); err != nil {
		return nil, err
	}
	if input.Role != nil {
		if _, err := domain.ParseSessionRole(string(*input.Role)); err != nil {
			return nil, err
		}
	}
	if (input.Kind == domain.SessionEventKindToolCall || input.Kind == domain.SessionEventKindToolResult) && input.CallID == nil {
		return nil, domain.ValidationError{Message: "call ID is required for tool events"}
	}

	role := input.Role
	if role == nil {
		defaultRole := defaultRoleForSessionEvent(input.Kind)
		role = &defaultRole
	}

	event := &domain.SessionEvent{
		ID:          domain.NewID(),
		SessionID:   input.SessionID,
		Kind:        input.Kind,
		Role:        role,
		CallID:      cloneStringPtr(input.CallID),
		PayloadJSON: cloneMap(input.PayloadJSON),
		CreatedAt:   time.Now().UTC(),
	}
	event.Normalize()

	nextStatus, err := resolveSessionStatusTransition(input.Kind, event.PayloadJSON)
	if err != nil {
		return nil, err
	}
	if _, err := service.sessionStore.AppendEvent(event, nextStatus); err != nil {
		return nil, err
	}
	return cloneSessionEvent(event), nil
}

func (service *Service) ListSessionEvents(sessionID string, afterSeq, limit int) ([]*domain.SessionEvent, error) {
	if service.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	if sessionID == "" {
		return nil, domain.ValidationError{Message: "session ID is required"}
	}
	if afterSeq < 0 {
		return nil, domain.ValidationError{Message: "after-seq must be a non-negative integer"}
	}
	if limit < 0 {
		return nil, domain.ValidationError{Message: "limit must be a non-negative integer"}
	}
	return service.sessionStore.ListEvents(sessionID, afterSeq, limit)
}

func defaultRoleForSessionEvent(kind domain.SessionEventKind) domain.SessionRole {
	switch kind {
	case domain.SessionEventKindUserMessage:
		return domain.SessionRoleUser
	case domain.SessionEventKindAssistantMessage, domain.SessionEventKindToolCall:
		return domain.SessionRoleAssistant
	case domain.SessionEventKindToolResult:
		return domain.SessionRoleTool
	default:
		return domain.SessionRoleSystem
	}
}

func resolveSessionStatusTransition(kind domain.SessionEventKind, payload map[string]any) (*domain.SessionStatus, error) {
	switch kind {
	case domain.SessionEventKindError:
		status := domain.SessionStatusFailed
		return &status, nil
	case domain.SessionEventKindStatus:
		if payload == nil {
			return nil, domain.ValidationError{Message: "status event requires payload_json.status or payload_json.to"}
		}
		for _, key := range []string{"status", "to"} {
			if raw, exists := payload[key]; exists {
				text, ok := raw.(string)
				if !ok {
					return nil, domain.ValidationError{Message: fmt.Sprintf("status event field %s must be a string", key)}
				}
				status, err := domain.ParseSessionStatus(text)
				if err != nil {
					return nil, err
				}
				return &status, nil
			}
		}
		return nil, domain.ValidationError{Message: "status event requires payload_json.status or payload_json.to"}
	default:
		return nil, nil
	}
}

func cloneSession(session *domain.Session) *domain.Session {
	if session == nil {
		return nil
	}
	cloned := *session
	return &cloned
}

func cloneSessionEvent(event *domain.SessionEvent) *domain.SessionEvent {
	if event == nil {
		return nil
	}
	cloned := *event
	cloned.Role = cloneSessionRolePtr(event.Role)
	cloned.CallID = cloneStringPtr(event.CallID)
	cloned.PayloadJSON = cloneMap(event.PayloadJSON)
	return &cloned
}

func cloneSessionRolePtr(role *domain.SessionRole) *domain.SessionRole {
	if role == nil {
		return nil
	}
	value := *role
	return &value
}
