package trinkets

import "testing"

// An untagged menu may anchor itself after a well-known SLOT, so an app can
// depart from the canonical layout without leaving the canonical vocabulary:
// it says "after the file menu", never "third". Menus sharing an anchor keep
// their declared order; unanchored ones keep the trailing custom block.
func TestBucketMenusAnchorsCustomMenus(t *testing.T) {
	mk := func(title, wellKnown, anchor string) *Menu {
		m := NewMenu(title)
		if wellKnown != "" {
			m.SetWellKnownID(wellKnown)
		}
		if anchor != "" {
			m.SetAnchor(anchor)
		}
		return m
	}
	b := bucketMenus([]*Menu{
		mk("File", MenuIDFile, ""),
		mk("Edit", MenuIDEdit, ""),
		mk("Format", MenuIDFormat, ""),
		mk("View", MenuIDView, ""),
		mk("Input", "", MenuIDFile),  // after file
		mk("Search", "", MenuIDFile), // after file, declared second
		mk("History", "", MenuIDFormat),
		mk("Plain", "", ""), // no anchor: trailing block
	})

	if got := len(b.after(MenuIDFile)); got != 2 {
		t.Fatalf("file anchor holds %d menus, want 2", got)
	}
	if b.after(MenuIDFile)[0].Title() != "Input" || b.after(MenuIDFile)[1].Title() != "Search" {
		t.Error("menus sharing an anchor must keep declared order")
	}
	if got := b.after(MenuIDFormat); len(got) != 1 || got[0].Title() != "History" {
		t.Errorf("format anchor = %v, want [History]", got)
	}
	if len(b.custom) != 1 || b.custom[0].Title() != "Plain" {
		t.Errorf("unanchored menus should keep the trailing block, got %v", b.custom)
	}
}

// The anchor names a SLOT, not a live menu: anchoring after a role the app
// never declared still places the menu at that point in the canonical
// sequence, so a layout does not shift when a neighbour is added or removed.
func TestAnchorResolvesAgainstSlotNotMenu(t *testing.T) {
	input := NewMenu("Input")
	input.SetAnchor(MenuIDFile)
	view := NewMenu("View")
	view.SetWellKnownID(MenuIDView)

	b := bucketMenus([]*Menu{view, input}) // no file menu at all

	var got []string
	(&Desktop{}).appendAppBody(func(m *Menu) { got = append(got, m.Title()) }, nil, b)

	// (The system synthesizes its own Edit menu whether or not the app
	// declared one, so assert Input's POSITION rather than the exact list.)
	idx := func(title string) int {
		for i, t := range got {
			if t == title {
				return i
			}
		}
		return -1
	}
	if idx("Input") != 0 {
		t.Errorf("order = %v: an anchor after the absent file slot should still "+
			"lead, ahead of edit and view", got)
	}
	if idx("Input") > idx("View") {
		t.Errorf("order = %v: Input must precede View", got)
	}
}

// A menu that carries a well-known tag has its place fixed by that role; an
// anchor on it is ignored rather than silently moving a standard menu.
func TestAnchorIgnoredOnTaggedMenu(t *testing.T) {
	edit := NewMenu("Edit")
	edit.SetWellKnownID(MenuIDEdit)
	edit.SetAnchor(MenuIDView) // would move it after view, if honoured

	b := bucketMenus([]*Menu{edit})
	if b.edit == nil {
		t.Fatal("a tagged menu must still bucket by its role")
	}
	if len(b.after(MenuIDView)) != 0 {
		t.Error("an anchor on a tagged menu must be ignored")
	}
}

// The motivating case, end to end: mew's bar is a weaning system for the
// WordStar key families, so it wants Input and Search beside File Buffer and
// History beside Format — an order that is pedagogical, not canonical. Three
// anchors express it, and every well-known menu stays exactly where its role
// puts it.
func TestAnchoredBarMatchesMewsWeaningOrder(t *testing.T) {
	mk := func(title, wellKnown, anchor string) *Menu {
		m := NewMenu(title)
		if wellKnown != "" {
			m.SetWellKnownID(wellKnown)
		}
		if anchor != "" {
			m.SetAnchor(anchor)
		}
		return m
	}
	// Declared in canonical order, as an app naturally would.
	b := bucketMenus([]*Menu{
		mk("File Buffer", MenuIDFile, ""),
		mk("Edit Block", MenuIDEdit, ""),
		mk("Format", MenuIDFormat, ""),
		mk("Viewport", MenuIDView, ""),
		mk("Input", "", MenuIDFile),
		mk("Search", "", MenuIDFile),
		mk("History", "", MenuIDFormat),
	})

	var got []string
	(&Desktop{}).appendAppBody(func(m *Menu) { got = append(got, m.Title()) }, nil, b)

	want := []string{
		"File Buffer", "Input", "Search",
		"Edit Block", "Format", "History", "Viewport",
	}
	if len(got) != len(want) {
		t.Fatalf("bar = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bar = %v, want %v", got, want)
		}
	}
}
