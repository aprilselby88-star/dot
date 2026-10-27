package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/obsidian"
	"github.com/aprilselby/dot/storage"
)

// ---- messages ----

type ooPeopleLoadedMsg struct {
	people []storage.Person
	latest map[string]storage.OneOnOne // newest session per person ID
	err    error
}

type ooPersonLoadedMsg struct {
	personID string
	sessions []storage.OneOnOne
	mentions []storage.Mention
	err      error
}

type ooPersonSavedMsg struct {
	person storage.Person
	err    error
}

type ooDeletedMsg struct {
	text string
	err  error
}

// ooSavedMsg is sent after a session is saved. App also uses it to resync the
// daily note and reload todos, since saving can create action-item todos.
type ooSavedMsg struct {
	err      error
	autosave bool
	path     string // person's Obsidian file
}

type ooAutosaveTickMsg struct{}

const ooAutosaveInterval = 2 * time.Minute

func ooAutosaveTick() tea.Cmd {
	return tea.Tick(ooAutosaveInterval, func(time.Time) tea.Msg { return ooAutosaveTickMsg{} })
}

// mentionWindowDays is how far back to look for @mentions when there's no previous 1:1.
const mentionWindowDays = 30

// maxContextLines caps the "last time" / mentions block above the editor.
const maxContextLines = 8

// ---- mode ----

type ooMode int

const (
	ooModePeople ooMode = iota // list of people
	ooModeName                 // new/edit person: entering name
	ooModeKind                 // new/edit person: choosing kind
	ooModePerson               // one person: mentions + session history
	ooModeEdit                 // filling in a session
)

const notesSectionTitle = "Notes"

// ---- model ----

type oneOnOnesTab struct {
	cfg    *config.Config
	store  *storage.Store
	obs    *obsidian.Writer
	width  int
	height int

	mode ooMode

	// people list
	people []storage.Person
	latest map[string]storage.OneOnOne
	cursor int

	// person form
	nameInput    textinput.Model
	kindIdx      int
	editPersonID string // "" when adding a new person

	// person view
	person      storage.Person
	sessions    []storage.OneOnOne // newest first
	mentions    []storage.Mention
	sessCursor  int
	personReady bool // sessions for person have loaded

	// session editor
	area         textarea.Model
	session      storage.OneOnOne
	prev         *storage.OneOnOne // previous session, for "last time" context
	sections     []config.OneOnOneSection
	bodies       []string // one per section, plus a final entry for live notes
	secIdx       int
	showMentions bool
	dirty        bool
	saving       bool

	pendingDelete string // ID armed by the first delete press; the second press confirms
}

func newOneOnOnesTab(cfg *config.Config, store *storage.Store, obs *obsidian.Writer) oneOnOnesTab {
	ni := textinput.New()
	ni.Placeholder = "Name, e.g. Omar Haddad"
	ni.CharLimit = 80

	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = 0

	return oneOnOnesTab{
		cfg:       cfg,
		store:     store,
		obs:       obs,
		mode:      ooModePeople,
		nameInput: ni,
		area:      ta,
	}
}

func (t oneOnOnesTab) Editing() bool {
	return t.mode == ooModeName || t.mode == ooModeKind || t.mode == ooModeEdit
}

func (t oneOnOnesTab) Init() tea.Cmd {
	return tea.Batch(t.loadPeople(), ooAutosaveTick())
}

func (t oneOnOnesTab) UpdateSize(w, h int) (oneOnOnesTab, tea.Cmd) {
	t.width, t.height = w, h
	t.nameInput.Width = w - 8
	t.layoutEditor()
	return t, nil
}

// layoutEditor sizes the textarea to fill what's left after the header and context block.
func (t *oneOnOnesTab) layoutEditor() {
	ctx := len(t.contextLines())
	if ctx > 0 {
		ctx += 2 // label + spacer
	}
	// header, progress line, label, spacer, border (2), hints = 7
	h := t.height - 7 - ctx
	if h < 3 {
		h = 3
	}
	t.area.SetWidth(t.width - 6) // box is width-4 including 1-col padding each side
	t.area.SetHeight(h)
}

