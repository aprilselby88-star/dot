package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/jira"
)

// ---- messages ----

type jiraCreatedMsg struct {
	result jira.Result
	err    error
}

// ---- mode ----

type jiraMode int

const (
	jiraModeHome     jiraMode = iota // idle: instructions / last result summary
	jiraModeParent                   // entering parentIssueId
	jiraModeSummary                  // entering summary
	jiraModeDesc                     // entering description
	jiraModeAC                       // entering acceptance criteria
	jiraModeCreating                 // acli create in flight
	jiraModeResult                   // showing the created ticket link or the error
)

// ---- model ----

type jiraTab struct {
	cfg    *config.Config
	client *jira.Client // nil when Jira isn't configured / acli missing
	width  int
	height int

	mode jiraMode

	parentInput  textinput.Model
	summaryInput textinput.Model
	descArea     textarea.Model
	acArea       textarea.Model

	vp       viewport.Model
	result   *jira.Result
	lastErr  string
	showJSON bool
}

func newJiraTab(cfg *config.Config, client *jira.Client) jiraTab {
	pi := textinput.New()
	pi.Placeholder = "Parent work item ID (e.g. PROJ-123) — optional"
	pi.CharLimit = 60

	si := textinput.New()
	si.Placeholder = "Summary / title"
	si.CharLimit = 240

	da := textarea.New()
	da.Placeholder = "Description…"
	da.ShowLineNumbers = false
	da.CharLimit = 0
	da.MaxHeight = 0

	aa := textarea.New()
	aa.Placeholder = "Acceptance criteria…\n\nOne criterion per line."
	aa.ShowLineNumbers = false
	aa.CharLimit = 0
	aa.MaxHeight = 0

	return jiraTab{
		cfg:          cfg,
		client:       client,
		mode:         jiraModeHome,
		parentInput:  pi,
		summaryInput: si,
		descArea:     da,
		acArea:       aa,
		vp:           viewport.New(80, 20),
	}
}

func (t jiraTab) Editing() bool {
	switch t.mode {
	case jiraModeParent, jiraModeSummary, jiraModeDesc, jiraModeAC:
		return true
	}
	return false
}

func (t jiraTab) Init() tea.Cmd { return nil }

func (t jiraTab) UpdateSize(w, h int) (jiraTab, tea.Cmd) {
	t.width, t.height = w, h
	areaH := h - 7
	if areaH < 3 {
		areaH = 3
	}
	t.descArea.SetWidth(w - 4)
	t.descArea.SetHeight(areaH)
	t.acArea.SetWidth(w - 4)
	t.acArea.SetHeight(areaH)
	t.vp.Width = w
	t.vp.Height = h - 4
	return t, nil
}

func (t jiraTab) Update(msg tea.Msg) (jiraTab, tea.Cmd) {
	switch msg := msg.(type) {

	case jiraCreatedMsg:
		t.result = &msg.result
		if msg.err != nil {
			t.lastErr = msg.err.Error()
		} else {
			t.lastErr = ""
		}
		t.showJSON = false
		t.mode = jiraModeResult
		return t, nil

	case tea.KeyMsg:
		switch t.mode {
		case jiraModeHome:
			return t.handleHomeKey(msg)
		case jiraModeParent:
			return t.handleParentKey(msg)
		case jiraModeSummary:
			return t.handleSummaryKey(msg)
		case jiraModeDesc:
			return t.handleDescKey(msg)
		case jiraModeAC:
			return t.handleACKey(msg)
		case jiraModeResult:
			return t.handleResultKey(msg)
		case jiraModeCreating:
			return t, nil // ignore input while a create is in flight
		}
	}

	// Forward non-key messages to the focused component.
	var cmd tea.Cmd
	switch t.mode {
	case jiraModeParent:
		t.parentInput, cmd = t.parentInput.Update(msg)
	case jiraModeSummary:
		t.summaryInput, cmd = t.summaryInput.Update(msg)
	case jiraModeDesc:
		t.descArea, cmd = t.descArea.Update(msg)
	case jiraModeAC:
		t.acArea, cmd = t.acArea.Update(msg)
	case jiraModeResult:
		t.vp, cmd = t.vp.Update(msg)
	}
	return t, cmd
}

// ---- key handlers ----

func (t jiraTab) handleHomeKey(msg tea.KeyMsg) (jiraTab, tea.Cmd) {
	switch msg.String() {
	case t.cfg.Hotkeys.NewItem, "enter":
		if t.client == nil {
			return t, func() tea.Msg {
				return statusMsg{text: "Jira not configured — install acli and set jira.project_key in config", isError: true}
			}
		}
		return t.startForm(), textinput.Blink
	}
	return t, nil
}

