package domain

import "time"

type SessionStatus string

const (
	SessionStatusActive    SessionStatus = "active"
	SessionStatusWaiting   SessionStatus = "waiting"
	SessionStatusCompleted SessionStatus = "completed"
	SessionStatusFailed    SessionStatus = "failed"
)

type SessionRole string

const (
	SessionRoleUser      SessionRole = "user"
	SessionRoleAssistant SessionRole = "assistant"
	SessionRoleTool      SessionRole = "tool"
	SessionRoleSystem    SessionRole = "system"
)

const (
	SessionItemTypeMessage            = "message"
	SessionItemTypeFunctionCall       = "function_call"
	SessionItemTypeFunctionCallOutput = "function_call_output"
	SessionItemTypeReasoning          = "reasoning"
	SessionItemTypeWebSearchCall      = "web_search_call"
	SessionItemTypeFileSearchCall     = "file_search_call"
	SessionItemTypeComputerCall       = "computer_call"
	SessionItemTypeMCPCall            = "mcp_call"
	SessionItemTypeMCPListTools       = "mcp_list_tools"
	SessionItemTypeMCPApprovalRequest = "mcp_approval_request"
	SessionItemTypeImageGeneration    = "image_generation_call"
	SessionItemTypeCodeInterpreter    = "code_interpreter_call"
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
	ID             string         `json:"id"`
	SessionID      string         `json:"session_id"`
	Seq            int            `json:"seq"`
	ItemType       string         `json:"item_type"`
	ProviderItemID *string        `json:"provider_item_id,omitempty"`
	Role           *SessionRole   `json:"role,omitempty"`
	CallID         *string        `json:"call_id,omitempty"`
	PayloadJSON    map[string]any `json:"payload_json"`
	CreatedAt      time.Time      `json:"created_at"`
}

func (session *Session) Normalize() {
	if session.Status == "" {
		session.Status = SessionStatusActive
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

func IsToolSessionItemType(itemType string) bool {
	return itemType == SessionItemTypeFunctionCall || itemType == SessionItemTypeFunctionCallOutput
}