func (t oneOnOnesTab) Update(msg tea.Msg) (oneOnOnesTab, tea.Cmd) {
	switch msg := msg.(type) {
	case ooPeopleLoadedMsg:
		if msg.err != nil {
			return t, errStatus(msg.err)
		}
		t.people, t.latest = msg.people, msg.latest
		if t.cursor >= len(t.people) {
			t.cursor = max(0, len(t.people)-1)
		}
		return t, nil

	case ooPersonLoadedMsg:
		if msg.err != nil {
			return t, errStatus(msg.err)
		}
		if msg.personID != t.person.ID {
			return t, nil
		}
		t.sessions, t.mentions, t.personReady = msg.sessions, msg.mentions, true
		if t.sessCursor >= len(t.sessions) {
			t.sessCursor = max(0, len(t.sessions)-1)
		}
		return t, nil

	case ooPersonSavedMsg:
		if msg.err != nil {
			return t, errStatus(msg.err)
		}
		t.nameInput.Blur()
		t.openPerson(msg.person)
		return t, tea.Batch(t.loadPeople(), t.loadPerson())

	case ooDeletedMsg:
		if msg.err != nil {
			return t, errStatus(msg.err)
		}
		text := msg.text
		return t, tea.Batch(t.loadPeople(), t.loadPerson(), func() tea.Msg { return statusMsg{text: text} })

	case ooSavedMsg:
		t.saving = false
		if msg.err != nil {
			return t, errStatus(msg.err)
		}
		t.dirty = false
		text := "1:1 saved → " + msg.path
		if msg.autosave {
			text = "1:1 autosaved"
		} else if t.mode != ooModeEdit {
			// Esc already left the editor; refresh what the person view shows.
			return t, tea.Batch(t.loadPeople(), t.loadPerson(), func() tea.Msg { return statusMsg{text: text} })
		}
		return t, func() tea.Msg { return statusMsg{text: text} }

	case ooAutosaveTickMsg:
		if t.mode == ooModeEdit && t.dirty && !t.saving {
			t.saving = true
			return t, tea.Batch(t.saveSession(true), ooAutosaveTick())
		}
		return t, ooAutosaveTick()

	case tea.KeyMsg:
		armed := t.pendingDelete
		t.pendingDelete = ""
		switch t.mode {
		case ooModePeople:
			return t.handlePeopleKey(msg, armed)
		case ooModeName:
			return t.handleNameKey(msg)
		case ooModeKind:
			return t.handleKindKey(msg)
		case ooModePerson:
			return t.handlePersonKey(msg, armed)
		case ooModeEdit:
			return t.handleEditKey(msg)
		}
	}

	// Forward non-key messages (cursor blink etc.) to the focused component.
	var cmd tea.Cmd
	switch t.mode {
	case ooModeName:
		t.nameInput, cmd = t.nameInput.Update(msg)
	case ooModeEdit:
		t.area, cmd = t.area.Update(msg)
	}
	return t, cmd
}

// ---- people list ----

func (t oneOnOnesTab) handlePeopleKey(msg tea.KeyMsg, armed string) (oneOnOnesTab, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if t.cursor < len(t.people)-1 {
			t.cursor++
		}
	case "k", "up":
		if t.cursor > 0 {
			t.cursor--
		}
	case "enter":
		if len(t.people) > 0 {
			t.openPerson(t.people[t.cursor])
			return t, t.loadPerson()
		}
	case t.cfg.Hotkeys.NewItem:
		return t.startPersonForm(storage.Person{Kind: config.KindManager})
	case t.cfg.Hotkeys.Delete:
		if len(t.people) == 0 {
			return t, nil
		}
		p := t.people[t.cursor]
		if armed != p.ID {
			t.pendingDelete = p.ID
			return t, func() tea.Msg {
				return statusMsg{text: fmt.Sprintf("Press %s again to delete %s and all their 1:1s", t.cfg.Hotkeys.Delete, p.Name), isError: true}
			}
		}
		return t, func() tea.Msg {
			return ooDeletedMsg{text: "Deleted " + p.Name, err: t.store.DeletePerson(p.ID)}
		}
	case t.cfg.Hotkeys.Refresh:
		return t, t.loadPeople()
	}
	return t, nil
}

// ---- person form ----

