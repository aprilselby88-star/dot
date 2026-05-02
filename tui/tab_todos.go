package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

// ---- messages ----

type todosLoadedMsg struct {
	todos []storage.TodoItem
	err   error
}

type carryOverDoneMsg struct {
	count int
}

type allTagsLoadedMsg struct {
	tags []string
	err  error
}

// ---- mode ----

type todoMode int

const (
	todoModeList todoMode = iota
	todoModeAdd
	todoModeEditText
	todoModeAddTags
)

// ---- model ----

type todosTab struct {
	cfg    *config.Config
	store  *storage.Store
	width  int
	height int

	mode   todoMode
	todos  []storage.TodoItem
	cursor int
	loaded bool

	input    textinput.Model
	tagInput textinput.Model
	editID   string

	availableTags []string
	tagSuggestIdx int
}

func newTodosTab(cfg *config.Config, store *storage.Store) todosTab {
	ti := textinput.New()
	ti.Placeholder = "What needs doing? Use @person to assign"
	ti.CharLimit = 200

	tgi := textinput.New()
	tgi.Placeholder = "Tags (space-separated, e.g. #work #alpha) — optional"
	tgi.CharLimit = 150

	return todosTab{
		cfg:      cfg,
		store:    store,
		mode:     todoModeList,
		input:    ti,
		tagInput: tgi,
	}
}

func (t todosTab) Editing() bool { return t.mode != todoModeList }

func (t todosTab) Init() tea.Cmd {
	return tea.Batch(t.loadTodos(), t.loadAllTags())
}

func (t todosTab) UpdateSize(w, h int) (todosTab, tea.Cmd) {
	t.width, t.height = w, h
	return t, nil
}

func (t todosTab) Update(msg tea.Msg) (todosTab, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case todosLoadedMsg:
		if msg.err == nil {
			wasLoaded := t.loaded
			t.todos = msg.todos
			t.loaded = true
			if !wasLoaded {
				return t, t.carryOver()
			}
		}

	case carryOverDoneMsg:
		if msg.count > 0 {
			return t, tea.Batch(
				t.loadTodos(),
				func() tea.Msg {
					return statusMsg{text: fmt.Sprintf("Carried %d todo(s) forward from previous day", msg.count)}
				},
			)
		}

	case allTagsLoadedMsg:
		if msg.err == nil {
			t.availableTags = msg.tags
		}

	case tea.KeyMsg:
		var cmd tea.Cmd
		switch t.mode {
		case todoModeList:
			cmd = t.handleListKey(msg)
			return t, cmd
		case todoModeAdd, todoModeEditText:
			t, cmd = t.handleInputKey(msg)
			if cmd == nil {
				t.input, cmd = t.input.Update(msg)
			}
			return t, cmd
		case todoModeAddTags:
			t, cmd = t.handleTagKey(msg)
			if cmd == nil {
				t.tagInput, cmd = t.tagInput.Update(msg)
			}
			return t, cmd
		}
	}

	// Propagate to inputs
	if t.mode == todoModeAdd || t.mode == todoModeEditText {
		var cmd tea.Cmd
		t.input, cmd = t.input.Update(msg)
		cmds = append(cmds, cmd)
	}
	if t.mode == todoModeAddTags {
		var cmd tea.Cmd
		t.tagInput, cmd = t.tagInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	return t, tea.Batch(cmds...)
}

func (t *todosTab) handleListKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if t.cursor < len(t.todos)-1 {
			t.cursor++
		}
	case "k", "up":
		if t.cursor > 0 {
			t.cursor--
		}
	case t.cfg.Hotkeys.NewItem:
		t.mode = todoModeAdd
		t.input.SetValue("")
		t.input.Focus()
		return textinput.Blink
	case " ", "enter":
		if len(t.todos) > 0 {
			return t.toggleTodo(t.cursor)
		}
	case "e":
		if len(t.todos) > 0 {
			t.mode = todoModeEditText
			t.editID = t.todos[t.cursor].ID
			t.input.SetValue(t.todos[t.cursor].Text)
			t.input.Focus()
			return textinput.Blink
		}
	case t.cfg.Hotkeys.Delete:
		if len(t.todos) > 0 {
			return t.deleteTodo(t.cursor)
		}
	case t.cfg.Hotkeys.Refresh:
		return t.loadTodos()
	}
	return nil
}

