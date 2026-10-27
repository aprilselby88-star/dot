package storage

import "testing"

func TestMentions(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"chat with @omar about scope", true},
		{"@Omar: owns the rollout", true},
		{"ask @omar.", true},
		{"ping @omarh", false},
		{"email omar", false},
		{"@al and @omar", true},
	}
	for _, c := range cases {
		if got := mentions(c.text, "omar"); got != c.want {
			t.Errorf("mentions(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestOneOnOnesRoundTrip(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := Person{ID: "p1", Name: "Omar Haddad", Kind: "manager", Handle: DefaultHandle("Omar Haddad")}
	if p.Handle != "omar" {
		t.Fatalf("DefaultHandle = %q", p.Handle)
	}
	if err := s.SavePerson(p); err != nil {
		t.Fatal(err)
	}
	for _, o := range []OneOnOne{
		{ID: "a", PersonID: "p1", Date: "2026-10-14", Notes: "talked"},
		{ID: "b", PersonID: "p1", Date: "2026-10-21", Sections: []OneOnOneSection{{Title: "Wins", Body: "shipped"}}},
		{ID: "c", PersonID: "other", Date: "2026-10-21"},
	} {
		if err := s.SaveOneOnOne(o); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.LoadOneOnOnes("p1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("LoadOneOnOnes = %+v, want b then a", got)
	}
	if got[0].Section("Wins") != "shipped" || got[0].Held() || !got[1].Held() {
		t.Fatalf("unexpected section/held state: %+v", got)
	}

	if err := s.DeletePerson("p1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.LoadOneOnOnes("p1"); len(got) != 0 {
		t.Fatalf("sessions survived DeletePerson: %+v", got)
	}
	if got, _ := s.LoadOneOnOnesOn("2026-10-21"); len(got) != 1 || got[0].ID != "c" {
		t.Fatalf("other person's session affected: %+v", got)
	}
}

func TestMentionsInRange(t *testing.T) {
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SaveNote("2026-10-20", "nothing here\n@omar wants the catalog doc")
	_ = s.SaveNote("2026-10-01", "@omar too old")
	_ = s.SaveMeeting(Meeting{ID: "m", Date: "2026-10-21", Title: "Planning", RawNotes: "check with @omar on scope"})
	_ = s.SaveTodos([]TodoItem{{ID: "t", Date: "2026-10-22", Text: "send @omar the plan"}})

	got, err := s.MentionsInRange("omar", "2026-10-14", "2026-10-24")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d mentions, want 3: %+v", len(got), got)
	}
	if got[0].Text != "@omar wants the catalog doc" || got[1].Source != "Planning" || got[2].Source != "Todo" {
		t.Fatalf("unexpected mentions: %+v", got)
	}
}
