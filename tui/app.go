package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/ghclient"
	"github.com/aprilselby/dot/obsidian"
	"github.com/aprilselby/dot/storage"
)

type tabID int

const (
	tabGitHub tabID = iota
	tabMeetings
	tabTodos
	tabNotes
)

// statusMsg sets a temporary message in the status bar.
type statusMsg struct {
	text    string
	isError bool
}

// ghSyncRequestMsg asks app to sync the Obsidian daily note.
type ghSyncRequestMsg struct{}

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

	width  int
	height int
	status string
	isErr  bool
}

// New constructs the root App model. Heavy work is deferred to Init().
func New(cfg *config.Config) App {
	store, _ := storage.New(config.ExpandPath(cfg.Storage.Dir))

	var ghc *ghclient.Client
	if cfg.GitHub.Token != "" {
		ghc, _ = ghclient.New(&cfg.GitHub)
	}

	a := App{
		cfg:   cfg,
		store: store,
		obs:   obsidian.New(cfg),
		ghc:   ghc,
	}
	a.github = newGithubTab(cfg, store, ghc)
	a.meetings = newMeetingsTab(cfg, store)
	a.todos = newTodosTab(cfg, store)
	a.notes = newNotesTab(cfg, store)
	return a
}

func (a App) Init() tea.Cmd {
	return tea.Batch(
		a.github.Init(),
		a.meetings.Init(),
		a.todos.Init(),
		a.notes.Init(),
	)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		contentH := msg.Height - 3 // tab bar + status bar
		var cmd tea.Cmd
		a.github, cmd = a.github.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		a.meetings, cmd = a.meetings.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		a.todos, cmd = a.todos.UpdateSize(msg.Width, contentH)
		cmds = append(cmds, cmd)
		a.notes, cmd = a.notes.UpdateSize(msg.Width, contentH)
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
			case a.cfg.Hotkeys.TabGitHub:
				a.active = tabGitHub
				return a, nil
			case a.cfg.Hotkeys.TabMeetings:
				a.active = tabMeetings
				return a, nil
			case a.cfg.Hotkeys.TabTodos:
				a.active = tabTodos
				return a, nil
			case a.cfg.Hotkeys.TabNotes:
				a.active = tabNotes
				return a, nil
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
		}
		return a, cmd

	default:
		// Non-key messages (async results): route to all tabs so background
		// operations (GitHub polling, storage loads) are always delivered.
		todosWereLoaded := a.todos.loaded
		var cmd tea.Cmd
		a.github, cmd = a.github.Update(msg)
		cmds = append(cmds, cmd)
		a.meetings, cmd = a.meetings.Update(msg)
		cmds = append(cmds, cmd)
		a.todos, cmd = a.todos.Update(msg)
		cmds = append(cmds, cmd)
		a.notes, cmd = a.notes.Update(msg)
		cmds = append(cmds, cmd)

		// Auto-sync to Obsidian whenever the user changes data.
		switch msg.(type) {
		case noteSavedMsg:
			cmds = append(cmds, a.syncObsidian())
		case meetingSavedMsg:
			// Reload todos tab — saveMeeting may have created action-item todos.
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
	entries := []entry{
		{"GitHub", tabGitHub, a.cfg.Hotkeys.TabGitHub},
		{"Meetings", tabMeetings, a.cfg.Hotkeys.TabMeetings},
		{"Todos", tabTodos, a.cfg.Hotkeys.TabTodos},
		{"Notes", tabNotes, a.cfg.Hotkeys.TabNotes},
	}

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
	hints := keyStyle.Render("1-4") + mutedStyle.Render(" tabs") +
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
		path, err := a.obs.Write(stats, todos, meetings, prs, note)
		if err != nil {
			return statusMsg{text: "Obsidian sync failed: " + err.Error(), isError: true}
		}
		return statusMsg{text: "Obsidian → " + path}
	}
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
	}
	return false
}
