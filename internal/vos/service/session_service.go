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
	SessionID      string
	ItemType       string
	ProviderItemID *string
	Role           *domain.SessionRole
	CallID         *string
	PayloadJSON    map[string]any
	NextStatus     *domain.SessionStatus
}

func (service *Service) CreateSession(input CreateSessionInput) (*domain.Session, error) {
	if service.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	if input.NodeID == "" {
		return nil, domain.ValidationError{Message: "node ID is required"}
	}
	if input.Status == "" {
		input.Status = domain.SessionStatusActive
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
	if _, err := domain.ParseSessionItemType(input.ItemType); err != nil {
		return nil, err
	}
	if input.Role != nil {
		if _, err := domain.ParseSessionRole(string(*input.Role)); err != nil {
			return nil, err
		}
	}
	if input.NextStatus != nil {
		if _, err := domain.ParseSessionStatus(string(*input.NextStatus)); err != nil {
			return nil, err
		}
	}
	if input.CallID == nil {
		return nil, domain.ValidationError{Message: "call ID is required for tool events"}
	}

	role := input.Role
	if role == nil {
		inferredRole, ok, err := inferSessionRole(input.PayloadJSON)
		if err != nil {
			return nil, err
		}
		if ok {
			role = &inferredRole
		}
	}

	event := &domain.SessionEvent{
		ID:             domain.NewID(),
		SessionID:      input.SessionID,
		ItemType:       input.ItemType,
		ProviderItemID: cloneStringPtr(input.ProviderItemID),
		Role:           role,
		CallID:         cloneStringPtr(input.CallID),
		PayloadJSON:    cloneMap(input.PayloadJSON),
		CreatedAt:      time.Now().UTC(),
	}
	event.Normalize()

	if _, err := service.sessionStore.AppendEvent(event, input.NextStatus); err != nil {
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

func (service *Service) ListSessionEventsByCallID(sessionID, callID string, limit int) ([]*domain.SessionEvent, error) {
	if service.sessionStore == nil {
		return nil, fmt.Errorf("session store is not configured")
	}
	if sessionID == "" {
		return nil, domain.ValidationError{Message: "session ID is required"}
	}
	if callID == "" {
		return nil, domain.ValidationError{Message: "call ID is required"}
	}
	if limit < 0 {
		return nil, domain.ValidationError{Message: "limit must be a non-negative integer"}
	}
	return service.sessionStore.ListEventsByCallID(sessionID, callID, limit)
}

func inferSessionRole(payload map[string]any) (domain.SessionRole, bool, error) {
	if payload == nil {
		return "", false, nil
	}
	raw, exists := payload["role"]
	if !exists {
		return "", false, nil
	}
	text, ok := raw.(string)
	if !ok {
		return "", false, domain.ValidationError{Message: "payload_json.role must be a string"}
	}
	role, err := domain.ParseSessionRole(text)
	if err != nil {
		return "", false, err
	}
	return role, true, nil
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
	cloned.ProviderItemID = cloneStringPtr(event.ProviderItemID)
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
