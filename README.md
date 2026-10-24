# dot

A terminal-based work organizer built with Go and [bubbletea](https://github.com/charmbracelet/bubbletea).

Tabs: meeting notes, todos, daily notes, Jira, 1:1 prep, plus an optional GitHub notifications tab (off by default). Syncs to an Obsidian daily note automatically.

## Quick start

**Prerequisites:** Go 1.22+

```bash
git clone <your-repo>
cd dot
go mod tidy
```

### Run without installing

```bash
export GITHUB_TOKEN=ghp_your_token_here
make run
```

### Install the binary

```bash
make install
```

Places the `dot` binary in `$GOPATH/bin` (typically `~/go/bin`). Make sure that's on your `$PATH`:

```bash
echo 'export PATH="$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
source ~/.zshrc
dot
```

### Build a local binary

```bash
make build
./dot
```

---

## Configuration

On first run, `~/.config/dot/config.yaml` is created with defaults. A fully annotated example is at [config/example.yaml](config/example.yaml).

### Minimal config for github.com

```yaml
github:
  token: ""          # or set GITHUB_TOKEN env var
  priority_repos:
    - myorg/backend
    - myorg/frontend

obsidian:
  vault_path: "~/Documents/Obsidian Vault"
  notes_template: "Daily Notes/2006/01 January/02-01-06"

storage:
  dir: "~/Documents/Obsidian Vault/.dot"
```

### GitHub Enterprise

```yaml
github:
  token: ""
  base_url: "https://github.mycompany.com"
  priority_repos:
    - myorg/backend
```

### Jira

Ticket creation shells out to the Atlassian CLI (`acli`), so `acli` must be on your `$PATH`. Authentication uses `acli`'s own login — either run `acli jira auth login` once yourself, or set `site`/`email`/`token` below and `dot` will log `acli` in for you when a token is present.

```yaml
jira:
  site:  "mysite.atlassian.net"   # your Atlassian site, no https://
  email: "you@company.com"        # acli login email

  # API token — or leave blank and set JIRA_API_TOKEN (or DOT_JIRA_TOKEN) env var.
  # If blank, dot relies on an existing `acli jira auth login` session.
  token: ""

  project_key: "PROJ"
  issue_type:  "Task"             # case-sensitive: Task, Story, Bug, Epic
  assignee:    "you@company.com"  # email or accountId; "@me" also works
  reporter:    ""                 # optional

  # Prefilled as the parentIssueId when you start a new ticket (still editable)
  default_parent: ""

  # Optional labels applied to every ticket
  labels: []

  # Custom field the Acceptance Criteria text is written into (blank to skip AC)
  acceptance_criteria_field: ""   # e.g. customfield_10050

  # Reporting custom fields, written into acli's additionalAttributes verbatim.
  # Value shape must match the Jira field type — a plain string/number, or an
  # option object like { value: "..." }:
  custom_fields: {}
  #   customfield_10001: { value: "Team Rocket" }
  #   customfield_10002: "some string"
  #   customfield_10003: 50
```

The token can also be supplied via the `JIRA_API_TOKEN` or `DOT_JIRA_TOKEN` environment variable instead of the config file.

### Obsidian path template

`notes_template` is a Go [time.Format](https://pkg.go.dev/time#Layout) string relative to `vault_path`. Year and month folders are created automatically.

| Code | Output |
|------|--------|
| `2006` | 4-digit year (`2026`) |
| `06` | 2-digit year (`26`) |
| `01` | month number (`05`) |
| `January` | month name (`May`) |
| `02` | day (`02`) |

The default `"Daily Notes/2006/01 January/02-01-06"` produces `Daily Notes/2026/05 May/02-05-26.md`.

---

## Tabs

### GitHub (`1`)

Disabled by default. Turn it on in your config:

```yaml
features:
  github: true
```

Two panes, toggled with `Tab`:

- **Notifications** — all active GitHub notifications. Press `w` to add a PR to the watch zone, `d` to mark as done (archives in GitHub), `Enter`/`o` to open in browser.
- **Watch Zone** — PRs you're tracking. Press `d` to mark done: strikes through in the Obsidian daily note with a completion date, archives the GitHub notification.

Supports github.com and GitHub Enterprise.

### Meetings (`2`)

Step-by-step form: title → attendees → tags → raw notes.

- Lines prefixed with `Action:`, `Todo:`, `AP:`, `Follow up:`, or `- [ ]` in the raw notes are automatically extracted as todos, linked back to the meeting.
- Tag people with `@name` in your notes — they appear highlighted in the TUI and grouped in a Team Actions section in the Obsidian note.
- `ctrl+t` jumps to title, `ctrl+w` jumps to attendees, `ctrl+b` steps back, `ctrl+s` saves.

### Todos (`3`)

Daily todo list with tag support (`#tag`). Incomplete todos carry forward automatically on first launch of a new day. Action items created from meetings appear here with a link back to the source meeting.

- `@mention` someone in a todo to assign it — grouped under Team Actions in Obsidian.
- Tab key completes tags from your existing tag set.

### Notes (`4`)

Free-form daily notes textarea. Saved per day, included in your Obsidian daily note.

### Jira (`5`)

Create Jira tickets without leaving the terminal. Press `n` to start the ticket wizard (parent → summary → description → acceptance criteria), `ctrl+n` to advance, `ctrl+b` to step back, `ctrl+s` to submit. Ticket creation shells out to `acli`; after a ticket is created, press `o`/`Enter` to open it in the browser or `J` to start another. Requires `acli` on your `$PATH` and a configured `jira:` block — see [Configuration](#jira).

### 1:1s (`6`)

Prep sheets for recurring 1:1s, organised by person. Add someone with `n` and pick their type: **my manager**, **skip level**, **peer**, or **works for me**. Each type has its own prep sheet template.

Opening a person shows:

- **Since last 1:1** — every line in your daily notes, meeting notes, and todos that `@mentions` them since your last held 1:1 (or the last 30 days). Their handle is the lowercased first name, e.g. `@omar`.
- **Sessions** — their 1:1 history, newest first.

Press `n` to start a prep sheet (or continue one you haven't held yet). `Tab` / `Shift+Tab` move between sections; the last section is **Notes**, for during the meeting. Where it helps, a section shows what you wrote last time above the box. For example, your manager prep's **Wins** shows last session's **Next priorities**. `Ctrl+O` swaps that for the `@mentions` list. `Esc` saves and closes; it autosaves every 2 minutes too.

- `Action:` / `- [ ]` lines in any section become todos, linked back to the 1:1.
- A session is dated the day you first add Notes, so prepping the day before still files it under the day it happened.
- Each person gets a running Obsidian file at `<vault>/1-1s/<Name>.md`, and your daily note links to any 1:1s that day.

Override any template (or the folder) in config:

```yaml
one_on_ones:
  dir: "1-1s"
  templates:
    peer:                              # manager | skip | peer | report
      - title: "Shared priorities"
        hint: "What are you and {name} both trying to land?"
      - title: "Asks / offers"
        hint: "What do you need from {name}?"
        context_from: "Asks / offers"  # show this section from last time
```

---

## Key bindings

| Key | Action | Context |
|-----|--------|---------|
| `1` – `6` | Switch tabs | When not typing |
| `q` / `ctrl+c` | Quit | — |
| `j` / `k` | Move cursor | List views |
| `n` | New item | List mode |
| `d` | Mark done / done | GitHub, Watch Zone, Todos |
| `Enter` / `o` | Open in browser | GitHub |
| `w` | Add PR to watch zone | GitHub notifications |
| `Tab` | Toggle Notifications / Watch Zone | GitHub tab |
| `R` | Refresh | GitHub |
| `Space` / `Enter` | Toggle todo done | Todos list |
| `e` | Edit todo text | Todos list |
| `ctrl+s` | Save | Notes, Meetings |
| `ctrl+t` | Jump to title | Meetings form |
| `ctrl+w` | Jump to attendees | Meetings form |
| `ctrl+b` | Step back | Meetings form |
| `Tab` / `Shift+Tab` | Next / previous section | 1:1 prep sheet |
| `ctrl+o` | Toggle last time / @mentions | 1:1 prep sheet |
| `e` | Edit name / type | 1:1 person view |
| `Esc` | Cancel / back | Forms |

All keys are configurable in `~/.config/dot/config.yaml` under `hotkeys:`.

---

## Data storage

Plain JSON files stored inside your Obsidian vault at `<vault_path>/.dot/`:

| File | Contents |
|------|----------|
| `todos.json` | All todos across all days |
| `meetings.json` | All meetings |
| `tracked_prs.json` | Watch zone PRs (including done history) |
| `daily_notes.json` | Free-form notes keyed by date |
| `one_on_ones.json` | 1:1 people and sessions |

Keeping data inside the vault means your Obsidian GitHub backup plugin covers everything — notes and structured data together.

---

## Obsidian daily note

The daily note is regenerated on every save and includes:

- **Overview** — meeting and todo counts for today / this week / this month
- **Todos** — today's tasks with done times, carried-from dates, source meetings
- **Team Actions** — todos with `@mentions`, grouped by person
- **Meetings** — summaries with attendees, tags, raw notes in a collapsible block
- **1:1s** — links to the running file for anyone you had a 1:1 with today
- **PRs to Watch** — open PRs in a table; done PRs struck through with completion date
- **Notes** — free-form daily notes

---

## Development

```bash
make vet     # go vet all packages
make build   # build ./dot binary
make install # install to $GOPATH/bin
make clean   # remove local binary
```
