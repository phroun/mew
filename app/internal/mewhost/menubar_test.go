package mewhost

import (
	"testing"

	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The menu bar's canonical order is app, file, edit, select, format, view,
// custom..., window, help — driven by each menu's WELL-KNOWN tag, not by its
// title. That is what lets mew call its file menu "File Buffer" and its view
// menu "Viewport" and still merge correctly, and what puts the untagged
// Input / Search / History between view and window in their declared order.
//
// Titles are exactly as mew declares them: these menus name the WordStar
// command groups they wean users onto, so nothing here invents an accelerator
// or renames toward a platform convention.
func TestMenuBarWellKnownOrder(t *testing.T) {
	for _, c := range []struct {
		multiWindow bool
		want        []string // wellknown id, "" for an untagged (custom) menu
		wantTitles  []string
	}{
		{false,
			[]string{"file", "edit", "format", "view", "", "", "", "help"},
			[]string{"File Buffer", "Edit Block", "Format", "Viewport", "Input", "Search", "History", "Help"}},
		{true,
			[]string{"app", "file", "edit", "format", "view", "", "", "", "window", "help"},
			[]string{"mew", "File Buffer", "Edit Block", "Format", "Viewport", "Input", "Search", "History", "Window", "Help"}},
	} {
		menus := buildMenus(trinkets.NewDesktop(), app.New(nil), c.multiWindow)
		if len(menus) != len(c.want) {
			t.Fatalf("multiWindow=%v: %d menus, want %d", c.multiWindow, len(menus), len(c.want))
		}
		for i, m := range menus {
			if got := m.WellKnownID(); got != c.want[i] {
				t.Errorf("multiWindow=%v menu %d: wellknown %q, want %q", c.multiWindow, i, got, c.want[i])
			}
			if got := m.Title(); got != c.wantTitles[i] {
				t.Errorf("multiWindow=%v menu %d: title %q, want %q", c.multiWindow, i, got, c.wantTitles[i])
			}
		}
	}
}

// Every placeholder item carries an action the host registered, so the bar is
// exercised end to end rather than being inert scenery, and every one of them
// is listed for live shortcut-text resolution.
func TestPlaceholderItemsAreWired(t *testing.T) {
	application := app.New(nil)
	menus := buildMenus(trinkets.NewDesktop(), application, true)
	commands := application.Commands()

	seen := map[string]bool{}
	for _, m := range menus {
		for _, it := range m.Items() {
			id := it.ID()
			if id == "" || it.Text == "" {
				continue // separators carry an auto-generated id and no text
			}
			seen[id] = true
			if !commands.Has(id) {
				t.Errorf("menu item %q (%q) has no registered command", id, it.Text)
			}
		}
	}
	for id := range placeholderShortcuts {
		if !seen[id] {
			t.Errorf("placeholderShortcuts names %q, which no menu item carries", id)
		}
	}
}
