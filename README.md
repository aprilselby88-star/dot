# dot

A terminal-based work organizer built with Go and [bubbletea](https://github.com/charmbracelet/bubbletea).

Four tabs: GitHub notifications, meeting notes, todos, daily notes. Syncs to an Obsidian daily note automatically.

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

---

## Key bindings

| Key | Action | Context |
|-----|--------|---------|
| `1` – `4` | Switch tabs | When not typing |
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

Keeping data inside the vault means your Obsidian GitHub backup plugin covers everything — notes and structured data together.

---

## Obsidian daily note

The daily note is regenerated on every save and includes:

- **Overview** — meeting and todo counts for today / this week / this month
- **Todos** — today's tasks with done times, carried-from dates, source meetings
- **Team Actions** — todos with `@mentions`, grouped by person
- **Meetings** — summaries with attendees, tags, raw notes in a collapsible block
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