func (t oneOnOnesTab) startPersonForm(p storage.Person) (oneOnOnesTab, tea.Cmd) {
	t.mode = ooModeName
	t.editPersonID = p.ID
	t.nameInput.SetValue(p.Name)
	t.nameInput.CursorEnd()
	t.kindIdx = 0
	for i, k := range config.OneOnOneKinds {
		if k == p.Kind {
			t.kindIdx = i
		}
	}
	t.nameInput.Focus()
	return t, textinput.Blink
}

func (t oneOnOnesTab) cancelPersonForm() oneOnOnesTab {
	t.nameInput.Blur()
	if t.editPersonID != "" {
		t.mode = ooModePerson
	} else {
		t.mode = ooModePeople
	}
	return t
}

func (t oneOnOnesTab) handleNameKey(msg tea.KeyMsg) (oneOnOnesTab, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if strings.TrimSpace(t.nameInput.Value()) != "" {
			t.mode = ooModeKind
			t.nameInput.Blur()
		}
		return t, nil
	case "esc":
		return t.cancelPersonForm(), nil
	}
	var cmd tea.Cmd
	t.nameInput, cmd = t.nameInput.Update(msg)
	return t, cmd
}

func (t oneOnOnesTab) handleKindKey(msg tea.KeyMsg) (oneOnOnesTab, tea.Cmd) {
	switch key := msg.String(); key {
	case "j", "down", "l", "right", "tab":
		t.kindIdx = (t.kindIdx + 1) % len(config.OneOnOneKinds)
	case "k", "up", "h", "left", "shift+tab":
		t.kindIdx = (t.kindIdx + len(config.OneOnOneKinds) - 1) % len(config.OneOnOneKinds)
	case "1", "2", "3", "4":
		t.kindIdx = int(key[0] - '1')
	case "ctrl+b":
		t.mode = ooModeName
		t.nameInput.Focus()
		return t, textinput.Blink
	case "esc":
		return t.cancelPersonForm(), nil
	case "enter":
		return t, t.savePerson()
	}
	return t, nil
}

func (t oneOnOnesTab) savePerson() tea.Cmd {
	name := strings.TrimSpace(t.nameInput.Value())
	p := storage.Person{
		ID:     t.editPersonID,
		Name:   name,
		Kind:   config.OneOnOneKinds[t.kindIdx],
		Handle: storage.DefaultHandle(name),
	}
	if p.ID == "" {
		p.ID = storage.NewID()
	} else if t.person.ID == p.ID && t.person.Handle != "" {
		p.Handle = t.person.Handle // keep a hand-edited handle across renames
	}
	return func() tea.Msg {
		return ooPersonSavedMsg{person: p, err: t.store.SavePerson(p)}
	}
}

// ---- person view ----

func (t *oneOnOnesTab) openPerson(p storage.Person) {
	if t.person.ID != p.ID {
		t.sessions, t.mentions, t.sessCursor, t.personReady = nil, nil, 0, false
	}
	t.person = p
	t.mode = ooModePerson
}

func (t oneOnOnesTab) handlePersonKey(msg tea.KeyMsg, armed string) (oneOnOnesTab, tea.Cmd) {
	switch msg.String() {
	case "esc":
		t.mode = ooModePeople
		return t, t.loadPeople()
	case "j", "down":
		if t.sessCursor < len(t.sessions)-1 {
			t.sessCursor++
		}
	case "k", "up":
		if t.sessCursor > 0 {
			t.sessCursor--
		}
	case "enter":
		if len(t.sessions) > 0 {
			return t.openSession(t.sessCursor)
		}
	case t.cfg.Hotkeys.NewItem:
		if !t.personReady {
			return t, nil
		}
		// Reuse a prep sheet that hasn't been held yet rather than starting another.
		if len(t.sessions) > 0 && !t.sessions[0].Held() {
			return t.openSession(0)
		}
		return t.newSession()
	case "e":
		return t.startPersonForm(t.person)
	case t.cfg.Hotkeys.Delete:
		if len(t.sessions) == 0 {
			return t, nil
		}
		o := t.sessions[t.sessCursor]
		if armed != o.ID {
			t.pendingDelete = o.ID
			return t, func() tea.Msg {
				return statusMsg{text: fmt.Sprintf("Press %s again to delete the %s 1:1", t.cfg.Hotkeys.Delete, o.Date), isError: true}
			}
		}
		p := t.person
		return t, func() tea.Msg {
			if err := t.store.DeleteOneOnOne(o.ID); err != nil {
				return ooDeletedMsg{err: err}
			}
			t.writePersonFile(p)
			return ooDeletedMsg{text: "Deleted the " + o.Date + " 1:1"}
		}
	case t.cfg.Hotkeys.Refresh:
		return t, t.loadPerson()
	}
	return t, nil
}

