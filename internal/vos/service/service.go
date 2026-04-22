package service

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"vos/internal/vos/domain"
	"vos/internal/vos/store"
)

const childMemoryCacheKey = "_child_memory_cache"

type Service struct {
	store        store.StateStore
	sessionStore store.SessionStore
}

type CreateTopicInput struct {
	TopicID      string
	Name         string
	Description  *string
	Metadata     map[string]any
	Tags         []string
	RootNodeID   string
	RootNodeName *string
}

type UpdateTopicInput struct {
	TopicID          string
	Name             *string
	Description      *string
	ClearDescription bool
	Metadata         map[string]any
	ReplaceMetadata  bool
	Tags             []string
	ReplaceTags      bool
}

type DeleteTopicResult struct {
	Topic          *domain.Topic `json:"topic"`
	DeletedNodeIDs []string      `json:"deleted_node_ids"`
}

type CreateNodeInput struct {
	TopicID     string
	Name        string
	ParentID    *string
	NodeID      string
	Description *string
	Status      domain.NodeStatus
	Memory      map[string]any
	Input       map[string]any
	Output      map[string]any
}

type UpdateNodeInput struct {
	NodeID           string
	ExpectedVersion  *int
	Name             *string
	Description      *string
	ClearDescription bool
	Status           *domain.NodeStatus
	Memory           map[string]any
	Input            map[string]any
	Output           map[string]any
	SessionIDs       []string
	Progress         []string
}

type NodeListFilter struct {
	LeafOnly        bool
	Statuses        []domain.NodeStatus
	ExcludeStatuses []domain.NodeStatus
}

func New(stateStore store.StateStore) *Service {
	return &Service{store: stateStore}
}

func NewWithSessionStore(stateStore store.StateStore, sessionStore store.SessionStore) *Service {
	return &Service{
		store:        stateStore,
		sessionStore: sessionStore,
	}
}

func (service *Service) CreateTopic(input CreateTopicInput) (*domain.Topic, *domain.Node, error) {
	if input.Name == "" {
		return nil, nil, domain.ValidationError{Message: "topic name is required"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, nil, err
	}

	topicID := input.TopicID
	if topicID == "" {
		topicID = domain.NewID()
	}
	rootNodeID := input.RootNodeID
	if rootNodeID == "" {
		rootNodeID = fmt.Sprintf("%s:root", topicID)
	}

	if _, exists := state.Topics[topicID]; exists {
		return nil, nil, domain.DuplicateEntityError{Kind: "topic", ID: topicID}
	}
	if _, exists := state.Nodes[rootNodeID]; exists {
		return nil, nil, domain.DuplicateEntityError{Kind: "node", ID: rootNodeID}
	}

	now := time.Now().UTC()
	rootName := input.Name
	if input.RootNodeName != nil {
		rootName = *input.RootNodeName
	}

	topic := &domain.Topic{
		ID:          topicID,
		Name:        input.Name,
		RootNodeID:  rootNodeID,
		Metadata:    cloneMap(input.Metadata),
		Description: cloneStringPtr(input.Description),
		Tags:        cloneStrings(input.Tags),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	topic.Normalize()

	node := &domain.Node{
		ID:          rootNodeID,
		TopicID:     topicID,
		Name:        rootName,
		Description: cloneStringPtr(input.Description),
		ParentID:    nil,
		ChildrenIDs: []string{},
		Session:     []string{},
		Memory:      nil,
		Input:       map[string]any{},
		Output:      map[string]any{},
		Progress:    []string{},
		Status:      domain.NodeStatusReady,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	node.Normalize()

	state.Topics[topic.ID] = topic
	state.Nodes[node.ID] = node
	if err := service.store.Save(state); err != nil {
		return nil, nil, err
	}
	return cloneTopic(topic), cloneNode(node), nil
}

func (service *Service) UpdateTopic(input UpdateTopicInput) (*domain.Topic, error) {
	if input.TopicID == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}
	if input.Name != nil && *input.Name == "" {
		return nil, domain.ValidationError{Message: "topic name cannot be empty"}
	}
	if input.Description != nil && input.ClearDescription {
		return nil, domain.ValidationError{Message: "description and clear-description cannot be used together"}
	}
	if !hasTopicChanges(input) {
		return nil, domain.ValidationError{Message: "at least one topic field must be updated"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, input.TopicID)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		topic.Name = *input.Name
	}
	if input.Description != nil {
		topic.Description = cloneStringPtr(input.Description)
	}
	if input.ClearDescription {
		topic.Description = nil
	}
	if input.ReplaceMetadata {
		topic.Metadata = cloneMap(input.Metadata)
	}
	if input.ReplaceTags {
		topic.Tags = cloneStrings(input.Tags)
	}

	touchTopic(topic)
	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return cloneTopic(topic), nil
}

func (service *Service) DeleteTopic(topicID string) (*DeleteTopicResult, error) {
	if topicID == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, topicID)
	if err != nil {
		return nil, err
	}

	deletedNodeIDs := nodeIDsForTopic(state, topicID)
	deletedTopic := cloneTopic(topic)
	for _, nodeID := range deletedNodeIDs {
		delete(state.Nodes, nodeID)
	}
	delete(state.Topics, topicID)

	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return &DeleteTopicResult{
		Topic:          deletedTopic,
		DeletedNodeIDs: deletedNodeIDs,
	}, nil
}

func (service *Service) ListTopics() ([]*domain.Topic, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}

	topics := make([]*domain.Topic, 0, len(state.Topics))
	for _, topic := range state.Topics {
		topics = append(topics, cloneTopic(topic))
	}
	sort.Slice(topics, func(i, j int) bool {
		if topics[i].CreatedAt.Equal(topics[j].CreatedAt) {
			return topics[i].ID < topics[j].ID
		}
		return topics[i].CreatedAt.Before(topics[j].CreatedAt)
	})
	return topics, nil
}

