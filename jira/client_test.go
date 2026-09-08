package jira

import (
	"encoding/json"
	"testing"

	"github.com/aprilselby/dot/config"
)

func TestBuildPayload(t *testing.T) {
	c := &Client{cfg: &config.JiraConfig{
		Site:       "acme.atlassian.net",
		ProjectKey: "PROJ",
		IssueType:  "Task",
		Assignee:   "me@acme.com",
		Reporter:   "", // omitted
		Labels:     []string{"cli", "auto"},
		ACField:    "customfield_10050",
		CustomFields: map[string]interface{}{
			"customfield_10001": map[string]interface{}{"value": "Team Rocket"},
			"customfield_10002": "plain",
		},
	}}

	raw, err := c.BuildPayload(CreateRequest{
		ParentID:           "PROJ-1",
		Summary:            "Do the thing",
		Description:        "line one\n\nline two",
		AcceptanceCriteria: "given/when/then",
	})
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}

	var p map[string]interface{}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if p["projectKey"] != "PROJ" || p["type"] != "Task" || p["summary"] != "Do the thing" {
		t.Errorf("base fields wrong: %v", p)
	}
	if p["parentIssueId"] != "PROJ-1" {
		t.Errorf("parentIssueId = %v, want PROJ-1", p["parentIssueId"])
	}
	if _, ok := p["reporter"]; ok {
		t.Errorf("empty reporter should be omitted, got %v", p["reporter"])
	}

	// Description must be an ADF doc with one paragraph per non-empty line.
	desc, ok := p["description"].(map[string]interface{})
	if !ok || desc["type"] != "doc" {
		t.Fatalf("description not an ADF doc: %v", p["description"])
	}
	content, ok := desc["content"].([]interface{})
	if !ok || len(content) != 2 {
		t.Fatalf("expected 2 paragraphs, got %v", desc["content"])
	}

	// Custom fields + AC live under additionalAttributes.
	attrs, ok := p["additionalAttributes"].(map[string]interface{})
	if !ok {
		t.Fatalf("additionalAttributes missing: %v", p)
	}
	if attrs["customfield_10002"] != "plain" {
		t.Errorf("custom field not passed through: %v", attrs["customfield_10002"])
	}
	if attrs["customfield_10050"] != "given/when/then" {
		t.Errorf("AC field = %v, want the AC text", attrs["customfield_10050"])
	}
	opt, ok := attrs["customfield_10001"].(map[string]interface{})
	if !ok || opt["value"] != "Team Rocket" {
		t.Errorf("option-shaped custom field not preserved: %v", attrs["customfield_10001"])
	}
}

func TestBuildPayloadOmitsEmptyOptionals(t *testing.T) {
	c := &Client{cfg: &config.JiraConfig{ProjectKey: "PROJ"}}

	raw, err := c.BuildPayload(CreateRequest{Summary: "x"})
	if err != nil {
		t.Fatalf("BuildPayload: %v", err)
	}
	var p map[string]interface{}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if p["type"] != "Task" {
		t.Errorf("issue type should default to Task, got %v", p["type"])
	}
	for _, k := range []string{"assignee", "reporter", "parentIssueId", "labels", "additionalAttributes"} {
		if _, ok := p[k]; ok {
			t.Errorf("%q should be omitted when empty, got %v", k, p[k])
		}
	}
}

func TestBrowseURL(t *testing.T) {
	c := &Client{cfg: &config.JiraConfig{Site: "https://acme.atlassian.net/"}}
	if got := c.browseURL("PROJ-42"); got != "https://acme.atlassian.net/browse/PROJ-42" {
		t.Errorf("browseURL = %q", got)
	}
}
