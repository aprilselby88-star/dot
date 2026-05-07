package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

// ---- messages ----

type meetingsLoadedMsg struct {
	meetings []storage.Meeting
	err      error
}
type meetingSavedMsg struct{ err error }

// ---- meeting form mode ----

type meetingMode int

const (
	meetingModeList      meetingMode = iota
	meetingModeView                  // viewing a single meeting
	meetingModeTitle                 // new: entering title
	meetingModeAttend                // new: entering attendees
	meetingModeTags                  // new: entering tags
	meetingModeNotes                 // new: entering raw notes
	meetingModeDecisions             // new: entering explicit decisions
)

// ---- model ----

type meetingsTab struct {
	cfg    *config.Config
	store  *storage.Store
	width  int
	height int

	mode     meetingMode
	meetings []storage.Meeting
	cursor   int

	// form fields
	titleInput     textinput.Model
	attendInput    textinput.Model
	tagsInput      textinput.Model
	notesArea      textarea.Model
	decisionsArea  textarea.Model

	// view pane
	vp viewport.Model

	formDate string // date being created

	availableTags []string
	tagSuggestIdx int
}

func newMeetingsTab(cfg *config.Config, store *storage.Store) meetingsTab {
	ti := textinput.New()
	ti.Placeholder = "Meeting title"
	ti.CharLimit = 120

	ai := textinput.New()
	ai.Placeholder = "Attendees (comma-separated)"
	ai.CharLimit = 300

	tgi := textinput.New()
	tgi.Placeholder = "Tags e.g. #alpha #work (optional)"
	tgi.CharLimit = 200

	ta := textarea.New()
	ta.Placeholder = "Dump your raw meeting notes here…\n\nTip: prefix lines with 'Action:' or '- [ ]' to auto-create todos."
	ta.ShowLineNumbers = false

	da := textarea.New()
	da.Placeholder = "One decision per line…\n\nExample:\nWe will migrate to PostgreSQL by Q3\n@alice owns the auth refactor"
	da.ShowLineNumbers = false

	return meetingsTab{
		cfg:           cfg,
		store:         store,
		mode:          meetingModeList,
		titleInput:    ti,
		attendInput:   ai,
		tagsInput:     tgi,
		notesArea:     ta,
		decisionsArea: da,
		vp:            viewport.New(80, 20),
	}
}

func (t meetingsTab) Editing() bool {
	return t.mode != meetingModeList && t.mode != meetingModeView
}

func (t meetingsTab) Init() tea.Cmd {
	return tea.Batch(t.loadMeetings(), t.loadAllTags())
}

func (t meetingsTab) UpdateSize(w, h int) (meetingsTab, tea.Cmd) {
	t.width, t.height = w, h
	t.notesArea.SetWidth(w - 4)
	t.notesArea.SetHeight(h - 10)
	t.decisionsArea.SetWidth(w - 4)
	t.decisionsArea.SetHeight(h - 10)
	t.vp.Width = w
	t.vp.Height = h - 4
	return t, nil
}