func (service *Service) GetTopic(topicID string) (*domain.Topic, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, topicID)
	if err != nil {
		return nil, err
	}
	return cloneTopic(topic), nil
}

func (service *Service) ListDisplayRootNodes() ([]*domain.Node, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}

	roots := make([]*domain.Node, 0)
	for _, node := range state.Nodes {
		if node == nil || node.ParentID != nil {
			continue
		}
		roots = append(roots, cloneNode(node))
	}

	sort.Slice(roots, func(i, j int) bool {
		if roots[i].UpdatedAt.Equal(roots[j].UpdatedAt) {
			return roots[i].CreatedAt.After(roots[j].CreatedAt)
		}
		return roots[i].UpdatedAt.After(roots[j].UpdatedAt)
	})
	return roots, nil
}

func (service *Service) CreateNode(input CreateNodeInput) (*domain.Node, error) {
	if input.Name == "" {
		return nil, domain.ValidationError{Message: "node name is required"}
	}
	input.TopicID = strings.TrimSpace(input.TopicID)
	if input.ParentID != nil {
		trimmedParentID := strings.TrimSpace(*input.ParentID)
		if trimmedParentID == "" {
			input.ParentID = nil
		} else {
			input.ParentID = &trimmedParentID
		}
	}
	if input.Status == "" {
		input.Status = domain.NodeStatusDraft
	}
	if _, err := domain.ParseNodeStatus(string(input.Status)); err != nil {
		return nil, err
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}

	if input.TopicID == "" && input.ParentID != nil && *input.ParentID != "" {
		parent, err := requireNode(state, *input.ParentID)
		if err != nil {
			return nil, err
		}
		input.TopicID = parent.TopicID
	}
	if input.TopicID == "" {
		topic, _, err := service.CreateTopic(CreateTopicInput{
			Name: inferAutoTopicName(input.Name),
		})
		if err != nil {
			return nil, err
		}
		input.TopicID = topic.ID
		state, err = service.store.Load()
		if err != nil {
			return nil, err
		}
	}

	topic, err := requireTopic(state, input.TopicID)
	if err != nil {
		return nil, err
	}

	parentID := topic.RootNodeID
	if input.ParentID != nil {
		parentID = *input.ParentID
	}
	parent, err := requireNode(state, parentID)
	if err != nil {
		return nil, err
	}
	if parent.TopicID != input.TopicID {
		return nil, domain.InvalidTreeError{Message: "parent node belongs to a different topic"}
	}

	nodeID := input.NodeID
	if nodeID == "" {
		nodeID = domain.NewID()
	}
	if _, exists := state.Nodes[nodeID]; exists {
		return nil, domain.DuplicateEntityError{Kind: "node", ID: nodeID}
	}

	now := time.Now().UTC()
	node := &domain.Node{
		ID:          nodeID,
		TopicID:     input.TopicID,
		Name:        input.Name,
		Description: cloneStringPtr(input.Description),
		ParentID:    stringPtr(parent.ID),
		ChildrenIDs: []string{},
		Session:     []string{},
		Memory:      cloneMapNil(input.Memory),
		Input:       cloneMap(input.Input),
		Output:      cloneMap(input.Output),
		Progress:    []string{},
		Status:      input.Status,
		Version:     1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	node.Normalize()

	parent.ChildrenIDs = append(parent.ChildrenIDs, node.ID)
	if input.Memory != nil {
		rememberChildMemory(parent, node)
	}
	touchNode(parent)
	touchTopic(topic)

	state.Nodes[node.ID] = node
	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return cloneNode(node), nil
}

func (service *Service) ListNodesByFilter(topicID string, filter NodeListFilter) ([]*domain.Node, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	if _, err := requireTopic(state, topicID); err != nil {
		return nil, err
	}

	nodes := make([]*domain.Node, 0)
	for _, node := range state.Nodes {
		if node.TopicID != topicID {
			continue
		}
		if !matchesNodeFilter(node, filter) {
			continue
		}
		nodes = append(nodes, cloneNode(node))
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].CreatedAt.Equal(nodes[j].CreatedAt) {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].CreatedAt.Before(nodes[j].CreatedAt)
	})
	return nodes, nil
}

