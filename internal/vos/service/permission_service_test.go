package service_test

import (
	"testing"

	"vos/internal/vos/service"
)

func TestTopicToolPermissionCRUD(t *testing.T) {
	svc := newTestService(t)
	if _, _, err := svc.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-permission",
		Name:    "Topic Permission",
	}); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	items, err := svc.ListTopicToolPermissions("topic-permission")
	if err != nil {
		t.Fatalf("ListTopicToolPermissions(before) error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(items))
	}

	created, err := svc.AddTopicToolPermission("topic-permission", "write", "D:/workspace/project/src")
	if err != nil {
		t.Fatalf("AddTopicToolPermission() error = %v", err)
	}
	if created.ID == "" {
		t.Fatalf("created.ID should not be empty")
	}

	items, err = svc.ListTopicToolPermissions("topic-permission")
	if err != nil {
		t.Fatalf("ListTopicToolPermissions(after add) error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ToolName != "write" {
		t.Fatalf("items[0].ToolName = %s, want write", items[0].ToolName)
	}
	if items[0].DirPrefix != "D:/workspace/project/src" {
		t.Fatalf("items[0].DirPrefix = %s, want D:/workspace/project/src", items[0].DirPrefix)
	}

	deleted, err := svc.DeleteTopicToolPermission("topic-permission", created.ID)
	if err != nil {
		t.Fatalf("DeleteTopicToolPermission() error = %v", err)
	}
	if !deleted {
		t.Fatalf("deleted = false, want true")
	}
}

func TestUserSkillPermissionCRUD(t *testing.T) {
	svc := newTestService(t)

	before, err := svc.ListUserSkillPermissions()
	if err != nil {
		t.Fatalf("ListUserSkillPermissions(before) error = %v", err)
	}
	if len(before) != 0 {
		t.Fatalf("len(before) = %d, want 0", len(before))
	}

	name, err := svc.AddUserSkillPermission("skill.alpha")
	if err != nil {
		t.Fatalf("AddUserSkillPermission() error = %v", err)
	}
	if name != "skill.alpha" {
		t.Fatalf("name = %s, want skill.alpha", name)
	}

	after, err := svc.ListUserSkillPermissions()
	if err != nil {
		t.Fatalf("ListUserSkillPermissions(after) error = %v", err)
	}
	if len(after) != 1 || after[0] != "skill.alpha" {
		t.Fatalf("after = %v, want [skill.alpha]", after)
	}

	deleted, err := svc.DeleteUserSkillPermission("skill.alpha")
	if err != nil {
		t.Fatalf("DeleteUserSkillPermission() error = %v", err)
	}
	if !deleted {
		t.Fatalf("deleted = false, want true")
	}
}
