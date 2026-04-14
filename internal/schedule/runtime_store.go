package schedule

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

var errNotFound = errors.New("not found")

type TopicControlState struct {
	TopicID             string
	QueueLevel          TopicQueueLevel
	PriorityDirty       bool
	PriorityNodeID      *string
	PriorityPlanVersion int
	LastPriorityError   *string
	LastServedAt        *time.Time
	CurrentNodeID       *string
	LastWorkedNodeID    *string
	LastWorkedAt        *time.Time
	SwitchCount         int
	RunningNodeIDs      []string
	UpdatedAt           time.Time
}

type NodeQueueState struct {
	TopicID           string
	NodeID            string
	Name              string
	IsPriorityNode    bool
	PriorityLabel     string
	PriorityRank      int
	Status            NodeStatus
	EnteredPriorityAt time.Time
	LastWorkedAt      *time.Time
	AgentSpec         AgentSpec
	SessionID         *string
	IdempotencyKey    string
	UpdatedAt         time.Time
}

type RuntimeStore struct {
	db  *sql.DB
	now func() time.Time
}

func OpenRuntimeStore(path string, now func() time.Time) (*RuntimeStore, error) {
	if path == "" {
		return nil, ValidationError{Message: "runtime db path is required"}
	}
	absPath := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return nil, fmt.Errorf("create runtime db dir: %w", err)
	}
	db, err := sql.Open("sqlite3", absPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite runtime db: %w", err)
	}
	store := &RuntimeStore{db: db, now: now}
	if store.now == nil {
		store.now = func() time.Time { return time.Now().UTC() }
	}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (store *RuntimeStore) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *RuntimeStore) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS topic_runtime (
			topic_id TEXT PRIMARY KEY,
			queue_level TEXT NOT NULL,
			priority_dirty INTEGER NOT NULL DEFAULT 1,
			priority_node_id TEXT NULL,
			priority_plan_version INTEGER NOT NULL DEFAULT 0,
			last_priority_error TEXT NULL,
			last_served_at TEXT NULL,
			current_node_id TEXT NULL,
			last_worked_node_id TEXT NULL,
			last_worked_at TEXT NULL,
			switch_count INTEGER NOT NULL DEFAULT 0,
			running_node_ids_json TEXT NOT NULL DEFAULT '[]',
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS node_queue (
			topic_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			name TEXT NOT NULL,
			is_priority_node INTEGER NOT NULL DEFAULT 0,
			priority_label TEXT NOT NULL,
			priority_rank INTEGER NOT NULL,
			status TEXT NOT NULL,
			entered_priority_at TEXT NOT NULL,
			last_worked_at TEXT NULL,
			agent_spec_json TEXT NOT NULL DEFAULT '{}',
			session_id TEXT NULL,
			idempotency_key TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL,
			PRIMARY KEY (topic_id, node_id),
			FOREIGN KEY (topic_id) REFERENCES topic_runtime(topic_id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_node_queue_topic ON node_queue(topic_id);`,
		`CREATE INDEX IF NOT EXISTS idx_node_queue_status ON node_queue(status);`,
		`CREATE TABLE IF NOT EXISTS dispatch_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			request_id TEXT NOT NULL,
			topic_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			event_id TEXT NULL,
			status TEXT NOT NULL,
			error_message TEXT NULL,
			retryable INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_dispatch_history_topic_created ON dispatch_history(topic_id, created_at DESC);`,
	}
	for _, statement := range statements {
		if _, err := store.db.Exec(statement); err != nil {
			return fmt.Errorf("migrate schedule runtime db: %w", err)
		}
	}
	return nil
}

func (store *RuntimeStore) EnsureTopic(topicID string) error {
	if topicID == "" {
		return ValidationError{Message: "topic_id must not be empty"}
	}
	now := formatTime(store.now())
	_, err := store.db.Exec(
		`INSERT INTO topic_runtime (topic_id, queue_level, priority_dirty, updated_at)
		 VALUES (?, ?, 1, ?)
		 ON CONFLICT(topic_id) DO NOTHING`,
		topicID,
		string(TopicQueueLevelL0),
		now,
	)
	if err != nil {
		return fmt.Errorf("ensure topic runtime: %w", err)
	}
	return nil
}

func (store *RuntimeStore) UpsertEnqueueNode(request EnqueueRequest) (bool, error) {
	now := store.now()
	if request.TopicID == "" {
		return false, ValidationError{Message: "topic_id must not be empty"}
	}
	if request.NodeID == "" {
		return false, ValidationError{Message: "node_id must not be empty"}
	}
	if request.NodeName == "" {
		return false, ValidationError{Message: "node_name must not be empty"}
	}
	if err := request.Priority.Validate(); err != nil {
		return false, err
	}
	if err := store.EnsureTopic(request.TopicID); err != nil {
		return false, err
	}

	specRaw, err := json.Marshal(request.AgentSpec)
	if err != nil {
		return false, fmt.Errorf("marshal agent spec: %w", err)
	}

	tx, err := store.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin enqueue tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	var existingNodeID string
	err = tx.QueryRow(`SELECT node_id FROM node_queue WHERE topic_id = ? AND node_id = ?`, request.TopicID, request.NodeID).Scan(&existingNodeID)
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("query node existence: %w", err)
	}

	if created {
		_, err = tx.Exec(
			`INSERT INTO node_queue (
				topic_id, node_id, name, is_priority_node, priority_label, priority_rank, status,
				entered_priority_at, last_worked_at, agent_spec_json, session_id, idempotency_key, updated_at
			) VALUES (?, ?, ?, 0, ?, ?, ?, ?, NULL, ?, NULL, ?, ?)`,
			request.TopicID,
			request.NodeID,
			request.NodeName,
			request.Priority.Label,
			request.Priority.Rank,
			string(NodeStatusReady),
			formatTime(now),
			string(specRaw),
			request.IdempotencyKey,
			formatTime(now),
		)
		if err != nil {
			return false, fmt.Errorf("insert node queue: %w", err)
		}
	} else {
		_, err = tx.Exec(
			`UPDATE node_queue
				SET name = ?, agent_spec_json = ?, idempotency_key = ?, updated_at = ?
			  WHERE topic_id = ? AND node_id = ?`,
			request.NodeName,
			string(specRaw),
			request.IdempotencyKey,
			formatTime(now),
			request.TopicID,
			request.NodeID,
		)
		if err != nil {
			return false, fmt.Errorf("update node queue: %w", err)
		}
	}

	if request.MarkPriorityDirty {
		_, err = tx.Exec(
			`UPDATE topic_runtime
				SET priority_dirty = 1, updated_at = ?
			  WHERE topic_id = ?`,
			formatTime(now),
			request.TopicID,
		)
		if err != nil {
			return false, fmt.Errorf("mark topic dirty on enqueue: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit enqueue tx: %w", err)
	}
	tx = nil
	return created, nil
}

func (store *RuntimeStore) SetTopicPriorityNode(topicID, nodeID string) error {
	_, err := store.db.Exec(
		`UPDATE topic_runtime SET priority_node_id = ?, updated_at = ? WHERE topic_id = ?`,
		nodeID,
		formatTime(store.now()),
		topicID,
	)
	if err != nil {
		return fmt.Errorf("set priority node id: %w", err)
	}
	return nil
}

func (store *RuntimeStore) UpsertPriorityNode(topicID, nodeID string, spec AgentSpec) error {
	if topicID == "" || nodeID == "" {
		return ValidationError{Message: "topic_id and node_id are required"}
	}
	if err := store.EnsureTopic(topicID); err != nil {
		return err
	}
	specRaw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("marshal priority node agent spec: %w", err)
	}
	now := formatTime(store.now())
	_, err = store.db.Exec(
		`INSERT INTO node_queue (
			topic_id, node_id, name, is_priority_node, priority_label, priority_rank, status,
			entered_priority_at, last_worked_at, agent_spec_json, session_id, idempotency_key, updated_at
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, NULL, ?, NULL, ?, ?)
		ON CONFLICT(topic_id, node_id) DO UPDATE SET
			name = excluded.name,
			is_priority_node = 1,
			priority_label = excluded.priority_label,
			priority_rank = excluded.priority_rank,
			agent_spec_json = excluded.agent_spec_json,
			updated_at = excluded.updated_at`,
		topicID,
		nodeID,
		PriorityNodeName,
		PriorityNodeLabel,
		0,
		string(NodeStatusReady),
		now,
		string(specRaw),
		"priority-node",
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert priority node: %w", err)
	}
	if err := store.SetTopicPriorityNode(topicID, nodeID); err != nil {
		return err
	}
	return nil
}

func (store *RuntimeStore) MarkPriorityNodeReady(topicID string) error {
	topic, err := store.GetTopic(topicID)
	if err != nil {
		return err
	}
	if topic.PriorityNodeID == nil {
		return ValidationError{Message: "priority node is not set"}
	}
	now := formatTime(store.now())
	_, err = store.db.Exec(
		`UPDATE node_queue
			SET status = ?, priority_rank = ?, priority_label = ?, entered_priority_at = ?, updated_at = ?
		  WHERE topic_id = ? AND node_id = ?`,
		string(NodeStatusReady),
		0,
		PriorityNodeLabel,
		now,
		now,
		topicID,
		*topic.PriorityNodeID,
	)
	if err != nil {
		return fmt.Errorf("mark priority node ready: %w", err)
	}
	return nil
}

func (store *RuntimeStore) ClearPriorityDirty(topicID string, lastErr *string) error {
	_, err := store.db.Exec(
		`UPDATE topic_runtime
			SET priority_dirty = 0, last_priority_error = ?, updated_at = ?
		  WHERE topic_id = ?`,
		nullableString(lastErr),
		formatTime(store.now()),
		topicID,
	)
	if err != nil {
		return fmt.Errorf("clear priority dirty: %w", err)
	}
	return nil
}

func (store *RuntimeStore) ApplyPriorityPlan(topicID string, assignments []WorkerPriorityAssignment) error {
	tx, err := store.db.Begin()
	if err != nil {
		return fmt.Errorf("begin apply priority plan tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	now := formatTime(store.now())
	for _, assignment := range assignments {
		if assignment.NodeID == "" {
			continue
		}
		_, err = tx.Exec(
			`UPDATE node_queue
				SET priority_label = ?, priority_rank = ?, entered_priority_at = ?, updated_at = ?
			  WHERE topic_id = ? AND node_id = ? AND is_priority_node = 0`,
			assignment.Label,
			assignment.Rank,
			now,
			now,
			topicID,
			assignment.NodeID,
		)
		if err != nil {
			return fmt.Errorf("update priority assignment for %s: %w", assignment.NodeID, err)
		}
	}
	_, err = tx.Exec(
		`UPDATE topic_runtime
			SET priority_dirty = 0, last_priority_error = NULL, priority_plan_version = priority_plan_version + 1, updated_at = ?
		  WHERE topic_id = ?`,
		now,
		topicID,
	)
	if err != nil {
		return fmt.Errorf("update topic runtime priority version: %w", err)
	}
	topic, err := store.getTopicTx(tx, topicID)
	if err != nil {
		return err
	}
	if topic.PriorityNodeID != nil {
		_, err = tx.Exec(
			`UPDATE node_queue SET status = ?, updated_at = ? WHERE topic_id = ? AND node_id = ?`,
			string(NodeStatusSucceeded),
			now,
			topicID,
			*topic.PriorityNodeID,
		)
		if err != nil {
			return fmt.Errorf("mark priority node succeeded: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit priority plan tx: %w", err)
	}
	tx = nil
	return nil
}

func (store *RuntimeStore) ListTopics() ([]TopicControlState, error) {
	rows, err := store.db.Query(
		`SELECT topic_id, queue_level, priority_dirty, priority_node_id, priority_plan_version, last_priority_error,
		        last_served_at, current_node_id, last_worked_node_id, last_worked_at, switch_count, running_node_ids_json, updated_at
		   FROM topic_runtime`,
	)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	defer rows.Close()

	result := []TopicControlState{}
	for rows.Next() {
		topic, err := scanTopic(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, topic)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics: %w", err)
	}
	return result, nil
}

func (store *RuntimeStore) GetTopic(topicID string) (TopicControlState, error) {
	row := store.db.QueryRow(
		`SELECT topic_id, queue_level, priority_dirty, priority_node_id, priority_plan_version, last_priority_error,
		        last_served_at, current_node_id, last_worked_node_id, last_worked_at, switch_count, running_node_ids_json, updated_at
		   FROM topic_runtime
		  WHERE topic_id = ?`,
		topicID,
	)
	topic, err := scanTopic(row)
	if err != nil {
		return TopicControlState{}, err
	}
	return topic, nil
}

func (store *RuntimeStore) getTopicTx(tx *sql.Tx, topicID string) (TopicControlState, error) {
	row := tx.QueryRow(
		`SELECT topic_id, queue_level, priority_dirty, priority_node_id, priority_plan_version, last_priority_error,
		        last_served_at, current_node_id, last_worked_node_id, last_worked_at, switch_count, running_node_ids_json, updated_at
		   FROM topic_runtime
		  WHERE topic_id = ?`,
		topicID,
	)
	topic, err := scanTopic(row)
	if err != nil {
		return TopicControlState{}, err
	}
	return topic, nil
}

func (store *RuntimeStore) BuildTopicSnapshot(topicID string) (TopicSnapshot, error) {
	topic, err := store.GetTopic(topicID)
	if err != nil {
		return TopicSnapshot{}, err
	}
	nodes, err := store.ListNodes(topicID)
	if err != nil {
		return TopicSnapshot{}, err
	}
	converted := make([]TopicNode, 0, len(nodes))
	for _, node := range nodes {
		converted = append(converted, TopicNode{
			NodeID:            node.NodeID,
			Name:              node.Name,
			Priority:          NodePriority{Label: node.PriorityLabel, Rank: node.PriorityRank},
			Status:            node.Status,
			EnteredPriorityAt: node.EnteredPriorityAt,
			LastWorkedAt:      node.LastWorkedAt,
		})
	}
	snapshot := TopicSnapshot{
		TopicID:    topic.TopicID,
		QueueLevel: topic.QueueLevel,
		Nodes:      converted,
		Runtime: TopicRuntimeState{
			TopicID:          topic.TopicID,
			ActivePriority:   nil,
			CurrentNodeID:    cloneString(topic.CurrentNodeID),
			RunningNodeIDs:   append([]string{}, topic.RunningNodeIDs...),
			LastWorkedNodeID: cloneString(topic.LastWorkedNodeID),
			LastWorkedAt:     cloneTime(topic.LastWorkedAt),
			SwitchCount:      topic.SwitchCount,
			PriorityDirty:    topic.PriorityDirty,
		},
	}
	snapshot.Normalize()
	if err := snapshot.Validate(); err != nil {
		return TopicSnapshot{}, err
	}
	return snapshot, nil
}

func (store *RuntimeStore) ListNodes(topicID string) ([]NodeQueueState, error) {
	rows, err := store.db.Query(
		`SELECT topic_id, node_id, name, is_priority_node, priority_label, priority_rank, status,
		        entered_priority_at, last_worked_at, agent_spec_json, session_id, idempotency_key, updated_at
		   FROM node_queue
		  WHERE topic_id = ?`,
		topicID,
	)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	nodes := []NodeQueueState{}
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return nodes, nil
}

func (store *RuntimeStore) GetNode(topicID, nodeID string) (NodeQueueState, error) {
	row := store.db.QueryRow(
		`SELECT topic_id, node_id, name, is_priority_node, priority_label, priority_rank, status,
		        entered_priority_at, last_worked_at, agent_spec_json, session_id, idempotency_key, updated_at
		   FROM node_queue
		  WHERE topic_id = ? AND node_id = ?`,
		topicID,
		nodeID,
	)
	return scanNode(row)
}

func (store *RuntimeStore) SetNodeSessionID(topicID, nodeID, sessionID string) error {
	_, err := store.db.Exec(
		`UPDATE node_queue SET session_id = ?, updated_at = ? WHERE topic_id = ? AND node_id = ?`,
		sessionID,
		formatTime(store.now()),
		topicID,
		nodeID,
	)
	if err != nil {
		return fmt.Errorf("set node session id: %w", err)
	}
	return nil
}

func (store *RuntimeStore) MarkNodeRunning(topicID, nodeID string) error {
	tx, err := store.db.Begin()
	if err != nil {
		return fmt.Errorf("begin mark running tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	now := formatTime(store.now())
	_, err = tx.Exec(
		`UPDATE node_queue SET status = ?, updated_at = ? WHERE topic_id = ? AND node_id = ?`,
		string(NodeStatusRunning),
		now,
		topicID,
		nodeID,
	)
	if err != nil {
		return fmt.Errorf("update running node status: %w", err)
	}
	if err := store.syncTopicRunningNodeIDsTx(tx, topicID, nodeID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark running tx: %w", err)
	}
	tx = nil
	return nil
}

func (store *RuntimeStore) MarkNodeFinished(topicID, nodeID string, status NodeStatus) error {
	tx, err := store.db.Begin()
	if err != nil {
		return fmt.Errorf("begin mark finished tx: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	now := store.now()
	nowText := formatTime(now)
	_, err = tx.Exec(
		`UPDATE node_queue
			SET status = ?, last_worked_at = ?, updated_at = ?
		  WHERE topic_id = ? AND node_id = ?`,
		string(status),
		nowText,
		nowText,
		topicID,
		nodeID,
	)
	if err != nil {
		return fmt.Errorf("update finished node status: %w", err)
	}
	if err := store.syncTopicRunningNodeIDsTx(tx, topicID, ""); err != nil {
		return err
	}
	currentNode, err := store.getTopicCurrentNodeTx(tx, topicID)
	if err != nil {
		return err
	}
	switchCountAdd := 0
	if currentNode == nil || *currentNode != nodeID {
		switchCountAdd = 1
	}
	_, err = tx.Exec(
		`UPDATE topic_runtime
			SET last_worked_node_id = ?, last_worked_at = ?, current_node_id = ?, switch_count = switch_count + ?, updated_at = ?
		  WHERE topic_id = ?`,
		nodeID,
		nowText,
		nodeID,
		switchCountAdd,
		nowText,
		topicID,
	)
	if err != nil {
		return fmt.Errorf("update topic worked pointers: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit mark finished tx: %w", err)
	}
	tx = nil
	return nil
}

func (store *RuntimeStore) RecordDispatch(record DispatchRecord) error {
	_, err := store.db.Exec(
		`INSERT INTO dispatch_history (
			request_id, topic_id, node_id, event_id, status, error_message, retryable, duration_ms, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RequestID,
		record.TopicID,
		record.NodeID,
		nullableString(stringPtr(record.EventID)),
		record.Status,
		nullableString(stringPtr(record.Error)),
		boolToInt(record.Retryable),
		record.DurationMS,
		formatTime(store.now()),
	)
	if err != nil {
		return fmt.Errorf("insert dispatch history: %w", err)
	}
	return nil
}

func (store *RuntimeStore) SetTopicQueueLevel(topicID string, level TopicQueueLevel) error {
	if err := level.Validate(); err != nil {
		return err
	}
	_, err := store.db.Exec(
		`UPDATE topic_runtime SET queue_level = ?, updated_at = ? WHERE topic_id = ?`,
		string(level),
		formatTime(store.now()),
		topicID,
	)
	if err != nil {
		return fmt.Errorf("set topic queue level: %w", err)
	}
	return nil
}

func (store *RuntimeStore) TouchTopicServed(topicID string) error {
	_, err := store.db.Exec(
		`UPDATE topic_runtime SET last_served_at = ?, updated_at = ? WHERE topic_id = ?`,
		formatTime(store.now()),
		formatTime(store.now()),
		topicID,
	)
	if err != nil {
		return fmt.Errorf("touch topic served: %w", err)
	}
	return nil
}

func (store *RuntimeStore) PromoteTopic(topicID string) error {
	topic, err := store.GetTopic(topicID)
	if err != nil {
		return err
	}
	level := promoteLevel(topic.QueueLevel)
	if level == topic.QueueLevel {
		return nil
	}
	return store.SetTopicQueueLevel(topicID, level)
}

func (store *RuntimeStore) DemoteTopic(topicID string) error {
	topic, err := store.GetTopic(topicID)
	if err != nil {
		return err
	}
	level := demoteLevel(topic.QueueLevel)
	if level == topic.QueueLevel {
		return nil
	}
	return store.SetTopicQueueLevel(topicID, level)
}

func (store *RuntimeStore) HasRunnableNodes(topicID string) (bool, error) {
	row := store.db.QueryRow(
		`SELECT COUNT(1)
		   FROM node_queue
		  WHERE topic_id = ?
		    AND status NOT IN (?, ?, ?)
		    AND status NOT IN (?, ?, ?)`,
		topicID,
		string(NodeStatusSucceeded),
		string(NodeStatusFailed),
		string(NodeStatusCancelled),
		string(NodeStatusBlocked),
		string(NodeStatusRetryCooldown),
		string(NodeStatusWaitingExternal),
	)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, fmt.Errorf("query runnable nodes count: %w", err)
	}
	return count > 0, nil
}

func (store *RuntimeStore) EnsurePriorityDirty(topicID string, reason string) error {
	_, err := store.db.Exec(
		`UPDATE topic_runtime
			SET priority_dirty = 1, last_priority_error = ?, updated_at = ?
		  WHERE topic_id = ?`,
		reason,
		formatTime(store.now()),
		topicID,
	)
	if err != nil {
		return fmt.Errorf("set topic priority dirty: %w", err)
	}
	return nil
}

func (store *RuntimeStore) PromoteAgedTopics(threshold time.Duration) error {
	if threshold <= 0 {
		return nil
	}
	topics, err := store.ListTopics()
	if err != nil {
		return err
	}
	now := store.now()
	for _, topic := range topics {
		if topic.LastServedAt == nil {
			continue
		}
		if now.Sub(*topic.LastServedAt) < threshold {
			continue
		}
		if err := store.PromoteTopic(topic.TopicID); err != nil {
			return err
		}
	}
	return nil
}

func (store *RuntimeStore) syncTopicRunningNodeIDsTx(tx *sql.Tx, topicID, currentNodeID string) error {
	rows, err := tx.Query(
		`SELECT node_id FROM node_queue WHERE topic_id = ? AND status = ?`,
		topicID,
		string(NodeStatusRunning),
	)
	if err != nil {
		return fmt.Errorf("query running node ids: %w", err)
	}
	defer rows.Close()
	running := []string{}
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			return fmt.Errorf("scan running node id: %w", err)
		}
		running = append(running, nodeID)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate running node ids: %w", err)
	}
	payload, err := json.Marshal(running)
	if err != nil {
		return fmt.Errorf("marshal running node ids: %w", err)
	}
	now := formatTime(store.now())
	var current any
	if currentNodeID != "" {
		current = currentNodeID
	}
	_, err = tx.Exec(
		`UPDATE topic_runtime SET running_node_ids_json = ?, current_node_id = COALESCE(?, current_node_id), updated_at = ? WHERE topic_id = ?`,
		string(payload),
		current,
		now,
		topicID,
	)
	if err != nil {
		return fmt.Errorf("update running node ids: %w", err)
	}
	return nil
}

