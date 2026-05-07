package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TodoItem is a single task that can carry forward across days.
type TodoItem struct {
	ID                 string     `json:"id"`
	Text               string     `json:"text"`
	Done               bool       `json:"done"`
	Date               string     `json:"date"`                   // "2006-01-02"
	DoneAt             *time.Time `json:"done_at,omitempty"`
	CarriedFrom        string     `json:"carried_from,omitempty"` // original date if carried over
	Tags               []string   `json:"tags,omitempty"`
	SourceMeetingID    string     `json:"source_meeting_id,omitempty"`
	SourceMeetingTitle string     `json:"source_meeting_title,omitempty"`
}

// Meeting captures notes and auto-generated summary for a meeting.
type Meeting struct {
	ID        string   `json:"id"`
	Date      string   `json:"date"`
	Title     string   `json:"title"`
	Attendees []string `json:"attendees"`
	RawNotes  string   `json:"raw_notes"`
	Summary   string   `json:"summary"`
	Tags      []string `json:"tags,omitempty"`
	Decisions []string `json:"decisions,omitempty"`
}

// TrackedPR is a pull request added to the watch zone.
type TrackedPR struct {
	Repo           string     `json:"repo"`
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	Author         string     `json:"author"`
	URL            string     `json:"url"`
	State          string     `json:"state"` // "open", "merged", "closed"
	AddedAt        string     `json:"added_at"`
	NotificationID string     `json:"notification_id,omitempty"`
	Tags           []string   `json:"tags,omitempty"`
	DoneAt         *time.Time `json:"done_at,omitempty"`
}

func (p TrackedPR) Key() string {
	return fmt.Sprintf("%s#%d", p.Repo, p.Number)
}

// Stats holds computed usage metrics for the summary and motivation views.
type Stats struct {
	TodayDate     string
	TodayMeetings int
	TodayDone     int
	TodayTotal    int
	WeekMeetings  int
	WeekDone      int
	MonthMeetings int
	MonthDone     int
	TotalMeetings int
	TotalDone     int
	TrackedPRs    int
	Streak        int
}

// Store is the JSON file-backed data store.
type Store struct {
	dir string
}

func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// ---- Todos ----

type todosFile struct {
	Items []TodoItem `json:"items"`
}

func (s *Store) LoadTodos() ([]TodoItem, error) {
	var f todosFile
	if err := s.load("todos.json", &f); err != nil {
		return nil, err
	}
	return f.Items, nil
}

func (s *Store) SaveTodos(items []TodoItem) error {
	return s.save("todos.json", todosFile{Items: items})
}

func (s *Store) TodaysTodos() ([]TodoItem, error) {
	all, err := s.LoadTodos()
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	var result []TodoItem
	for _, t := range all {
		if t.Date == today {
			result = append(result, t)
		}
	}
	return result, nil
}

// CarryOver promotes incomplete todos from previous days into today.
// It is idempotent: running twice will not duplicate todos.
// A todo is considered complete if any version of it (original or any
// carried copy) has been marked done — it will not be carried forward.
// Returns the count of newly added items.
func (s *Store) CarryOver() (int, error) {
	all, err := s.LoadTodos()
	if err != nil {
		return 0, err
	}
	today := time.Now().Format("2006-01-02")

	// Build a key for each todo: "text|originalDate".
	todoKey := func(t TodoItem) string {
		orig := t.Date
		if t.CarriedFrom != "" {
			orig = t.CarriedFrom
		}
		return t.Text + "|" + orig
	}

	// Any version marked done means the whole chain is done — don't carry.
	doneKeys := map[string]bool{}
	for _, t := range all {
		if t.Done {
			doneKeys[todoKey(t)] = true
		}
	}

	// Track items already carried into today so we stay idempotent.
	existing := map[string]bool{}
	for _, t := range all {
		if t.Date == today && t.CarriedFrom != "" {
			existing[todoKey(t)] = true
		}
	}

	var toAdd []TodoItem
	for _, t := range all {
		if t.Date >= today {
			continue
		}
		key := todoKey(t)
		if doneKeys[key] || existing[key] {
			continue
		}
		orig := t.Date
		if t.CarriedFrom != "" {
			orig = t.CarriedFrom
		}
		toAdd = append(toAdd, TodoItem{
			ID:          newID(),
			Text:        t.Text,
			Date:        today,
			CarriedFrom: orig,
			Tags:        t.Tags,
		})
		existing[key] = true
	}

	if len(toAdd) > 0 {
		all = append(all, toAdd...)
		if err := s.SaveTodos(all); err != nil {
			return 0, err
		}
	}
	return len(toAdd), nil
}

