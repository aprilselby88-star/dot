package config

import "testing"

func TestOneOnOneTemplateFallback(t *testing.T) {
	c := OneOnOneConfig{Templates: map[string][]OneOnOneSection{
		KindPeer: {{Title: "Custom"}},
	}}
	if got := c.Template(KindPeer); len(got) != 1 || got[0].Title != "Custom" {
		t.Errorf("override not used: %+v", got)
	}
	if got := c.Template(KindManager); len(got) != 5 || got[0].Title != "Wins" {
		t.Errorf("default manager template not used: %+v", got)
	}
	for _, k := range OneOnOneKinds {
		if len(c.Template(k)) == 0 {
			t.Errorf("no template for %s", k)
		}
	}
}