func (store *RuntimeStore) getTopicCurrentNodeTx(tx *sql.Tx, topicID string) (*string, error) {
	row := tx.QueryRow(`SELECT current_node_id FROM topic_runtime WHERE topic_id = ?`, topicID)
	var current sql.NullString
	if err := row.Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errNotFound
		}
		return nil, fmt.Errorf("scan current node id: %w", err)
	}
	if !current.Valid || strings.TrimSpace(current.String) == "" {
		return nil, nil
	}
	value := current.String
	return &value, nil
}

func scanTopic(scanner interface {
	Scan(dest ...any) error
}) (TopicControlState, error) {
	var (
		topicID             string
		queueLevelRaw       string
		priorityDirtyRaw    int
		priorityNodeIDRaw   sql.NullString
		priorityPlanVersion int
		lastPriorityErrRaw  sql.NullString
		lastServedAtRaw     sql.NullString
		currentNodeIDRaw    sql.NullString
		lastWorkedNodeIDRaw sql.NullString
		lastWorkedAtRaw     sql.NullString
		switchCount         int
		runningRaw          string
		updatedAtRaw        string
	)
	if err := scanner.Scan(
		&topicID,
		&queueLevelRaw,
		&priorityDirtyRaw,
		&priorityNodeIDRaw,
		&priorityPlanVersion,
		&lastPriorityErrRaw,
		&lastServedAtRaw,
		&currentNodeIDRaw,
		&lastWorkedNodeIDRaw,
		&lastWorkedAtRaw,
		&switchCount,
		&runningRaw,
		&updatedAtRaw,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return TopicControlState{}, errNotFound
		}
		return TopicControlState{}, fmt.Errorf("scan topic runtime: %w", err)
	}
	level := TopicQueueLevel(queueLevelRaw)
	if err := level.Validate(); err != nil {
		return TopicControlState{}, err
	}
	updatedAt, err := parseTimeString(updatedAtRaw)
	if err != nil {
		return TopicControlState{}, err
	}
	running := []string{}
	if err := json.Unmarshal([]byte(runningRaw), &running); err != nil {
		return TopicControlState{}, fmt.Errorf("decode running_node_ids_json: %w", err)
	}
	return TopicControlState{
		TopicID:             topicID,
		QueueLevel:          level,
		PriorityDirty:       priorityDirtyRaw == 1,
		PriorityNodeID:      nullableToStringPtr(priorityNodeIDRaw),
		PriorityPlanVersion: priorityPlanVersion,
		LastPriorityError:   nullableToStringPtr(lastPriorityErrRaw),
		LastServedAt:        nullableToTime(lastServedAtRaw),
		CurrentNodeID:       nullableToStringPtr(currentNodeIDRaw),
		LastWorkedNodeID:    nullableToStringPtr(lastWorkedNodeIDRaw),
		LastWorkedAt:        nullableToTime(lastWorkedAtRaw),
		SwitchCount:         switchCount,
		RunningNodeIDs:      running,
		UpdatedAt:           updatedAt,
	}, nil
}

