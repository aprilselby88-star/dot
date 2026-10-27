package tui

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/obsidian"
	"github.com/aprilselby/dot/storage"
)

func keys(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// run executes a command and returns its message, or nil for nil commands.
func run(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestOneOnOnePrepFlow(t *testing.T) {
	vault := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Obsidian.VaultPath = vault
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tab := newOneOnOnesTab(cfg, store, obsidian.New(cfg))
	tab, _ = tab.UpdateSize(100, 40)

	// Add a person: n → name → enter → kind (manager is first) → enter.
	tab, _ = tab.Update(keys("n"))
	tab, _ = tab.Update(keys("Omar Haddad"))
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab, cmd := tab.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tab, cmd = tab.Update(run(cmd))
	if tab.mode != ooModePerson || tab.person.Kind != config.KindManager || tab.person.Handle != "omar" {
		t.Fatalf("after adding person: mode=%v person=%+v", tab.mode, tab.person)
	}
	// cmd is a batch (people + person); load the person directly.
	tab, _ = tab.Update(run(tab.loadPerson()))

	// Start a prep sheet and fill in Wins, then the last section (Notes).
	tab, _ = tab.Update(keys("n"))
	if tab.mode != ooModeEdit || tab.sections[0].Title != "Wins" {
		t.Fatalf("editor not opened on Wins: mode=%v", tab.mode)
	}
	tab, _ = tab.Update(keys("shipped catalog v1"))
	tab, _ = tab.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if tab.sections[tab.secIdx].Title != notesSectionTitle {
		t.Fatalf("shift+tab from first section should wrap to Notes, got %q", tab.sections[tab.secIdx].Title)
	}
	tab, _ = tab.Update(keys("Action: send Omar the plan"))
	tab, cmd = tab.Update(tea.KeyMsg{Type: tea.KeyEsc})
	saved, ok := run(cmd).(ooSavedMsg)
	if !ok || saved.err != nil {
		t.Fatalf("esc should save: %#v", saved)
	}

	sessions, _ := store.LoadOneOnOnes(tab.person.ID)
	if len(sessions) != 1 || sessions[0].Section("Wins") != "shipped catalog v1" || !sessions[0].Held() {
		t.Fatalf("saved session wrong: %+v", sessions)
	}
	todos, _ := store.LoadTodos()
	if len(todos) != 1 || todos[0].Text != "send Omar the plan" || todos[0].SourceMeetingTitle != "1:1 · Omar Haddad" {
		t.Fatalf("action item todo wrong: %+v", todos)
	}
	data, err := os.ReadFile(saved.path)
	if err != nil || !strings.Contains(string(data), "### Wins\n\nshipped catalog v1") {
		t.Fatalf("person file %s: err=%v\n%s", saved.path, err, data)
	}

	// Next prep shows last session's Next priorities / Asks as context; Wins links to Next priorities.
	tab, _ = tab.Update(saved)
	tab, _ = tab.Update(run(tab.loadPerson()))
	tab, _ = tab.Update(keys("n"))
	if tab.session.ID == sessions[0].ID {
		t.Fatal("held session should not be reused for a new prep")
	}
	if tab.prev == nil || tab.prev.ID != sessions[0].ID {
		t.Fatalf("prev session not linked: %+v", tab.prev)
	}
	if !strings.Contains(tab.View(), "1:1 with Omar Haddad") {
		t.Fatalf("editor view missing header:\n%s", tab.View())
	}
}