func (t meetingsTab) Update(msg tea.Msg) (meetingsTab, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case meetingsLoadedMsg:
		if msg.err == nil {
			t.meetings = msg.meetings
			t.cursor = 0
		}
		return t, nil

	case meetingSavedMsg:
		if msg.err == nil {
			t.mode = meetingModeList
			t.titleInput.SetValue("")
			t.titleInput.Blur()
			t.attendInput.SetValue("")
			t.tagsInput.SetValue("")
			t.notesArea.SetValue("")
			t.notesArea.Blur()
			t.decisionsArea.SetValue("")
			t.decisionsArea.Blur()
			return t, tea.Batch(
				t.loadMeetings(),
				func() tea.Msg { return statusMsg{text: "Meeting saved"} },
			)
		}
		return t, func() tea.Msg { return statusMsg{text: msg.err.Error(), isError: true} }

	case allTagsLoadedMsg:
		if msg.err == nil {
			t.availableTags = msg.tags
		}
		return t, nil

	case tea.KeyMsg:
		switch t.mode {
		case meetingModeList:
			return t, t.handleListKey(msg)
		case meetingModeView:
			return t, t.handleViewKey(msg)
		case meetingModeTitle:
			return t.handleTitleKey(msg)
		case meetingModeAttend:
			return t.handleAttendKey(msg)
		case meetingModeTags:
			return t.handleTagsKey(msg)
		case meetingModeNotes:
			return t.handleNotesKey(msg)
		case meetingModeDecisions:
			return t.handleDecisionsKey(msg)
		}
	}

	// Forward to focused components
	switch t.mode {
	case meetingModeTitle:
		var cmd tea.Cmd
		t.titleInput, cmd = t.titleInput.Update(msg)
		cmds = append(cmds, cmd)
	case meetingModeAttend:
		var cmd tea.Cmd
		t.attendInput, cmd = t.attendInput.Update(msg)
		cmds = append(cmds, cmd)
	case meetingModeTags:
		var cmd tea.Cmd
		t.tagsInput, cmd = t.tagsInput.Update(msg)
		cmds = append(cmds, cmd)
	case meetingModeNotes:
		var cmd tea.Cmd
		t.notesArea, cmd = t.notesArea.Update(msg)
		cmds = append(cmds, cmd)
	case meetingModeDecisions:
		var cmd tea.Cmd
		t.decisionsArea, cmd = t.decisionsArea.Update(msg)
		cmds = append(cmds, cmd)
	case meetingModeView:
		var cmd tea.Cmd
		t.vp, cmd = t.vp.Update(msg)
		cmds = append(cmds, cmd)
	}

	return t, tea.Batch(cmds...)
}

func (t *meetingsTab) handleListKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "j", "down":
		if t.cursor < len(t.meetings)-1 {
			t.cursor++
		}
	case "k", "up":
		if t.cursor > 0 {
			t.cursor--
		}
	case "enter":
		if len(t.meetings) > 0 {
			t.mode = meetingModeView
			t.vp.SetContent(t.renderMeetingDetail(t.meetings[t.cursor]))
			t.vp.GotoTop()
		}
	case t.cfg.Hotkeys.NewItem:
		t.mode = meetingModeTitle
		t.formDate = time.Now().Format("2006-01-02")
		t.titleInput.SetValue("")
		t.titleInput.Focus()
		return textinput.Blink
	case t.cfg.Hotkeys.Delete, "d":
		if len(t.meetings) > 0 {
			id := t.meetings[t.cursor].ID
			return func() tea.Msg {
				_ = t.store.DeleteMeeting(id)
				meetings, err := t.store.TodaysMeetings()
				return meetingsLoadedMsg{meetings: meetings, err: err}
			}
		}
	case t.cfg.Hotkeys.Refresh:
		return t.loadMeetings()
	}
	return nil
}

func (t *meetingsTab) handleViewKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "q":
		t.mode = meetingModeList
	}
	return nil
}

func (t meetingsTab) handleTitleKey(msg tea.KeyMsg) (meetingsTab, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if strings.TrimSpace(t.titleInput.Value()) != "" {
			t.mode = meetingModeAttend
			t.titleInput.Blur()
			t.attendInput.SetValue("")
			t.attendInput.Focus()
			return t, textinput.Blink
		}
	case "esc":
		t.mode = meetingModeList
		t.titleInput.Blur()
		return t, nil
	}
	var cmd tea.Cmd
	t.titleInput, cmd = t.titleInput.Update(msg)
	return t, cmd
}

