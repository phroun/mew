package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

func TestCommandRegistryBasics(t *testing.T) {
	reg := core.NewCommandRegistry()

	fired := 0
	reg.Register("file.open", func() { fired++ })

	if !reg.Has("file.open") {
		t.Fatal("Has should report registered command")
	}
	if !reg.Dispatch("file.open") {
		t.Fatal("Dispatch should run the handler")
	}
	if fired != 1 {
		t.Fatalf("handler fired %d times, want 1", fired)
	}
	if reg.Dispatch("no.such.command") {
		t.Fatal("Dispatch of unknown ID should return false")
	}

	reg.Unregister("file.open")
	if reg.Dispatch("file.open") {
		t.Fatal("Dispatch after Unregister should return false")
	}
	if fired != 1 {
		t.Fatalf("handler fired %d times after unregister, want 1", fired)
	}
}

func TestMenuItemAutoIDsAreUnique(t *testing.T) {
	a := NewMenuItem("A")
	b := NewMenuItem("B")
	if a.ID() == "" || b.ID() == "" {
		t.Fatal("items should receive auto IDs")
	}
	if a.ID() == b.ID() {
		t.Fatalf("auto IDs collide: %q", a.ID())
	}
}

func TestMenuItemSemanticID(t *testing.T) {
	item := NewMenuItem("&Open").SetID(core.StandardActions.Open)
	if item.ID() != "file.open" {
		t.Fatalf("got %q, want file.open", item.ID())
	}
	// Empty SetID is ignored (IDs are the dispatch key).
	item.SetID("")
	if item.ID() != "file.open" {
		t.Fatalf("empty SetID overwrote ID: %q", item.ID())
	}
}

func TestMenuBindCommandsDispatchesByID(t *testing.T) {
	reg := core.NewCommandRegistry()

	fired := 0
	sub := NewMenu("Sub")
	subItem := NewMenuItem("Deep").SetOnTriggered(func() { fired += 10 })
	sub.AddItem(subItem)

	menu := NewMenu("&File")
	item := NewMenuItem("&Open").SetID("file.open").SetOnTriggered(func() { fired++ })
	menu.AddItem(item)
	menu.AddItem(NewSeparator())
	menu.AddItem(NewMenuItem("More").SetSubMenu(sub))

	menu.BindCommands(reg)

	// Handlers registered by ID, including through the submenu.
	if !reg.Has("file.open") || !reg.Has(subItem.ID()) {
		t.Fatal("BindCommands should register handlers recursively")
	}

	// Trigger dispatches through the registry, exactly once.
	item.Trigger()
	subItem.Trigger()
	if fired != 11 {
		t.Fatalf("fired = %d, want 11", fired)
	}

	// External dispatch by ID reaches the same handler - the seam a
	// display service will use.
	reg.Dispatch("file.open")
	if fired != 12 {
		t.Fatalf("fired = %d after registry dispatch, want 12", fired)
	}
}

func TestMenuItemFallbackWithoutRegistry(t *testing.T) {
	fired := 0
	item := NewMenuItem("Standalone").SetOnTriggered(func() { fired++ })
	item.Trigger()
	if fired != 1 {
		t.Fatalf("unbound item should fall back to its closure; fired=%d", fired)
	}
}

func TestObjectIDsAreUniqueAcrossTrinketTypes(t *testing.T) {
	seen := map[core.ObjectID]bool{}
	for _, w := range []interface{ ObjectID() core.ObjectID }{
		NewLabel("a"), NewCheckbox("b"), NewPanel(), NewComboBox(),
		NewDockRow(), NewVSplitter(),
	} {
		id := w.ObjectID()
		if id == 0 {
			t.Fatal("trinket has zero ObjectID")
		}
		if seen[id] {
			t.Fatalf("duplicate ObjectID %d", id)
		}
		seen[id] = true
	}
}

func TestComboBoxPopupIDsAreUniqueWhenUnnamed(t *testing.T) {
	a := NewComboBox()
	b := NewComboBox()
	if a.popupID() == b.popupID() {
		t.Fatalf("two unnamed comboboxes share popup ID %q", a.popupID())
	}
}

func TestDockRemoveEntryByIDWithDuplicateTitles(t *testing.T) {
	dock := NewDockRow()
	first := &DockEntry{Title: "Untitled", WindowID: core.NextObjectID()}
	second := &DockEntry{Title: "Untitled", WindowID: core.NextObjectID()}
	dock.AddEntry(first)
	dock.AddEntry(second)

	// Removing by ID takes exactly the right entry despite the shared
	// title (RemoveEntryByTitle would have removed the first).
	dock.RemoveEntryByID(second.WindowID)

	if got := len(dock.Entries()); got != 1 {
		t.Fatalf("entries = %d, want 1", got)
	}
	if dock.Entries()[0].WindowID != first.WindowID {
		t.Fatal("wrong entry removed")
	}
}

func TestSetOnTriggeredAfterBindRefreshesRegistration(t *testing.T) {
	reg := core.NewCommandRegistry()
	menu := NewMenu("M")
	item := NewMenuItem("X").SetID("x.cmd").SetOnTriggered(func() {})
	menu.AddItem(item)
	menu.BindCommands(reg)

	fired := 0
	item.SetOnTriggered(func() { fired++ })

	reg.Dispatch("x.cmd")
	if fired != 1 {
		t.Fatalf("registry should hold the refreshed handler; fired=%d", fired)
	}
}
