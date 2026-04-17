package schedule

import (
	"context"
	"fmt"
	"strings"

	"vos/internal/vos/domain"
	vosservice "vos/internal/vos/service"
	vosstore "vos/internal/vos/store"
)

type DirectVOSGateway struct {
	service *vosservice.Service
}

func NewDirectVOSGateway(service *vosservice.Service) (*DirectVOSGateway, error) {
	if service == nil {
		return nil, ValidationError{Message: "vos service is required"}
	}
	return &DirectVOSGateway{service: service}, nil
}

func OpenDirectVOSGateway(stateFile string, sessionDBFile string) (*DirectVOSGateway, func(), error) {
	if strings.TrimSpace(stateFile) == "" {
		return nil, nil, ValidationError{Message: "state file is required"}
	}
	if strings.TrimSpace(sessionDBFile) == "" {
		return nil, nil, ValidationError{Message: "session db file is required"}
	}
	sessionStore, err := vosstore.NewSQLiteSessionStore(sessionDBFile)
	if err != nil {
		return nil, nil, err
	}
	service := vosservice.NewWithSessionStore(
		vosstore.NewJSONStateStore(stateFile),
		sessionStore,
	)
	gateway, err := NewDirectVOSGateway(service)
	if err != nil {
		_ = sessionStore.Close()
		return nil, nil, err
	}
	cleanup := func() {
		_ = sessionStore.Close()
	}
	return gateway, cleanup, nil
}

func (gateway *DirectVOSGateway) EnsurePriorityNode(ctx context.Context, topicID string) (string, error) {
	_ = ctx
	nodes, err := gateway.service.ListNodesByFilter(topicID, vosservice.NodeListFilter{
		LeafOnly: true,
	})
	if err != nil {
		return "", err
	}
	for _, node := range nodes {
		if node.Name == PriorityNodeName {
			return node.ID, nil
		}
	}

	description := "schedule priority node"
	node, err := gateway.service.CreateNode(vosservice.CreateNodeInput{
		TopicID:     topicID,
		Name:        PriorityNodeName,
		Description: &description,
		Status:      domain.NodeStatusReady,
	})
	if err != nil {
		return "", err
	}
	if node == nil || strings.TrimSpace(node.ID) == "" {
		return "", fmt.Errorf("vos priority node create returned empty id")
	}
	return node.ID, nil
}

func (gateway *DirectVOSGateway) EnsureSession(ctx context.Context, nodeID string, knownSessionID *string) (string, error) {
	_ = ctx
	if knownSessionID != nil && strings.TrimSpace(*knownSessionID) != "" {
		return strings.TrimSpace(*knownSessionID), nil
	}
	session, err := gateway.service.CreateSession(vosservice.CreateSessionInput{
		NodeID: nodeID,
		Status: domain.SessionStatusActive,
	})
	if err != nil {
		return "", err
	}
	if session == nil || strings.TrimSpace(session.ID) == "" {
		return "", fmt.Errorf("session create returned empty id")
	}
	return session.ID, nil
}

func (gateway *DirectVOSGateway) AppendDispatchAuthorizedEvent(ctx context.Context, sessionID string, payload map[string]any) (SessionEventRecord, error) {
	_ = ctx
	role := domain.SessionRoleSystem
	nextStatus := domain.SessionStatusActive
	event, err := gateway.service.AppendSessionEvent(vosservice.AppendSessionEventInput{
		SessionID:   sessionID,
		ItemType:    domain.SessionItemTypeMessage,
		Role:        &role,
		PayloadJSON: payload,
		NextStatus:  &nextStatus,
	})
	if err != nil {
		return SessionEventRecord{}, err
	}
	return SessionEventRecord{
		ID:        event.ID,
		SessionID: event.SessionID,
		Seq:       event.Seq,
	}, nil
}

func (gateway *DirectVOSGateway) AppendDispatchResultEvent(ctx context.Context, sessionID string, payload map[string]any) error {
	_ = ctx
	role := domain.SessionRoleSystem
	nextStatus := domain.SessionStatusActive
	if status, ok := payload["status"].(string); ok {
		switch strings.TrimSpace(status) {
		case "succeeded":
			nextStatus = domain.SessionStatusCompleted
		case "failed":
			nextStatus = domain.SessionStatusFailed
		}
	}
	_, err := gateway.service.AppendSessionEvent(vosservice.AppendSessionEventInput{
		SessionID:   sessionID,
		ItemType:    domain.SessionItemTypeMessage,
		Role:        &role,
		PayloadJSON: payload,
		NextStatus:  &nextStatus,
	})
	return err
}
