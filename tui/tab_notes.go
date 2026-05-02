package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

// ---- messages ----

type noteLoadedMsg struct {
	date    string
	content string
}

type noteSavedMsg struct{ err error }

// ---- model ----

type notesTab struct {
	cfg    *config.Config
	store  *storage.Store
	width  int
	height int

	today   string
	area    textarea.Model
	tagLine textinput.Model
	dirty   bool
	loaded  bool
	saved   bool
}

func newNotesTab(cfg *config.Config, store *storage.Store) notesTab {
	ta := textarea.New()
	ta.Placeholder = "Today's notes — dump anything here. It will appear in your daily Obsidian summary."
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // unlimited

	tl := textinput.New()
	tl.Placeholder = "Tags for today (e.g. #work #alpha)"
	tl.CharLimit = 200

	return notesTab{
		cfg:     cfg,
		store:   store,
		today:   time.Now().Format("2006-01-02"),
		area:    ta,
		tagLine: tl,
	}
}

func (t notesTab) Editing() bool { return t.area.Focused() }

func (t notesTab) Init() tea.Cmd {
	return t.loadNote()
}

func (t notesTab) UpdateSize(w, h int) (notesTab, tea.Cmd) {
	t.width, t.height = w, h
	t.area.SetWidth(w - 4)
	t.area.SetHeight(h - 10)
	return t, nil
}

func (t notesTab) Update(msg tea.Msg) (notesTab, tea.Cmd) {
	switch msg := msg.(type) {

	case noteLoadedMsg:
		if msg.date == t.today {
			t.area.SetValue(msg.content)
			t.loaded = true
			t.area.Focus()
			return t, textarea.Blink
		}

	case noteSavedMsg:
		if msg.err == nil {
			t.dirty = false
			t.saved = true
		}
		return t, func() tea.Msg {
			if msg.err != nil {
				return statusMsg{text: "Save failed: " + msg.err.Error(), isError: true}
			}
			return statusMsg{text: "Notes saved"}
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+s":
			return t, t.saveNote()
		case "esc":
			if t.area.Focused() {
				t.area.Blur()
			}
			return t, nil
		case "i", "enter":
			if !t.area.Focused() {
				t.area.Focus()
				return t, textarea.Blink
			}
		}

		var cmds []tea.Cmd
		var cmd tea.Cmd
		t.area, cmd = t.area.Update(msg)
		cmds = append(cmds, cmd)
		t.tagLine, cmd = t.tagLine.Update(msg)
		cmds = append(cmds, cmd)
		t.dirty = true
		return t, tea.Batch(cmds...)
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	t.area, cmd = t.area.Update(msg)
	cmds = append(cmds, cmd)
	return t, tea.Batch(cmds...)
}

func (t notesTab) saveNote() tea.Cmd {
	content := t.area.Value()
	today := t.today
	return func() tea.Msg {
		err := t.store.SaveNote(today, content)
		return noteSavedMsg{err: err}
	}
}

func (t notesTab) loadNote() tea.Cmd {
	today := t.today
	return func() tea.Msg {
		content, _ := t.store.LoadNote(today)
		return noteLoadedMsg{date: today, content: content}
	}
}

func (t notesTab) View() string {
	var b strings.Builder

	dateLabel := mutedStyle.Render(time.Now().Format("Monday, 02 January 2006"))
	unsaved := ""
	if t.dirty {
		unsaved = "  " + subtleStyle.Render("unsaved")
	}
	b.WriteString("\n  " + titleStyle.Render("Notes") + "  " + dateLabel + unsaved + "\n\n")

	boxStyle := inputBoxStyle
	if t.area.Focused() {
		boxStyle = inputBoxFocusedStyle
	}
	b.WriteString(boxStyle.Width(t.width-4).Render(t.area.View()) + "\n\n")

	if t.area.Focused() {
		b.WriteString(buildHints([]string{"Ctrl+S", "Esc"}, []string{"save", "unfocus"}))
	} else {
		b.WriteString(buildHints([]string{"i / Enter"}, []string{"edit"}))
	}

	b.WriteString(hintStyle.Render("  Tip: use #tags in your notes — they appear in your Obsidian daily note.") + "\n")

	return b.String()
}