func (t meetingsTab) handleAttendKey(msg tea.KeyMsg) (meetingsTab, tea.Cmd) {
	switch msg.String() {
	case "enter":
		t.mode = meetingModeTags
		t.attendInput.Blur()
		t.tagsInput.SetValue("")
		t.tagSuggestIdx = 0
		t.tagsInput.Focus()
		return t, tea.Batch(textinput.Blink, t.loadAllTags())
	case "esc":
		t.mode = meetingModeList
		t.attendInput.Blur()
		return t, nil
	case "ctrl+b":
		t.mode = meetingModeTitle
		t.attendInput.Blur()
		t.titleInput.Focus()
		return t, textinput.Blink
	}
	var cmd tea.Cmd
	t.attendInput, cmd = t.attendInput.Update(msg)
	return t, cmd
}

func (t meetingsTab) handleTagsKey(msg tea.KeyMsg) (meetingsTab, tea.Cmd) {
	switch msg.String() {
	case "enter":
		t.mode = meetingModeNotes
		t.tagsInput.Blur()
		t.notesArea.SetValue("")
		t.notesArea.Focus()
		return t, textarea.Blink
	case "esc":
		t.mode = meetingModeList
		t.tagsInput.Blur()
		return t, nil
	case "ctrl+b":
		t.mode = meetingModeAttend
		t.tagsInput.Blur()
		t.attendInput.Focus()
		return t, textinput.Blink
	case "ctrl+t":
		t.mode = meetingModeTitle
		t.tagsInput.Blur()
		t.titleInput.Focus()
		return t, textinput.Blink
	case "tab":
		newVal := tagComplete(t.tagsInput.Value(), t.availableTags, t.tagSuggestIdx)
		t.tagsInput.SetValue(newVal)
		t.tagSuggestIdx++
		return t, textinput.Blink
	default:
		t.tagSuggestIdx = 0
	}
	var cmd tea.Cmd
	t.tagsInput, cmd = t.tagsInput.Update(msg)
	return t, cmd
}

func (t meetingsTab) handleNotesKey(msg tea.KeyMsg) (meetingsTab, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		return t, t.saveMeeting()
	case "ctrl+y":
		_ = clipboard.WriteAll(t.notesArea.Value())
		return t, func() tea.Msg { return statusMsg{text: "Notes copied to clipboard"} }
	case "ctrl+d":
		t.mode = meetingModeDecisions
		t.notesArea.Blur()
		t.decisionsArea.SetValue("")
		t.decisionsArea.Focus()
		return t, textarea.Blink
	case "esc":
		t.mode = meetingModeList
		t.notesArea.Blur()
		return t, nil
	case "ctrl+b":
		t.mode = meetingModeTags
		t.notesArea.Blur()
		t.tagsInput.Focus()
		return t, textinput.Blink
	case "ctrl+w":
		t.mode = meetingModeAttend
		t.notesArea.Blur()
		t.attendInput.Focus()
		return t, textinput.Blink
	case "ctrl+t":
		t.mode = meetingModeTitle
		t.notesArea.Blur()
		t.titleInput.Focus()
		return t, textinput.Blink
	}
	var cmd tea.Cmd
	t.notesArea, cmd = t.notesArea.Update(msg)
	return t, cmd
}

func (t meetingsTab) handleDecisionsKey(msg tea.KeyMsg) (meetingsTab, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		return t, t.saveMeeting()
	case "ctrl+y":
		_ = clipboard.WriteAll(t.decisionsArea.Value())
		return t, func() tea.Msg { return statusMsg{text: "Decisions copied to clipboard"} }
	case "esc":
		t.mode = meetingModeList
		t.decisionsArea.Blur()
		return t, nil
	case "ctrl+b":
		t.mode = meetingModeNotes
		t.decisionsArea.Blur()
		t.notesArea.Focus()
		return t, textarea.Blink
	}
	var cmd tea.Cmd
	t.decisionsArea, cmd = t.decisionsArea.Update(msg)
	return t, cmd
}

