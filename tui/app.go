package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/ghclient"
	"github.com/aprilselby/dot/jira"
	"github.com/aprilselby/dot/obsidian"
	"github.com/aprilselby/dot/storage"
)

type tabID int

const (
	tabGitHub tabID = iota
	tabMeetings
	tabTodos
	tabNotes
	tabJira
	tabOneOnOnes
)

// statusMsg sets a temporary message in the status bar.
type statusMsg struct {
	text    string
	isError bool
}

// ghSyncRequestMsg asks app to sync the Obsidian daily note.
type ghSyncRequestMsg struct{}

// summaryGeneratedMsg is returned after attempting periodic summary generation.
type summaryGeneratedMsg struct {
	paths []string
	err   error
}

// App is the root bubbletea model.
type App struct {
	cfg    *config.Config
	store  *storage.Store
	obs    *obsidian.Writer
	ghc    *ghclient.Client // nil if no token configured
	active tabID

	github   githubTab
	meetings meetingsTab
	todos    todosTab
	notes    notesTab
	jira     jiraTab
	ones     oneOnOnesTab

	width  int
	height int
	status string
	isErr  bool
}

// New constructs the root App model. Heavy work is deferred to Init().
func New(cfg *config.Config) App {
	store, _ := storage.New(config.ExpandPath(cfg.Storage.Dir))

	var ghc *ghclient.Client
	if cfg.Features.GitHub && cfg.GitHub.Token != "" {
		ghc, _ = ghclient.New(&cfg.GitHub)
	}

	// jc is nil when Jira isn't configured or acli is missing; the tab handles that.
	jc, _ := jira.New(&cfg.Jira)

	a := App{
		cfg:   cfg,
		store: store,
		obs:   obsidian.New(cfg),
		ghc:   ghc,
	}
	if !cfg.Features.GitHub {
		a.active = tabMeetings
	}
	a.github = newGithubTab(cfg, store, ghc)
	a.meetings = newMeetingsTab(cfg, store)
	a.todos = newTodosTab(cfg, store)
	a.notes = newNotesTab(cfg, store)
	a.jira = newJiraTab(cfg, jc)
	a.ones = newOneOnOnesTab(cfg, store, a.obs)
	return a
}