// ---- session editor ----

func (t oneOnOnesTab) newSession() (oneOnOnesTab, tea.Cmd) {
	o := storage.OneOnOne{
		ID:       storage.NewID(),
		PersonID: t.person.ID,
		Date:     time.Now().Format("2006-01-02"),
	}
	for _, s := range t.cfg.OneOnOne.Template(t.person.Kind) {
		o.Sections = append(o.Sections, storage.OneOnOneSection{Title: s.Title})
	}
	var prev *storage.OneOnOne
	if len(t.sessions) > 0 {
		prev = &t.sessions[0]
	}
	t.sessCursor = 0
	return t.startEditor(o, prev)
}

func (t oneOnOnesTab) openSession(idx int) (oneOnOnesTab, tea.Cmd) {
	var prev *storage.OneOnOne
	if idx+1 < len(t.sessions) {
		prev = &t.sessions[idx+1]
	}
	return t.startEditor(t.sessions[idx], prev)
}

// startEditor loads a session into the editor. Sections come from the saved
// session, followed by any the current template has added since.
func (t oneOnOnesTab) startEditor(o storage.OneOnOne, prev *storage.OneOnOne) (oneOnOnesTab, tea.Cmd) {
	tmpl := t.cfg.OneOnOne.Template(t.person.Kind)
	byTitle := map[string]config.OneOnOneSection{}
	for _, s := range tmpl {
		byTitle[s.Title] = s
	}

	t.sections, t.bodies = nil, nil
	seen := map[string]bool{}
	for _, s := range o.Sections {
		sec, ok := byTitle[s.Title]
		if !ok {
			sec = config.OneOnOneSection{Title: s.Title}
		}
		t.sections = append(t.sections, sec)
		t.bodies = append(t.bodies, s.Body)
		seen[s.Title] = true
	}
	for _, s := range tmpl {
		if !seen[s.Title] {
			t.sections = append(t.sections, s)
			t.bodies = append(t.bodies, "")
		}
	}
	t.sections = append(t.sections, config.OneOnOneSection{
		Title: notesSectionTitle,
		Hint:  "Notes during the meeting…\n\nTip: prefix lines with 'Action:' or '- [ ]' to create todos.",
	})
	t.bodies = append(t.bodies, o.Notes)

	t.session, t.prev = o, prev
	t.mode = ooModeEdit
	t.dirty, t.showMentions = false, false
	t.setSection(0)
	return t, textarea.Blink
}

// setSection stashes the current text and loads section i into the textarea.
func (t *oneOnOnesTab) setSection(i int) {
	if t.secIdx < len(t.bodies) && t.area.Focused() {
		t.bodies[t.secIdx] = t.area.Value()
	}
	t.secIdx = i
	t.area.Placeholder = strings.ReplaceAll(t.sections[i].Hint, "{name}", t.person.Name)
	t.area.SetValue(t.bodies[i])
	t.area.Focus()
	t.layoutEditor()
}

func (t oneOnOnesTab) handleEditKey(msg tea.KeyMsg) (oneOnOnesTab, tea.Cmd) {
	switch msg.String() {
	case "tab":
		t.setSection((t.secIdx + 1) % len(t.sections))
		return t, nil
	case "shift+tab":
		t.setSection((t.secIdx + len(t.sections) - 1) % len(t.sections))
		return t, nil
	case "ctrl+o":
		t.showMentions = !t.showMentions
		t.layoutEditor()
		return t, nil
	case "ctrl+s":
		if t.saving {
			return t, nil
		}
		t.saving = true
		return t, t.saveSession(false)
	case "esc":
		var cmd tea.Cmd
		if t.dirty && !t.saving {
			t.saving = true
			cmd = t.saveSession(false)
		}
		t.area.Blur()
		t.mode = ooModePerson
		if cmd == nil {
			cmd = t.loadPerson()
		}
		return t, cmd
	}
	t.dirty = true
	var cmd tea.Cmd
	t.area, cmd = t.area.Update(msg)
	return t, cmd
}