// saveMeeting saves the meeting and auto-creates todos from any action items found in the notes.
func (t meetingsTab) saveMeeting() tea.Cmd {
	title := strings.TrimSpace(t.titleInput.Value())
	raw := strings.TrimSpace(t.notesArea.Value())
	attendees := parseAttendees(t.attendInput.Value())
	tags := parseTags(t.tagsInput.Value())
	decisions := parseDecisions(t.decisionsArea.Value())
	summary := summarizeMeeting(raw)
	actions := extractActions(raw)
	meetingID := fmt.Sprintf("%d", time.Now().UnixNano())

	m := storage.Meeting{
		ID:        meetingID,
		Date:      t.formDate,
		Title:     title,
		Attendees: attendees,
		RawNotes:  raw,
		Summary:   summary,
		Tags:      tags,
		Decisions: decisions,
	}

	return func() tea.Msg {
		err := t.store.SaveMeeting(m)
		if err != nil {
			return meetingSavedMsg{err: err}
		}
		if len(actions) > 0 {
			all, _ := t.store.LoadTodos()
			today := time.Now().Format("2006-01-02")
			base := time.Now().UnixNano()
			for i, action := range actions {
				all = append(all, storage.TodoItem{
					ID:                 fmt.Sprintf("%d", base+int64(i+1)),
					Text:               action,
					Date:               today,
					SourceMeetingID:    meetingID,
					SourceMeetingTitle: title,
				})
			}
			_ = t.store.SaveTodos(all)
		}
		return meetingSavedMsg{err: nil}
	}
}

func (t meetingsTab) loadMeetings() tea.Cmd {
	return func() tea.Msg {
		meetings, err := t.store.TodaysMeetings()
		return meetingsLoadedMsg{meetings: meetings, err: err}
	}
}

func (t meetingsTab) loadAllTags() tea.Cmd {
	return func() tea.Msg {
		tags, err := t.store.AllTags()
		return allTagsLoadedMsg{tags: tags, err: err}
	}
}

func (t meetingsTab) View() string {
	var b strings.Builder

	switch t.mode {
	case meetingModeList:
		b.WriteString(t.renderList())

	case meetingModeView:
		b.WriteString("\n  " + titleStyle.Render("Meeting Detail") + "\n\n")
		b.WriteString(t.vp.View() + "\n")
		b.WriteString(buildHints([]string{"Esc"}, []string{"back"}))

	case meetingModeTitle:
		b.WriteString("\n  " + t.renderStepBreadcrumb(meetingModeTitle) + "\n\n")
		b.WriteString(inputLabelStyle.Render("  Title") + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.titleInput.View()) + "\n\n")
		b.WriteString(buildHints([]string{"Enter", "Esc"}, []string{"next", "cancel"}))

	case meetingModeAttend:
		b.WriteString("\n  " + t.renderStepBreadcrumb(meetingModeAttend) + "\n\n")
		b.WriteString(inputLabelStyle.Render("  Attendees") + "  " + mutedStyle.Render(t.titleInput.Value()) + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.attendInput.View()) + "\n\n")
		b.WriteString(buildHints([]string{"Enter", "ctrl+b", "Esc"}, []string{"next", "back", "cancel"}))

	case meetingModeTags:
		b.WriteString("\n  " + t.renderStepBreadcrumb(meetingModeTags) + "\n\n")
		b.WriteString(inputLabelStyle.Render("  Tags") + "  " + mutedStyle.Render(t.titleInput.Value()) + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.tagsInput.View()) + "\n")
		if hint := tagSuggestionLine(t.tagsInput.Value(), t.availableTags); hint != "" {
			b.WriteString("  " + subtleStyle.Render(hint) + "\n")
		}
		b.WriteString("\n")
		b.WriteString(buildHints([]string{"Enter", "Tab", "ctrl+b", "ctrl+t", "Esc"}, []string{"next", "complete", "back", "title", "cancel"}))

	case meetingModeNotes:
		b.WriteString("\n  " + t.renderStepBreadcrumb(meetingModeNotes) + "\n\n")
		b.WriteString(inputLabelStyle.Render("  Notes") + "  " + mutedStyle.Render(t.titleInput.Value()))
		if t.attendInput.Value() != "" {
			b.WriteString(subtleStyle.Render("  ·  " + t.attendInput.Value()))
		}
		b.WriteString("\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.notesArea.View()) + "\n\n")
		b.WriteString(buildHints(
			[]string{"Ctrl+S", "Ctrl+D", "Ctrl+Y", "ctrl+b", "Esc"},
			[]string{"save", "decisions", "copy", "tags", "cancel"},
		))
		b.WriteString(hintStyle.Render("  Tip: prefix lines with Action: or - [ ] to auto-create todos") + "\n")

	case meetingModeDecisions:
		b.WriteString("\n  " + t.renderStepBreadcrumb(meetingModeDecisions) + "\n\n")
		b.WriteString(inputLabelStyle.Render("  Decisions") + "  " + mutedStyle.Render(t.titleInput.Value()))
		b.WriteString("\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.decisionsArea.View()) + "\n\n")
		b.WriteString(buildHints(
			[]string{"Ctrl+S", "Ctrl+Y", "ctrl+b", "Esc"},
			[]string{"save", "copy", "back to notes", "cancel"},
		))
		b.WriteString(hintStyle.Render("  One decision per line — these are saved separately from your notes") + "\n")
	}

	return b.String()
}

