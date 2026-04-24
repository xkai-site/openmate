package domain

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

type NodeStatus string

const (
	NodeStatusDraft   NodeStatus = "draft"
	NodeStatusReady   NodeStatus = "ready"
	NodeStatusActive  NodeStatus = "active"
	NodeStatusBlocked NodeStatus = "blocked"
	NodeStatusDone    NodeStatus = "done"
)

type ProcessStatus string

const (
	ProcessStatusTodo ProcessStatus = "todo"
	ProcessStatusDone ProcessStatus = "done"
)

type SessionRange struct {
	StartSessionID string `json:"start_session_id"`
	EndSessionID   string `json:"end_session_id,omitempty"`
	StartEventSeq  *int   `json:"start_event_seq,omitempty"`
	EndEventSeq    *int   `json:"end_event_seq,omitempty"`
}

type ProcessItem struct {
	Name         string        `json:"name"`
	Status       ProcessStatus `json:"status"`
	SessionRange *SessionRange `json:"session_range,omitempty"`
	Memory       map[string]any `json:"memory,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
}

func (sr *SessionRange) Closed() bool {
	return sr.EndSessionID != ""
}

type Topic struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	RootNodeID  string         `json:"root_node_id"`
	Metadata    map[string]any `json:"metadata"`
	Description *string        `json:"description"`
	Tags        []string       `json:"tags"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type Node struct {
	ID          string         `json:"id"`
	TopicID     string         `json:"topic_id"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	ParentID    *string        `json:"parent_id"`
	ChildrenIDs []string       `json:"children_ids"`
	Session     []string       `json:"session"`
	Memory      map[string]any `json:"memory"`
	Input       map[string]any `json:"input"`
	Output      map[string]any `json:"output"`
	Process     []ProcessItem  `json:"process"`
	Status      NodeStatus     `json:"status"`
	Version     int            `json:"version"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type VfsState struct {
	Topics map[string]*Topic `json:"topics"`
	Nodes  map[string]*Node  `json:"nodes"`
}

func NewVfsState() VfsState {
	return VfsState{
		Topics: map[string]*Topic{},
		Nodes:  map[string]*Node{},
	}
}

func (state *VfsState) Normalize() {
	if state.Topics == nil {
		state.Topics = map[string]*Topic{}
	}
	if state.Nodes == nil {
		state.Nodes = map[string]*Node{}
	}
	for _, topic := range state.Topics {
		if topic == nil {
			continue
		}
		topic.Normalize()
	}
	for _, node := range state.Nodes {
		if node == nil {
			continue
		}
		node.Normalize()
	}
}

func (topic *Topic) Normalize() {
	if topic.Metadata == nil {
		topic.Metadata = map[string]any{}
	}
	if topic.Tags == nil {
		topic.Tags = []string{}
	}
}

func (node *Node) Normalize() {
	if node.ChildrenIDs == nil {
		node.ChildrenIDs = []string{}
	}
	if node.Session == nil {
		node.Session = []string{}
	}
	if node.Input == nil {
		node.Input = map[string]any{}
	}
	if node.Output == nil {
		node.Output = map[string]any{}
	}
	if node.Process == nil {
		node.Process = []ProcessItem{}
	}
	for index := range node.Process {
		node.Process[index].Normalize(node.UpdatedAt, node.CreatedAt)
	}
	if node.Version <= 0 {
		node.Version = 1
	}
	if node.Status == "" {
		node.Status = NodeStatusDraft
	}
}

func (item *ProcessItem) Normalize(nodeUpdatedAt, nodeCreatedAt time.Time) {
	item.Name = strings.TrimSpace(item.Name)
	if item.Status != ProcessStatusDone {
		item.Status = ProcessStatusTodo
	}
	if item.Timestamp.IsZero() {
		if !nodeUpdatedAt.IsZero() {
			item.Timestamp = nodeUpdatedAt.UTC()
		} else if !nodeCreatedAt.IsZero() {
			item.Timestamp = nodeCreatedAt.UTC()
		}
	} else {
		item.Timestamp = item.Timestamp.UTC()
	}
	if item.SessionRange != nil {
		item.SessionRange.Normalize()
	}
	if item.Memory == nil {
		item.Memory = nil
	}
}

func (sr *SessionRange) Normalize() {
	if sr.EndSessionID == "" {
		sr.EndSessionID = ""
	}
}

func (node *Node) IsLeaf() bool {
	return len(node.ChildrenIDs) == 0
}

func NewID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	buf := make([]byte, 36)
	hex.Encode(buf[0:8], raw[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], raw[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], raw[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], raw[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], raw[10:16])
	return string(buf)
}