func (t todosTab) handleInputKey(msg tea.KeyMsg) (todosTab, tea.Cmd) {
	switch msg.String() {
	case "enter":
		text := strings.TrimSpace(t.input.Value())
		if text == "" {
			t.mode = todoModeList
			t.input.Blur()
			return t, nil
		}
		if t.mode == todoModeAdd {
			t.mode = todoModeAddTags
			t.tagInput.SetValue("")
			t.tagSuggestIdx = 0
			t.input.Blur()
			t.tagInput.Focus()
			return t, tea.Batch(textinput.Blink, t.loadAllTags())
		}
		if t.mode == todoModeEditText {
			cmd := t.editTodo(t.editID, text)
			t.mode = todoModeList
			t.input.Blur()
			return t, cmd
		}
	case "esc":
		t.mode = todoModeList
		t.input.Blur()
	}
	return t, nil
}

func (t todosTab) handleTagKey(msg tea.KeyMsg) (todosTab, tea.Cmd) {
	text := strings.TrimSpace(t.input.Value())
	switch msg.String() {
	case "enter":
		tags := parseTags(t.tagInput.Value())
		t.mode = todoModeList
		t.input.Blur()
		t.tagInput.Blur()
		if text != "" {
			return t, t.addTodo(text, tags)
		}
	case "esc":
		t.mode = todoModeList
		t.input.Blur()
		t.tagInput.Blur()
		if text != "" {
			return t, t.addTodo(text, nil)
		}
	case "tab":
		newVal := tagComplete(t.tagInput.Value(), t.availableTags, t.tagSuggestIdx)
		t.tagInput.SetValue(newVal)
		t.tagSuggestIdx++
		return t, textinput.Blink
	default:
		t.tagSuggestIdx = 0
	}
	return t, nil
}

func (t todosTab) addTodo(text string, tags []string) tea.Cmd {
	return func() tea.Msg {
		all, _ := t.store.LoadTodos()
		now := time.Now()
		all = append(all, storage.TodoItem{
			ID:   fmt.Sprintf("%d", now.UnixNano()),
			Text: text,
			Date: now.Format("2006-01-02"),
			Tags: tags,
		})
		_ = t.store.SaveTodos(all)
		todos, err := t.store.TodaysTodos()
		return todosLoadedMsg{todos: todos, err: err}
	}
}

func (t todosTab) toggleTodo(idx int) tea.Cmd {
	if idx >= len(t.todos) {
		return nil
	}
	id := t.todos[idx].ID
	return func() tea.Msg {
		all, _ := t.store.LoadTodos()
		for i, item := range all {
			if item.ID == id {
				all[i].Done = !all[i].Done
				if all[i].Done {
					now := time.Now()
					all[i].DoneAt = &now
				} else {
					all[i].DoneAt = nil
				}
				break
			}
		}
		_ = t.store.SaveTodos(all)
		todos, err := t.store.TodaysTodos()
		return todosLoadedMsg{todos: todos, err: err}
	}
}

func (t todosTab) deleteTodo(idx int) tea.Cmd {
	if idx >= len(t.todos) {
		return nil
	}
	id := t.todos[idx].ID
	return func() tea.Msg {
		all, _ := t.store.LoadTodos()
		out := all[:0]
		for _, item := range all {
			if item.ID != id {
				out = append(out, item)
			}
		}
		_ = t.store.SaveTodos(out)
		todos, err := t.store.TodaysTodos()
		return todosLoadedMsg{todos: todos, err: err}
	}
}

func (t todosTab) editTodo(id, text string) tea.Cmd {
	return func() tea.Msg {
		all, _ := t.store.LoadTodos()
		for i, item := range all {
			if item.ID == id {
				all[i].Text = text
				break
			}
		}
		_ = t.store.SaveTodos(all)
		todos, err := t.store.TodaysTodos()
		return todosLoadedMsg{todos: todos, err: err}
	}
}

