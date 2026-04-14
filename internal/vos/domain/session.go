package domain

import "time"

type SessionStatus string

const (
	SessionStatusOpen   SessionStatus = "open"
	SessionStatusClosed SessionStatus = "closed"
	SessionStatusFailed SessionStatus = "failed"
)

type SessionEventKind string

const (
	SessionEventKindUserMessage      SessionEventKind = "user_message"
	SessionEventKindAssistantMessage SessionEventKind = "assistant_message"
	SessionEventKindToolCall         SessionEventKind = "tool_call"
	SessionEventKindToolResult       SessionEventKind = "tool_result"
	SessionEventKindStatus           SessionEventKind = "status"
	SessionEventKindError            SessionEventKind = "error"
)

type SessionRole string

const (
	SessionRoleUser      SessionRole = "user"
	SessionRoleAssistant SessionRole = "assistant"
	SessionRoleTool      SessionRole = "tool"
	SessionRoleSystem    SessionRole = "system"
)

type Session struct {
	ID        string        `json:"id"`
	NodeID    string        `json:"node_id"`
	Status    SessionStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
	LastSeq   int           `json:"last_seq"`
}

type SessionEvent struct {
	ID          string           `json:"id"`
	SessionID   string           `json:"session_id"`
	Seq         int              `json:"seq"`
	Kind        SessionEventKind `json:"kind"`
	Role        *SessionRole     `json:"role"`
	CallID      *string          `json:"call_id"`
	PayloadJSON map[string]any   `json:"payload_json"`
	CreatedAt   time.Time        `json:"created_at"`
}

func (session *Session) Normalize() {
	if session.Status == "" {
		session.Status = SessionStatusOpen
	}
	if session.LastSeq < 0 {
		session.LastSeq = 0
	}
}

func (event *SessionEvent) Normalize() {
	if event.PayloadJSON == nil {
		event.PayloadJSON = map[string]any{}
	}
}
