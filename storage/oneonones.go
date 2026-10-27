package storage

import (
	"sort"
	"strings"
	"unicode"
)

// Person is someone you hold recurring 1:1s with.
// Kind is one of the config.Kind* values and selects the prep sheet template.
// Handle is the @mention (without the @) used to find notes about them.
type Person struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Handle string `json:"handle"`
}

// OneOnOneSection is a filled-in heading on a prep sheet. The title is stored
// with the body so old sessions still read correctly if the template changes.
type OneOnOneSection struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// OneOnOne is a single session: the prep sheet plus notes taken during the meeting.
type OneOnOne struct {
	ID       string            `json:"id"`
	PersonID string            `json:"person_id"`
	Date     string            `json:"date"` // "2006-01-02"
	Sections []OneOnOneSection `json:"sections"`
	Notes    string            `json:"notes,omitempty"`
}

// Section returns the body of the section with the given title, or "" if absent.
// "Notes" returns the live meeting notes.
func (o OneOnOne) Section(title string) string {
	if title == "Notes" {
		return o.Notes
	}
	for _, s := range o.Sections {
		if s.Title == title {
			return s.Body
		}
	}
	return ""
}

// Held reports whether notes were taken, i.e. the meeting has happened.
func (o OneOnOne) Held() bool {
	return strings.TrimSpace(o.Notes) != ""
}

// DefaultHandle derives an @mention handle from a name: the lowercased first word.
func DefaultHandle(name string) string {
	f := strings.Fields(name)
	if len(f) == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(f[0], "@"))
}

type oneOnOnesFile struct {
	People   []Person   `json:"people"`
	Sessions []OneOnOne `json:"sessions"`
}

func (s *Store) loadOneOnOnes() (oneOnOnesFile, error) {
	var f oneOnOnesFile
	err := s.load("one_on_ones.json", &f)
	return f, err
}

func (s *Store) saveOneOnOnes(f oneOnOnesFile) error {
	return s.save("one_on_ones.json", f)
}

// LoadPeople returns everyone you hold 1:1s with, sorted by name.
func (s *Store) LoadPeople() ([]Person, error) {
	f, err := s.loadOneOnOnes()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(f.People, func(i, j int) bool {
		return strings.ToLower(f.People[i].Name) < strings.ToLower(f.People[j].Name)
	})
	return f.People, nil
}

// SavePerson inserts or updates a person by ID.
func (s *Store) SavePerson(p Person) error {
	f, err := s.loadOneOnOnes()
	if err != nil {
		return err
	}
	for i, ex := range f.People {
		if ex.ID == p.ID {
			f.People[i] = p
			return s.saveOneOnOnes(f)
		}
	}
	f.People = append(f.People, p)
	return s.saveOneOnOnes(f)
}

// DeletePerson removes a person and all of their sessions.
func (s *Store) DeletePerson(id string) error {
	f, err := s.loadOneOnOnes()
	if err != nil {
		return err
	}
	people := f.People[:0]
	for _, p := range f.People {
		if p.ID != id {
			people = append(people, p)
		}
	}
	sessions := f.Sessions[:0]
	for _, o := range f.Sessions {
		if o.PersonID != id {
			sessions = append(sessions, o)
		}
	}
	f.People, f.Sessions = people, sessions
	return s.saveOneOnOnes(f)
}

// LoadOneOnOnes returns every session for a person, newest first.
func (s *Store) LoadOneOnOnes(personID string) ([]OneOnOne, error) {
	f, err := s.loadOneOnOnes()
	if err != nil {
		return nil, err
	}
	var out []OneOnOne
	for _, o := range f.Sessions {
		if o.PersonID == personID {
			out = append(out, o)
		}
	}
	sortSessionsNewestFirst(out)
	return out, nil
}

// LoadOneOnOnesOn returns every session held on the given date ("2006-01-02").
func (s *Store) LoadOneOnOnesOn(date string) ([]OneOnOne, error) {
	f, err := s.loadOneOnOnes()
	if err != nil {
		return nil, err
	}
	var out []OneOnOne
	for _, o := range f.Sessions {
		if o.Date == date {
			out = append(out, o)
		}
	}
	return out, nil
}

// SaveOneOnOne inserts or updates a session by ID.
func (s *Store) SaveOneOnOne(o OneOnOne) error {
	f, err := s.loadOneOnOnes()
	if err != nil {
		return err
	}
	for i, ex := range f.Sessions {
		if ex.ID == o.ID {
			f.Sessions[i] = o
			return s.saveOneOnOnes(f)
		}
	}
	f.Sessions = append(f.Sessions, o)
	return s.saveOneOnOnes(f)
}

// DeleteOneOnOne removes a single session.
func (s *Store) DeleteOneOnOne(id string) error {
	f, err := s.loadOneOnOnes()
	if err != nil {
		return err
	}
	out := f.Sessions[:0]
	for _, o := range f.Sessions {
		if o.ID != id {
			out = append(out, o)
		}
	}
	f.Sessions = out
	return s.saveOneOnOnes(f)
}

func sortSessionsNewestFirst(sessions []OneOnOne) {
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].Date > sessions[j].Date
	})
}

// Mention is a line from your notes that @mentions someone.
type Mention struct {
	Date   string // "2006-01-02"
	Source string // "Daily note", a meeting title, or "Todo"
	Text   string
}

// MentionsInRange returns lines from daily notes, meetings, and todos dated within
// [from, to] that mention @handle (case-insensitive), oldest first.
func (s *Store) MentionsInRange(handle, from, to string) ([]Mention, error) {
	if handle == "" {
		return nil, nil
	}
	var out []Mention

	notes, err := s.LoadNotesInRange(from, to)
	if err != nil {
		return nil, err
	}
	for date, note := range notes {
		for _, line := range mentioningLines(note, handle) {
			out = append(out, Mention{Date: date, Source: "Daily note", Text: line})
		}
	}

	meetings, err := s.LoadMeetingsInRange(from, to)
	if err != nil {
		return nil, err
	}
	for _, m := range meetings {
		for _, line := range mentioningLines(m.RawNotes+"\n"+strings.Join(m.Decisions, "\n"), handle) {
			out = append(out, Mention{Date: m.Date, Source: m.Title, Text: line})
		}
	}

	todos, err := s.LoadTodosInRange(from, to)
	if err != nil {
		return nil, err
	}
	for _, t := range todos {
		if mentions(t.Text, handle) {
			out = append(out, Mention{Date: t.Date, Source: "Todo", Text: t.Text})
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// mentioningLines returns the trimmed, non-empty lines of text that mention @handle.
func mentioningLines(text, handle string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && mentions(line, handle) {
			out = append(out, line)
		}
	}
	return out
}

// mentions reports whether text contains @handle as a whole token, so @al does not match @alice.
func mentions(text, handle string) bool {
	lower := strings.ToLower(text)
	needle := "@" + strings.ToLower(handle)
	for i := 0; ; {
		j := strings.Index(lower[i:], needle)
		if j < 0 {
			return false
		}
		end := i + j + len(needle)
		if end == len(lower) {
			return true
		}
		next := rune(lower[end])
		if !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' && next != '-' {
			return true
		}
		i = end
	}
}