func scanNode(scanner interface {
	Scan(dest ...any) error
}) (NodeQueueState, error) {
	var (
		node              NodeQueueState
		isPriorityNodeRaw int
		statusRaw         string
		enteredRaw        string
		lastWorkedAtRaw   sql.NullString
		specRaw           string
		sessionIDRaw      sql.NullString
		updatedAtRaw      string
	)
	if err := scanner.Scan(
		&node.TopicID,
		&node.NodeID,
		&node.Name,
		&isPriorityNodeRaw,
		&node.PriorityLabel,
		&node.PriorityRank,
		&statusRaw,
		&enteredRaw,
		&lastWorkedAtRaw,
		&specRaw,
		&sessionIDRaw,
		&node.IdempotencyKey,
		&updatedAtRaw,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeQueueState{}, errNotFound
		}
		return NodeQueueState{}, fmt.Errorf("scan node queue: %w", err)
	}
	node.IsPriorityNode = isPriorityNodeRaw == 1
	node.Status = NodeStatus(statusRaw)
	enteredAt, parseErr := parseTimeString(enteredRaw)
	if parseErr != nil {
		return NodeQueueState{}, fmt.Errorf("parse node entered_priority_at: %w", parseErr)
	}
	node.EnteredPriorityAt = enteredAt
	node.LastWorkedAt = nullableToTime(lastWorkedAtRaw)
	if err := json.Unmarshal([]byte(specRaw), &node.AgentSpec); err != nil {
		return NodeQueueState{}, fmt.Errorf("decode node agent_spec_json: %w", err)
	}
	node.SessionID = nullableToStringPtr(sessionIDRaw)
	updatedAt, parseErr := parseTimeString(updatedAtRaw)
	if parseErr != nil {
		return NodeQueueState{}, fmt.Errorf("parse node updated_at: %w", parseErr)
	}
	node.UpdatedAt = updatedAt
	return node, nil
}

