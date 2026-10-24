package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is loaded from ~/.config/organizer/config.yaml.
type Config struct {
	GitHub   GitHubConfig   `yaml:"github"`
	Obsidian ObsidianConfig `yaml:"obsidian"`
	Storage  StorageConfig  `yaml:"storage"`
	Jira     JiraConfig     `yaml:"jira"`
	Hotkeys  HotkeyConfig   `yaml:"hotkeys"`
	Tags     []TagConfig    `yaml:"tags"`
	Features FeatureConfig  `yaml:"features"`
}

// FeatureConfig toggles optional tabs on or off.
type FeatureConfig struct {
	GitHub bool `yaml:"github"` // GitHub notifications / watch zone tab
}

// JiraConfig holds everything needed to create tickets through the Atlassian CLI (acli).
// Token is overridden by JIRA_API_TOKEN or DOT_JIRA_TOKEN env vars.
// CustomFields and ACField are written into acli's "additionalAttributes" verbatim,
// so their value shape must match what the Jira field expects (string, number, or {value: ...}).
type JiraConfig struct {
	Site          string                 `yaml:"site"`                      // e.g. mysite.atlassian.net (no https://)
	Email         string                 `yaml:"email"`                     // acli login email
	Token         string                 `yaml:"token"`                     // API token; env-overridable
	ProjectKey    string                 `yaml:"project_key"`               // e.g. PROJ
	IssueType     string                 `yaml:"issue_type"`                // default "Task"
	Assignee      string                 `yaml:"assignee"`                  // email or accountId; "@me" also works
	Reporter      string                 `yaml:"reporter"`                  // optional
	DefaultParent string                 `yaml:"default_parent"`            // default parentIssueId prefilled in the form
	Labels        []string               `yaml:"labels"`                    // optional labels applied to every ticket
	ACField       string                 `yaml:"acceptance_criteria_field"` // customfield id the AC text is written to
	CustomFields  map[string]interface{} `yaml:"custom_fields"`             // reporting fields → additionalAttributes
}

// GitHubConfig holds authentication and repo targeting.
// Token is overridden by GITHUB_TOKEN or ORGANIZER_GITHUB_TOKEN env vars.
// BaseURL is empty for github.com; set it to your enterprise root URL.
type GitHubConfig struct {
	Token           string   `yaml:"token"`
	BaseURL         string   `yaml:"base_url"`
	PriorityRepos   []string `yaml:"priority_repos"`
	PollIntervalSec int      `yaml:"poll_interval_sec"`
}

// ObsidianConfig controls where daily notes are written.
// NotesTemplate is a Go time.Format string relative to VaultPath.
// Example: "DailyNotes/06/01 January/02-01-06" → DailyNotes/26/05 May/02-05-26.md
type ObsidianConfig struct {
	VaultPath     string `yaml:"vault_path"`
	NotesTemplate string `yaml:"notes_template"`
}

type StorageConfig struct {
	Dir string `yaml:"dir"`
}

// HotkeyConfig maps actions to key strings matching bubbletea's msg.String().
// Examples: "1", "ctrl+s", "enter", " " (space).
type HotkeyConfig struct {
	TabGitHub   string `yaml:"tab_github"`
	TabMeetings string `yaml:"tab_meetings"`
	TabTodos    string `yaml:"tab_todos"`
	TabNotes    string `yaml:"tab_notes"`
	TabJira     string `yaml:"tab_jira"`
	NewItem     string `yaml:"new_item"`
	Delete      string `yaml:"delete"`
	WatchPR     string `yaml:"watch_pr"`
	Refresh     string `yaml:"refresh"`
	Save        string `yaml:"save"`
	OpenBrowser string `yaml:"open_browser"`
}

// TagConfig defines a named capability/project tag for cross-cutting concerns.
type TagConfig struct {
	Name string `yaml:"name"`
	Tag  string `yaml:"tag"`
}

func DefaultConfig() *Config {
	return &Config{
		GitHub: GitHubConfig{
			PollIntervalSec: 300,
		},
		Obsidian: ObsidianConfig{
			VaultPath:     "~/Documents/Obsidian Vault",
			NotesTemplate: "Daily Notes/2006/01 January/02-01-06",
		},
		Storage: StorageConfig{
			Dir: "~/Documents/Obsidian Vault/.dot",
		},
		Jira: JiraConfig{
			IssueType: "Task",
		},
		Hotkeys: HotkeyConfig{
			TabGitHub:   "1",
			TabMeetings: "2",
			TabTodos:    "3",
			TabNotes:    "4",
			TabJira:     "5",
			NewItem:     "n",
			Delete:      "d",
			WatchPR:     "w",
			Refresh:     "R",
			Save:        "ctrl+s",
			OpenBrowser: "o",
		},
	}
}

func Load() (*Config, error) {
	path := Path()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := DefaultConfig()
		_ = Save(cfg)
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		cfg.GitHub.Token = t
	}
	if t := os.Getenv("DOT_GITHUB_TOKEN"); t != "" {
		cfg.GitHub.Token = t
	}

	if t := os.Getenv("JIRA_API_TOKEN"); t != "" {
		cfg.Jira.Token = t
	}
	if t := os.Getenv("DOT_JIRA_TOKEN"); t != "" {
		cfg.Jira.Token = t
	}

	return cfg, nil
}

func Save(cfg *Config) error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "dot", "config.yaml")
}

// ExpandPath replaces a leading ~ with the user's home directory.
func ExpandPath(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, p[1:])
}
