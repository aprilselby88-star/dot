package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

// weeklySummaryPath returns the vault path for a week's summary.
// weekStart must be the Monday of that week.
func (w *Writer) weeklySummaryPath(weekStart time.Time) string {
	_, week := weekStart.ISOWeek()
	filename := fmt.Sprintf("%d-W%02d.md", weekStart.Year(), week)
	return filepath.Join(config.ExpandPath(w.cfg.Obsidian.VaultPath),
		"Daily Notes", "Summaries", "Week", filename)
}

// monthlySummaryPath returns the vault path for a month's summary.
func (w *Writer) monthlySummaryPath(month time.Time) string {
	filename := month.Format("2006-01 January") + ".md"
	return filepath.Join(config.ExpandPath(w.cfg.Obsidian.VaultPath),
		"Daily Notes", "Summaries", "Month", filename)
}

// WriteWeeklySummaryIfNeeded generates the most recently completed week's summary
// if the file does not yet exist. A "complete" week is Mon–Sun; the current
// in-progress week is never summarised.
func (w *Writer) WriteWeeklySummaryIfNeeded(now time.Time, store *storage.Store) (string, error) {
	weekStart, weekEnd := lastCompleteWeek(now)
	path := w.weeklySummaryPath(weekStart)
	if _, err := os.Stat(path); err == nil {
		return "", nil // already exists
	}

	from := weekStart.Format("2006-01-02")
	to := weekEnd.Format("2006-01-02")

	todos, _ := store.LoadTodosInRange(from, to)
	meetings, _ := store.LoadMeetingsInRange(from, to)
	prs, _ := store.LoadPRsInRange(from, to)
	notes, _ := store.LoadNotesInRange(from, to)

	_, week := weekStart.ISOWeek()
	title := fmt.Sprintf("Week %d: %s – %s %d",
		week,
		weekStart.Format("02 Jan"),
		weekEnd.Format("02 Jan"),
		weekStart.Year(),
	)

	content := buildPeriodSummary(title, todos, meetings, prs, notes)
	return path, writeSummary(path, content)
}

// WriteMonthlySummaryIfNeeded generates the most recently completed calendar
// month's summary if the file does not yet exist.
func (w *Writer) WriteMonthlySummaryIfNeeded(now time.Time, store *storage.Store) (string, error) {
	// Always summarise the previous calendar month (last complete month).
	lastMonth := time.Date(now.Year(), now.Month()-1, 1, 0, 0, 0, 0, now.Location())
	path := w.monthlySummaryPath(lastMonth)
	if _, err := os.Stat(path); err == nil {
		return "", nil // already exists
	}

	firstDay := lastMonth
	lastDay := firstDay.AddDate(0, 1, -1)
	from := firstDay.Format("2006-01-02")
	to := lastDay.Format("2006-01-02")

	todos, _ := store.LoadTodosInRange(from, to)
	meetings, _ := store.LoadMeetingsInRange(from, to)
	prs, _ := store.LoadPRsInRange(from, to)
	notes, _ := store.LoadNotesInRange(from, to)

	title := fmt.Sprintf("%s %d", lastMonth.Format("January"), lastMonth.Year())
	content := buildPeriodSummary(title, todos, meetings, prs, notes)
	return path, writeSummary(path, content)
}

// lastCompleteWeek returns the Monday and Sunday of the most recently completed
// Mon–Sun week. The current in-progress week is never returned.
func lastCompleteWeek(now time.Time) (monday, sunday time.Time) {
	// weekday: Mon=1 … Sun=0 in Go; normalise so Mon=1, Sun=7
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	// thisMonday is the Monday of the current (possibly in-progress) week
	thisMonday := now.AddDate(0, 0, -(wd - 1))
	monday = thisMonday.AddDate(0, 0, -7)
	sunday = thisMonday.AddDate(0, 0, -1)
	return
}

func writeSummary(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	return os.WriteFile(path, []byte(content), 0644)
}