func (t todosTab) loadTodos() tea.Cmd {
	return func() tea.Msg {
		todos, err := t.store.TodaysTodos()
		return todosLoadedMsg{todos: todos, err: err}
	}
}

func (t todosTab) loadAllTags() tea.Cmd {
	return func() tea.Msg {
		tags, err := t.store.AllTags()
		return allTagsLoadedMsg{tags: tags, err: err}
	}
}

func (t todosTab) carryOver() tea.Cmd {
	return func() tea.Msg {
		count, _ := t.store.CarryOver()
		return carryOverDoneMsg{count: count}
	}
}

func (t todosTab) View() string {
	var b strings.Builder

	switch t.mode {
	case todoModeAdd:
		b.WriteString("\n")
		b.WriteString(inputLabelStyle.Render("  New Todo") + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.input.View()) + "\n\n")
		b.WriteString(buildHints([]string{"Enter", "Esc"}, []string{"add tags", "cancel"}))
		b.WriteString(hintStyle.Render("  Tip: use @person to assign") + "\n")
		return b.String()

	case todoModeEditText:
		b.WriteString("\n")
		b.WriteString(inputLabelStyle.Render("  Edit Todo") + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.input.View()) + "\n\n")
		b.WriteString(buildHints([]string{"Enter", "Esc"}, []string{"save", "cancel"}))
		return b.String()

	case todoModeAddTags:
		b.WriteString("\n")
		b.WriteString(inputLabelStyle.Render("  Tags") + "  " + mutedStyle.Render(t.input.Value()) + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.tagInput.View()) + "\n")
		if hint := tagSuggestionLine(t.tagInput.Value(), t.availableTags); hint != "" {
			b.WriteString("  " + subtleStyle.Render(hint) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(buildHints([]string{"Enter", "Tab", "Esc"}, []string{"save", "complete tag", "save without tags"}))
		return b.String()
	}

	// ---- List mode ----
	today := time.Now().Format("2006-01-02")

	done, total := 0, len(t.todos)
	for _, todo := range t.todos {
		if todo.Done {
			done++
		}
	}

	// Header
	dateLabel := mutedStyle.Render(time.Now().Format("Monday, 02 Jan"))
	progress := ""
	if total > 0 {
		progress = "  " + renderProgress(done, total)
	}
	b.WriteString("\n  " + titleStyle.Render("Todos") + "  " + dateLabel + progress + "\n\n")

	if !t.loaded {
		b.WriteString("  " + mutedStyle.Render("Loading…") + "\n")
		return b.String()
	}
	if total == 0 {
		b.WriteString("  " + mutedStyle.Render("Nothing here yet.") + "\n")
		b.WriteString("  " + hintStyle.Render("Press "+keyStyle.Render(t.cfg.Hotkeys.NewItem)+" to add a todo.") + "\n")
		return b.String()
	}

	maxRows := t.height - 7
	if maxRows < 1 {
		maxRows = 10
	}

	for i, todo := range t.todos {
		if i >= maxRows {
			b.WriteString("  " + mutedStyle.Render(fmt.Sprintf("… %d more", total-maxRows)) + "\n")
			break
		}

		cursor := "  "
		if i == t.cursor {
			cursor = "▸ "
		}

		check := lipgloss.NewStyle().Foreground(colorSubtle).Render("○")
		if todo.Done {
			check = lipgloss.NewStyle().Foreground(colorPine).Render("✓")
		}

		carried := ""
		if todo.CarriedFrom != "" && todo.CarriedFrom != today {
			carried = "  " + carriedStyle.Render("↑ "+todo.CarriedFrom)
		}

		tagStr := ""
		if len(todo.Tags) > 0 {
			tagStr = "  " + tagStyle.Render(strings.Join(todo.Tags, " "))
		}

		meta := ""
		if todo.Done && todo.DoneAt != nil {
			if todo.DoneAt.Format("2006-01-02") == today {
				meta = "  " + subtleStyle.Render("done "+todo.DoneAt.Format("15:04"))
			} else {
				meta = "  " + subtleStyle.Render("done "+todo.DoneAt.Format("02 Jan"))
			}
		}

		if todo.SourceMeetingTitle != "" {
			meta += "  " + subtleStyle.Render("↩ "+todo.SourceMeetingTitle)
		}

		var line string
		if todo.Done {
			raw := fmt.Sprintf("%s%s  %s", cursor, "✓", todo.Text)
			line = doneStyle.Render(raw) + meta
		} else {
			body := cursor + check + "  " + renderTodoText(todo.Text) + carried + tagStr + meta
			if i == t.cursor {
				line = selectedStyle.Width(t.width).Render(cursor + "○  " + todo.Text + carried + tagStr + meta)
			} else if todo.CarriedFrom != "" {
				line = carriedStyle.Render(cursor+"○") + "  " + renderTodoText(todo.Text) + carried + tagStr + meta
			} else {
				line = body
			}
		}

		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	b.WriteString(buildHints(
		[]string{"j/k", "Space", t.cfg.Hotkeys.NewItem, "e", t.cfg.Hotkeys.Delete},
		[]string{"navigate", "toggle done", "new", "edit", "delete"},
	))
	return b.String()
}

// renderProgress returns a compact "done/total" indicator with colour.
func renderProgress(done, total int) string {
	ratio := fmt.Sprintf("%d/%d", done, total)
	if done == total && total > 0 {
		return successStyle.Render("✓ " + ratio)
	}
	return mutedStyle.Render(ratio)
}

// renderTodoText renders todo text with @mentions highlighted in cyan.
func renderTodoText(text string) string {
	words := strings.Fields(text)
	var parts []string
	for _, w := range words {
		clean := strings.TrimRight(w, ".,;:!?")
		if strings.HasPrefix(clean, "@") && len(clean) > 1 {
			parts = append(parts, mentionStyle.Render(w))
		} else {
			parts = append(parts, w)
		}
	}
	return strings.Join(parts, " ")
}

// tagComplete returns the new input value with the current partial word completed.
// idx cycles through matching tags (caller increments after use).
// The user's # prefix choice is preserved: if they typed without #, completion won't add it.
func tagComplete(current string, available []string, idx int) string {
	if len(available) == 0 {
		return current
	}
	endsWithSpace := strings.HasSuffix(current, " ")
	words := strings.Fields(current)

	var last, prefix string
	if !endsWithSpace && len(words) > 0 {
		last = words[len(words)-1]
		prefix = strings.ToLower(strings.TrimPrefix(last, "#"))
	}

	var matches []string
	for _, tag := range available {
		if strings.HasPrefix(strings.ToLower(strings.TrimPrefix(tag, "#")), prefix) {
			matches = append(matches, tag)
		}
	}
	if len(matches) == 0 {
		return current
	}

	completed := matches[idx%len(matches)]
	// Preserve user's style: if they didn't type #, don't add it
	if last != "" && !strings.HasPrefix(last, "#") {
		completed = strings.TrimPrefix(completed, "#")
	}

	if endsWithSpace || len(words) == 0 {
		return strings.TrimRight(current, " ") + " " + completed + " "
	}
	words[len(words)-1] = completed
	return strings.Join(words, " ") + " "
}

// tagSuggestionLine returns a compact hint showing tags that match the current partial word.
func tagSuggestionLine(current string, available []string) string {
	if len(available) == 0 {
		return ""
	}
	endsWithSpace := strings.HasSuffix(current, " ")
	words := strings.Fields(current)

	var prefix string
	if !endsWithSpace && len(words) > 0 {
		prefix = strings.ToLower(strings.TrimPrefix(words[len(words)-1], "#"))
	}

	var matches []string
	for _, tag := range available {
		if strings.HasPrefix(strings.ToLower(strings.TrimPrefix(tag, "#")), prefix) {
			matches = append(matches, tag)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	const maxShow = 8
	shown := matches
	ellipsis := ""
	if len(shown) > maxShow {
		shown = shown[:maxShow]
		ellipsis = " …"
	}
	return strings.Join(shown, "  ") + ellipsis
}
