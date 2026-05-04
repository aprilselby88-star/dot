package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

// Writer handles generating and writing daily notes to an Obsidian vault.
type Writer struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Writer {
	return &Writer{cfg: cfg}
}

// DailyNotePath returns the absolute path for today's note file.
func (w *Writer) DailyNotePath() string {
	rel := time.Now().Format(w.cfg.Obsidian.NotesTemplate)
	if !strings.HasSuffix(rel, ".md") {
		rel += ".md"
	}
	return filepath.Join(config.ExpandPath(w.cfg.Obsidian.VaultPath), rel)
}

// Write generates today's daily note and saves it to the Obsidian vault.
// Returns the path it was written to.
func (w *Writer) Write(
	stats storage.Stats,
	todos []storage.TodoItem,
	meetings []storage.Meeting,
	trackedPRs []storage.TrackedPR,
	note string,
) (string, error) {
	content := w.Build(stats, todos, meetings, trackedPRs, note)
	path := w.DailyNotePath()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// Build generates the markdown content without writing to disk.
func (w *Writer) Build(
	stats storage.Stats,
	todos []storage.TodoItem,
	meetings []storage.Meeting,
	trackedPRs []storage.TrackedPR,
	note string,
) string {
	today := time.Now()
	todayStr := today.Format("2006-01-02")
	allTags := collectTags(todos, meetings, todayStr)

	var b strings.Builder

	// YAML frontmatter — Obsidian 1.4+ renders tags from here as pills.
	// Strip leading # so tag names are valid frontmatter values.
	if len(allTags) > 0 {
		b.WriteString("---\ntags:\n")
		for _, tag := range allTags {
			fmt.Fprintf(&b, "  - %s\n", strings.TrimPrefix(tag, "#"))
		}
		b.WriteString("---\n\n")
	}

	fmt.Fprintf(&b, "# %s\n\n", today.Format("Monday, 02 January 2006"))

	// Overview table
	b.WriteString("## Overview\n\n")
	b.WriteString("| | Today | This Week | This Month |\n")
	b.WriteString("|---|---|---|---|\n")
	fmt.Fprintf(&b, "| Meetings | %d | %d | %d |\n", stats.TodayMeetings, stats.WeekMeetings, stats.MonthMeetings)
	fmt.Fprintf(&b, "| Todos done | %d/%d | %d | %d |\n\n", stats.TodayDone, stats.TodayTotal, stats.WeekDone, stats.MonthDone)

	if stats.Streak > 1 {
		fmt.Fprintf(&b, "**%d day streak!**\n\n", stats.Streak)
	}
	fmt.Fprintf(&b, "> %s\n\n", Motivation(stats))

	// Todos
	b.WriteString("## Todos\n\n")
	hasTodos := false
	for _, t := range todos {
		if t.Date != todayStr {
			continue
		}
		hasTodos = true
		check := "- [ ]"
		if t.Done {
			check = "- [x]"
		}
		carried := ""
		if t.CarriedFrom != "" {
			carried = fmt.Sprintf(" *(carried from %s)*", t.CarriedFrom)
		}
		tagStr := ""
		if len(t.Tags) > 0 {
			tagStr = " " + strings.Join(t.Tags, " ")
		}
		doneStr := ""
		if t.Done && t.DoneAt != nil {
			if t.DoneAt.Format("2006-01-02") == todayStr {
				doneStr = fmt.Sprintf(" *(done %s)*", t.DoneAt.Format("15:04"))
			} else {
				doneStr = fmt.Sprintf(" *(done %s)*", t.DoneAt.Format("02 Jan 2006"))
			}
		}
		sourceStr := ""
		if t.SourceMeetingTitle != "" {
			sourceStr = fmt.Sprintf(" *(↩ %s)*", t.SourceMeetingTitle)
		}
		text := boldMentions(t.Text)
		fmt.Fprintf(&b, "%s %s%s%s%s%s\n", check, text, carried, tagStr, doneStr, sourceStr)
	}
	if !hasTodos {
		b.WriteString("*No todos today.*\n")
	}
	b.WriteString("\n")

	// Team Actions — todos assigned to others via @mentions
	var teamTodos []storage.TodoItem
	for _, t := range todos {
		if t.Date == todayStr && !t.Done && hasMentions(t.Text) {
			teamTodos = append(teamTodos, t)
		}
	}
	if len(teamTodos) > 0 {
		b.WriteString("## Team Actions\n\n")
		// group by assignee
		type assignment struct {
			person string
			todos  []storage.TodoItem
		}
		byPerson := map[string][]storage.TodoItem{}
		order := []string{}
		for _, t := range teamTodos {
			for _, m := range extractMentions(t.Text) {
				if _, exists := byPerson[m]; !exists {
					order = append(order, m)
				}
				byPerson[m] = append(byPerson[m], t)
			}
		}
		for _, person := range order {
			fmt.Fprintf(&b, "### %s\n\n", person)
			for _, t := range byPerson[person] {
				sourceStr := ""
				if t.SourceMeetingTitle != "" {
					sourceStr = fmt.Sprintf(" *(↩ %s)*", t.SourceMeetingTitle)
				}
				fmt.Fprintf(&b, "- [ ] %s%s\n", boldMentions(t.Text), sourceStr)
			}
			b.WriteString("\n")
		}
	}

	// Meetings
	b.WriteString("## Meetings\n\n")
	hasMeetings := false
	for _, m := range meetings {
		if m.Date != todayStr {
			continue
		}
		hasMeetings = true
		fmt.Fprintf(&b, "### %s\n\n", m.Title)
		if len(m.Attendees) > 0 {
			fmt.Fprintf(&b, "**Attendees:** %s\n\n", strings.Join(m.Attendees, ", "))
		}
		if len(m.Tags) > 0 {
			fmt.Fprintf(&b, "**Tags:** %s\n\n", strings.Join(m.Tags, " "))
		}
		if len(m.Decisions) > 0 {
			b.WriteString("**Decisions:**\n")
			for _, d := range m.Decisions {
				fmt.Fprintf(&b, "- %s\n", d)
			}
			b.WriteString("\n")
		}
		if m.Summary != "" {
			b.WriteString(m.Summary)
			b.WriteString("\n")
		}
		if m.RawNotes != "" {
			b.WriteString("\n<details><summary>Raw Notes</summary>\n\n")
			b.WriteString(m.RawNotes)
			b.WriteString("\n\n</details>\n\n")
		}
	}
	if !hasMeetings {
		b.WriteString("*No meetings today.*\n\n")
	}

	// Tracked PRs
	b.WriteString("## PRs to Watch\n\n")
	var openPRs, donePRs []storage.TrackedPR
	for _, pr := range trackedPRs {
		if pr.DoneAt != nil {
			donePRs = append(donePRs, pr)
		} else if pr.State == "open" || pr.State == "" {
			openPRs = append(openPRs, pr)
		}
	}
	if len(openPRs) > 0 {
		b.WriteString("| Repo | PR | Title | Author | State |\n")
		b.WriteString("|---|---|---|---|---|\n")
		for _, pr := range openPRs {
			fmt.Fprintf(&b, "| %s | [#%d](%s) | %s | @%s | %s |\n",
				pr.Repo, pr.Number, pr.URL, pr.Title, pr.Author, pr.State)
		}
		b.WriteString("\n")
	} else {
		b.WriteString("*No open PRs being tracked.*\n\n")
	}
	if len(donePRs) > 0 {
		for _, pr := range donePRs {
			doneStr := pr.DoneAt.Format("Jan 02 15:04")
			fmt.Fprintf(&b, "- ~~[#%d %s](%s) — %s @%s~~ *(done %s)*\n",
				pr.Number, pr.Title, pr.URL, pr.Repo, pr.Author, doneStr)
		}
		b.WriteString("\n")
	}

	// Free-form notes
	b.WriteString("## Notes\n\n")
	if strings.TrimSpace(note) != "" {
		b.WriteString(note)
		b.WriteString("\n\n")
	} else {
		b.WriteString("*No notes today.*\n\n")
	}

	return b.String()
}

// boldMentions wraps @person tokens in markdown bold so they stand out in Obsidian.
func boldMentions(text string) string {
	words := strings.Fields(text)
	for i, w := range words {
		clean := strings.TrimRight(w, ".,;:!?")
		if strings.HasPrefix(clean, "@") && len(clean) > 1 {
			suffix := w[len(clean):]
			words[i] = "**" + clean + "**" + suffix
		}
	}
	return strings.Join(words, " ")
}

// hasMentions returns true if the text contains at least one @person token.
func hasMentions(text string) bool {
	return len(extractMentions(text)) > 0
}

// extractMentions returns unique @person tokens from text.
func extractMentions(text string) []string {
	seen := map[string]bool{}
	var mentions []string
	for _, w := range strings.Fields(text) {
		clean := strings.TrimRight(w, ".,;:!?")
		if strings.HasPrefix(clean, "@") && len(clean) > 1 && !seen[clean] {
			seen[clean] = true
			mentions = append(mentions, clean)
		}
	}
	return mentions
}

func collectTags(todos []storage.TodoItem, meetings []storage.Meeting, today string) []string {
	seen := map[string]bool{}
	var tags []string
	for _, t := range todos {
		if t.Date == today {
			for _, tag := range t.Tags {
				if !seen[tag] {
					seen[tag] = true
					tags = append(tags, tag)
				}
			}
		}
	}
	for _, m := range meetings {
		if m.Date == today {
			for _, tag := range m.Tags {
				if !seen[tag] {
					seen[tag] = true
					tags = append(tags, tag)
				}
			}
		}
	}
	return tags
}

var motivations = []string{
	"Every small step forward is progress. Keep going!",
	"You're doing great — one meeting, one task at a time.",
	"Focus is your superpower today.",
	"Today's effort is tomorrow's progress.",
	"Consistency beats intensity every time.",
	"Progress, not perfection.",
	"What you do today matters more than you know.",
	"Small wins compound into big results.",
	"You're not behind. You're exactly where you need to be.",
	"One task at a time. You've got this.",
}

// Motivation returns a contextual motivational message based on stats.
func Motivation(stats storage.Stats) string {
	if stats.Streak >= 7 {
		return fmt.Sprintf("%d day streak! You're absolutely on a roll — keep it going!", stats.Streak)
	}
	if stats.TodayDone >= 5 {
		return fmt.Sprintf("%d todos crushed today. Outstanding!", stats.TodayDone)
	}
	if stats.TodayMeetings >= 4 {
		return fmt.Sprintf("%d meetings and still going. Your calendar is a workout!", stats.TodayMeetings)
	}
	if stats.TotalMeetings >= 100 {
		return fmt.Sprintf("%d total meetings attended. You're a collaboration legend!", stats.TotalMeetings)
	}
	if stats.WeekDone >= 15 {
		return fmt.Sprintf("%d todos done this week. What a productive stretch!", stats.WeekDone)
	}
	idx := time.Now().YearDay() % len(motivations)
	return motivations[idx]
}