func (t meetingsTab) renderList() string {
	var b strings.Builder

	dateLabel := mutedStyle.Render(time.Now().Format("Monday, 02 Jan"))
	b.WriteString("\n  " + titleStyle.Render("Meetings") + "  " + dateLabel + "\n\n")

	if len(t.meetings) == 0 {
		b.WriteString("  " + mutedStyle.Render("No meetings today.") + "\n")
	} else {
		for i, m := range t.meetings {
			cursor := "  "
			if i == t.cursor {
				cursor = "▸ "
			}
			attn := ""
			if len(m.Attendees) > 0 {
				attn = mutedStyle.Render(fmt.Sprintf("  %d people", len(m.Attendees)))
			}
			tagStr := ""
			if len(m.Tags) > 0 {
				tagStr = "  " + tagStyle.Render(strings.Join(m.Tags, " "))
			}
			line := cursor + truncate(m.Title, 48) + attn + tagStr
			if i == t.cursor {
				b.WriteString(selectedStyle.Width(t.width).Render(cursor+truncate(m.Title, 48)+fmt.Sprintf("  %d people", len(m.Attendees))+tagStr) + "\n")
			} else {
				b.WriteString(line + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(buildHints(
		[]string{"j/k", "Enter", t.cfg.Hotkeys.NewItem, t.cfg.Hotkeys.Delete},
		[]string{"navigate", "view", "new", "delete"},
	))
	return b.String()
}

// renderStepBreadcrumb renders "● Title ─ ○ Attendees ─ ○ Tags ─ ○ Notes" with the current step highlighted.
func (t meetingsTab) renderStepBreadcrumb(current meetingMode) string {
	type step struct {
		mode  meetingMode
		label string
	}
	steps := []step{
		{meetingModeTitle, "Title"},
		{meetingModeAttend, "Attendees"},
		{meetingModeTags, "Tags"},
		{meetingModeNotes, "Notes"},
		{meetingModeDecisions, "Decisions"},
	}
	sep := subtleStyle.Render(" ─ ")
	var parts []string
	for _, s := range steps {
		if s.mode == current {
			parts = append(parts, lipgloss.NewStyle().Bold(true).Foreground(colorIris).Render("● "+s.label))
		} else {
			parts = append(parts, mutedStyle.Render("○ "+s.label))
		}
	}
	return strings.Join(parts, sep)
}

func (t meetingsTab) renderMeetingDetail(m storage.Meeting) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", m.Title)
	fmt.Fprintf(&b, "**Date:** %s\n", m.Date)
	if len(m.Attendees) > 0 {
		fmt.Fprintf(&b, "**Attendees:** %s\n", strings.Join(m.Attendees, ", "))
	}
	if len(m.Tags) > 0 {
		fmt.Fprintf(&b, "**Tags:** %s\n", strings.Join(m.Tags, " "))
	}
	b.WriteString("\n")

	if len(m.Decisions) > 0 {
		b.WriteString("## Decisions\n\n")
		for _, d := range m.Decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
		b.WriteString("\n")
	}

	if m.Summary != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(m.Summary)
		b.WriteString("\n")
	}

	if m.RawNotes != "" {
		b.WriteString("## Raw Notes\n\n")
		b.WriteString(m.RawNotes)
	}

	return b.String()
}

// summarizeMeeting applies rule-based extraction to produce a structured summary.
func summarizeMeeting(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	var actions, decisions, keyPoints []string

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "action:"),
			strings.HasPrefix(lower, "todo:"),
			strings.HasPrefix(lower, "ap:"),
			strings.HasPrefix(lower, "follow up:"),
			strings.HasPrefix(lower, "action item:"),
			strings.HasPrefix(lower, "- [ ]"):
			actions = append(actions, cleanAction(line))
		case strings.HasPrefix(lower, "decision:"),
			strings.HasPrefix(lower, "decided:"),
			strings.HasPrefix(lower, "agreed:"),
			strings.HasPrefix(lower, "resolved:"),
			strings.HasPrefix(lower, "conclusion:"):
			decisions = append(decisions, cleanBullet(line))
		default:
			if len(line) > 15 {
				keyPoints = append(keyPoints, cleanBullet(line))
			}
		}
	}

	if len(keyPoints) > 7 {
		keyPoints = keyPoints[:7]
	}

	var b strings.Builder

	if len(keyPoints) > 0 {
		b.WriteString("**Key Points:**\n")
		for _, p := range keyPoints {
			fmt.Fprintf(&b, "- %s\n", p)
		}
		b.WriteString("\n")
	}
	if len(decisions) > 0 {
		b.WriteString("**Decisions:**\n")
		for _, d := range decisions {
			fmt.Fprintf(&b, "- %s\n", d)
		}
		b.WriteString("\n")
	}
	if len(actions) > 0 {
		b.WriteString("**Action Items:**\n")
		for _, a := range actions {
			fmt.Fprintf(&b, "- [ ] %s\n", a)
		}
	}

	return b.String()
}

