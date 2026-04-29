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
		append(base, "permission", "user", "add", "--skill-name", "skill.alpha", "--skill-path", "D:/workspace/skills/alpha.md", "--skill-mtime", "2026-04-29T00:00:00Z", "--allow-root", "D:/workspace"),
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
		SkillAllows []struct {
			SkillName string `json:"skill_name"`
		} `json:"skill_allows"`
	}
	if err := json.Unmarshal(userListOut.Bytes(), &userList); err != nil {
		t.Fatalf("json.Unmarshal(userListOut) error = %v", err)
	}
	if len(userList.SkillAllows) != 1 || userList.SkillAllows[0].SkillName != "skill.alpha" {
		t.Fatalf("user skill_allows = %v, want [skill.alpha]", userList.SkillAllows)
	}

	var auditAddOut bytes.Buffer
	if code := cli.Run(
		append(
			base,
			"permission", "audit", "add",
			"--topic-id", "topic-p",
			"--node-id", "node-1",
			"--tool-name", "write",
			"--decision", "allow",
			"--requested-capabilities-json", `{"write_file":{"path":"D:/workspace/project/a.txt"}}`,
			"--matched-rule-id", "rule-1",
			"--risk-tag", "path",
		),
		&auditAddOut,
		&bytes.Buffer{},
	); code != 0 {
		t.Fatalf("permission audit add code = %d, want 0", code)
	}
	var auditAdded struct {
		RequestedCapabilities map[string]any `json:"requested_capabilities"`
		MatchedRuleID         string         `json:"matched_rule_id"`
	}
	if err := json.Unmarshal(auditAddOut.Bytes(), &auditAdded); err != nil {
		t.Fatalf("json.Unmarshal(auditAddOut) error = %v", err)
	}
	if auditAdded.MatchedRuleID != "rule-1" {
		t.Fatalf("audit matched_rule_id = %q, want rule-1", auditAdded.MatchedRuleID)
	}
	if _, ok := auditAdded.RequestedCapabilities["write_file"]; !ok {
		t.Fatalf("audit requested_capabilities = %v, want key write_file", auditAdded.RequestedCapabilities)
	}
}