func (service *Service) GetNode(nodeID string) (*domain.Node, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	node, err := requireNode(state, nodeID)
	if err != nil {
		return nil, err
	}
	return cloneNode(node), nil
}

func (service *Service) ListChildren(nodeID string) ([]*domain.Node, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	node, err := requireNode(state, nodeID)
	if err != nil {
		return nil, err
	}

	children := make([]*domain.Node, 0, len(node.ChildrenIDs))
	for _, childID := range node.ChildrenIDs {
		child, err := requireNode(state, childID)
		if err != nil {
			return nil, err
		}
		children = append(children, cloneNode(child))
	}
	return children, nil
}

func (service *Service) MoveNode(nodeID, newParentID string) (*domain.Node, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	node, err := requireNode(state, nodeID)
	if err != nil {
		return nil, err
	}
	if node.ParentID == nil {
		return nil, domain.InvalidTreeError{Message: "root node cannot be moved"}
	}

	newParent, err := requireNode(state, newParentID)
	if err != nil {
		return nil, err
	}
	if node.TopicID != newParent.TopicID {
		return nil, domain.InvalidTreeError{Message: "cannot move node across topics"}
	}
	if err := ensureNoCycle(state, node.ID, newParent.ID); err != nil {
		return nil, err
	}
	if node.ParentID != nil && *node.ParentID == newParent.ID {
		return cloneNode(node), nil
	}

	oldParent, err := requireNode(state, *node.ParentID)
	if err != nil {
		return nil, err
	}
	oldParent.ChildrenIDs = removeChild(oldParent.ChildrenIDs, node.ID)
	touchNode(oldParent)

	newParent.ChildrenIDs = append(newParent.ChildrenIDs, node.ID)
	touchNode(newParent)

	node.ParentID = stringPtr(newParent.ID)
	touchNode(node)

	topic, err := requireTopic(state, node.TopicID)
	if err != nil {
		return nil, err
	}
	touchTopic(topic)

	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return cloneNode(node), nil
}

func (service *Service) DeleteNode(nodeID string) (*domain.Node, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	node, err := requireNode(state, nodeID)
	if err != nil {
		return nil, err
	}
	if node.ParentID == nil {
		return nil, domain.InvalidTreeError{Message: "root node cannot be deleted"}
	}
	if !node.IsLeaf() {
		return nil, domain.LeafOperationError{NodeID: nodeID}
	}

	parent, err := requireNode(state, *node.ParentID)
	if err != nil {
		return nil, err
	}
	parent.ChildrenIDs = removeChild(parent.ChildrenIDs, node.ID)
	touchNode(parent)

	topic, err := requireTopic(state, node.TopicID)
	if err != nil {
		return nil, err
	}
	touchTopic(topic)

	deleted := cloneNode(node)
	delete(state.Nodes, node.ID)
	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return deleted, nil
}

