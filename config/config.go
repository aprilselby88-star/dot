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
	OneOnOne OneOnOneConfig `yaml:"one_on_ones"`
}

// OneOnOneConfig controls the 1:1 prep tab.
// Dir is vault-relative; each person gets a running file at <Dir>/<Name>.md.
// Templates overrides the built-in prep sheet for a kind (manager, skip, peer, report);
// kinds left out keep the built-in default from DefaultOneOnOneTemplates.
type OneOnOneConfig struct {
	Dir       string                       `yaml:"dir"`
	Templates map[string][]OneOnOneSection `yaml:"templates,omitempty"`
}

// OneOnOneSection is one heading on a prep sheet.
// Hint is shown as placeholder text in the empty box; "{name}" is replaced with the person's name.
// ContextFrom names a section of the previous session (or "Notes" for its live notes)
// whose text is shown above this section's editor while prepping.
type OneOnOneSection struct {
	Title       string `yaml:"title"`
	Hint        string `yaml:"hint"`
	ContextFrom string `yaml:"context_from,omitempty"`
}

// 1:1 kinds. Each maps to a prep sheet template.
const (
	KindManager = "manager"
	KindSkip    = "skip"
	KindPeer    = "peer"
	KindReport  = "report"
)

// OneOnOneKinds lists the kinds in display order.
var OneOnOneKinds = []string{KindManager, KindSkip, KindPeer, KindReport}

// Template returns the prep sheet sections for a kind: the config override if set,
// otherwise the built-in default.
func (c OneOnOneConfig) Template(kind string) []OneOnOneSection {
	if t := c.Templates[kind]; len(t) > 0 {
		return t
	}
	return DefaultOneOnOneTemplates()[kind]
}

// DefaultOneOnOneTemplates returns the built-in prep sheet for each 1:1 kind.
func DefaultOneOnOneTemplates() map[string][]OneOnOneSection {
	return map[string][]OneOnOneSection{
		KindManager: {
			{
				Title:       "Wins",
				Hint:        "What materially improved? Focus on outcomes, not activity.\n\ne.g. Mapped the streaming dependencies and identified two manual provisioning steps suitable for automation.",
				ContextFrom: "Next priorities",
			},
			{
				Title:       "Misses / Learnings",
				Hint:        "What slipped, failed, or turned out differently than expected? Include the corrective action.\n\ne.g. Catalog API requirements aren't yet aligned across consumers. I'm arranging a design review before committing to an interface.",
				ContextFrom: "Next priorities",
			},
			{
				Title: "Blockers / Risks",
				Hint:  "What needs intervention, a decision, or escalation?\n\ne.g. Automated vending depends on agreement about infrastructure ownership and access policies. I need alignment with the relevant platform stakeholders.",
			},
			{
				Title: "Next priorities",
				Hint:  "What are the 1–3 most important outcomes before your next 1:1?\n\ne.g. Agree the catalog API's initial scope, establish an operational baseline, and nominate an owner for the first automation opportunity.",
			},
			{
				Title:       "Asks / Decisions",
				Hint:        "What specifically do you need from {name}?\n\ne.g. Confirm whether the first release should prioritize internal platform consumers or broader self-service.",
				ContextFrom: "Asks / Decisions",
			},
		},
		KindSkip: {
			{Title: "What I want them to know", Hint: "Themes, not status. What should {name} understand about your area?"},
			{Title: "Org / strategy questions", Hint: "What's unclear about direction, priorities, or how decisions are being made?"},
			{Title: "Risks they may not see", Hint: "What's brewing that won't show up in status reports?"},
			{Title: "Feedback on team / process", Hint: "What's working, what isn't, what would you change?"},
			{Title: "Asks", Hint: "What specifically do you need from {name}?", ContextFrom: "Asks"},
		},
		KindPeer: {
			{Title: "What I'm working on that touches you", Hint: "Changes, launches, or decisions that affect {name}'s area."},
			{Title: "Overlaps / blockers", Hint: "Where are we overlapping or blocking each other?"},
			{Title: "Asks / offers", Hint: "What do you need from {name}, and what can you offer?", ContextFrom: "Asks / offers"},
			{Title: "Things they should know", Hint: "What are you hearing that {name} should know about?"},
		},
		KindReport: {
			{Title: "How are you doing?", Hint: "Energy, workload, anything outside work affecting things."},
			{Title: "What's on your mind?", Hint: "Their agenda first. What does {name} want to talk about?"},
			{Title: "Progress on their priorities", Hint: "How are {name}'s priorities tracking?", ContextFrom: "Progress on their priorities"},
			{Title: "Feedback", Hint: "Both ways: what to give {name}, and what to ask for."},
			{Title: "Growth / career", Hint: "The longer-running thread: goals, stretch work, skills."},
			{Title: "Follow-ups from last time", Hint: "What did you each commit to last time?", ContextFrom: "Notes"},
		},
	}
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
	TabOneOnOne string `yaml:"tab_one_on_ones"`
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
		OneOnOne: OneOnOneConfig{
			Dir: "1-1s",
		},
		Hotkeys: HotkeyConfig{
			TabGitHub:   "1",
			TabMeetings: "2",
			TabTodos:    "3",
			TabNotes:    "4",
			TabJira:     "5",
			TabOneOnOne: "6",
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
