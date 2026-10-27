package obsidian

import (
	"strings"
	"testing"

	"github.com/aprilselby/dot/config"
	"github.com/aprilselby/dot/storage"
)

func TestBuildPerson(t *testing.T) {
	p := storage.Person{Name: "Omar", Kind: "manager", Handle: "omar"}
	out := BuildPerson(p, []storage.OneOnOne{
		{Date: "2026-10-21", Sections: []storage.OneOnOneSection{{Title: "Wins", Body: "shipped"}, {Title: "Asks"}}},
		{Date: "2026-10-14", Notes: "talked about scope"},
	})
	for _, want := range []string{"# 1:1s with Omar", "## 2026-10-21", "### Wins\n\nshipped", "### Notes\n\ntalked about scope"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "### Asks") {
		t.Errorf("empty section rendered:\n%s", out)
	}
	if strings.Index(out, "2026-10-21") > strings.Index(out, "2026-10-14") {
		t.Errorf("sessions not newest first:\n%s", out)
	}
}

func TestPersonLink(t *testing.T) {
	w := New(&config.Config{OneOnOne: config.OneOnOneConfig{Dir: "1-1s"}})
	if got := w.PersonLink(storage.Person{Name: "Ana / Ops"}); got != "1-1s/Ana  Ops" {
		t.Errorf("PersonLink = %q", got)
	}
}
