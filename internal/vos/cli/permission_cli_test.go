package cli_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"vos/internal/vos/cli"
)

func TestPermissionCLIFlow(t *testing.T) {
	stateFile := t.TempDir() + "/vos_state.json"
	base := []string{"--state-file", stateFile}

	if code := cli.Run(append(base, "topic", "create", "--topic-id", "topic-p", "--name", "Topic Permission"), &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("topic create code = %d, want 0", code)
	}

	var addOut bytes.Buffer
	if code := cli.Run(
		append(base, "permission", "topic", "add", "--topic-id", "topic-p", "--tool-name", "write", "--dir-prefix", "D:/workspace/project"),
		&addOut,
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("permission topic add code = %d, want 0", code)
	}
	var added struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(addOut.Bytes(), &added); err != nil {
		t.Fatalf("json.Unmarshal(addOut) error = %v", err)
	}
	if added.ID == "" {
		t.Fatalf("added.ID should not be empty")
	}

	var listOut bytes.Buffer
	if code := cli.Run(
		append(base, "permission", "topic", "list", "--topic-id", "topic-p"),
		&listOut,
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("permission topic list code = %d, want 0", code)
	}
	var listed struct {
		ToolAllows []struct {
			ToolName string `json:"tool_name"`
		} `json:"tool_allows"`
	}
	if err := json.Unmarshal(listOut.Bytes(), &listed); err != nil {
		t.Fatalf("json.Unmarshal(listOut) error = %v", err)
	}
	if len(listed.ToolAllows) != 1 || listed.ToolAllows[0].ToolName != "write" {
		t.Fatalf("listed.ToolAllows = %+v, want one write rule", listed.ToolAllows)
	}

	if code := cli.Run(
		append(base, "permission", "topic", "delete", "--topic-id", "topic-p", "--id", added.ID),
		&bytes.Buffer{},
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("permission topic delete code = %d, want 0", code)
	}

	var userAddOut bytes.Buffer
	if code := cli.Run(
		append(base, "permission", "user", "add", "--skill-name", "skill.alpha"),
		&userAddOut,
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("permission user add code = %d, want 0", code)
	}

	var userListOut bytes.Buffer
	if code := cli.Run(
		append(base, "permission", "user", "list"),
		&userListOut,
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("permission user list code = %d, want 0", code)
	}
	var userList struct {
		SkillAllows []string `json:"skill_allows"`
	}
	if err := json.Unmarshal(userListOut.Bytes(), &userList); err != nil {
		t.Fatalf("json.Unmarshal(userListOut) error = %v", err)
	}
	if len(userList.SkillAllows) != 1 || userList.SkillAllows[0] != "skill.alpha" {
		t.Fatalf("user skill_allows = %v, want [skill.alpha]", userList.SkillAllows)
	}
}
