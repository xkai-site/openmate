package domain

import "testing"

func TestVfsStateNormalizeInitializesDefaultUser(t *testing.T) {
	state := NewVfsState()
	state.User = nil

	state.Normalize()

	if state.User == nil {
		t.Fatalf("state.User = nil, want default user")
	}
	if state.User.ID != DefaultUserID {
		t.Fatalf("state.User.ID = %q, want %q", state.User.ID, DefaultUserID)
	}
	if state.User.ModelJSON == nil {
		t.Fatalf("state.User.ModelJSON = nil, want empty map")
	}
	if state.User.UserPermission == nil {
		t.Fatalf("state.User.UserPermission = nil, want empty map")
	}
}

func TestVfsStateNormalizeMigratesLegacyTopicUserMemory(t *testing.T) {
	state := NewVfsState()
	state.User = nil
	state.Topics["topic-1"] = &Topic{
		ID:         "topic-1",
		Name:       "Topic One",
		RootNodeID: "topic-1:root",
		Metadata: map[string]any{
			"user_memory": map[string]any{
				"user_id": "u-1",
				"role":    "owner",
			},
			"topic_memory": map[string]any{"summary": "topic"},
		},
	}

	state.Normalize()

	if state.User == nil || state.User.UserMemory == nil {
		t.Fatalf("state.User.UserMemory = %v, want migrated memory", state.User)
	}
	if state.User.UserMemory["user_id"] != "u-1" {
		t.Fatalf("state.User.UserMemory = %v, want user_id=u-1", state.User.UserMemory)
	}
	if state.User.UserMemory["role"] != "owner" {
		t.Fatalf("state.User.UserMemory = %v, want role=owner", state.User.UserMemory)
	}
	if _, exists := state.Topics["topic-1"].Metadata["user_memory"]; exists {
		t.Fatalf("topic metadata should not contain user_memory after migration: %v", state.Topics["topic-1"].Metadata)
	}
}
