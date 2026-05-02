package ghclient

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

// Client wraps the go-github client with organizer-specific helpers.
type Client struct {
	gh  *github.Client
	cfg *config.GitHubConfig
}

// Notification is a simplified view of a GitHub notification.
type Notification struct {
	ID        string
	Repo      string
	Title     string
	Type      string // PullRequest, Issue, Commit, Release...
	Reason    string // review_requested, mention, assign...
	HTMLURL   string
	UpdatedAt string
	Unread    bool
}

func New(cfg *config.GitHubConfig) (*Client, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("no GitHub token — set GITHUB_TOKEN env var or github.token in config")
	}

	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.Token})
	httpClient := oauth2.NewClient(context.Background(), ts)

	var ghc *github.Client
	var err error

	if cfg.BaseURL != "" {
		apiURL := normalizeEnterpriseURL(cfg.BaseURL)
		ghc, err = github.NewEnterpriseClient(apiURL, apiURL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("enterprise client: %w", err)
		}
	} else {
		ghc = github.NewClient(httpClient)
	}

	return &Client{gh: ghc, cfg: cfg}, nil
}

// FetchNotifications returns all active notifications (read + unread, excluding done/muted),
// filtered to priority repos if configured.
func (c *Client) FetchNotifications(ctx context.Context) ([]Notification, error) {
	opts := &github.NotificationListOptions{
		All:         true,
		ListOptions: github.ListOptions{PerPage: 50},
	}

	raw, _, err := c.gh.Activity.ListNotifications(ctx, opts)
	if err != nil {
		return nil, err
	}

	prioritySet := make(map[string]bool)
	for _, r := range c.cfg.PriorityRepos {
		prioritySet[strings.ToLower(r)] = true
	}

	var result []Notification
	for _, n := range raw {
		repo := ""
		if n.Repository != nil && n.Repository.FullName != nil {
			repo = *n.Repository.FullName
		}
		if len(prioritySet) > 0 && !prioritySet[strings.ToLower(repo)] {
			continue
		}

		notif := Notification{Repo: repo, Unread: true}
		if n.ID != nil {
			notif.ID = *n.ID
		}
		if n.Subject != nil {
			if n.Subject.Title != nil {
				notif.Title = *n.Subject.Title
			}
			if n.Subject.Type != nil {
				notif.Type = *n.Subject.Type
			}
			if n.Subject.URL != nil {
				notif.HTMLURL = apiToHTMLURL(*n.Subject.URL, c.cfg.BaseURL)
			}
		}
		if n.Reason != nil {
			notif.Reason = *n.Reason
		}
		if n.UpdatedAt != nil {
			notif.UpdatedAt = n.UpdatedAt.Time.Format("Jan 02 15:04")
		}
		if n.Unread != nil {
			notif.Unread = *n.Unread
		}

		result = append(result, notif)
	}
	return result, nil
}

// FetchPRDetails fetches current state of a PR for the watch zone.
func (c *Client) FetchPRDetails(ctx context.Context, repo string, number int) (*storage.TrackedPR, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format %q", repo)
	}

	pr, _, err := c.gh.PullRequests.Get(ctx, parts[0], parts[1], number)
	if err != nil {
		return nil, err
	}

	tracked := &storage.TrackedPR{Repo: repo, Number: number}
	if pr.Title != nil {
		tracked.Title = *pr.Title
	}
	if pr.User != nil && pr.User.Login != nil {
		tracked.Author = *pr.User.Login
	}
	if pr.State != nil {
		tracked.State = *pr.State
	}
	if pr.Merged != nil && *pr.Merged {
		tracked.State = "merged"
	}
	if pr.HTMLURL != nil {
		tracked.URL = *pr.HTMLURL
	}
	return tracked, nil
}

// MarkNotificationDone marks a notification thread as done via DELETE, which mirrors
// GitHub's "Done" button — the thread is archived and no longer appears in any inbox view.
func (c *Client) MarkNotificationDone(ctx context.Context, id string) error {
	req, err := c.gh.NewRequest("DELETE", fmt.Sprintf("notifications/threads/%s", id), nil)
	if err != nil {
		return err
	}
	resp, err := c.gh.Do(ctx, req, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// MarkPRNotificationsDone finds all notifications for a specific PR by repo/number
// and marks them as done. Works even when we don't have a stored notification ID.
func (c *Client) MarkPRNotificationsDone(ctx context.Context, repo string, prNumber int) error {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return nil
	}
	opts := &github.NotificationListOptions{
		All:         true,
		ListOptions: github.ListOptions{PerPage: 50},
	}
	notifs, _, err := c.gh.Activity.ListRepositoryNotifications(ctx, parts[0], parts[1], opts)
	if err != nil {
		return err
	}
	prSuffix := fmt.Sprintf("/pull/%d", prNumber)
	for _, n := range notifs {
		if n.Subject == nil || n.Subject.URL == nil || n.ID == nil {
			continue
		}
		htmlURL := apiToHTMLURL(*n.Subject.URL, c.cfg.BaseURL)
		if strings.HasSuffix(htmlURL, prSuffix) {
			_ = c.MarkNotificationDone(ctx, *n.ID)
		}
	}
	return nil
}

// apiToHTMLURL converts a GitHub REST API URL to its browser-viewable equivalent.
func apiToHTMLURL(apiURL, enterpriseBaseURL string) string {
	if enterpriseBaseURL != "" {
		apiBase := strings.TrimRight(normalizeEnterpriseURL(enterpriseBaseURL), "/")
		webBase := strings.TrimRight(enterpriseBaseURL, "/")
		apiURL = strings.Replace(apiURL, apiBase, webBase, 1)
	} else {
		apiURL = strings.Replace(apiURL, "https://api.github.com/repos/", "https://github.com/", 1)
	}
	apiURL = strings.Replace(apiURL, "/pulls/", "/pull/", 1)
	return apiURL
}

func normalizeEnterpriseURL(base string) string {
	base = strings.TrimRight(base, "/")
	if !strings.HasSuffix(base, "/api/v3") {
		return base + "/api/v3/"
	}
	return base + "/"
}
