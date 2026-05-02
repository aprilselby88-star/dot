package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/ghclient"
	"github.com/aprilselby/dot/storage"
)

// ---- messages ----

type ghNotificationsMsg struct {
	items []ghclient.Notification
	err   error
}

type ghPRDetailsMsg struct {
	pr  *storage.TrackedPR
	err error
}

type ghWatchedPRsLoadedMsg struct {
	prs []storage.TrackedPR
}

// ghMarkDoneMsg is returned after marking a notification as done.
// The item is already removed from the local list optimistically, so no re-fetch.
type ghMarkDoneMsg struct{ err error }

type ghWatchPRDoneMsg struct{ ghErr error }

// ---- model ----

type ghPane int

const (
	ghPaneNotifs ghPane = iota
	ghPaneWatchZone
)

type githubTab struct {
	cfg    *config.Config
	store  *storage.Store
	ghc    *ghclient.Client
	width  int
	height int

	pane   ghPane
	notifs []ghclient.Notification
	prs    []storage.TrackedPR
	cursor int

	loading bool
	lastErr string
	vp      viewport.Model
}

func newGithubTab(cfg *config.Config, store *storage.Store, ghc *ghclient.Client) githubTab {
	return githubTab{
		cfg:   cfg,
		store: store,
		ghc:   ghc,
		vp:    viewport.New(80, 20),
	}
}

func (t githubTab) Editing() bool { return false }

func (t githubTab) Init() tea.Cmd {
	return tea.Batch(t.loadWatchedPRs(), t.fetchNotifications())
}

func (t githubTab) UpdateSize(w, h int) (githubTab, tea.Cmd) {
	t.width, t.height = w, h
	t.vp.Width = w
	t.vp.Height = h - 4
	return t, nil
}

func (t githubTab) Update(msg tea.Msg) (githubTab, tea.Cmd) {
	switch msg := msg.(type) {

	case ghWatchedPRsLoadedMsg:
		t.prs = msg.prs

	case ghNotificationsMsg:
		t.loading = false
		if msg.err != nil {
			t.lastErr = msg.err.Error()
		} else {
			t.notifs = msg.items
			t.lastErr = ""
			t.cursor = 0
		}

	case ghPRDetailsMsg:
		if msg.err == nil && msg.pr != nil {
			_ = t.store.AddTrackedPR(*msg.pr)
			return t, t.loadWatchedPRs()
		}

	case ghWatchPRDoneMsg:
		statusText := "PR marked as done"
		isErr := false
		if msg.ghErr != nil {
			statusText = "Marked done locally; GitHub failed: " + msg.ghErr.Error()
			isErr = true
		}
		return t, tea.Batch(
			t.loadWatchedPRs(),
			func() tea.Msg { return statusMsg{text: statusText, isError: isErr} },
			func() tea.Msg { return ghSyncRequestMsg{} },
		)

	case ghMarkDoneMsg:
		if msg.err != nil {
			return t, func() tea.Msg { return statusMsg{text: "Mark done failed: " + msg.err.Error(), isError: true} }
		}
		return t, func() tea.Msg { return statusMsg{text: "Marked as done"} }

	case tea.KeyMsg:
		switch msg.String() {
		case "tab":
			if t.pane == ghPaneNotifs {
				t.pane = ghPaneWatchZone
			} else {
				t.pane = ghPaneNotifs
			}
			t.cursor = 0

		case "j", "down":
			t.cursorDown()
		case "k", "up":
			t.cursorUp()

		case "enter", t.cfg.Hotkeys.OpenBrowser:
			t.openCurrent()

		case t.cfg.Hotkeys.WatchPR:
			if t.pane == ghPaneNotifs && len(t.notifs) > 0 && t.cursor < len(t.notifs) {
				id := t.notifs[t.cursor].ID
				watchCmd := t.watchCurrentNotif() // capture before modifying list
				notifs := make([]ghclient.Notification, 0, len(t.notifs)-1)
				notifs = append(notifs, t.notifs[:t.cursor]...)
				notifs = append(notifs, t.notifs[t.cursor+1:]...)
				t.notifs = notifs
				if t.cursor >= len(t.notifs) && t.cursor > 0 {
					t.cursor--
				}
				cmds := []tea.Cmd{
					func() tea.Msg { return statusMsg{text: "Added to Watch Zone"} },
					func() tea.Msg { return ghSyncRequestMsg{} },
				}
				if watchCmd != nil {
					cmds = append(cmds, watchCmd)
				}
				if t.ghc != nil {
					cmds = append(cmds, t.markDoneFireAndForget(id))
				}
				return t, tea.Batch(cmds...)
			}

		case "d", t.cfg.Hotkeys.Delete:
			if t.pane == ghPaneWatchZone {
				return t, t.markCurrentPRDone()
			}
			if t.pane == ghPaneNotifs && t.ghc != nil && len(t.notifs) > 0 && t.cursor < len(t.notifs) {
				id := t.notifs[t.cursor].ID
				notifs := make([]ghclient.Notification, 0, len(t.notifs)-1)
				notifs = append(notifs, t.notifs[:t.cursor]...)
				notifs = append(notifs, t.notifs[t.cursor+1:]...)
				t.notifs = notifs
				if t.cursor >= len(t.notifs) && t.cursor > 0 {
					t.cursor--
				}
				return t, t.markDoneByID(id)
			}

		case t.cfg.Hotkeys.Refresh:
			t.loading = true
			return t, tea.Batch(t.fetchNotifications(), t.loadWatchedPRs())
		}

		// viewport scroll in watch zone
		if t.pane == ghPaneWatchZone {
			var cmd tea.Cmd
			t.vp, cmd = t.vp.Update(msg)
			return t, cmd
		}
	}
	return t, nil
}

