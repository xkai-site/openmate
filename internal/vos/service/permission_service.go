package service

import (
	"fmt"
	"strings"
	"time"

	"vos/internal/vos/domain"
)

const (
	topicPermissionMetadataKey = "permission"
	topicToolAllowsKey         = "tool_allows"
	topicPolicyAuditsKey       = "policy_audits"
	topicPolicyAuditSummaryKey = "policy_audit_summary"
	userSkillAllowsKey         = "skill_allows"
)

type TopicToolPermission struct {
	ID                 string    `json:"id"`
	ToolName           string    `json:"tool_name"`
	DirPrefix          string    `json:"dir_prefix,omitempty"`
	ReadPathPrefixes   []string  `json:"read_path_prefixes"`
	WritePathPrefixes  []string  `json:"write_path_prefixes"`
	AllowNetwork       bool      `json:"allow_network"`
	AllowShellFeatures bool      `json:"allow_shell_features"`
	RiskLevel          string    `json:"risk_level"`
	CreatedBy          string    `json:"created_by,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	LastUsedAt         time.Time `json:"last_used_at,omitempty"`
	Enabled            bool      `json:"enabled"`
}

type TopicToolPermissionInput struct {
	ToolName           string
	DirPrefix          string
	ReadPathPrefixes   []string
	WritePathPrefixes  []string
	AllowNetwork       bool
	AllowShellFeatures bool
	RiskLevel          string
	CreatedBy          string
	Enabled            *bool
}

type PolicyAuditEntry struct {
	EventID               string         `json:"event_id"`
	TopicID               string         `json:"topic_id"`
	NodeID                string         `json:"node_id,omitempty"`
	ToolName              string         `json:"tool_name,omitempty"`
	Decision              string         `json:"decision"`
	RiskTags              []string       `json:"risk_tags"`
	RequestedCapabilities map[string]any `json:"requested_capabilities,omitempty"`
	MatchedRuleID         string         `json:"matched_rule_id,omitempty"`
	Reason                string         `json:"reason,omitempty"`
	RequestID             string         `json:"request_id,omitempty"`
	DurationMS            int            `json:"duration_ms"`
	CreatedAt             time.Time      `json:"created_at"`
}

type PolicyAuditSummaryItem struct {
	TopicID    string    `json:"topic_id"`
	ToolName   string    `json:"tool_name"`
	RiskTag    string    `json:"risk_tag"`
	Decision   string    `json:"decision"`
	TimeBucket string    `json:"time_bucket"`
	Count      int       `json:"count"`
	LastSeenAt time.Time `json:"last_seen_at"`
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

func (service *Service) AddTopicToolPermissionV2(topicID string, input TopicToolPermissionInput) (*TopicToolPermission, error) {
	trimmedTopicID := strings.TrimSpace(topicID)
	if trimmedTopicID == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}
	trimmedToolName := strings.TrimSpace(input.ToolName)
	if trimmedToolName == "" {
		return nil, domain.ValidationError{Message: "tool_name is required"}
	}
	normalizedDirPrefix := normalizeDirPrefix(input.DirPrefix)
	readPrefixes := normalizePrefixes(input.ReadPathPrefixes)
	writePrefixes := normalizePrefixes(input.WritePathPrefixes)
	if normalizedDirPrefix != "" {
		if len(readPrefixes) == 0 {
			readPrefixes = []string{normalizedDirPrefix}
		}
		if len(writePrefixes) == 0 {
			writePrefixes = []string{normalizedDirPrefix}
		}
	}
	if len(readPrefixes) == 0 && len(writePrefixes) == 0 {
		return nil, domain.ValidationError{Message: "read_path_prefixes/write_path_prefixes/dir_prefix is required"}
	}
	riskLevel := normalizeRiskLevel(input.RiskLevel)

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
		if allow.ToolName == trimmedToolName &&
			strings.Join(allow.ReadPathPrefixes, "|") == strings.Join(readPrefixes, "|") &&
			strings.Join(allow.WritePathPrefixes, "|") == strings.Join(writePrefixes, "|") &&
			allow.AllowNetwork == input.AllowNetwork &&
			allow.AllowShellFeatures == input.AllowShellFeatures &&
			allow.RiskLevel == riskLevel {
			return &allow, nil
		}
	}

	now := time.Now().UTC()
	created := TopicToolPermission{
		ID:                 domain.NewID(),
		ToolName:           trimmedToolName,
		DirPrefix:          normalizedDirPrefix,
		ReadPathPrefixes:   readPrefixes,
		WritePathPrefixes:  writePrefixes,
		AllowNetwork:       input.AllowNetwork,
		AllowShellFeatures: input.AllowShellFeatures,
		RiskLevel:          riskLevel,
		CreatedBy:          strings.TrimSpace(input.CreatedBy),
		CreatedAt:          now,
		Enabled:            true,
	}
	if input.Enabled != nil {
		created.Enabled = *input.Enabled
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

func (service *Service) RecordTopicPolicyAudit(entry PolicyAuditEntry) (*PolicyAuditEntry, error) {
	trimmedTopicID := strings.TrimSpace(entry.TopicID)
	if trimmedTopicID == "" {
		return nil, domain.ValidationError{Message: "topic ID is required"}
	}
	decision := strings.ToLower(strings.TrimSpace(entry.Decision))
	if decision == "" {
		return nil, domain.ValidationError{Message: "decision is required"}
	}
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, trimmedTopicID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(entry.EventID) == "" {
		entry.EventID = domain.NewID()
	}
	entry.TopicID = trimmedTopicID
	entry.Decision = decision
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	} else {
		entry.CreatedAt = entry.CreatedAt.UTC()
	}
	ensureTopicPermissionMetadata(topic)
	permission := topic.Metadata[topicPermissionMetadataKey].(map[string]any)
	audits := decodePolicyAudits(topic.Metadata)
	audits = append(audits, entry)
	permission[topicPolicyAuditsKey] = encodePolicyAudits(audits)
	summary := decodePolicyAuditSummary(topic.Metadata)
	summary = applyAuditSummary(summary, entry)
	permission[topicPolicyAuditSummaryKey] = encodePolicyAuditSummary(summary)
	touchTopic(topic)
	if err := service.store.Save(state); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (service *Service) ListTopicPolicyAudits(topicID string) ([]PolicyAuditEntry, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, strings.TrimSpace(topicID))
	if err != nil {
		return nil, err
	}
	return decodePolicyAudits(topic.Metadata), nil
}

func (service *Service) ListTopicPolicyAuditSummary(topicID string) ([]PolicyAuditSummaryItem, error) {
	state, err := service.store.Load()
	if err != nil {
		return nil, err
	}
	topic, err := requireTopic(state, strings.TrimSpace(topicID))
	if err != nil {
		return nil, err
	}
	return decodePolicyAuditSummary(topic.Metadata), nil
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
	if _, ok := m[topicPolicyAuditsKey]; !ok {
		m[topicPolicyAuditsKey] = []any{}
	}
	if _, ok := m[topicPolicyAuditSummaryKey]; !ok {
		m[topicPolicyAuditSummaryKey] = []any{}
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
		readPrefixes := normalizePrefixes(readStringArray(row["read_path_prefixes"]))
		writePrefixes := normalizePrefixes(readStringArray(row["write_path_prefixes"]))
		normalizedDirPrefix := normalizeDirPrefix(dirPrefix)
		if len(readPrefixes) == 0 && normalizedDirPrefix != "" {
			readPrefixes = []string{normalizedDirPrefix}
		}
		if len(writePrefixes) == 0 && normalizedDirPrefix != "" {
			writePrefixes = []string{normalizedDirPrefix}
		}
		if strings.TrimSpace(toolName) == "" || (len(readPrefixes) == 0 && len(writePrefixes) == 0) {
			continue
		}
		createdAt := time.Time{}
		if raw, ok := row["created_at"].(string); ok && strings.TrimSpace(raw) != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				createdAt = parsed.UTC()
			}
		}
		lastUsedAt := time.Time{}
		if raw, ok := row["last_used_at"].(string); ok && strings.TrimSpace(raw) != "" {
			if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
				lastUsedAt = parsed.UTC()
			}
		}
		createdBy, _ := row["created_by"].(string)
		riskLevel, _ := row["risk_level"].(string)
		enabled, _ := row["enabled"].(bool)
		if _, exists := row["enabled"]; !exists {
			enabled = true
		}
		if id == "" {
			id = domain.NewID()
		}
		allows = append(allows, TopicToolPermission{
			ID:                 id,
			ToolName:           strings.TrimSpace(toolName),
			DirPrefix:          normalizedDirPrefix,
			ReadPathPrefixes:   readPrefixes,
			WritePathPrefixes:  writePrefixes,
			AllowNetwork:       readBool(row["allow_network"]),
			AllowShellFeatures: readBool(row["allow_shell_features"]),
			RiskLevel:          normalizeRiskLevel(riskLevel),
			CreatedBy:          strings.TrimSpace(createdBy),
			CreatedAt:          createdAt,
			LastUsedAt:         lastUsedAt,
			Enabled:            enabled,
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
			"id":                   strings.TrimSpace(allow.ID),
			"tool_name":            strings.TrimSpace(allow.ToolName),
			"dir_prefix":           normalizeDirPrefix(allow.DirPrefix),
			"read_path_prefixes":   normalizePrefixes(allow.ReadPathPrefixes),
			"write_path_prefixes":  normalizePrefixes(allow.WritePathPrefixes),
			"allow_network":        allow.AllowNetwork,
			"allow_shell_features": allow.AllowShellFeatures,
			"risk_level":           normalizeRiskLevel(allow.RiskLevel),
			"created_by":           strings.TrimSpace(allow.CreatedBy),
			"created_at":           createdAtRaw,
			"last_used_at":         encodeTime(allow.LastUsedAt),
			"enabled":              allow.Enabled,
		})
	}
	return rows
}

func decodePolicyAudits(metadata map[string]any) []PolicyAuditEntry {
	permission := readPermissionMap(metadata)
	raw, _ := permission[topicPolicyAuditsKey]
	items, _ := raw.([]any)
	result := make([]PolicyAuditEntry, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		createdAt, _ := parseTime(row["created_at"])
		entry := PolicyAuditEntry{
			EventID:               readString(row["event_id"]),
			TopicID:               readString(row["topic_id"]),
			NodeID:                readString(row["node_id"]),
			ToolName:              readString(row["tool_name"]),
			Decision:              strings.ToLower(readString(row["decision"])),
			RiskTags:              readStringArray(row["risk_tags"]),
			RequestedCapabilities: readMap(row["requested_capabilities"]),
			MatchedRuleID:         readString(row["matched_rule_id"]),
			Reason:                readString(row["reason"]),
			RequestID:             readString(row["request_id"]),
			DurationMS:            readInt(row["duration_ms"]),
			CreatedAt:             createdAt,
		}
		if entry.EventID == "" || entry.Decision == "" {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func encodePolicyAudits(items []PolicyAuditEntry) []any {
	rows := make([]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"event_id":               strings.TrimSpace(item.EventID),
			"topic_id":               strings.TrimSpace(item.TopicID),
			"node_id":                strings.TrimSpace(item.NodeID),
			"tool_name":              strings.TrimSpace(item.ToolName),
			"decision":               strings.ToLower(strings.TrimSpace(item.Decision)),
			"risk_tags":              cloneStrings(item.RiskTags),
			"requested_capabilities": cloneMapNil(item.RequestedCapabilities),
			"matched_rule_id":        strings.TrimSpace(item.MatchedRuleID),
			"reason":                 strings.TrimSpace(item.Reason),
			"request_id":             strings.TrimSpace(item.RequestID),
			"duration_ms":            item.DurationMS,
			"created_at":             encodeTime(item.CreatedAt),
		})
	}
	return rows
}

func decodePolicyAuditSummary(metadata map[string]any) []PolicyAuditSummaryItem {
	permission := readPermissionMap(metadata)
	raw, _ := permission[topicPolicyAuditSummaryKey]
	items, _ := raw.([]any)
	result := make([]PolicyAuditSummaryItem, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		lastSeenAt, _ := parseTime(row["last_seen_at"])
		result = append(result, PolicyAuditSummaryItem{
			TopicID:    readString(row["topic_id"]),
			ToolName:   readString(row["tool_name"]),
			RiskTag:    readString(row["risk_tag"]),
			Decision:   readString(row["decision"]),
			TimeBucket: readString(row["time_bucket"]),
			Count:      readInt(row["count"]),
			LastSeenAt: lastSeenAt,
		})
	}
	return result
}

func encodePolicyAuditSummary(items []PolicyAuditSummaryItem) []any {
	rows := make([]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"topic_id":     strings.TrimSpace(item.TopicID),
			"tool_name":    strings.TrimSpace(item.ToolName),
			"risk_tag":     strings.TrimSpace(item.RiskTag),
			"decision":     strings.TrimSpace(item.Decision),
			"time_bucket":  strings.TrimSpace(item.TimeBucket),
			"count":        item.Count,
			"last_seen_at": encodeTime(item.LastSeenAt),
		})
	}
	return rows
}

func applyAuditSummary(summary []PolicyAuditSummaryItem, entry PolicyAuditEntry) []PolicyAuditSummaryItem {
	if len(entry.RiskTags) == 0 {
		entry.RiskTags = []string{"none"}
	}
	bucket := entry.CreatedAt.UTC().Format("2006-01-02T15:00:00Z")
	for _, riskTag := range entry.RiskTags {
		keyFound := false
		for i := range summary {
			if summary[i].TopicID == entry.TopicID &&
				summary[i].ToolName == entry.ToolName &&
				summary[i].RiskTag == riskTag &&
				summary[i].Decision == entry.Decision &&
				summary[i].TimeBucket == bucket {
				summary[i].Count++
				summary[i].LastSeenAt = entry.CreatedAt
				keyFound = true
				break
			}
		}
		if !keyFound {
			summary = append(summary, PolicyAuditSummaryItem{
				TopicID:    entry.TopicID,
				ToolName:   entry.ToolName,
				RiskTag:    riskTag,
				Decision:   entry.Decision,
				TimeBucket: bucket,
				Count:      1,
				LastSeenAt: entry.CreatedAt,
			})
		}
	}
	return summary
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

func normalizePrefixes(values []string) []string {
	items := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		normalized := normalizeDirPrefix(value)
		if normalized == "" || seen[normalized] {
			continue
		}
		seen[normalized] = true
		items = append(items, normalized)
	}
	return items
}

func normalizeRiskLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "medium"
	}
}

func readPermissionMap(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	raw, _ := metadata[topicPermissionMetadataKey]
	m, _ := raw.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func readString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func readStringArray(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return []string{}
	}
	result := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			result = append(result, strings.TrimSpace(text))
		}
	}
	return result
}

func readBool(value any) bool {
	typed, _ := value.(bool)
	return typed
}

func readInt(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	default:
		return 0
	}
}

func readMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func parseTime(value any) (time.Time, error) {
	text := readString(value)
	if text == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func encodeTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
