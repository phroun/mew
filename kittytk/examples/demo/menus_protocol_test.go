package main

import "testing"

// Guard both menu scripts: they must execute and surface the items
// the handlers read state from.
func TestMenuScriptsBuild(t *testing.T) {
	menus, byID, reply := buildMenuBar(mainMenuScript())
	if len(menus) != 6 {
		t.Errorf("main menus = %d, want 6", len(menus))
	}
	for _, key := range []string{"announce", "speakitem"} {
		if _, ok := byID[reply.IDs[key]]; !ok {
			t.Errorf("missing surfaced item %q", key)
		}
	}
	// Alphabet menu carries its 26 generated letters plus one demo
	// separator (after "Letter C").
	if n := len(menus[4].Items()); n != 27 {
		t.Errorf("alphabet items = %d, want 27", n)
	}

	secondary, _, _ := buildMenuBar(secondaryMenuScript(3))
	if len(secondary) != 4 {
		t.Errorf("secondary menus = %d, want 4", len(secondary))
	}
	if secondary[0].Title() != "App 3" {
		t.Errorf("secondary app menu title = %q", secondary[0].Title())
	}
}