func (t githubTab) View() string {
	var b strings.Builder

	// Inner pane switcher
	nStyle, wStyle := tabInactiveStyle, tabInactiveStyle
	if t.pane == ghPaneNotifs {
		nStyle = tabActiveStyle
	} else {
		wStyle = tabActiveStyle
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top,
		nStyle.Render("Tab Notifications"),
		wStyle.Render("Tab Watch Zone"),
	))
	b.WriteString("\n\n")

	if t.ghc == nil {
		b.WriteString(mutedStyle.Render("No GitHub token configured.\n"))
		b.WriteString(hintStyle.Render("Set GITHUB_TOKEN env var or github.token in ~/.config/dot/config.yaml\n"))
		if t.pane == ghPaneWatchZone {
			b.WriteString("\n")
			b.WriteString(t.renderWatchZone())
		}
		return b.String()
	}

	switch t.pane {
	case ghPaneNotifs:
		b.WriteString(t.renderNotifications())
	case ghPaneWatchZone:
		b.WriteString(t.renderWatchZone())
	}

	return b.String()
}

func (t *githubTab) renderNotifications() string {
	var b strings.Builder

	if t.loading {
		b.WriteString("\n  " + mutedStyle.Render("Fetching notifications…") + "\n")
		return b.String()
	}
	if t.lastErr != "" {
		b.WriteString("\n  " + errorStyle.Render("✗  "+t.lastErr) + "\n")
		b.WriteString("  " + hintStyle.Render(keyStyle.Render("R")+" to retry") + "\n")
		return b.String()
	}
	if len(t.notifs) == 0 {
		empty := "No notifications"
		if len(t.cfg.GitHub.PriorityRepos) > 0 {
			empty += " in priority repos"
		}
		b.WriteString("\n  " + mutedStyle.Render(empty) + "\n")
		b.WriteString("  " + hintStyle.Render(keyStyle.Render("R")+" to refresh") + "\n")
		return b.String()
	}

	b.WriteString("  " + mutedStyle.Render(fmt.Sprintf("%d notification(s)", len(t.notifs))) + "\n\n")

	maxRows := t.height - 6
	if maxRows < 1 {
		maxRows = 10
	}

	for i, n := range t.notifs {
		if i >= maxRows {
			b.WriteString("  " + mutedStyle.Render(fmt.Sprintf("… %d more", len(t.notifs)-maxRows)) + "\n")
			break
		}

		cursor := "  "
		if i == t.cursor {
			cursor = "▸ "
		}

		icon := iconForType(n.Type)
		reason := reasonLabel(n.Reason)
		reasonColored := reasonStyle(n.Reason).Render(reason)

		repoTitle := truncate(n.Repo+": "+n.Title, 56)
		line := fmt.Sprintf("%s%s %-56s  %-20s  %s",
			cursor, icon, repoTitle,
			reasonColored,
			mutedStyle.Render(n.UpdatedAt),
		)

		if i == t.cursor {
			b.WriteString(selectedStyle.Width(t.width).Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(buildHints(
		[]string{"j/k", "Enter", "w", "d", "R"},
		[]string{"navigate", "open", "watch PR", "done", "refresh"},
	))
	return b.String()
}

func (t *githubTab) renderWatchZone() string {
	var b strings.Builder
	b.WriteString("  " + sectionHeader.Render("Watch Zone") + "\n\n")

	if len(t.prs) == 0 {
		b.WriteString("  " + mutedStyle.Render("No PRs being watched.") + "\n")
		b.WriteString("  " + hintStyle.Render("Press "+keyStyle.Render("w")+" on a notification to add one.") + "\n")
		return b.String()
	}

	hdr := fmt.Sprintf("  %-30s  %-6s  %-32s  %-14s  %s",
		"Repo", "PR", "Title", "Author", "State")
	b.WriteString(mutedStyle.Render(hdr) + "\n")
	b.WriteString(subtleStyle.Render("  "+strings.Repeat("─", min(t.width-4, 96))) + "\n")

	for i, pr := range t.prs {
		cursor := "  "
		if i == t.cursor && t.pane == ghPaneWatchZone {
			cursor = "▸ "
		}

		stateStr := prStateStyle(pr.State).Render(pr.State)
		base := fmt.Sprintf("%s%-30s  #%-5d  %-32s  %-14s",
			cursor,
			truncate(pr.Repo, 30),
			pr.Number,
			truncate(pr.Title, 32),
			truncate(pr.Author, 14),
		)

		if i == t.cursor && t.pane == ghPaneWatchZone {
			b.WriteString(selectedStyle.Width(t.width).Render(base+"  "+pr.State) + "\n")
		} else {
			b.WriteString(base + "  " + stateStr + "\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(buildHints(
		[]string{"j/k", "Enter", "d", "R"},
		[]string{"navigate", "open", "done", "refresh"},
	))
	return b.String()
}

// ---- commands ----

func (t githubTab) fetchNotifications() tea.Cmd {
	return func() tea.Msg {
		if t.ghc == nil {
			return ghNotificationsMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		items, err := t.ghc.FetchNotifications(ctx)
		return ghNotificationsMsg{items: items, err: err}
	}
}

func (t githubTab) loadWatchedPRs() tea.Cmd {
	return func() tea.Msg {
		all, _ := t.store.LoadTrackedPRs()
		var active []storage.TrackedPR
		for _, pr := range all {
			if pr.DoneAt == nil {
				active = append(active, pr)
			}
		}
		return ghWatchedPRsLoadedMsg{prs: active}
	}
}

func (t githubTab) watchCurrentNotif() tea.Cmd {
	if len(t.notifs) == 0 || t.cursor >= len(t.notifs) {
		return nil
	}
	n := t.notifs[t.cursor]
	if n.Type != "PullRequest" {
		return func() tea.Msg {
			return statusMsg{text: "Only pull requests can be added to the watch zone", isError: true}
		}
	}

	// Parse repo and number from URL: https://github.com/owner/repo/pull/123
	repo, number := parseGHPRURL(n.HTMLURL)
	if repo == "" {
		// Fallback: just store what we know from the notification
		pr := storage.TrackedPR{
			Repo:           n.Repo,
			Number:         number,
			Title:          n.Title,
			URL:            n.HTMLURL,
			State:          "open",
			AddedAt:        time.Now().Format("2006-01-02"),
			NotificationID: n.ID,
		}
		return func() tea.Msg {
			_ = t.store.AddTrackedPR(pr)
			prs, _ := t.store.LoadTrackedPRs()
			return ghWatchedPRsLoadedMsg{prs: prs}
		}
	}

	if t.ghc != nil {
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			pr, err := t.ghc.FetchPRDetails(ctx, repo, number)
			if err != nil {
				fallback := &storage.TrackedPR{
					Repo:           n.Repo,
					Number:         number,
					Title:          n.Title,
					URL:            n.HTMLURL,
					State:          "open",
					AddedAt:        time.Now().Format("2006-01-02"),
					NotificationID: n.ID,
				}
				_ = t.store.AddTrackedPR(*fallback)
				prs, _ := t.store.LoadTrackedPRs()
				return ghWatchedPRsLoadedMsg{prs: prs}
			}
			pr.AddedAt = time.Now().Format("2006-01-02")
			pr.NotificationID = n.ID
			_ = t.store.AddTrackedPR(*pr)
			prs, _ := t.store.LoadTrackedPRs()
			return ghWatchedPRsLoadedMsg{prs: prs}
		}
	}

	pr := storage.TrackedPR{
		Repo:           repo,
		Number:         number,
		Title:          n.Title,
		URL:            n.HTMLURL,
		State:          "open",
		AddedAt:        time.Now().Format("2006-01-02"),
		NotificationID: n.ID,
	}
	return func() tea.Msg {
		_ = t.store.AddTrackedPR(pr)
		prs, _ := t.store.LoadTrackedPRs()
		return ghWatchedPRsLoadedMsg{prs: prs}
	}
}


func (t githubTab) markCurrentPRDone() tea.Cmd {
	if len(t.prs) == 0 || t.cursor >= len(t.prs) {
		return nil
	}
	pr := t.prs[t.cursor]
	key := pr.Key()
	notifID := pr.NotificationID
	ghc := t.ghc
	store := t.store
	return func() tea.Msg {
		_ = store.MarkTrackedPRDone(key)
		if ghc != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var err error
			if notifID != "" {
				err = ghc.MarkNotificationDone(ctx, notifID)
			} else {
				err = ghc.MarkPRNotificationsDone(ctx, pr.Repo, pr.Number)
			}
			return ghWatchPRDoneMsg{ghErr: err}
		}
		return ghWatchPRDoneMsg{}
	}
}

// markDoneByID marks a notification as done via the API.
// The item is already removed from the local list optimistically, so no re-fetch needed.
func (t githubTab) markDoneByID(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return ghMarkDoneMsg{err: t.ghc.MarkNotificationDone(ctx, id)}
	}
}

// markDoneFireAndForget marks a notification as done in the background with no UI update.
// Used when watching a PR — the notification is already removed from the local list.
func (t githubTab) markDoneFireAndForget(id string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = t.ghc.MarkNotificationDone(ctx, id)
		return nil
	}
}

// ---- helpers ----

func (t *githubTab) cursorDown() {
	list := t.listLen()
	if t.cursor < list-1 {
		t.cursor++
	}
}

func (t *githubTab) cursorUp() {
	if t.cursor > 0 {
		t.cursor--
	}
}

func (t *githubTab) listLen() int {
	if t.pane == ghPaneNotifs {
		return len(t.notifs)
	}
	return len(t.prs)
}

func (t *githubTab) openCurrent() {
	if t.pane == ghPaneNotifs && t.cursor < len(t.notifs) {
		openURL(t.notifs[t.cursor].HTMLURL)
	} else if t.pane == ghPaneWatchZone && t.cursor < len(t.prs) {
		openURL(t.prs[t.cursor].URL)
	}
}

func iconForType(t string) string {
	style := mutedStyle
	switch t {
	case "PullRequest":
		return lipgloss.NewStyle().Foreground(colorIris).Render("⎇ ")
	case "Issue":
		return lipgloss.NewStyle().Foreground(colorFoam).Render("◉ ")
	case "Release":
		return lipgloss.NewStyle().Foreground(colorGold).Render("⬆ ")
	default:
		return style.Render("· ")
	}
}

func reasonLabel(r string) string {
	switch r {
	case "review_requested":
		return "review requested"
	case "mention":
		return "mentioned"
	case "assign":
		return "assigned"
	case "author":
		return "author"
	case "comment":
		return "commented"
	case "subscribed":
		return "watching"
	default:
		return r
	}
}

func reasonStyle(r string) lipgloss.Style {
	switch r {
	case "review_requested", "assign":
		return lipgloss.NewStyle().Foreground(colorGold)
	case "mention", "author":
		return lipgloss.NewStyle().Foreground(colorFoam)
	default:
		return mutedStyle
	}
}

// parseGHPRURL extracts "owner/repo" and PR number from a GitHub PR URL.
func parseGHPRURL(url string) (string, int) {
	// https://github.com/owner/repo/pull/123
	parts := strings.Split(url, "/")
	if len(parts) < 7 {
		return "", 0
	}
	// parts: ["https:", "", "github.com", "owner", "repo", "pull", "123"]
	owner := parts[len(parts)-4]
	repo := parts[len(parts)-3]
	var num int
	fmt.Sscanf(parts[len(parts)-1], "%d", &num)
	return owner + "/" + repo, num
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func buildHints(keys, descs []string) string {
	var parts []string
	for i, k := range keys {
		if i < len(descs) {
			parts = append(parts, keyStyle.Render(k)+" "+descs[i])
		}
	}
	return hintStyle.Render(strings.Join(parts, "  ")) + "\n"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
