package schedule

import "time"

const (
	PriorityNodeName  = "__priority__"
	PriorityNodeLabel = "priority"

	BusinessNodePriorityLabel = "interactive"
	BusinessNodePriorityRank  = 1
)

type AgentSpec struct {
	Mode          string `json:"mode,omitempty"`
	WorkspaceRoot string `json:"workspace_root,omitempty"`

	PoolDBFile      string `json:"pool_db_file,omitempty"`
	PoolModelConfig string `json:"pool_model_config,omitempty"`
	PoolBinary      string `json:"pool_binary,omitempty"`

	VOSStateFile    string `json:"vos_state_file,omitempty"`
	VOSSessionDB    string `json:"vos_session_db,omitempty"`
	VOSBinary       string `json:"vos_binary,omitempty"`
	UseSessionEvent bool   `json:"use_session_event,omitempty"`
}

type EnqueueRequest struct {
	TopicID           string       `json:"topic_id"`
	NodeID            string       `json:"node_id"`
	NodeName          string       `json:"node_name"`
	SessionID         string       `json:"session_id,omitempty"`
	AgentSpec         AgentSpec    `json:"agent_spec"`
	Priority          NodePriority `json:"priority"`
	IdempotencyKey    string       `json:"idempotency_key,omitempty"`
	MarkPriorityDirty bool         `json:"mark_priority_dirty"`
}

type EnqueueResult struct {
	TopicID        string `json:"topic_id"`
	NodeID         string `json:"node_id"`
	Created        bool   `json:"created"`
	PriorityDirty  bool   `json:"priority_dirty"`
	PriorityNodeID string `json:"priority_node_id,omitempty"`
	QueueLevel     string `json:"queue_level"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

type WorkerCandidateNode struct {
	NodeID            string       `json:"node_id"`
	Name              string       `json:"name"`
	Status            NodeStatus   `json:"status"`
	CurrentPriority   NodePriority `json:"current_priority"`
	EnteredPriorityAt time.Time    `json:"entered_priority_at"`
	LastWorkedAt      *time.Time   `json:"last_worked_at,omitempty"`
}

type WorkerExecuteRequest struct {
	RequestID   string    `json:"request_id"`
	TopicID     string    `json:"topic_id"`
	NodeID      string    `json:"node_id"`
	NodeName    string    `json:"node_name"`
	NodeKind    string    `json:"node_kind"`
	AgentSpec   AgentSpec `json:"agent_spec"`
	SessionID   string    `json:"session_id,omitempty"`
	EventID     string    `json:"event_id,omitempty"`
	TimeoutMS   int       `json:"timeout_ms"`
	CancelToken string    `json:"cancel_token,omitempty"`

	PriorityCandidates []WorkerCandidateNode `json:"priority_candidates,omitempty"`
}

type WorkerPriorityAssignment struct {
	NodeID string `json:"node_id"`
	Label  string `json:"label"`
	Rank   int    `json:"rank"`
}

type WorkerExecuteResponse struct {
	RequestID      string                     `json:"request_id"`
	TopicID        string                     `json:"topic_id"`
	NodeID         string                     `json:"node_id"`
	SessionID      string                     `json:"session_id,omitempty"`
	EventID        string                     `json:"event_id,omitempty"`
	Status         string                     `json:"status"`
	NextNodeStatus *NodeStatus                `json:"next_node_status,omitempty"`
	Output         string                     `json:"output,omitempty"`
	Error          string                     `json:"error,omitempty"`
	Retryable      bool                       `json:"retryable"`
	DurationMS     int64                      `json:"duration_ms"`
	PriorityPlan   []WorkerPriorityAssignment `json:"priority_plan,omitempty"`
}

type DispatchRecord struct {
	RequestID  string `json:"request_id"`
	TopicID    string `json:"topic_id"`
	NodeID     string `json:"node_id"`
	NodeKind   string `json:"node_kind"`
	SessionID  string `json:"session_id,omitempty"`
	EventID    string `json:"event_id,omitempty"`
	Status     string `json:"status"`
	Retryable  bool   `json:"retryable"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type TickResult struct {
	SelectedTopicID string           `json:"selected_topic_id,omitempty"`
	QueueLevel      string           `json:"queue_level,omitempty"`
	Dispatched      []DispatchRecord `json:"dispatched"`
	Reasons         []string         `json:"reasons"`
}

func (result *TickResult) Normalize() {
	if result.Dispatched == nil {
		result.Dispatched = []DispatchRecord{}
	}
	if result.Reasons == nil {
		result.Reasons = []string{}
	}
}