// collectSession copies the editor state back into a session record.
func (t *oneOnOnesTab) collectSession() storage.OneOnOne {
	t.bodies[t.secIdx] = t.area.Value()
	o := t.session
	o.Sections = nil
	last := len(t.sections) - 1
	for i, s := range t.sections[:last] {
		o.Sections = append(o.Sections, storage.OneOnOneSection{Title: s.Title, Body: strings.TrimSpace(t.bodies[i])})
	}
	o.Notes = strings.TrimSpace(t.bodies[last])
	// A prep sheet is dated when it's held, so prepping the day before still
	// files the 1:1 under the day it happened.
	if !t.session.Held() && o.Held() {
		o.Date = time.Now().Format("2006-01-02")
	}
	return o
}

// saveSession persists the session, creates todos from action items, and
// rewrites the person's Obsidian file.
func (t *oneOnOnesTab) saveSession(autosave bool) tea.Cmd {
	o := t.collectSession()
	t.session = o
	p := t.person
	var all []string
	for _, s := range o.Sections {
		all = append(all, s.Body)
	}
	all = append(all, o.Notes)
	actions := extractActions(strings.Join(all, "\n"))
	store := t.store

	return func() tea.Msg {
		if err := store.SaveOneOnOne(o); err != nil {
			return ooSavedMsg{err: err, autosave: autosave}
		}
		if len(actions) > 0 {
			addSourcedTodos(store, o.ID, "1:1 · "+p.Name, actions)
		}
		path, err := t.writePersonFile(p)
		return ooSavedMsg{err: err, autosave: autosave, path: path}
	}
}

// addSourcedTodos adds action items as todos, skipping any already created for sourceID
// so repeated saves never duplicate todos or resurrect completed ones.
func addSourcedTodos(store *storage.Store, sourceID, sourceTitle string, actions []string) {
	all, _ := store.LoadTodos()
	existing := map[string]bool{}
	for _, td := range all {
		if td.SourceMeetingID == sourceID {
			existing[td.Text] = true
		}
	}
	today := time.Now().Format("2006-01-02")
	added := false
	for _, a := range actions {
		if existing[a] {
			continue
		}
		all = append(all, storage.TodoItem{
			ID:                 storage.NewID(),
			Text:               a,
			Date:               today,
			SourceMeetingID:    sourceID,
			SourceMeetingTitle: sourceTitle,
		})
		existing[a] = true
		added = true
	}
	if added {
		_ = store.SaveTodos(all)
	}
}

func (t oneOnOnesTab) writePersonFile(p storage.Person) (string, error) {
	sessions, err := t.store.LoadOneOnOnes(p.ID)
	if err != nil {
		return "", err
	}
	return t.obs.WritePerson(p, sessions)
}

// ---- loading ----

func (t oneOnOnesTab) loadPeople() tea.Cmd {
	store := t.store
	return func() tea.Msg {
		people, err := store.LoadPeople()
		if err != nil {
			return ooPeopleLoadedMsg{err: err}
		}
		latest := map[string]storage.OneOnOne{}
		for _, p := range people {
			sessions, err := store.LoadOneOnOnes(p.ID)
			if err != nil {
				return ooPeopleLoadedMsg{err: err}
			}
			if len(sessions) > 0 {
				latest[p.ID] = sessions[0]
			}
		}
		return ooPeopleLoadedMsg{people: people, latest: latest}
	}
}

// loadPerson fetches the current person's sessions and their @mentions since the last held 1:1.
func (t oneOnOnesTab) loadPerson() tea.Cmd {
	p, store := t.person, t.store
	if p.ID == "" {
		return nil
	}
	return func() tea.Msg {
		sessions, err := store.LoadOneOnOnes(p.ID)
		if err != nil {
			return ooPersonLoadedMsg{personID: p.ID, err: err}
		}
		now := time.Now()
		from := now.AddDate(0, 0, -mentionWindowDays).Format("2006-01-02")
		for _, o := range sessions {
			if o.Held() {
				from = o.Date
				break
			}
		}
		mentions, err := store.MentionsInRange(p.Handle, from, now.Format("2006-01-02"))
		return ooPersonLoadedMsg{personID: p.ID, sessions: sessions, mentions: mentions, err: err}
	}
}

