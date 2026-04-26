package service

import (
	"strings"
	"time"

	"vos/internal/vos/domain"
)

const (
	topicPermissionMetadataKey = "permission"
	topicToolAllowsKey         = "tool_allows"
	userSkillAllowsKey         = "skill_allows"
)

type TopicToolPermission struct {
	ID        string    `json:"id"`
	ToolName  string    `json:"tool_name"`
	DirPrefix string    `json:"dir_prefix"`
	CreatedAt time.Time `json:"created_at"`
}

func (service *Service) ListTopicToolPermissions(topicID string) ([]TopicToolPermission, error) {
	trimmedTopicID := strings.TrimSpace(topicID)
	if trimmedTopicID == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, trimmedTopicID)
	if err != nil {
		return nil, err
	}
	return decodeTopicToolAllows(topic.Metadata), nil
}

func (service *Service) AddTopicToolPermission(topicID, toolName, dirPrefix string) (*TopicToolPermission, error) {
	trimmedTopicID := strings.TrimSpace(topicID)
	if trimmedTopicID == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}
	trimmedToolName := strings.TrimSpace(toolName)
	if trimmedToolName == "" {
		return nil, domain.ValidationError{Message: "tool_name is required"}
	}
	normalizedDirPrefix := normalizeDirPrefix(dirPrefix)
	if normalizedDirPrefix == "" {
		return nil, domain.ValidationError{Message: "dir_prefix is required"}
	}

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, trimmedTopicID)
	if err != nil {
		return nil, err
	}

	allows := decodeTopicToolAllows(topic.Metadata)
	for _, allow := range allows {
		if allow.ToolName == trimmedToolName && normalizeDirPrefix(allow.DirPrefix) == normalizedDirPrefix {
			return &allow, nil
		}
	}

	now := time.Now().UTC()
	created := TopicToolPermission{
		ID:        domain.NewID(),
		ToolName:  trimmedToolName,
		DirPrefix: normalizedDirPrefix,
		CreatedAt: now,
	}
	allows = append(allows, created)
	ensureTopicPermissionMetadata(topic)
	topic.Metadata[topicPermissionMetadataKey].(map[string]any)[topicToolAllowsKey] = encodeTopicToolAllows(allows)
	touchTopic(topic)
	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return &created, nil
}

func (service *Service) DeleteTopicToolPermission(topicID, permissionID string) (bool, error) {
	trimmedTopicID := strings.TrimSpace(topicID)
	if trimmedTopicID == "" {
		return false, domain.ValidationError{Message: "topic ID is required"}
	}
	trimmedPermissionID := strings.TrimSpace(permissionID)
	if trimmedPermissionID == "" {
		return false, domain.ValidationError{Message: "id is required"}
	}

	state, err := service.store.Load()
	if err != nil {
		return false, err
	}
	topic, err := requireTopic(state, trimmedTopicID)
	if err != nil {
		return false, err
	}
	allows := decodeTopicToolAllows(topic.Metadata)
	next := make([]TopicToolPermission, 0, len(allows))
	deleted := false
	for _, allow := range allows {
		if allow.ID == trimmedPermissionID {
			deleted = true
			continue
		}
		next = append(next, allow)
	}
	if !deleted {
		return false, nil
	}
	ensureTopicPermissionMetadata(topic)
	topic.Metadata[topicPermissionMetadataKey].(map[string]any)[topicToolAllowsKey] = encodeTopicToolAllows(next)
	touchTopic(topic)
	if err := service.store.Save(state); err != nil {
		return false, err
	}
	return true, nil
}

func (service *Service) ListUserSkillPermissions() ([]string, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	if state.User == nil {
		return []string{}, nil
	}
	return decodeUserSkillAllows(state.User.UserPermission), nil
}

func (service *Service) AddUserSkillPermission(skillName string) (string, error) {
	trimmedSkillName := strings.TrimSpace(skillName)
	if trimmedSkillName == "" {
		return "", domain.ValidationError{Message: "skill_name is required"}
	}
	state, err := service.store.Load()
	if err != nil {
		return "", err
	}
	if state.User == nil {
		state.User = domain.NewDefaultUser()
	}
	if state.User.UserPermission == nil {
		state.User.UserPermission = map[string]any{}
	}
	skills := decodeUserSkillAllows(state.User.UserPermission)
	for _, skill := range skills {
		if skill == trimmedSkillName {
			return trimmedSkillName, nil
		}
	}
	skills = append(skills, trimmedSkillName)
	state.User.UserPermission[userSkillAllowsKey] = cloneStrings(skills)
	if err := service.store.Save(state); err != nil {
		return "", err
	}
	return trimmedSkillName, nil
}

