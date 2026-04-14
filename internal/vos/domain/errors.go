package domain

import (
	"errors"
	"fmt"
)

type userFacingError interface {
	error
	UserFacing() bool
}

type ValidationError struct {
	Message string
}

func (err ValidationError) Error() string {
	return err.Message
}

func (err ValidationError) UserFacing() bool {
	return true
}

type TopicNotFoundError struct {
	TopicID string
}

func (err TopicNotFoundError) Error() string {
	return fmt.Sprintf("topic not found: %s", err.TopicID)
}

func (err TopicNotFoundError) UserFacing() bool {
	return true
}

type NodeNotFoundError struct {
	NodeID string
}

func (err NodeNotFoundError) Error() string {
	return fmt.Sprintf("node not found: %s", err.NodeID)
}

func (err NodeNotFoundError) UserFacing() bool {
	return true
}

type DuplicateEntityError struct {
	Kind string
	ID   string
}

func (err DuplicateEntityError) Error() string {
	return fmt.Sprintf("%s already exists: %s", err.Kind, err.ID)
}

func (err DuplicateEntityError) UserFacing() bool {
	return true
}

type InvalidTreeError struct {
	Message string
}

func (err InvalidTreeError) Error() string {
	return err.Message
}

func (err InvalidTreeError) UserFacing() bool {
	return true
}

type LeafOperationError struct {
	NodeID string
}

func (err LeafOperationError) Error() string {
	return fmt.Sprintf("node is not a leaf: %s", err.NodeID)
}

func (err LeafOperationError) UserFacing() bool {
	return true
}

type VersionConflictError struct {
	Kind     string
	ID       string
	Expected int
	Actual   int
}

func (err VersionConflictError) Error() string {
	return fmt.Sprintf("%s version conflict: %s expected version %d, got %d", err.Kind, err.ID, err.Expected, err.Actual)
}

func (err VersionConflictError) UserFacing() bool {
	return true
}

type SessionNotFoundError struct {
	SessionID string
}

func (err SessionNotFoundError) Error() string {
	return fmt.Sprintf("session not found: %s", err.SessionID)
}

func (err SessionNotFoundError) UserFacing() bool {
	return true
}

type SessionSequenceConflictError struct {
	SessionID string
	Expected  int
	Actual    int
}

func (err SessionSequenceConflictError) Error() string {
	return fmt.Sprintf("session sequence conflict: %s expected seq %d, got %d", err.SessionID, err.Expected, err.Actual)
}

func (err SessionSequenceConflictError) UserFacing() bool {
	return true
}

func IsUserFacingError(err error) bool {
	if err == nil {
		return false
	}
	var target userFacingError
	return errors.As(err, &target)
}

func ParseNodeStatus(raw string) (NodeStatus, error) {
	switch NodeStatus(raw) {
	case NodeStatusDraft, NodeStatusReady, NodeStatusActive, NodeStatusBlocked, NodeStatusDone:
		return NodeStatus(raw), nil
	default:
		return "", ValidationError{Message: fmt.Sprintf("invalid node status: %s", raw)}
	}
}

func ParseSessionStatus(raw string) (SessionStatus, error) {
	switch SessionStatus(raw) {
	case SessionStatusActive, SessionStatusWaiting, SessionStatusCompleted, SessionStatusFailed:
		return SessionStatus(raw), nil
	default:
		return "", ValidationError{Message: fmt.Sprintf("invalid session status: %s", raw)}
	}
}

func ParseSessionItemType(raw string) (string, error) {
	switch raw {
	case "":
		return "", ValidationError{Message: "session event item_type is required"}
	default:
		return raw, nil
	}
}

func ParseSessionRole(raw string) (SessionRole, error) {
	switch SessionRole(raw) {
	case SessionRoleUser, SessionRoleAssistant, SessionRoleTool, SessionRoleSystem:
		return SessionRole(raw), nil
	default:
		return "", ValidationError{Message: fmt.Sprintf("invalid session role: %s", raw)}
	}
}