// startForm resets the wizard to a fresh ticket beginning at the parent step.
func (t jiraTab) startForm() jiraTab {
	t.parentInput.SetValue(t.cfg.Jira.DefaultParent)
	t.summaryInput.SetValue("")
	t.descArea.SetValue("")
	t.acArea.SetValue("")
	t.parentInput.CursorEnd()
	t.mode = jiraModeParent
	t.parentInput.Focus()
	t.summaryInput.Blur()
	t.descArea.Blur()
	t.acArea.Blur()
	return t
}

func (t jiraTab) handleParentKey(msg tea.KeyMsg) (jiraTab, tea.Cmd) {
	switch msg.String() {
	case "enter", "ctrl+n":
		t.mode = jiraModeSummary
		t.parentInput.Blur()
		t.summaryInput.Focus()
		return t, textinput.Blink
	case "esc":
		return t.cancelForm(), nil
	}
	var cmd tea.Cmd
	t.parentInput, cmd = t.parentInput.Update(msg)
	return t, cmd
}

func (t jiraTab) handleSummaryKey(msg tea.KeyMsg) (jiraTab, tea.Cmd) {
	switch msg.String() {
	case "enter", "ctrl+n":
		if strings.TrimSpace(t.summaryInput.Value()) == "" {
			return t, func() tea.Msg { return statusMsg{text: "Summary is required", isError: true} }
		}
		t.mode = jiraModeDesc
		t.summaryInput.Blur()
		t.descArea.Focus()
		return t, textarea.Blink
	case "ctrl+b":
		t.mode = jiraModeParent
		t.summaryInput.Blur()
		t.parentInput.Focus()
		return t, textinput.Blink
	case "esc":
		return t.cancelForm(), nil
	}
	var cmd tea.Cmd
	t.summaryInput, cmd = t.summaryInput.Update(msg)
	return t, cmd
}

func (t jiraTab) handleDescKey(msg tea.KeyMsg) (jiraTab, tea.Cmd) {
	switch msg.String() {
	case "ctrl+n":
		t.mode = jiraModeAC
		t.descArea.Blur()
		t.acArea.Focus()
		return t, textarea.Blink
	case "ctrl+b":
		t.mode = jiraModeSummary
		t.descArea.Blur()
		t.summaryInput.Focus()
		return t, textinput.Blink
	case "esc":
		return t.cancelForm(), nil
	}
	var cmd tea.Cmd
	t.descArea, cmd = t.descArea.Update(msg)
	return t, cmd
}

func (t jiraTab) handleACKey(msg tea.KeyMsg) (jiraTab, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		if strings.TrimSpace(t.summaryInput.Value()) == "" {
			return t, func() tea.Msg { return statusMsg{text: "Summary is required", isError: true} }
		}
		req := jira.CreateRequest{
			ParentID:           strings.TrimSpace(t.parentInput.Value()),
			Summary:            strings.TrimSpace(t.summaryInput.Value()),
			Description:        t.descArea.Value(),
			AcceptanceCriteria: t.acArea.Value(),
		}
		t.mode = jiraModeCreating
		t.acArea.Blur()
		return t, t.createTicket(req)
	case "ctrl+b":
		t.mode = jiraModeDesc
		t.acArea.Blur()
		t.descArea.Focus()
		return t, textarea.Blink
	case "esc":
		return t.cancelForm(), nil
	}
	var cmd tea.Cmd
	t.acArea, cmd = t.acArea.Update(msg)
	return t, cmd
}

func (t jiraTab) handleResultKey(msg tea.KeyMsg) (jiraTab, tea.Cmd) {
	switch msg.String() {
	case t.cfg.Hotkeys.NewItem:
		return t.startForm(), textinput.Blink
	case t.cfg.Hotkeys.OpenBrowser, "enter":
		if t.result != nil && t.result.URL != "" {
			openURL(t.result.URL)
		}
	case "J":
		t.showJSON = !t.showJSON
		if t.showJSON && t.result != nil {
			t.vp.SetContent(string(t.result.RequestJSON))
			t.vp.GotoTop()
		}
	case "esc":
		t.mode = jiraModeHome
		t.showJSON = false
		return t, nil
	}
	if t.showJSON {
		var cmd tea.Cmd
		t.vp, cmd = t.vp.Update(msg)
		return t, cmd
	}
	return t, nil
}

func (t jiraTab) cancelForm() jiraTab {
	t.mode = jiraModeHome
	t.parentInput.Blur()
	t.summaryInput.Blur()
	t.descArea.Blur()
	t.acArea.Blur()
	return t
}

// ---- commands ----

func (t jiraTab) createTicket(req jira.CreateRequest) tea.Cmd {
	client := t.client
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		res, err := client.Create(ctx, req)
		return jiraCreatedMsg{result: res, err: err}
	}
}

// ---- view ----