func (service *Service) DeleteUserSkillPermission(skillName string) (bool, error) {
	trimmedSkillName := strings.TrimSpace(skillName)
	if trimmedSkillName == "" {
		return false, domain.ValidationError{Message: "skill_name is required"}
	}
	state, err := service.store.Load()
	if err != nil {
		return false, err
	}
	if state.User == nil {
		return false, nil
	}
	skills := decodeUserSkillAllows(state.User.UserPermission)
	next := make([]string, 0, len(skills))
	deleted := false
	for _, skill := range skills {
		if skill == trimmedSkillName {
			deleted = true
			continue
		}
		next = append(next, skill)
	}
	if !deleted {
		return false, nil
	}
	if state.User.UserPermission == nil {
		state.User.UserPermission = map[string]any{}
	}
	state.User.UserPermission[userSkillAllowsKey] = cloneStrings(next)
	if err := service.store.Save(state); err != nil {
		return false, err
	}
	return true, nil
}

func ensureTopicPermissionMetadata(topic *domain.Topic) {
	if topic.Metadata == nil {
		topic.Metadata = map[string]any{}
	}
	raw, exists := topic.Metadata[topicPermissionMetadataKey]
	if !exists {
		topic.Metadata[topicPermissionMetadataKey] = map[string]any{
			topicToolAllowsKey: []any{},
		}
		return
	}
	m, ok := raw.(map[string]any)
	if !ok || m == nil {
		topic.Metadata[topicPermissionMetadataKey] = map[string]any{
			topicToolAllowsKey: []any{},
		}
		return
	}
	if _, ok := m[topicToolAllowsKey]; !ok {
		m[topicToolAllowsKey] = []any{}
	}
}

func decodeTopicToolAllows(metadata map[string]any) []TopicToolPermission {
	if metadata == nil {
		return []TopicToolPermission{}
	}
	rawPermission, ok := metadata[topicPermissionMetadataKey]
	if !ok {
		return []TopicToolPermission{}
	}
	permission, ok := rawPermission.(map[string]any)
	if !ok || permission == nil {
		return []TopicToolPermission{}
	}
	rawAllows, ok := permission[topicToolAllowsKey]
	if !ok {
		return []TopicToolPermission{}
	}
	items, ok := rawAllows.([]any)
	if !ok {
		return []TopicToolPermission{}
	}
	allows := make([]TopicToolPermission, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := row["id"].(string)
		toolName, _ := row["tool_name"].(string)
		dirPrefix, _ := row["dir_prefix"].(string)
		if strings.TrimSpace(toolName) == "" || strings.TrimSpace(dirPrefix) == "" {
			continue
		}
		createdAt := time.Time{}
		if raw, ok := row["created_at"].(string); ok && strings.TrimSpace(raw) != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				createdAt = parsed.UTC()
			}
		}
		if id == "" {
			id = domain.NewID()
		}
		allows = append(allows, TopicToolPermission{
			ID:        id,
			ToolName:  strings.TrimSpace(toolName),
			DirPrefix: normalizeDirPrefix(dirPrefix),
			CreatedAt: createdAt,
		})
	}
	return allows
}

func encodeTopicToolAllows(allows []TopicToolPermission) []any {
	rows := make([]any, 0, len(allows))
	for _, allow := range allows {
		createdAt := allow.CreatedAt.UTC()
		createdAtRaw := ""
		if !createdAt.IsZero() {
			createdAtRaw = createdAt.Format(time.RFC3339Nano)
		}
		rows = append(rows, map[string]any{
			"id":         strings.TrimSpace(allow.ID),
			"tool_name":  strings.TrimSpace(allow.ToolName),
			"dir_prefix": normalizeDirPrefix(allow.DirPrefix),
			"created_at": createdAtRaw,
		})
	}
	return rows
}

func decodeUserSkillAllows(permission map[string]any) []string {
	if permission == nil {
		return []string{}
	}
	raw, exists := permission[userSkillAllowsKey]
	if !exists {
		return []string{}
	}
	values, ok := raw.([]any)
	if ok {
		result := make([]string, 0, len(values))
		for _, item := range values {
			text, ok := item.(string)
			if !ok {
				continue
			}
			trimmed := strings.TrimSpace(text)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		return result
	}
	typed, ok := raw.([]string)
	if ok {
		return cloneStrings(typed)
	}
	return []string{}
}

func normalizeDirPrefix(raw string) string {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, "\\", "/")
	for strings.Contains(normalized, "//") {
		normalized = strings.ReplaceAll(normalized, "//", "/")
	}
	if len(normalized) > 1 {
		normalized = strings.TrimSuffix(normalized, "/")
	}
	return normalized
}
