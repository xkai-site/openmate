package httpapi

import (
	"net/http"
	"testing"

	"vos/internal/vos/service"
)

func TestServerV1TopicPermissionsCRUD(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	if _, _, err := server.service.CreateTopic(service.CreateTopicInput{
		TopicID: "topic-http-permission",
		Name:    "Topic HTTP Permission",
	}); err != nil {
		t.Fatalf("CreateTopic() error = %v", err)
	}

	addEnv := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodPost,
		testServer.URL+"/api/v1/topics/topic-http-permission/permissions",
		map[string]any{
			"tool_name":  "write",
			"dir_prefix": "D:/workspace/project",
		},
		http.StatusOK,
	)
	added := struct {
		ID       string `json:"id"`
		ToolName string `json:"tool_name"`
	}{}
	mustDecodeEnvelopeData(t, addEnv, &added)
	if added.ID == "" || added.ToolName != "write" {
		t.Fatalf("added = %+v, want id and tool_name=write", added)
	}

	listEnv := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodGet,
		testServer.URL+"/api/v1/topics/topic-http-permission/permissions",
		nil,
		http.StatusOK,
	)
	listed := struct {
		ToolAllows []map[string]any `json:"tool_allows"`
	}{}
	mustDecodeEnvelopeData(t, listEnv, &listed)
	if len(listed.ToolAllows) != 1 {
		t.Fatalf("len(tool_allows) = %d, want 1", len(listed.ToolAllows))
	}

	deleteEnv := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodDelete,
		testServer.URL+"/api/v1/topics/topic-http-permission/permissions?id="+added.ID,
		nil,
		http.StatusOK,
	)
	deleted := struct {
		Deleted bool `json:"deleted"`
	}{}
	mustDecodeEnvelopeData(t, deleteEnv, &deleted)
	if !deleted.Deleted {
		t.Fatalf("deleted = false, want true")
	}
}

func TestServerV1UserPermissionsCRUD(t *testing.T) {
	server, testServer := openTestServer(t)
	defer func() {
		_ = server.Close()
		testServer.Close()
	}()

	addEnv := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodPost,
		testServer.URL+"/api/v1/user/permissions",
		map[string]any{
			"skill_name": "skill.alpha",
		},
		http.StatusOK,
	)
	added := struct {
		SkillName string `json:"skill_name"`
	}{}
	mustDecodeEnvelopeData(t, addEnv, &added)
	if added.SkillName != "skill.alpha" {
		t.Fatalf("skill_name = %s, want skill.alpha", added.SkillName)
	}

	listEnv := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodGet,
		testServer.URL+"/api/v1/user/permissions",
		nil,
		http.StatusOK,
	)
	listed := struct {
		SkillAllows []string `json:"skill_allows"`
	}{}
	mustDecodeEnvelopeData(t, listEnv, &listed)
	if len(listed.SkillAllows) != 1 || listed.SkillAllows[0] != "skill.alpha" {
		t.Fatalf("skill_allows = %v, want [skill.alpha]", listed.SkillAllows)
	}

	deleteEnv := mustRequestEnvelope(
		t,
		testServer.Client(),
		http.MethodDelete,
		testServer.URL+"/api/v1/user/permissions?skill_name=skill.alpha",
		nil,
		http.StatusOK,
	)
	deleted := struct {
		Deleted bool `json:"deleted"`
	}{}
	mustDecodeEnvelopeData(t, deleteEnv, &deleted)
	if !deleted.Deleted {
		t.Fatalf("deleted = false, want true")
	}
}