// buildPeriodSummary generates the markdown for a weekly or monthly summary.
// Items are grouped by tag; PRs and untagged items appear in their own sections.
func buildPeriodSummary(
	title string,
	todos []storage.TodoItem,
	meetings []storage.Meeting,
	prs []storage.TrackedPR,
	notes map[string]string,
) string {
	var b strings.Builder

	// ---- Header ----
	fmt.Fprintf(&b, "# %s\n\n", title)

	done := 0
	for _, t := range todos {
		if t.Done {
			done++
		}
	}
	fmt.Fprintf(&b, "*%d/%d todos done · %d meetings · %d PRs*\n\n---\n\n",
		done, len(todos), len(meetings), len(prs))

	// ---- Per-tag sections ----
	tags := collectPeriodTags(todos, meetings)

	for _, tag := range tags {
		tagTodos := filterTodosByTag(todos, tag)
		tagMeetings := filterMeetingsByTag(meetings, tag)
		tagLines := noteLinesByTag(notes, tag)

		if len(tagTodos) == 0 && len(tagMeetings) == 0 && len(tagLines) == 0 {
			continue
		}

		fmt.Fprintf(&b, "## %s\n\n", tag)

		if len(tagTodos) > 0 {
			b.WriteString("### Todos\n\n")
			for _, t := range tagTodos {
				check := "- [ ]"
				if t.Done {
					check = "- [x]"
				}
				meta := ""
				if t.Done && t.DoneAt != nil {
					meta = fmt.Sprintf(" *(done %s)*", t.DoneAt.Format("Jan 02"))
				}
				if t.SourceMeetingTitle != "" {
					meta += fmt.Sprintf(" *(↩ %s)*", t.SourceMeetingTitle)
				}
				fmt.Fprintf(&b, "%s %s%s\n", check, boldMentions(t.Text), meta)
			}
			b.WriteString("\n")
		}

		if len(tagMeetings) > 0 {
			b.WriteString("### Meetings\n\n")
			for _, m := range tagMeetings {
				date, _ := time.Parse("2006-01-02", m.Date)
				attn := ""
				if len(m.Attendees) > 0 {
					attn = " *(" + strings.Join(m.Attendees, ", ") + ")*"
				}
				fmt.Fprintf(&b, "**%s — %s**%s\n\n", date.Format("Jan 02"), m.Title, attn)
				if len(m.Decisions) > 0 {
					b.WriteString("Decisions:\n")
					for _, d := range m.Decisions {
						fmt.Fprintf(&b, "- %s\n", d)
					}
					b.WriteString("\n")
				}
				if m.Summary != "" {
					b.WriteString(m.Summary)
					b.WriteString("\n")
				}
			}
		}

		if len(tagLines) > 0 {
			b.WriteString("### Notes\n\n")
			dates := make([]string, 0, len(tagLines))
			for d := range tagLines {
				dates = append(dates, d)
			}
			sort.Strings(dates)
			for _, d := range dates {
				date, _ := time.Parse("2006-01-02", d)
				for _, line := range tagLines[d] {
					fmt.Fprintf(&b, "> *%s:* %s\n", date.Format("Jan 02"), line)
				}
			}
			b.WriteString("\n")
		}

		b.WriteString("---\n\n")
	}

	// ---- PRs table ----
	if len(prs) > 0 {
		b.WriteString("## PRs\n\n")
		b.WriteString("| PR | Title | Repo | Status |\n")
		b.WriteString("|---|---|---|---|\n")
		for _, pr := range prs {
			status := pr.State
			if pr.DoneAt != nil {
				status = "done " + pr.DoneAt.Format("Jan 02")
			}
			fmt.Fprintf(&b, "| [#%d](%s) | %s | %s | %s |\n",
				pr.Number, pr.URL, pr.Title, pr.Repo, status)
		}
		b.WriteString("\n")
	}

	// ---- Untagged ----
	untaggedTodos := filterTodosByTag(todos, "")
	untaggedMeetings := filterMeetingsByTag(meetings, "")

	if len(untaggedTodos) > 0 || len(untaggedMeetings) > 0 {
		b.WriteString("## Untagged\n\n")

		if len(untaggedTodos) > 0 {
			b.WriteString("### Todos\n\n")
			for _, t := range untaggedTodos {
				check := "- [ ]"
				if t.Done {
					check = "- [x]"
				}
				fmt.Fprintf(&b, "%s %s\n", check, boldMentions(t.Text))
			}
			b.WriteString("\n")
		}

		if len(untaggedMeetings) > 0 {
			b.WriteString("### Meetings\n\n")
			for _, m := range untaggedMeetings {
				date, _ := time.Parse("2006-01-02", m.Date)
				fmt.Fprintf(&b, "**%s — %s**\n\n", date.Format("Jan 02"), m.Title)
				if len(m.Decisions) > 0 {
					b.WriteString("Decisions:\n")
					for _, d := range m.Decisions {
						fmt.Fprintf(&b, "- %s\n", d)
					}
					b.WriteString("\n")
				}
				if m.Summary != "" {
					b.WriteString(m.Summary)
					b.WriteString("\n")
				}
			}
		}
	}

	return b.String()
}

// collectPeriodTags returns sorted unique tags from todos and meetings.
func collectPeriodTags(todos []storage.TodoItem, meetings []storage.Meeting) []string {
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
	return tags
}

// filterTodosByTag returns todos that have the given tag.
// Pass tag="" to return todos with no tags at all.
func filterTodosByTag(todos []storage.TodoItem, tag string) []storage.TodoItem {
	var result []storage.TodoItem
	for _, t := range todos {
		if tag == "" {
			if len(t.Tags) == 0 {
				result = append(result, t)
			}
		} else {
			for _, tg := range t.Tags {
				if tg == tag {
					result = append(result, t)
					break
				}
			}
		}
	}
	return result
}

// filterMeetingsByTag returns meetings that have the given tag.
// Pass tag="" to return meetings with no tags at all.
func filterMeetingsByTag(meetings []storage.Meeting, tag string) []storage.Meeting {
	var result []storage.Meeting
	for _, m := range meetings {
		if tag == "" {
			if len(m.Tags) == 0 {
				result = append(result, m)
			}
		} else {
			for _, tg := range m.Tags {
				if tg == tag {
					result = append(result, m)
					break
				}
			}
		}
	}
	return result
}

// noteLinesByTag scans each day's note for lines containing the tag and returns
// a map of date → matching lines.
func noteLinesByTag(notes map[string]string, tag string) map[string][]string {
	result := map[string][]string{}
	tagLower := strings.ToLower(tag)
	for date, content := range notes {
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.Contains(strings.ToLower(line), tagLower) {
				result[date] = append(result[date], line)
			}
		}
	}
	return result
}
