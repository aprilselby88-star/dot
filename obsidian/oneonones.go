package obsidian

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

// OneOnOneRef pairs a session with its person for the daily note.
type OneOnOneRef struct {
	Person  storage.Person
	Session storage.OneOnOne
}

// PersonLink returns the vault-relative wikilink target (no .md) for a person's 1:1 file.
func (w *Writer) PersonLink(p storage.Person) string {
	return filepath.ToSlash(filepath.Join(w.cfg.OneOnOne.Dir, safeFileName(p.Name)))
}

// PersonPath returns the absolute path of a person's running 1:1 file.
func (w *Writer) PersonPath(p storage.Person) string {
	return filepath.Join(config.ExpandPath(w.cfg.Obsidian.VaultPath), w.PersonLink(p)+".md")
}

// WritePerson regenerates a person's running 1:1 file from all of their sessions.
// Returns the path it was written to.
func (w *Writer) WritePerson(p storage.Person, sessions []storage.OneOnOne) (string, error) {
	path := w.PersonPath(p)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("creating directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(BuildPerson(p, sessions)), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// BuildPerson renders a person's 1:1 history, newest session first. Empty sections are skipped.
func BuildPerson(p storage.Person, sessions []storage.OneOnOne) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# 1:1s with %s\n\n", p.Name)
	meta := p.Kind
	if p.Handle != "" {
		meta += " · @" + p.Handle
	}
	fmt.Fprintf(&b, "*%s*\n\n", meta)

	if len(sessions) == 0 {
		b.WriteString("*No 1:1s yet.*\n")
		return b.String()
	}

	for i, o := range sessions {
		if i > 0 {
			b.WriteString("---\n\n")
		}
		fmt.Fprintf(&b, "## %s\n\n", o.Date)
		wrote := false
		for _, s := range o.Sections {
			if strings.TrimSpace(s.Body) == "" {
				continue
			}
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", s.Title, strings.TrimSpace(s.Body))
			wrote = true
		}
		if o.Held() {
			fmt.Fprintf(&b, "### Notes\n\n%s\n\n", strings.TrimSpace(o.Notes))
			wrote = true
		}
		if !wrote {
			b.WriteString("*Not prepped yet.*\n\n")
		}
	}
	return b.String()
}

// safeFileName strips characters that are invalid in file names or break wikilinks.
func safeFileName(name string) string {
	name = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|[]#^`, r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" {
		return "Unnamed"
	}
	return name
}
