package domain

type ContextSessionHistory struct {
	Session *Session        `json:"session"`
	Events  []*SessionEvent `json:"events"`
}

type ProcessContext struct {
	Name          string          `json:"name"`
	Status        ProcessStatus   `json:"status"`
	Summary       map[string]any  `json:"summary,omitempty"`
	SessionEvents []*SessionEvent `json:"session_events,omitempty"`
}

type ContextSnapshot struct {
	NodeID          string                  `json:"node_id"`
	UserMemory      map[string]any          `json:"user_memory"`
	TopicMemory     map[string]any          `json:"topic_memory"`
	NodeMemory      map[string]any          `json:"node_memory"`
	GlobalIndex     any                     `json:"global_index"`
	SessionHistory  []ContextSessionHistory `json:"session_history"`
	ProcessContexts []ProcessContext        `json:"process_contexts"`
}