func nullableToTime(raw sql.NullString) *time.Time {
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return nil
	}
	value, err := parseTimeString(raw.String)
	if err != nil {
		return nil
	}
	return &value
}

func parseTimeString(raw string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", raw, err)
	}
	return value.UTC(), nil
}

func nullableToStringPtr(raw sql.NullString) *string {
	if !raw.Valid {
		return nil
	}
	text := strings.TrimSpace(raw.String)
	if text == "" {
		return nil
	}
	return &text
}

func nullableString(raw *string) any {
	if raw == nil {
		return nil
	}
	if strings.TrimSpace(*raw) == "" {
		return nil
	}
	return *raw
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func promoteLevel(level TopicQueueLevel) TopicQueueLevel {
	switch level {
	case TopicQueueLevelL3:
		return TopicQueueLevelL2
	case TopicQueueLevelL2:
		return TopicQueueLevelL1
	case TopicQueueLevelL1:
		return TopicQueueLevelL0
	default:
		return TopicQueueLevelL0
	}
}

func demoteLevel(level TopicQueueLevel) TopicQueueLevel {
	switch level {
	case TopicQueueLevelL0:
		return TopicQueueLevelL1
	case TopicQueueLevelL1:
		return TopicQueueLevelL2
	case TopicQueueLevelL2:
		return TopicQueueLevelL3
	default:
		return TopicQueueLevelL3
	}
}