// extractActions returns the cleaned action item texts from raw meeting notes.
// These are used to auto-create todos when a meeting is saved.
func extractActions(raw string) []string {
	var actions []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "action:") ||
			strings.HasPrefix(lower, "todo:") ||
			strings.HasPrefix(lower, "ap:") ||
			strings.HasPrefix(lower, "follow up:") ||
			strings.HasPrefix(lower, "action item:") ||
			strings.HasPrefix(lower, "- [ ]") {
			cleaned := cleanAction(line)
			if cleaned != "" {
				actions = append(actions, cleaned)
			}
		}
	}
	return actions
}

func cleanBullet(line string) string {
	for _, pfx := range []string{"- [ ] ", "- [x] ", "- ", "* ", "• "} {
		if strings.HasPrefix(line, pfx) {
			return strings.TrimSpace(line[len(pfx):])
		}
	}
	return strings.TrimSpace(line)
}

func cleanAction(line string) string {
	for _, pfx := range []string{"action item:", "action:", "todo:", "ap:", "follow up:", "- [ ] "} {
		if strings.HasPrefix(strings.ToLower(line), pfx) {
			return strings.TrimSpace(line[len(pfx):])
		}
	}
	return cleanBullet(line)
}

func parseDecisions(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func parseAttendees(s string) []string {
	var result []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func parseTags(s string) []string {
	var result []string
	for _, p := range strings.Fields(s) {
		if p != "" {
			if !strings.HasPrefix(p, "#") {
				p = "#" + p
			}
			result = append(result, p)
		}
	}
	return result
}