func (t jiraTab) View() string {
	var b strings.Builder

	switch t.mode {
	case jiraModeHome:
		b.WriteString(t.renderHome())

	case jiraModeParent:
		b.WriteString("\n  " + t.renderBreadcrumb(jiraModeParent) + "\n\n")
		b.WriteString(inputLabelStyle.Render("  Parent work item ID") + "  " + mutedStyle.Render("optional") + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.parentInput.View()) + "\n\n")
		b.WriteString(buildHints([]string{"Enter", "Esc"}, []string{"next", "cancel"}))

	case jiraModeSummary:
		b.WriteString("\n  " + t.renderBreadcrumb(jiraModeSummary) + "\n\n")
		b.WriteString(inputLabelStyle.Render("  Summary") + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.summaryInput.View()) + "\n\n")
		b.WriteString(buildHints([]string{"Enter", "ctrl+b", "Esc"}, []string{"next", "back", "cancel"}))

	case jiraModeDesc:
		b.WriteString("  " + t.renderBreadcrumb(jiraModeDesc) + "\n")
		b.WriteString(inputLabelStyle.Render("  Description") + "  " + mutedStyle.Render(t.summaryInput.Value()) + "\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.descArea.View()) + "\n")
		b.WriteString(buildHints([]string{"Ctrl+N", "ctrl+b", "Esc"}, []string{"next", "back", "cancel"}))

	case jiraModeAC:
		b.WriteString("  " + t.renderBreadcrumb(jiraModeAC) + "\n")
		b.WriteString(inputLabelStyle.Render("  Acceptance Criteria") + "  " + mutedStyle.Render(t.summaryInput.Value()) + "\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.acArea.View()) + "\n")
		b.WriteString(buildHints([]string{"Ctrl+S", "ctrl+b", "Esc"}, []string{"create", "back", "cancel"}))

	case jiraModeCreating:
		b.WriteString("\n  " + titleStyle.Render("Jira") + "\n\n")
		b.WriteString("  " + mutedStyle.Render("Creating ticket…") + "\n")

	case jiraModeResult:
		b.WriteString(t.renderResult())
	}

	return b.String()
}

func (t jiraTab) renderHome() string {
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render("Jira") + "\n\n")

	if t.client == nil {
		b.WriteString("  " + mutedStyle.Render("Jira not configured.") + "\n")
		b.WriteString("  " + hintStyle.Render("Install acli and set jira.project_key (and site/email) in ~/.config/dot/config.yaml") + "\n")
		return b.String()
	}

	b.WriteString("  " + mutedStyle.Render("Create a Jira ticket in "+t.cfg.Jira.ProjectKey) + "\n\n")
	b.WriteString("  " + hintStyle.Render(keyStyle.Render(t.cfg.Hotkeys.NewItem)+" new ticket") + "\n")
	return b.String()
}

func (t jiraTab) renderResult() string {
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render("Jira") + "\n\n")

	if t.lastErr != "" {
		b.WriteString("  " + errorStyle.Render("✗  Create failed") + "\n")
		b.WriteString("  " + mutedStyle.Render(t.lastErr) + "\n\n")
		if t.result != nil && len(t.result.RawOutput) > 0 {
			b.WriteString(subtleStyle.Render("  acli output:") + "\n")
			b.WriteString(indentBlock(strings.TrimSpace(string(t.result.RawOutput))) + "\n")
		}
	} else if t.result != nil {
		b.WriteString("  " + successStyle.Render("✓  Created "+t.result.Key) + "\n")
		b.WriteString("  " + keyStyle.Render(t.result.URL) + "\n")
	}

	if t.showJSON {
		b.WriteString("\n" + subtleStyle.Render("  Request JSON (for manual retry via acli --from-json):") + "\n")
		b.WriteString(t.vp.View() + "\n")
	}

	b.WriteString("\n")
	b.WriteString(buildHints(
		[]string{t.cfg.Hotkeys.OpenBrowser, "J", t.cfg.Hotkeys.NewItem, "Esc"},
		[]string{"open in browser", "toggle JSON", "new", "home"},
	))
	return b.String()
}

// renderBreadcrumb shows the four wizard steps with the current one highlighted.
func (t jiraTab) renderBreadcrumb(current jiraMode) string {
	steps := []struct {
		mode  jiraMode
		label string
	}{
		{jiraModeParent, "Parent"},
		{jiraModeSummary, "Summary"},
		{jiraModeDesc, "Description"},
		{jiraModeAC, "Acceptance"},
	}
	var parts []string
	for _, s := range steps {
		if s.mode == current {
			parts = append(parts, inputLabelStyle.Render("● "+s.label))
		} else {
			parts = append(parts, mutedStyle.Render("○ "+s.label))
		}
	}
	return strings.Join(parts, subtleStyle.Render(" ─ "))
}

// indentBlock indents each line by two spaces for muted display.
func indentBlock(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  " + mutedStyle.Render(l)
	}
	return strings.Join(lines, "\n")
}
