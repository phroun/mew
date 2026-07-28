package mewhost

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
)

// The menu bar's canonical order is app, file, edit, select, format, view,
// custom..., window, help — driven by each menu's WELL-KNOWN tag, not by its
// title. That is what lets mew call its file menu "File Buffer" and its view
// menu "Viewport" and still merge correctly, and what puts the untagged
// Input / Search / History anchored after the file and format SLOTS.
//
// This asserts what mew DECLARES; the desktop turns declarations into the bar's
// final order (see TestAnchoredBarMatchesMewsWeaningOrder in KittyTK, which
// runs this exact shape through the merge).
//
// Titles are exactly as mew declares them: these menus name the WordStar
// command groups they wean users onto, so nothing here invents an accelerator
// or renames toward a platform convention.
func TestMenuBarWellKnownOrder(t *testing.T) {
	for _, c := range []struct {
		multiWindow bool
		want        [][3]string // title, well-known role, anchor slot
	}{
		{false, [][3]string{
			{"File Buffer", "file", ""},
			{"Edit Block", "edit", ""},
			{"Format", "format", ""},
			{"Viewport", "view", ""},
			{"Input", "", "file"},
			{"Search", "", "file"},
			{"History", "", "format"},
			{"Help", "help", ""},
		}},
		{true, [][3]string{
			{"mew", "app", ""},
			{"File Buffer", "file", ""},
			{"Edit Block", "edit", ""},
			{"Format", "format", ""},
			{"Viewport", "view", ""},
			{"Input", "", "file"},
			{"Search", "", "file"},
			{"History", "", "format"},
			{"Window", "window", ""},
			{"Help", "help", ""},
		}},
	} {
		menus := buildMenus(trinkets.NewDesktop(), app.New(nil), c.multiWindow)
		if len(menus) != len(c.want) {
			t.Fatalf("multiWindow=%v: %d menus, want %d", c.multiWindow, len(menus), len(c.want))
		}
		for i, m := range menus {
			got := [3]string{m.Title(), m.WellKnownID(), m.Anchor()}
			if got != c.want[i] {
				t.Errorf("multiWindow=%v menu %d: %v, want %v", c.multiWindow, i, got, c.want[i])
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
	roles := map[string]bool{}
	for _, m := range menus {
		for _, it := range m.Items() {
			id := it.ID()
			if id == "" || it.Text == "" {
				continue // separators carry an auto-generated id and no text
			}
			// An item tagged with a well-known ROLE is deliberately actionless:
			// the desktop wires the standard behaviour onto it (handler, host
			// shortcut, enable/disable) when it merges the edit menu, so a mew
			// command here would fight the toolkit rather than help it.
			if role := it.WellKnownID(); role != "" {
				roles[role] = true
				if commands.Has(id) {
					t.Errorf("item %q carries role %q AND a mew command; the role supplies its behaviour", it.Text, role)
				}
				continue
			}
			seen[id] = true
			if !commands.Has(id) {
				t.Errorf("menu item %q (%q) has no registered command", id, it.Text)
			}
		}
	}
	// mew re-captions all four standard clipboard items rather than letting the
	// desktop prepend its own set under toolkit names.
	for _, want := range []string{"cut", "copy", "paste", "selectall"} {
		if !roles[want] {
			t.Errorf("no menu item claims the %q role", want)
		}
	}
	for id := range menuActions {
		if !seen[id] {
			t.Errorf("menuActions names %q, which no menu item carries", id)
		}
	}
}

// The option items REFLECT as well as set: a checkable shows the option's
// current state, and a "[?]" caption is filled in with the current value. Both
// are refreshed per menu-open against the live cascade, so the bar shows what
// the editor holds now rather than what it held when the bar was built.
//
// This asserts the table's shape and the caption substitution; the live read
// itself needs a running mew session (Editor.Option is empty before one).
func TestOptionItemsAreDeclaredForEveryReflectingItem(t *testing.T) {
	application := app.New(nil)
	menus := buildMenus(trinkets.NewDesktop(), application, true)

	seen := map[string]bool{}
	for _, m := range menus {
		for _, it := range m.Items() {
			id := it.ID()
			if id == "" {
				continue
			}
			seen[id] = true
			spec, reflects := optionItems[id]
			// Anything whose caption carries the "[?]" placeholder must be
			// declared as reflecting, or the placeholder would ship to screen.
			if strings.Contains(it.Text, "[?]") && !reflects {
				t.Errorf("item %q shows a %q placeholder but is not in optionItems", it.Text, "[?]")
			}
			if it.Checkable && !reflects && id != "mew.help.quickhelp" {
				t.Errorf("checkable item %q has nothing to sync its checkmark from", it.Text)
			}
			if reflects && spec.template != "" && !strings.Contains(spec.template, "[?]") {
				t.Errorf("optionItems[%q] template %q has no [?] to substitute", id, spec.template)
			}
		}
	}
	for id := range optionItems {
		if !seen[id] {
			t.Errorf("optionItems names %q, which no menu item carries", id)
		}
	}
}