func (service *Service) UpdateNode(input UpdateNodeInput) (*domain.Node, error) {
	if input.NodeID == "" {
		return nil, domain.ValidationError{Message: "node ID is required"}
	}
	if input.Name != nil && *input.Name == "" {
		return nil, domain.ValidationError{Message: "node name cannot be empty"}
	}
	if input.Description != nil && input.ClearDescription {
		return nil, domain.ValidationError{Message: "description and clear-description cannot be used together"}
	}
	if input.ExpectedVersion != nil && *input.ExpectedVersion <= 0 {
		return nil, domain.ValidationError{Message: "expected version must be a positive integer"}
	}
	if input.Status != nil {
		if _, err := domain.ParseNodeStatus(string(*input.Status)); err != nil {
			return nil, err
		}
	}
	if !hasNodeChanges(input) {
		return nil, domain.ValidationError{Message: "at least one node field must be updated"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	node, err := requireNode(state, input.NodeID)
	if err != nil {
		return nil, err
	}
	if input.ExpectedVersion != nil && node.Version != *input.ExpectedVersion {
		return nil, domain.VersionConflictError{
			Kind:     "node",
			ID:       node.ID,
			Expected: *input.ExpectedVersion,
			Actual:   node.Version,
		}
	}

	if input.Name != nil {
		node.Name = *input.Name
	}
	if input.Description != nil {
		node.Description = cloneStringPtr(input.Description)
	}
	if input.ClearDescription {
		node.Description = nil
	}
	if input.Status != nil {
		node.Status = *input.Status
	}

	memoryChanged := false
	if input.Memory != nil {
		node.Memory = cloneMap(input.Memory)
		memoryChanged = true
	}
	if input.Input != nil {
		node.Input = cloneMap(input.Input)
	}
	if input.Output != nil {
		node.Output = cloneMap(input.Output)
	}
	if len(input.SessionIDs) > 0 {
		node.Session = append(node.Session, input.SessionIDs...)
	}
	if len(input.Progress) > 0 {
		node.Progress = append(node.Progress, input.Progress...)
	}

	touchNode(node)
	if memoryChanged && node.ParentID != nil {
		parent, err := requireNode(state, *node.ParentID)
		if err != nil {
			return nil, err
		}
		rememberChildMemory(parent, node)
		touchNode(parent)
	}

	topic, err := requireTopic(state, node.TopicID)
	if err != nil {
		return nil, err
	}
	touchTopic(topic)

	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return cloneNode(node), nil
}

func (service *Service) IsLeafOperable(nodeID string) (bool, error) {
	state, err := service.store.Load()
	if err != nil {
		return false, err
	}
	node, err := requireNode(state, nodeID)
	if err != nil {
		return false, err
	}
	return node.IsLeaf(), nil
}

func requireTopic(state domain.VfsState, topicID string) (*domain.Topic, error) {
	topic, exists := state.Topics[topicID]
	if !exists || topic == nil {
		return nil, domain.TopicNotFoundError{TopicID: topicID}
	}
	return topic, nil
}

func requireNode(state domain.VfsState, nodeID string) (*domain.Node, error) {
	node, exists := state.Nodes[nodeID]
	if !exists || node == nil {
		return nil, domain.NodeNotFoundError{NodeID: nodeID}
	}
	return node, nil
}

func ensureNoCycle(state domain.VfsState, nodeID, newParentID string) error {
	if nodeID == newParentID {
		return domain.InvalidTreeError{Message: "node cannot become its own parent"}
	}

	cursor, err := requireNode(state, newParentID)
	if err != nil {
		return err
	}

	for {
		if cursor.ID == nodeID {
			return domain.InvalidTreeError{Message: "tree move would create a cycle"}
		}
		if cursor.ParentID == nil {
			return nil
		}
		cursor, err = requireNode(state, *cursor.ParentID)
		if err != nil {
			return err
		}
	}
}

func hasTopicChanges(input UpdateTopicInput) bool {
	return input.Name != nil ||
		input.Description != nil ||
		input.ClearDescription ||
		input.ReplaceMetadata ||
		input.ReplaceTags
}

func hasNodeChanges(input UpdateNodeInput) bool {
	return input.Name != nil ||
		input.Description != nil ||
		input.ClearDescription ||
		input.Status != nil ||
		input.Memory != nil ||
		input.Input != nil ||
		input.Output != nil ||
		len(input.SessionIDs) > 0 ||
		len(input.Progress) > 0
}

func matchesNodeFilter(node *domain.Node, filter NodeListFilter) bool {
	if filter.LeafOnly && !node.IsLeaf() {
		return false
	}
	if len(filter.Statuses) > 0 && !slices.Contains(filter.Statuses, node.Status) {
		return false
	}
	if len(filter.ExcludeStatuses) > 0 && slices.Contains(filter.ExcludeStatuses, node.Status) {
		return false
	}
	return true
}

func touchNode(node *domain.Node) {
	node.Version++
	node.UpdatedAt = time.Now().UTC()
}

func touchTopic(topic *domain.Topic) {
	topic.UpdatedAt = time.Now().UTC()
}

func removeChild(children []string, nodeID string) []string {
	filtered := make([]string, 0, len(children))
	for _, childID := range children {
		if childID != nodeID {
			filtered = append(filtered, childID)
		}
	}
	return filtered
}

func rememberChildMemory(parent, child *domain.Node) {
	if parent == nil || child == nil || child.Memory == nil {
		return
	}
	if parent.Memory == nil {
		parent.Memory = map[string]any{}
	}

	cache, ok := parent.Memory[childMemoryCacheKey].(map[string]any)
	if !ok || cache == nil {
		cache = map[string]any{}
	}
	cache[child.ID] = map[string]any{
		"node_id":    child.ID,
		"name":       child.Name,
		"updated_at": child.UpdatedAt.Format(time.RFC3339Nano),
		"memory":     cloneMap(child.Memory),
	}
	parent.Memory[childMemoryCacheKey] = cache
}

func nodeIDsForTopic(state domain.VfsState, topicID string) []string {
	nodes := make([]*domain.Node, 0)
	for _, node := range state.Nodes {
		if node.TopicID == topicID {
			nodes = append(nodes, node)
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].CreatedAt.Equal(nodes[j].CreatedAt) {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].CreatedAt.Before(nodes[j].CreatedAt)
	})

	nodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		nodeIDs = append(nodeIDs, node.ID)
	}
	return nodeIDs
}