func (a App) Init() tea.Cmd {
	var ghInit tea.Cmd
	if a.githubEnabled() {
		ghInit = a.github.Init()
	}
	return tea.Batch(
		tea.EnableBracketedPaste,
		ghInit,
		a.meetings.Init(),
		a.todos.Init(),
		a.notes.Init(),
		a.jira.Init(),
		a.ones.Init(),
		a.checkSummaries(),
	)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		contentH := msg.Height - 3 // tab bar + status bar
		var cmd tea.Cmd
		if a.githubEnabled() {
			a.github, cmd = a.github.UpdateSize(msg.Width, contentH)
			cmds = append(cmds, cmd)
		}
		a.meetings, cmd = a.meetings.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		a.todos, cmd = a.todos.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		a.notes, cmd = a.notes.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		a.jira, cmd = a.jira.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		a.ones, cmd = a.ones.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		return a, tea.Batch(cmds...)

	case statusMsg:
		a.status = msg.text
		a.isErr = msg.isError
		return a, nil

	case tea.KeyMsg:
		editing := a.isEditing()

		// ctrl+c always exits
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}

		// Global tab switching — only when not typing
		if !editing {
			switch msg.String() {
			case "q":
				return a, tea.Quit
			case a.githubHotkey():
				cmd := a.saveNotesIfLeaving(tabGitHub)
				a.active = tabGitHub
				return a, cmd
			case a.cfg.Hotkeys.TabMeetings:
				cmd := a.saveNotesIfLeaving(tabMeetings)
				a.active = tabMeetings
				return a, cmd
			case a.cfg.Hotkeys.TabTodos:
				cmd := a.saveNotesIfLeaving(tabTodos)
				a.active = tabTodos
				return a, cmd
			case a.cfg.Hotkeys.TabNotes:
				a.active = tabNotes
				return a, nil
			case a.cfg.Hotkeys.TabJira:
				cmd := a.saveNotesIfLeaving(tabJira)
				a.active = tabJira
				return a, cmd
			case a.cfg.Hotkeys.TabOneOnOne:
				cmd := a.saveNotesIfLeaving(tabOneOnOnes)
				a.active = tabOneOnOnes
				return a, cmd
			}
		}

		// Route key to the active tab only
		var cmd tea.Cmd
		switch a.active {
		case tabGitHub:
			a.github, cmd = a.github.Update(msg)
		case tabMeetings:
			a.meetings, cmd = a.meetings.Update(msg)
		case tabTodos:
			a.todos, cmd = a.todos.Update(msg)
		case tabNotes:
			a.notes, cmd = a.notes.Update(msg)
		case tabJira:
			a.jira, cmd = a.jira.Update(msg)
		case tabOneOnOnes:
			a.ones, cmd = a.ones.Update(msg)
		}
		return a, cmd

	default:
		// Non-key messages (async results): route to all tabs so background
		// operations (GitHub polling, storage loads) are always delivered.
		todosWereLoaded := a.todos.loaded
		var cmd tea.Cmd
		if a.githubEnabled() {
			a.github, cmd = a.github.Update(msg)
			cmds = append(cmds, cmd)
		}
		a.meetings, cmd = a.meetings.Update(msg)
		cmds = append(cmds, cmd)
		a.todos, cmd = a.todos.Update(msg)
		cmds = append(cmds, cmd)
		a.notes, cmd = a.notes.Update(msg)
		cmds = append(cmds, cmd)
		a.jira, cmd = a.jira.Update(msg)
		cmds = append(cmds, cmd)
		a.ones, cmd = a.ones.Update(msg)
		cmds = append(cmds, cmd)

		// Auto-sync to Obsidian whenever the user changes data.
		switch msg.(type) {
		case noteSavedMsg:
			cmds = append(cmds, a.syncObsidian())
		case meetingSavedMsg, ooSavedMsg:
			// Reload todos tab — saving may have created action-item todos.
			cmds = append(cmds, a.todos.loadTodos())
			cmds = append(cmds, a.syncObsidian())
		case meetingsLoadedMsg:
			// Sync after meetings reload so the file always reflects latest state.
			cmds = append(cmds, a.syncObsidian())
		case todosLoadedMsg:
			// Skip the very first load (init); sync on every subsequent load.
			if todosWereLoaded {
				cmds = append(cmds, a.syncObsidian())
			}
		case ghSyncRequestMsg, ghWatchPRDoneMsg:
			cmds = append(cmds, a.syncObsidian())
		case summaryGeneratedMsg:
			smsg := msg.(summaryGeneratedMsg)
			if smsg.err != nil {
				cmds = append(cmds, func() tea.Msg {
					return statusMsg{text: "Summary failed: " + smsg.err.Error(), isError: true}
				})
			} else if len(smsg.paths) > 0 {
				label := "Weekly summary"
				if len(smsg.paths) == 2 {
					label = "Weekly + monthly summaries"
				} else if strings.Contains(smsg.paths[0], "Month") {
					label = "Monthly summary"
				}
				p := smsg.paths[0]
				cmds = append(cmds, func() tea.Msg {
					return statusMsg{text: label + " → " + p}
				})
			}
		}

		return a, tea.Batch(cmds...)
	}
}

func (a App) View() string {
	if a.width == 0 {
		return "Loading…"
	}

	var content string
	switch a.active {
	case tabGitHub:
		content = a.github.View()
	case tabMeetings:
		content = a.meetings.View()
	case tabTodos:
		content = a.todos.View()
	case tabNotes:
		content = a.notes.View()
	case tabJira:
		content = a.jira.View()
	case tabOneOnOnes:
		content = a.ones.View()
	}

	sep := subtleStyle.Render(strings.Repeat("─", a.width))
	return a.tabBar() + "\n" + sep + "\n" + content + "\n" + a.statusBar()
}

