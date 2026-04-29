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

type UserSkillPermission struct {
	SkillName    string   `json:"skill_name"`
	SkillPath    string   `json:"skill_path"`
	SkillMTime   string   `json:"skill_mtime"`
	AllowedRoots []string `json:"allowed_roots"`
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

func (service *Service) ListUserSkillPermissions() ([]UserSkillPermission, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	if state.User == nil {
		return []UserSkillPermission{}, nil
	}
	return decodeUserSkillAllowRecords(state.User.UserPermission), nil
}

func (service *Service) AddUserSkillPermission(skillName, skillPath, skillMTime string, allowedRoots []string) (*UserSkillPermission, error) {
	trimmedSkillName := strings.TrimSpace(skillName)
	if trimmedSkillName == "" {
		return nil, domain.ValidationError{Message: "skill_name is required"}
	}
	trimmedSkillPath := normalizeDirPrefix(skillPath)
	if trimmedSkillPath == "" {
		return nil, domain.ValidationError{Message: "skill_path is required"}
	}
	trimmedSkillMTime := strings.TrimSpace(skillMTime)
	if trimmedSkillMTime == "" {
		return nil, domain.ValidationError{Message: "skill_mtime is required"}
	}
	parsedMtime, err := time.Parse(time.RFC3339Nano, trimmedSkillMTime)
	if err != nil {
		return nil, domain.ValidationError{Message: "skill_mtime must be RFC3339Nano"}
	}
	normalizedRoots := normalizeRoots(allowedRoots)

	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	if state.User == nil {
		state.User = domain.NewDefaultUser()
	}
	if state.User.UserPermission == nil {
		state.User.UserPermission = map[string]any{}
	}

	records := decodeUserSkillAllowRecords(state.User.UserPermission)
	candidate := UserSkillPermission{
		SkillName:    trimmedSkillName,
		SkillPath:    trimmedSkillPath,
		SkillMTime:   parsedMtime.UTC().Format(time.RFC3339Nano),
		AllowedRoots: normalizedRoots,
	}
	updated := false
	for i := range records {
		if records[i].SkillName == trimmedSkillName {
			records[i] = candidate
			updated = true
			break
		}
	}
	if !updated {
		records = append(records, candidate)
	}

	state.User.UserPermission[userSkillAllowsKey] = encodeUserSkillAllowRecords(records)
	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return &candidate, nil
}

func (service *Service) DeleteUserSkillPermission(skillName, skillPath string) (bool, error) {
	trimmedSkillName := strings.TrimSpace(skillName)
	if trimmedSkillName == "" {
		return false, domain.ValidationError{Message: "skill_name is required"}
	}
	trimmedSkillPath := normalizeDirPrefix(skillPath)
	state, err := service.store.Load()
	if err != nil {
		return false, err
	}
	if state.User == nil {
		return false, nil
	}
	records := decodeUserSkillAllowRecords(state.User.UserPermission)
	next := make([]UserSkillPermission, 0, len(records))
	deleted := false
	for _, record := range records {
		if record.SkillName != trimmedSkillName {
			next = append(next, record)
			continue
		}
		if trimmedSkillPath != "" && normalizeDirPrefix(record.SkillPath) != trimmedSkillPath {
			next = append(next, record)
			continue
		}
		deleted = true
	}
	if !deleted {
		return false, nil
	}
	if state.User.UserPermission == nil {
		state.User.UserPermission = map[string]any{}
	}
	state.User.UserPermission[userSkillAllowsKey] = encodeUserSkillAllowRecords(next)
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

func decodeUserSkillAllowRecords(permission map[string]any) []UserSkillPermission {
	if permission == nil {
		return []UserSkillPermission{}
	}
	raw, exists := permission[userSkillAllowsKey]
	if !exists {
		return []UserSkillPermission{}
	}
	values, ok := raw.([]any)
	if !ok {
		return []UserSkillPermission{}
	}
	result := make([]UserSkillPermission, 0, len(values))
	for _, item := range values {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		skillName, _ := row["skill_name"].(string)
		skillPath, _ := row["skill_path"].(string)
		skillMTime, _ := row["skill_mtime"].(string)
		if strings.TrimSpace(skillName) == "" || normalizeDirPrefix(skillPath) == "" || strings.TrimSpace(skillMTime) == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(skillMTime)); err != nil {
			continue
		}
		roots := decodeRoots(row["allowed_roots"])
		result = append(result, UserSkillPermission{
			SkillName:    strings.TrimSpace(skillName),
			SkillPath:    normalizeDirPrefix(skillPath),
			SkillMTime:   strings.TrimSpace(skillMTime),
			AllowedRoots: roots,
		})
	}
	return result
}

func encodeUserSkillAllowRecords(records []UserSkillPermission) []any {
	rows := make([]any, 0, len(records))
	for _, record := range records {
		if strings.TrimSpace(record.SkillName) == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"skill_name":    strings.TrimSpace(record.SkillName),
			"skill_path":    normalizeDirPrefix(record.SkillPath),
			"skill_mtime":   strings.TrimSpace(record.SkillMTime),
			"allowed_roots": cloneStrings(normalizeRoots(record.AllowedRoots)),
		})
	}
	return rows
}

func decodeRoots(raw any) []string {
	if raw == nil {
		return []string{}
	}
	if typed, ok := raw.([]string); ok {
		return normalizeRoots(typed)
	}
	values, ok := raw.([]any)
	if !ok {
		return []string{}
	}
	roots := make([]string, 0, len(values))
	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			continue
		}
		roots = append(roots, text)
	}
	return normalizeRoots(roots)
}

func normalizeRoots(values []string) []string {
	roots := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		normalized := normalizeDirPrefix(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		roots = append(roots, normalized)
	}
	return roots
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