// ---- Meetings ----

type meetingsFile struct {
	Meetings []Meeting `json:"meetings"`
}

func (s *Store) LoadMeetings() ([]Meeting, error) {
	var f meetingsFile
	if err := s.load("meetings.json", &f); err != nil {
		return nil, err
	}
	return f.Meetings, nil
}

func (s *Store) SaveMeeting(m Meeting) error {
	meetings, err := s.LoadMeetings()
	if err != nil {
		return err
	}
	for i, ex := range meetings {
		if ex.ID == m.ID {
			meetings[i] = m
			return s.save("meetings.json", meetingsFile{Meetings: meetings})
		}
	}
	return s.save("meetings.json", meetingsFile{Meetings: append(meetings, m)})
}

func (s *Store) DeleteMeeting(id string) error {
	meetings, err := s.LoadMeetings()
	if err != nil {
		return err
	}
	out := meetings[:0]
	for _, m := range meetings {
		if m.ID != id {
			out = append(out, m)
		}
	}
	return s.save("meetings.json", meetingsFile{Meetings: out})
}

func (s *Store) TodaysMeetings() ([]Meeting, error) {
	all, err := s.LoadMeetings()
	if err != nil {
		return nil, err
	}
	today := time.Now().Format("2006-01-02")
	var result []Meeting
	for _, m := range all {
		if m.Date == today {
			result = append(result, m)
		}
	}
	return result, nil
}

// ---- Tracked PRs ----

type prsFile struct {
	PRs []TrackedPR `json:"prs"`
}

func (s *Store) LoadTrackedPRs() ([]TrackedPR, error) {
	var f prsFile
	if err := s.load("tracked_prs.json", &f); err != nil {
		return nil, err
	}
	return f.PRs, nil
}

// LoadActiveTrackedPRs returns only PRs that have not been marked done.
func (s *Store) LoadActiveTrackedPRs() ([]TrackedPR, error) {
	all, err := s.LoadTrackedPRs()
	if err != nil {
		return nil, err
	}
	var active []TrackedPR
	for _, pr := range all {
		if pr.DoneAt == nil {
			active = append(active, pr)
		}
	}
	return active, nil
}

func (s *Store) SaveTrackedPRs(prs []TrackedPR) error {
	return s.save("tracked_prs.json", prsFile{PRs: prs})
}

func (s *Store) AddTrackedPR(pr TrackedPR) error {
	prs, err := s.LoadTrackedPRs()
	if err != nil {
		return err
	}
	for i, ex := range prs {
		if ex.Key() == pr.Key() {
			prs[i] = pr
			return s.SaveTrackedPRs(prs)
		}
	}
	return s.SaveTrackedPRs(append(prs, pr))
}

func (s *Store) RemoveTrackedPR(key string) error {
	prs, err := s.LoadTrackedPRs()
	if err != nil {
		return err
	}
	out := prs[:0]
	for _, p := range prs {
		if p.Key() != key {
			out = append(out, p)
		}
	}
	return s.SaveTrackedPRs(out)
}

func (s *Store) MarkTrackedPRDone(key string) error {
	prs, err := s.LoadTrackedPRs()
	if err != nil {
		return err
	}
	now := time.Now()
	for i, p := range prs {
		if p.Key() == key {
			prs[i].DoneAt = &now
			return s.SaveTrackedPRs(prs)
		}
	}
	return nil
}

// ---- Daily notes ----

type dailyNotesFile struct {
	Notes map[string]string `json:"notes"`
}

func (s *Store) LoadNote(date string) (string, error) {
	var f dailyNotesFile
	if err := s.load("daily_notes.json", &f); err != nil {
		return "", err
	}
	if f.Notes == nil {
		return "", nil
	}
	return f.Notes[date], nil
}

func (s *Store) SaveNote(date, content string) error {
	var f dailyNotesFile
	_ = s.load("daily_notes.json", &f)
	if f.Notes == nil {
		f.Notes = map[string]string{}
	}
	f.Notes[date] = content
	return s.save("daily_notes.json", f)
}

// ---- Tags ----