func (a App) tabBar() string {
	type entry struct {
		label string
		id    tabID
		key   string
	}
	var entries []entry
	if a.githubEnabled() {
		entries = append(entries, entry{"GitHub", tabGitHub, a.cfg.Hotkeys.TabGitHub})
	}
	entries = append(entries, []entry{
		{"Meetings", tabMeetings, a.cfg.Hotkeys.TabMeetings},
		{"Todos", tabTodos, a.cfg.Hotkeys.TabTodos},
		{"Notes", tabNotes, a.cfg.Hotkeys.TabNotes},
		{"Jira", tabJira, a.cfg.Hotkeys.TabJira},
		{"1:1s", tabOneOnOnes, a.cfg.Hotkeys.TabOneOnOne},
	}...)

	var parts []string
	for _, e := range entries {
		label := fmt.Sprintf("%s %s", e.key, e.label)
		if e.id == a.active {
			parts = append(parts, tabActiveStyle.Render(label))
		} else {
			parts = append(parts, tabInactiveStyle.Render(label))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func (a App) statusBar() string {
	bar := statusBarStyle.Width(a.width)
	if a.status != "" {
		if a.isErr {
			return bar.Render(errorStyle.Render("✗  " + a.status))
		}
		return bar.Render(successStyle.Render("✓  " + a.status))
	}
	tabKeys := "2-6"
	if a.githubEnabled() {
		tabKeys = "1-6"
	}
	hints := keyStyle.Render(tabKeys) + mutedStyle.Render(" tabs") +
		"   " + keyStyle.Render("q") + mutedStyle.Render(" quit")
	return bar.Render(hints)
}

// syncObsidian writes today's full daily note to the Obsidian vault.
func (a App) syncObsidian() tea.Cmd {
	return func() tea.Msg {
		today := time.Now().Format("2006-01-02")
		stats, _ := a.store.ComputeStats()
		todos, _ := a.store.LoadTodos()
		meetings, _ := a.store.LoadMeetings()
		prs, _ := a.store.LoadTrackedPRs()
		note, _ := a.store.LoadNote(today)
		path, err := a.obs.Write(stats, todos, meetings, prs, note, a.todaysOneOnOnes(today))
		if err != nil {
			return statusMsg{text: "Obsidian sync failed: " + err.Error(), isError: true}
		}
		return statusMsg{text: "Obsidian → " + path}
	}
}

// checkSummaries generates weekly/monthly summaries if today triggers one and
// the file doesn't already exist. Called once on startup.
func (a App) checkSummaries() tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		var paths []string

		weekPath, err := a.obs.WriteWeeklySummaryIfNeeded(now, a.store)
		if err != nil {
			return summaryGeneratedMsg{err: err}
		}
		if weekPath != "" {
			paths = append(paths, weekPath)
		}

		monthPath, err := a.obs.WriteMonthlySummaryIfNeeded(now, a.store)
		if err != nil {
			return summaryGeneratedMsg{err: err}
		}
		if monthPath != "" {
			paths = append(paths, monthPath)
		}

		return summaryGeneratedMsg{paths: paths}
	}
}

// saveNotesIfLeaving returns a save command when the user is switching away
// from the notes tab with unsaved changes.
func (a App) saveNotesIfLeaving(next tabID) tea.Cmd {
	if a.active == tabNotes && next != tabNotes && a.notes.dirty {
		return a.notes.saveNote()
	}
	return nil
}

// githubEnabled reports whether the GitHub tab is turned on via features.github.
func (a App) githubEnabled() bool {
	return a.cfg.Features.GitHub
}

// githubHotkey returns the GitHub tab hotkey, or "" when the tab is disabled
// so the key never matches a keypress.
func (a App) githubHotkey() string {
	if !a.githubEnabled() {
		return ""
	}
	return a.cfg.Hotkeys.TabGitHub
}

func (a App) isEditing() bool {
	switch a.active {
	case tabGitHub:
		return a.github.Editing()
	case tabMeetings:
		return a.meetings.Editing()
	case tabTodos:
		return a.todos.Editing()
	case tabNotes:
		return a.notes.Editing()
	case tabJira:
		return a.jira.Editing()
	case tabOneOnOnes:
		return a.ones.Editing()
	}
	return false
}

// todaysOneOnOnes pairs today's 1:1 sessions with their people for the daily note.
func (a App) todaysOneOnOnes(today string) []obsidian.OneOnOneRef {
	sessions, _ := a.store.LoadOneOnOnesOn(today)
	if len(sessions) == 0 {
		return nil
	}
	people, _ := a.store.LoadPeople()
	byID := map[string]storage.Person{}
	for _, p := range people {
		byID[p.ID] = p
	}
	var refs []obsidian.OneOnOneRef
	for _, o := range sessions {
		if p, ok := byID[o.PersonID]; ok {
			refs = append(refs, obsidian.OneOnOneRef{Person: p, Session: o})
		}
	}
	return refs
}
