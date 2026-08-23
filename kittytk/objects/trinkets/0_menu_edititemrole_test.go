package trinkets

import "testing"

// captions lists a menu's item texts, with separators shown as "---".
func captions(m *Menu) []string {
	var out []string
	for _, it := range m.Items() {
		if it.Separator {
			out = append(out, "---")
			continue
		}
		out = append(out, it.Text)
	}
	return out
}

// editMenuAfterMerge runs a declared edit menu through the desktop's merge and
// returns the menu the bar actually shows.
func editMenuAfterMerge(t *testing.T, declared *Menu) *Menu {
	t.Helper()
	var got *Menu
	(&Desktop{}).appendAppBody(func(m *Menu) {
		if m.WellKnownID() == MenuIDEdit {
			got = m
		}
	}, nil, bucketMenus([]*Menu{declared}))
	if got == nil {
		t.Fatal("no edit menu came out of the merge")
	}
	return got
}

func mkEditMenu(items ...*MenuItem) *Menu {
	m := NewMenu("Edit Block")
	m.SetWellKnownID(MenuIDEdit)
	for _, it := range items {
		m.AddItem(it)
	}
	return m
}

func roleItem(caption, role string) *MenuItem {
	it := NewMenuItem(caption)
	it.SetWellKnownID(role)
	return it
}

// Without role tags, an app that spells out its own clipboard items gets the
// system's set as well - two Cuts, two Copies, two Pastes, two Select Alls.
// This is the behaviour the roles exist to fix, so it is worth pinning.
func TestUntaggedClipboardItemsDuplicate(t *testing.T) {
	menu := editMenuAfterMerge(t, mkEditMenu(
		NewMenuItem("Cut to OS Clipboard"),
		NewMenuItem("Select All"),
	))
	got := captions(menu)
	want := []string{"Cut", "Copy", "Paste", "---", "Select All", "---", "Cut to OS Clipboard", "Select All"}
	if len(got) != len(want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("menu = %v, want %v", got, want)
		}
	}
}

// A tagged item BECOMES the standard one: the app's caption, in the app's
// position among its own items, and nothing prepended. The system supplies the
// behaviour - handler, host shortcut, enable/disable.
func TestTaggedItemsAdoptStandardBehaviour(t *testing.T) {
	cut := roleItem("Cut to OS Clipboard", ItemIDCut)
	copyIt := roleItem("Copy to OS Clipboard", ItemIDCopy)
	paste := roleItem("Paste from OS Clipboard", ItemIDPaste)
	all := roleItem("Select All", ItemIDSelectAll)

	menu := editMenuAfterMerge(t, mkEditMenu(
		NewMenuItem("Mark Block Beginning"), cut, copyIt, paste, all,
	))

	got := captions(menu)
	want := []string{"Mark Block Beginning", "Cut to OS Clipboard", "Copy to OS Clipboard",
		"Paste from OS Clipboard", "Select All"}
	if len(got) != len(want) {
		t.Fatalf("menu = %v, want %v (no synthesized block, no leading separator)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("menu = %v, want %v", got, want)
		}
	}

	// Behaviour came across: each adopted item is wired, and names the command
	// the synthesized one would have named -- which is how it gets a key at
	// all, resolved where the focus is rather than stamped on here.
	for _, it := range []*MenuItem{cut, copyIt, paste, all} {
		if it.OnTriggered == nil {
			t.Errorf("%q adopted its role but has no handler", it.Text)
		}
		if it.Command == "" {
			t.Errorf("%q adopted its role but names no command", it.Text)
		}
	}
}

// Claiming some roles and not others: the unclaimed ones are still
// synthesized, and the separator that belongs to that block only appears when
// there is something on both sides of it.
func TestPartialAdoptionSynthesizesTheRest(t *testing.T) {
	menu := editMenuAfterMerge(t, mkEditMenu(
		roleItem("Paste from OS Clipboard", ItemIDPaste),
		roleItem("Select All", ItemIDSelectAll),
	))
	got := captions(menu)
	want := []string{"Cut", "Copy", "---", "Paste from OS Clipboard", "Select All"}
	if len(got) != len(want) {
		t.Fatalf("menu = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("menu = %v, want %v", got, want)
		}
	}
}

// A second item claiming a role it already lost stays an ordinary item, rather
// than silently stealing a binding from the first.
func TestFirstClaimOfARoleWins(t *testing.T) {
	first := roleItem("Cut to OS Clipboard", ItemIDCut)
	second := roleItem("Cut Again", ItemIDCut)
	editMenuAfterMerge(t, mkEditMenu(first, second))

	if first.OnTriggered == nil {
		t.Error("the first item to claim a role should get the behaviour")
	}
	if second.OnTriggered != nil {
		t.Error("a duplicate claim must not also be wired")
	}
}

// The desktop rebuilds a declared edit menu into a menu of its OWN, moving the
// app's items across. Anything the app hung on the declared menu has to come
// with them — above all the about-to-show hook, which is where an app refreshes
// its items against live state. Dropping it does not blank the ITEMS, so the
// menu still looks right while quietly showing nothing the hook maintained
// (mew fills its whole shortcut column there, and lost it).
func TestDeclaredEditMenuKeepsItsAboutToShowHook(t *testing.T) {
	declared := mkEditMenu(NewMenuItem("Mark &Block Beginning"))
	appRan := false
	declared.SetOnAboutToShow(func() { appRan = true })

	menu := editMenuAfterMerge(t, declared)
	if menu.OnAboutToShow() == nil {
		t.Fatal("the merged menu has no about-to-show hook at all")
	}
	menu.OnAboutToShow()()
	if !appRan {
		t.Error("the app's about-to-show hook did not survive the merge")
	}
}

// The system's own hook still runs too — it is what enables/disables the
// standard clipboard items — so the two compose rather than replace.
func TestSystemAndAppAboutToShowBothRun(t *testing.T) {
	cut := roleItem("Cut to OS Clipboard", ItemIDCut)
	declared := mkEditMenu(cut)
	appRan := false
	declared.SetOnAboutToShow(func() { appRan = true })

	menu := editMenuAfterMerge(t, declared)
	cut.SetEnabled(true)
	menu.OnAboutToShow()()

	if !appRan {
		t.Error("app hook did not run")
	}
	// The system pass disables Cut when no editActor holds focus; that it
	// changed the item at all proves the system hook ran alongside the app's.
	if cut.Enabled {
		t.Error("system hook did not run: Cut should be disabled with no focused editor")
	}
}
