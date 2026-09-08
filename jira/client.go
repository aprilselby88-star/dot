// Package jira creates Jira work items by shelling out to the Atlassian CLI (acli).
//
// acli exposes no flags for custom fields, so tickets are created via
// `acli jira workitem create --from-json <file>`. The JSON is acli's own flat
// schema (see `acli jira workitem create --generate-json`): custom fields live
// under "additionalAttributes" and the description must be Atlassian Document
// Format (ADF).
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/aprilselby/dot/config"
)

// Client wraps invocations of the acli binary for a single Jira config.
type Client struct {
	cfg *config.JiraConfig
	bin string
}

// CreateRequest is the user-supplied portion of a new ticket. Everything else
// (project, type, assignee, reporter, labels, custom fields) comes from config.
type CreateRequest struct {
	ParentID           string
	Summary            string
	Description        string
	AcceptanceCriteria string
}

// Result describes the outcome of a create attempt. RequestJSON and RawOutput are
// always populated so the UI can reveal the exact payload for a manual retry.
type Result struct {
	Key         string
	URL         string
	RequestJSON []byte
	RawOutput   []byte
}

// issueKeyRe matches a Jira work item key like "PROJ-123".
var issueKeyRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)

// New validates that acli is available and the config has the minimum required
// fields, returning a Client ready to create tickets. It returns an error (and a
// nil Client) when Jira isn't usable, which the caller treats as "not configured".
func New(cfg *config.JiraConfig) (*Client, error) {
	if cfg == nil || strings.TrimSpace(cfg.ProjectKey) == "" {
		return nil, fmt.Errorf("no Jira project_key configured")
	}
	bin, err := exec.LookPath("acli")
	if err != nil {
		return nil, fmt.Errorf("acli not found on PATH — install the Atlassian CLI")
	}
	return &Client{cfg: cfg, bin: bin}, nil
}

// BuildPayload assembles the flat acli JSON for a create request. It is pure
// (no side effects), so it can be unit-tested and reused to show the retry payload.
func (c *Client) BuildPayload(req CreateRequest) ([]byte, error) {
	issueType := c.cfg.IssueType
	if strings.TrimSpace(issueType) == "" {
		issueType = "Task"
	}

	payload := map[string]interface{}{
		"projectKey":  c.cfg.ProjectKey,
		"type":        issueType,
		"summary":     req.Summary,
		"description": plainTextToADF(req.Description),
	}
	if s := strings.TrimSpace(c.cfg.Assignee); s != "" {
		payload["assignee"] = s
	}
	if s := strings.TrimSpace(c.cfg.Reporter); s != "" {
		payload["reporter"] = s
	}
	if s := strings.TrimSpace(req.ParentID); s != "" {
		payload["parentIssueId"] = s
	}
	if len(c.cfg.Labels) > 0 {
		payload["labels"] = c.cfg.Labels
	}

	// additionalAttributes carries reporting custom fields plus the AC field.
	attrs := make(map[string]interface{}, len(c.cfg.CustomFields)+1)
	for k, v := range c.cfg.CustomFields {
		attrs[k] = v
	}
	if f := strings.TrimSpace(c.cfg.ACField); f != "" && strings.TrimSpace(req.AcceptanceCriteria) != "" {
		attrs[f] = req.AcceptanceCriteria
	}
	if len(attrs) > 0 {
		payload["additionalAttributes"] = attrs
	}

	return json.MarshalIndent(payload, "", "  ")
}

// Create authenticates acli (if a token is configured), writes the payload to a
// temp file, runs the create, and parses out the new issue key.
func (c *Client) Create(ctx context.Context, req CreateRequest) (Result, error) {
	res := Result{}

	payload, err := c.BuildPayload(req)
	if err != nil {
		return res, fmt.Errorf("build payload: %w", err)
	}
	res.RequestJSON = payload

	if err := c.EnsureAuth(ctx); err != nil {
		return res, err
	}

	f, err := os.CreateTemp("", "dot-jira-*.json")
	if err != nil {
		return res, fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(payload); err != nil {
		f.Close()
		return res, fmt.Errorf("write payload: %w", err)
	}
	f.Close()

	out, err := c.run(ctx, nil, "jira", "workitem", "create", "--from-json", f.Name())
	res.RawOutput = out
	if err != nil {
		return res, fmt.Errorf("acli create failed: %w", err)
	}

	key := issueKeyRe.FindString(string(out))
	if key == "" {
		return res, fmt.Errorf("could not find an issue key in acli output")
	}
	res.Key = key
	res.URL = c.browseURL(key)
	return res, nil
}

// EnsureAuth logs acli in with the configured token when one is set and the
// current session isn't already for the configured account. When no token is
// configured it is a no-op and dot relies on an existing `acli jira auth login`.
func (c *Client) EnsureAuth(ctx context.Context) error {
	token := strings.TrimSpace(c.cfg.Token)
	site := strings.TrimSpace(c.cfg.Site)
	email := strings.TrimSpace(c.cfg.Email)
	if token == "" || site == "" || email == "" {
		return nil
	}

	// Skip re-login when acli already reports the configured account as active.
	if out, err := c.run(ctx, nil, "jira", "auth", "status"); err == nil {
		s := string(out)
		if strings.Contains(s, email) && strings.Contains(s, site) {
			return nil
		}
	}

	// Token is read from stdin by `acli jira auth login --token`.
	if _, err := c.run(ctx, strings.NewReader(token), "jira", "auth", "login",
		"--site", site, "--email", email, "--token"); err != nil {
		return fmt.Errorf("acli auth login failed: %w", err)
	}
	return nil
}

// run executes acli with the given args, returning combined stdout+stderr.
func (c *Client) run(ctx context.Context, stdin *strings.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, c.bin, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// browseURL builds the human-facing ticket URL from the configured site.
func (c *Client) browseURL(key string) string {
	site := strings.TrimSpace(c.cfg.Site)
	if site == "" {
		return ""
	}
	site = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(site, "https://"), "http://"), "/")
	return "https://" + site + "/browse/" + key
}

// plainTextToADF converts plain text into a minimal Atlassian Document Format
// doc: one paragraph per non-empty line. An empty input yields an empty doc,
// which Jira accepts.
func plainTextToADF(text string) map[string]interface{} {
	doc := map[string]interface{}{
		"type":    "doc",
		"version": 1,
		"content": []interface{}{},
	}
	var content []interface{}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		content = append(content, map[string]interface{}{
			"type": "paragraph",
			"content": []interface{}{
				map[string]interface{}{"type": "text", "text": line},
			},
		})
	}
	if content != nil {
		doc["content"] = content
	}
	return doc
}
