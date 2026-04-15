package domain

type ContextSessionHistory struct {
	Session *Session        `json:"session"`
	Events  []*SessionEvent `json:"events"`
}

type ContextSnapshot struct {
	NodeID         string                  `json:"node_id"`
	UserMemory     map[string]any          `json:"user_memory"`
	TopicMemory    map[string]any          `json:"topic_memory"`
	NodeMemory     map[string]any          `json:"node_memory"`
	GlobalIndex    any                     `json:"global_index"`
	SessionHistory []ContextSessionHistory `json:"session_history"`
}
