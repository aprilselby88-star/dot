# Testing & Tuning Guide

## Quick start

```bash
# From the organizer/ directory
go mod tidy          # fetch dependencies
go build ./...       # verify it compiles
go run .             # run the app
```

First run creates `~/.config/dot/config.yaml` with defaults.  
Data is stored in `~/.local/share/dot/`.

---

## 1 — GitHub setup

### Personal github.com

```bash
export GITHUB_TOKEN=ghp_your_token_here
go run .
```

The token needs `notifications` and `repo` scopes.  
Create one at: GitHub → Settings → Developer settings → Personal access tokens → Tokens (classic).

### GitHub Enterprise

In `~/.config/dot/config.yaml`:

```yaml
github:
  token: "ghp_your_enterprise_token"
  base_url: "https://github.mycompany.com"   # no trailing slash
  priority_repos:
    - myorg/my-repo
```

Or use the env var:
```bash
export ORGANIZER_GITHUB_TOKEN=ghp_your_enterprise_token
```

### Test without a real token (offline mode)

The app runs without a token — the GitHub tab shows a helpful message and the watch zone still works (you can manually add PRs to watch). All other tabs are fully functional offline.

---

## 2 — Obsidian path template

The `notes_template` config value is a [Go time.Format](https://pkg.go.dev/time#Layout) string.

| Code | Meaning | Example |
|------|---------|---------|
| `06` | 2-digit year | `26` |
| `2006` | 4-digit year | `2026` |
| `01` | month as number | `05` |
| `1` | month without leading zero | `5` |
| `January` | full month name | `May` |
| `Jan` | short month name | `May` |
| `02` | day with leading zero | `02` |
| `2` | day without leading zero | `2` |

**Your current format** `DailyNotes/06/01 January/02-01-06` produces:
```
DailyNotes/26/05 May/02-05-26.md
```

To match a different structure, just rearrange the codes:
```yaml
# DailyNotes/2026/2026-05-02.md
notes_template: "DailyNotes/2006/2006-01-02"

# DailyNotes/May 2026/02 May 2026.md
notes_template: "DailyNotes/January 2006/02 January 2006"
```

### Test the path without writing

In the Summary tab (`5`), the resolved path is shown next to the write button before you press it. No file is created until you press `w` or `ctrl+s`.

---

## 3 — Data files

All data lives in `~/.local/share/dot/` as plain JSON. You can edit them directly for testing.

| File | Contents |
|------|----------|
| `todos.json` | All todos across all days |
| `meetings.json` | All meetings |
| `tracked_prs.json` | Watch zone PRs |
| `daily_notes.json` | Free-form daily notes keyed by date |

### Seed test data

```bash
DATA=~/.local/share/organizer

# Add a sample todo for today
cat > $DATA/todos.json << 'EOF'
{
  "items": [
    {
      "id": "1",
      "text": "Review PR #42",
      "done": false,
      "date": "2026-05-02",
      "tags": ["#work"]
    },
    {
      "id": "2",
      "text": "Write tests for auth module",
      "done": true,
      "date": "2026-05-02",
      "tags": ["#alpha"]
    }
  ]
}
EOF

# Add a sample meeting
cat > $DATA/meetings.json << 'EOF'
{
  "meetings": [
    {
      "id": "1",
      "date": "2026-05-02",
      "title": "Team Standup",
      "attendees": ["Alice", "Bob", "Carol"],
      "raw_notes": "Action: Bob to fix login bug by EOD\nDecision: Postpone v2 release by one week\nDiscussed sprint velocity",
      "summary": "",
      "tags": ["#team"]
    }
  ]
}
EOF
```

After seeding, restart the app — it will show the data immediately.

### Test carryover

Set a todo's date to yesterday:
```bash
# Edit todos.json and change date from today to yesterday (2026-05-01)
# Then restart the app — the Todos tab will carry it forward
```

---

## 4 — Keyboard shortcuts

| Key | Action | Context |
|-----|--------|---------|
| `1–5` | Switch tabs | When not typing |
| `q` | Quit | When not typing |
| `ctrl+c` | Quit | Always |
| `j` / `k` | Move cursor | List views |
| `n` | New item | List mode |
| `d` | Delete item | List mode |
| `Enter` | Select / toggle | Varies |
| `Space` | Toggle todo done | Todos list |
| `e` | Edit todo text | Todos list |
| `w` | Add to watch zone | GitHub notifications |
| `r` | Mark notification read | GitHub notifications |
| `R` | Refresh | GitHub, Todos |
| `o` / `Enter` | Open in browser | GitHub items |
| `Tab` | Switch pane | GitHub tab |
| `ctrl+s` | Save / write | Notes, Meetings form, Summary |
| `Esc` | Cancel / back | Forms |
| `w` / `ctrl+s` | Write to Obsidian | Summary tab |

### Custom hotkeys

Edit `~/.config/dot/config.yaml` and change any key under `hotkeys:`.  
Keys are matched against bubbletea's `msg.String()` — common values:

- Single keys: `"a"`, `"1"`, `" "` (space)
- Modifiers: `"ctrl+s"`, `"ctrl+r"`, `"alt+n"`
- Special: `"enter"`, `"esc"`, `"tab"`, `"up"`, `"down"`, `"f1"`–`"f12"`

---

## 5 — Configuring for work (enterprise)

When switching between personal and work contexts:

**Option A — environment variables (recommended):**
```bash
# In your work shell profile:
export ORGANIZER_GITHUB_TOKEN="ghp_work_token_here"
```

**Option B — separate config file with shell alias:**
```bash
# ~/work-organizer.yaml
alias work-dot='ORGANIZER_CONFIG=~/work-dot.yaml dot'
```

Then extend `config.Load()` to check `ORGANIZER_CONFIG` env var for the path.

---

## 6 — Extending with Jira / Slack

The architecture is designed for extension. To add a new integration:

1. **Add config** — new struct in `config/config.go`
2. **Add a client package** — e.g. `jiraclient/client.go`
3. **Add a new tab** — copy `tui/tab_github.go` as a starting point, define your message types and model
4. **Wire it in** — add the tab to `tui/app.go` (new `tabID` const, add to `App` struct, route in `Update`, render in `View` and `tabBar`)
5. **Include in Obsidian output** — add a section in `obsidian/writer.go`'s `Build()` method

---

## 7 — Building a release binary

```bash
go build -o dot .

# Move to PATH
mv dot /usr/local/bin/dot

# Now just run:
dot
```

For cross-compilation to your work machine:
```bash
GOOS=darwin GOARCH=arm64 go build -o organizer-arm64 .   # M-series Mac
GOOS=darwin GOARCH=amd64 go build -o organizer-amd64 .   # Intel Mac
GOOS=linux  GOARCH=amd64 go build -o organizer-linux  .  # Linux
```

---

## 8 — Troubleshooting

**App won't start:**
```bash
go run . 2>&1 | head -20   # see error output
cat ~/.config/dot/config.yaml   # check for YAML syntax errors
```

**GitHub shows "no token":**
```bash
echo $GITHUB_TOKEN   # should print your token
# If empty: export GITHUB_TOKEN=ghp_...
```

**Obsidian note writes to wrong folder:**  
Check Summary tab — it shows the resolved path before you write. Adjust `notes_template` in config.

**Data got corrupted:**
```bash
# JSON files are human-readable — fix by hand or delete to reset:
rm ~/.local/share/dot/todos.json   # loses all todos
```

**Todos not carrying over:**  
Carryover runs on first launch each day. If it doesn't trigger, check that the todo's `date` field in `todos.json` is set to a past date and `done` is `false`.