func cloneTopic(topic *domain.Topic) *domain.Topic {
	if topic == nil {
		return nil
	}
	cloned := *topic
	cloned.Metadata = cloneMap(topic.Metadata)
	cloned.Tags = cloneStrings(topic.Tags)
	cloned.Description = cloneStringPtr(topic.Description)
	return &cloned
}

func cloneNode(node *domain.Node) *domain.Node {
	if node == nil {
		return nil
	}
	cloned := *node
	cloned.Description = cloneStringPtr(node.Description)
	cloned.ParentID = cloneStringPtr(node.ParentID)
	cloned.ChildrenIDs = cloneStrings(node.ChildrenIDs)
	cloned.Session = cloneStrings(node.Session)
	cloned.Memory = cloneMapNil(node.Memory)
	cloned.Input = cloneMap(node.Input)
	cloned.Output = cloneMap(node.Output)
	cloned.Progress = cloneStrings(node.Progress)
	return &cloned
}

func cloneStringPtr(raw *string) *string {
	if raw == nil {
		return nil
	}
	value := *raw
	return &value
}

func stringPtr(raw string) *string {
	return &raw
}

func cloneMap(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	cloned := make(map[string]any, len(raw))
	for key, value := range raw {
		cloned[key] = value
	}
	return cloned
}

func cloneMapNil(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	return cloneMap(raw)
}

func cloneStrings(raw []string) []string {
	if raw == nil {
		return []string{}
	}
	return slices.Clone(raw)
}

func inferAutoTopicName(nodeName string) string {
	trimmed := strings.TrimSpace(nodeName)
	if trimmed == "" {
		return "Untitled Topic"
	}
	return trimmed
}