// AllTags returns a sorted, deduplicated list of every tag used across todos and meetings.
func (s *Store) AllTags() ([]string, error) {
	todos, err := s.LoadTodos()
	if err != nil {
		return nil, err
	}
	meetings, err := s.LoadMeetings()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, t := range todos {
		for _, tag := range t.Tags {
			seen[tag] = true
		}
	}
	for _, m := range meetings {
		for _, tag := range m.Tags {
			seen[tag] = true
		}
	}
	tags := make([]string, 0, len(seen))
	for tag := range seen {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	return tags, nil
}

// ---- Date-range queries (used by summary generation) ----

// LoadTodosInRange returns todos whose date falls within [from, to] (inclusive, "2006-01-02" format).
func (s *Store) LoadTodosInRange(from, to string) ([]TodoItem, error) {
	all, err := s.LoadTodos()
	if err != nil {
		return nil, err
	}
	var result []TodoItem
	for _, t := range all {
		if t.Date >= from && t.Date <= to {
			result = append(result, t)
		}
	}
	return result, nil
}

// LoadMeetingsInRange returns meetings whose date falls within [from, to].
func (s *Store) LoadMeetingsInRange(from, to string) ([]Meeting, error) {
	all, err := s.LoadMeetings()
	if err != nil {
		return nil, err
	}
	var result []Meeting
	for _, m := range all {
		if m.Date >= from && m.Date <= to {
			result = append(result, m)
		}
	}
	return result, nil
}

// LoadNotesInRange returns a map of date → note content for all dates in [from, to].
func (s *Store) LoadNotesInRange(from, to string) (map[string]string, error) {
	var f dailyNotesFile
	if err := s.load("daily_notes.json", &f); err != nil {
		return nil, err
	}
	result := map[string]string{}
	for date, content := range f.Notes {
		if date >= from && date <= to && strings.TrimSpace(content) != "" {
			result[date] = content
		}
	}
	return result, nil
}

// LoadPRsInRange returns tracked PRs that were added or completed within [from, to].
func (s *Store) LoadPRsInRange(from, to string) ([]TrackedPR, error) {
	all, err := s.LoadTrackedPRs()
	if err != nil {
		return nil, err
	}
	var result []TrackedPR
	for _, pr := range all {
		addedIn := pr.AddedAt >= from && pr.AddedAt <= to
		doneIn := pr.DoneAt != nil && pr.DoneAt.Format("2006-01-02") >= from && pr.DoneAt.Format("2006-01-02") <= to
		if addedIn || doneIn {
			result = append(result, pr)
		}
	}
	return result, nil
}

// ---- Stats ----

func (s *Store) ComputeStats() (Stats, error) {
	meetings, err := s.LoadMeetings()
	if err != nil {
		return Stats{}, err
	}
	todos, err := s.LoadTodos()
	if err != nil {
		return Stats{}, err
	}
	prs, err := s.LoadTrackedPRs()
	if err != nil {
		return Stats{}, err
	}

	now := time.Now()
	today := now.Format("2006-01-02")
	weekAgo := now.AddDate(0, 0, -7).Format("2006-01-02")
	monthAgo := now.AddDate(0, -1, 0).Format("2006-01-02")

	st := Stats{
		TodayDate:  today,
		TrackedPRs: len(prs),
	}

	for _, m := range meetings {
		st.TotalMeetings++
		if m.Date == today {
			st.TodayMeetings++
		}
		if m.Date >= weekAgo {
			st.WeekMeetings++
		}
		if m.Date >= monthAgo {
			st.MonthMeetings++
		}
	}

	for _, t := range todos {
		if t.Date == today {
			st.TodayTotal++
			if t.Done {
				st.TodayDone++
			}
		}
		if t.Done {
			st.TotalDone++
			if t.Date >= weekAgo {
				st.WeekDone++
			}
			if t.Date >= monthAgo {
				st.MonthDone++
			}
		}
	}

	st.Streak = computeStreak(meetings, todos)
	return st, nil
}

func computeStreak(meetings []Meeting, todos []TodoItem) int {
	active := map[string]bool{}
	for _, m := range meetings {
		active[m.Date] = true
	}
	for _, t := range todos {
		if t.Done {
			active[t.Date] = true
		}
	}

	streak := 0
	d := time.Now()
	for i := 0; i < 365; i++ {
		ds := d.Format("2006-01-02")
		if !active[ds] {
			if i == 0 {
				// today has no activity yet, check yesterday
				d = d.AddDate(0, 0, -1)
				continue
			}
			break
		}
		streak++
		d = d.AddDate(0, 0, -1)
	}
	return streak
}

// ---- helpers ----

func (s *Store) load(name string, v any) error {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}

func (s *Store) save(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.dir, name), data, 0600)
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