func errStatus(err error) tea.Cmd {
	return func() tea.Msg { return statusMsg{text: err.Error(), isError: true} }
}

// ---- view ----

func (t oneOnOnesTab) View() string {
	switch t.mode {
	case ooModeName, ooModeKind:
		return t.renderPersonForm()
	case ooModePerson:
		return t.renderPerson()
	case ooModeEdit:
		return t.renderEditor()
	}
	return t.renderPeople()
}

func (t oneOnOnesTab) renderPeople() string {
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render("1:1s") + "\n\n")

	if len(t.people) == 0 {
		b.WriteString("  " + mutedStyle.Render("No one yet. Press "+t.cfg.Hotkeys.NewItem+" to add someone you have 1:1s with.") + "\n")
	}
	for i, p := range t.people {
		status := "no 1:1s yet"
		if o, ok := t.latest[p.ID]; ok {
			if o.Held() {
				status = "last " + o.Date
			} else {
				status = "prep in progress"
			}
		}
		name := fmt.Sprintf("%-24s", truncate(p.Name, 24))
		kind := fmt.Sprintf("%-8s", p.Kind)
		if i == t.cursor {
			b.WriteString(selectedStyle.Width(t.width).Render("▸ "+name+"  "+kind+"  "+status) + "\n")
		} else {
			b.WriteString("  " + name + "  " + tagStyle.Render(kind) + "  " + mutedStyle.Render(status) + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(buildHints(
		[]string{"j/k", "Enter", t.cfg.Hotkeys.NewItem, t.cfg.Hotkeys.Delete},
		[]string{"navigate", "open", "add person", "delete"},
	))
	return b.String()
}

func (t oneOnOnesTab) renderPersonForm() string {
	var b strings.Builder
	heading := "Add person"
	if t.editPersonID != "" {
		heading = "Edit person"
	}
	b.WriteString("\n  " + titleStyle.Render(heading) + "\n\n")

	if t.mode == ooModeName {
		b.WriteString(inputLabelStyle.Render("  Name") + "\n\n")
		b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.nameInput.View()) + "\n\n")
		b.WriteString(buildHints([]string{"Enter", "Esc"}, []string{"next", "cancel"}))
		return b.String()
	}

	b.WriteString(inputLabelStyle.Render("  1:1 type") + "  " + mutedStyle.Render(t.nameInput.Value()) + "\n\n")
	labels := map[string]string{
		config.KindManager: "My manager",
		config.KindSkip:    "Skip level",
		config.KindPeer:    "Peer",
		config.KindReport:  "Works for me",
	}
	for i, k := range config.OneOnOneKinds {
		line := fmt.Sprintf("%d  %s", i+1, labels[k])
		if i == t.kindIdx {
			b.WriteString(selectedStyle.Width(t.width).Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + mutedStyle.Render(line) + "\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(buildHints([]string{"j/k", "Enter", "ctrl+b", "Esc"}, []string{"choose", "save", "back", "cancel"}))
	return b.String()
}

func (t oneOnOnesTab) renderPerson() string {
	var b strings.Builder
	p := t.person
	b.WriteString("\n  " + titleStyle.Render(p.Name) + "  " + tagStyle.Render(p.Kind))
	if p.Handle != "" {
		b.WriteString("  " + keyStyle.Render("@"+p.Handle))
	}
	b.WriteString("\n\n")

	// Mentions get at most a third of the screen so the session list stays visible.
	b.WriteString("  " + sectionHeader.Render("Since last 1:1") + "\n")
	if len(t.mentions) == 0 {
		b.WriteString("  " + mutedStyle.Render("No @"+p.Handle+" mentions in your notes.") + "\n")
	}
	limit := max(3, t.height/3)
	start := max(0, len(t.mentions)-limit)
	if start > 0 {
		b.WriteString("  " + subtleStyle.Render(fmt.Sprintf("… %d earlier", start)) + "\n")
	}
	for _, m := range t.mentions[start:] {
		b.WriteString("  " + mutedStyle.Render(m.Date[5:]+"  "+truncate(m.Source, 18)+"  ") + truncate(m.Text, max(20, t.width-34)) + "\n")
	}

	b.WriteString("\n  " + sectionHeader.Render("Sessions") + "\n")
	if t.personReady && len(t.sessions) == 0 {
		b.WriteString("  " + mutedStyle.Render("No 1:1s yet. Press "+t.cfg.Hotkeys.NewItem+" to start a prep sheet.") + "\n")
	}
	for i, o := range t.sessions {
		status := "prep"
		if o.Held() {
			status = "held"
		}
		filled := 0
		for _, s := range o.Sections {
			if s.Body != "" {
				filled++
			}
		}
		line := fmt.Sprintf("%s  %-4s  %d/%d sections", o.Date, status, filled, len(o.Sections))
		if i == t.sessCursor {
			b.WriteString(selectedStyle.Width(t.width).Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}

	newLabel := "new prep"
	if len(t.sessions) > 0 && !t.sessions[0].Held() {
		newLabel = "continue prep"
	}
	b.WriteString("\n")
	b.WriteString(buildHints(
		[]string{t.cfg.Hotkeys.NewItem, "Enter", "e", t.cfg.Hotkeys.Delete, "Esc"},
		[]string{newLabel, "open", "edit person", "delete", "back"},
	))
	return b.String()
}

// contextLines returns the block shown above the editor: the previous session's
// linked section by default, or @mentions when toggled with ctrl+o.
func (t oneOnOnesTab) contextLines() []string {
	if t.mode != ooModeEdit || t.secIdx >= len(t.sections) {
		return nil
	}
	var lines []string
	if t.showMentions {
		for _, m := range t.mentions {
			lines = append(lines, m.Date[5:]+"  "+m.Text)
		}
		if len(lines) == 0 {
			lines = []string{"No @" + t.person.Handle + " mentions since the last 1:1."}
		}
	} else {
		from := t.sections[t.secIdx].ContextFrom
		if from == "" || t.prev == nil {
			return nil
		}
		body := strings.TrimSpace(t.prev.Section(from))
		if body == "" {
			return nil
		}
		lines = strings.Split(body, "\n")
	}
	if len(lines) > maxContextLines {
		more := len(lines) - maxContextLines + 1
		lines = append(lines[:maxContextLines-1], fmt.Sprintf("… %d more", more))
	}
	return lines
}

func (t oneOnOnesTab) renderEditor() string {
	var b strings.Builder
	sec := t.sections[t.secIdx]

	b.WriteString("  " + titleStyle.Render("1:1 with "+t.person.Name) + "  " +
		mutedStyle.Render(t.person.Kind+" · "+t.session.Date) + "\n")

	var dots strings.Builder
	for i := range t.sections {
		if i == t.secIdx {
			dots.WriteString(lipgloss.NewStyle().Foreground(colorIris).Render("●"))
		} else if strings.TrimSpace(t.bodies[i]) != "" {
			dots.WriteString(tagStyle.Render("●"))
		} else {
			dots.WriteString(subtleStyle.Render("○"))
		}
	}
	b.WriteString("  " + dots.String() + "  " + mutedStyle.Render(fmt.Sprintf("%d/%d", t.secIdx+1, len(t.sections))) + "\n")

	if ctx := t.contextLines(); len(ctx) > 0 {
		label := "Mentions since last 1:1"
		if !t.showMentions {
			label = fmt.Sprintf("Last time · %s (%s)", sec.ContextFrom, t.prev.Date)
		}
		b.WriteString("  " + carriedStyle.Render(label) + "\n")
		for _, l := range ctx {
			b.WriteString("  " + mutedStyle.Render(truncate(l, max(20, t.width-4))) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(inputLabelStyle.Render("  "+sec.Title) + "\n")
	b.WriteString(inputBoxFocusedStyle.Width(t.width-4).Render(t.area.View()) + "\n")

	mentionsLabel := "mentions"
	if t.showMentions {
		mentionsLabel = "last time"
	}
	b.WriteString(buildHints(
		[]string{"Tab", "Shift+Tab", "Ctrl+O", "Ctrl+S", "Esc"},
		[]string{"next section", "prev", mentionsLabel, "save", "save & close"},
	))
	return b.String()
}
