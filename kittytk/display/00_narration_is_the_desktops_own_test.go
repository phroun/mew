package display_test

// Whether the desktop speaks what it announces belongs to the desktop. There is
// one announcement handler and announcements come from every trinket on it, so
// a setting kept per connection was a setting whoever spoke last owned: two
// apps toggling it replaced each other's handler, and one turning its own off
// turned the other's off with it, while the other went on believing it was on.
//
// It is the desktop's now, and the Ψ menu's Narration item and the wire are two
// ways to the same switch.

import (
	"testing"

	"github.com/phroun/kittytk/objects/trinkets"
)

// psiItems is what the desktop's own menu holds, in order.
func psiItems(t *testing.T, d *trinkets.Desktop) []*trinkets.MenuItem {
	t.Helper()
	var items []*trinkets.MenuItem
	onUI(d, func() {
		bar := d.MenuBar()
		if bar == nil {
			return
		}
		for _, m := range bar.Menus() {
			if m == nil || m.RawTitle() != "Ψ" {
				continue
			}
			for _, it := range m.Items() {
				if it != nil {
					items = append(items, it)
				}
			}
		}
	})
	if len(items) == 0 {
		t.Fatal("the desktop has no Ψ menu")
	}
	return items
}

// psiItem finds one of them by the text it shows.
func psiItem(t *testing.T, d *trinkets.Desktop, text string) *trinkets.MenuItem {
	t.Helper()
	for _, it := range psiItems(t, d) {
		if it.Text == text {
			return it
		}
	}
	t.Fatalf("the Ψ menu has no %q item", text)
	return nil
}

func TestNarrationIsOnTheSystemMenuBeforeConnections(t *testing.T) {
	desktop, _, done := servedDesktop(t)
	defer done()

	// It is there, it ticks, and it sits directly before Connections.
	var captions []string
	for _, it := range psiItems(t, desktop) {
		captions = append(captions, it.Text)
	}
	narration, connections := -1, -1
	for i, c := range captions {
		switch c {
		case "Narration":
			narration = i
		case "Connections...":
			connections = i
		}
	}
	if narration < 0 {
		t.Fatalf("no Narration item; the Ψ menu reads %v", captions)
	}
	if connections < 0 {
		t.Fatalf("no Connections item; the Ψ menu reads %v", captions)
	}
	if narration != connections-1 {
		t.Errorf("Narration is at %d and Connections at %d; the Ψ menu reads %v",
			narration, connections, captions)
	}

	item := psiItem(t, desktop, "Narration")
	if !item.Checkable {
		t.Error("the Narration item does not carry a tick")
	}
}

// Choosing it turns narration on, and choosing it again turns it off.
func TestChoosingNarrationTurnsItOverAndTheTickFollows(t *testing.T) {
	desktop, _, done := servedDesktop(t)
	defer done()

	item := psiItem(t, desktop, "Narration")
	if desktop.Narration() {
		t.Fatal("narration started on")
	}

	onUI(desktop, func() { item.Trigger() })
	if !desktop.Narration() {
		t.Error("choosing Narration did not turn it on")
	}
	onUI(desktop, func() { item.Trigger() })
	if desktop.Narration() {
		t.Error("choosing it again did not turn it off")
	}
}

// The wire reaches the same switch, and the menu's tick is right afterwards --
// which is what the about-to-show refresh is for, since the item is not what
// changed it.
func TestTheWireAndTheMenuAreTheSameSwitch(t *testing.T) {
	desktop, sock, done := servedDesktop(t)
	defer done()

	conn := dialSocket(t, sock, "Narrating App")
	item := psiItem(t, desktop, "Narration")

	if _, err := conn.Exec("announce_speak"); err != nil {
		t.Fatalf("announce_speak: %v", err)
	}
	if !desktop.Narration() {
		t.Fatal("the wire did not turn narration on")
	}
	refreshPsi(t, desktop)
	if !item.Checked {
		t.Error("the menu item did not catch up with the wire")
	}

	// And back the other way: the menu turns off what the wire turned on.
	onUI(desktop, func() { item.Trigger() })
	if desktop.Narration() {
		t.Error("the menu did not turn off what the wire turned on")
	}
}

// Two connections no longer fight over it: what one sets, the other sees.
func TestTwoAppsShareTheOneSetting(t *testing.T) {
	desktop, sock, done := servedDesktop(t)
	defer done()

	a := dialSocket(t, sock, "App A")
	b := dialSocket(t, sock, "App B")

	if _, err := a.Exec("announce_speak"); err != nil {
		t.Fatalf("A: %v", err)
	}
	if !desktop.Narration() {
		t.Fatal("A did not turn narration on")
	}
	// B turning it over turns over the one setting, rather than replacing a
	// handler and leaving A believing something else.
	if _, err := b.Exec("announce_speak"); err != nil {
		t.Fatalf("B: %v", err)
	}
	if desktop.Narration() {
		t.Error("B's toggle did not reach the setting A had turned on")
	}
}

// refreshPsi runs the Ψ menu's about-to-show hook, which is what puts the tick
// right when something other than the item changed the setting.
func refreshPsi(t *testing.T, d *trinkets.Desktop) {
	t.Helper()
	onUI(d, func() {
		for _, m := range d.MenuBar().Menus() {
			if m != nil && m.RawTitle() == "Ψ" {
				if fn := m.OnAboutToShow(); fn != nil {
					fn()
				}
			}
		}
	})
}

// appMenuItems is what the app's own menu holds, in order, with the menu's
// about-to-show hook run first -- which is what opening it would do.
func appMenuItems(t *testing.T, d *trinkets.Desktop) []*trinkets.MenuItem {
	t.Helper()
	var items []*trinkets.MenuItem
	onUI(d, func() {
		bar := d.MenuBar()
		if bar == nil {
			return
		}
		for _, m := range bar.Menus() {
			if m == nil || m.RawTitle() == "Ψ" {
				continue
			}
			if fn := m.OnAboutToShow(); fn != nil {
				fn()
			}
			for _, it := range m.Items() {
				if it != nil {
					items = append(items, it)
				}
			}
			return // the app's menu leads the bar
		}
	})
	if len(items) == 0 {
		t.Fatal("no app menu on the bar")
	}
	return items
}

func appMenuItem(t *testing.T, d *trinkets.Desktop, text string) *trinkets.MenuItem {
	t.Helper()
	for _, it := range appMenuItems(t, d) {
		if it.Text == text {
			return it
		}
	}
	var rows []string
	for _, it := range appMenuItems(t, d) {
		rows = append(rows, it.Text)
	}
	t.Fatalf("the app menu has no %q item; it reads %v", text, rows)
	return nil
}

// The route to turning narration on is in every app's own menu, whatever the
// app declared -- a person who needs it cannot be asked to find an app that
// opted in, the way Connections asks.
func TestNarrationIsOnEveryAppsOwnMenu(t *testing.T) {
	for _, c := range []struct{ name, build string }{
		{"an app that declared no menus", `w=new window title="W" width=200 height=120`},
		{"an app that declared its own", `w=new window title="W" width=200 height=120
mb=new menubar children={
	new menu caption="&Mine" wellknown="app" children={
		new menuitem caption="&New" action=x.new
	}
}`},
	} {
		desktop, sock, done := servedDesktop(t)
		conn := dialSocket(t, sock, "Menu App")
		if _, err := conn.Exec(c.build); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}

		item := appMenuItem(t, desktop, "Narration")
		if !item.Checkable {
			t.Errorf("%s: the Narration item carries no tick", c.name)
		}
		// It reaches the same setting the Ψ menu does.
		onUI(desktop, func() { item.Trigger() })
		if !desktop.Narration() {
			t.Errorf("%s: the app menu's item did not turn narration on", c.name)
		}
		// And the tick is right when something else changed it.
		desktop.SetNarration(false)
		if appMenuItem(t, desktop, "Narration").Checked {
			t.Errorf("%s: the tick did not catch up", c.name)
		}
		done()
	}
}

// A menu opens on an item, not on a rule. The separator that offsets the
// system's items only belongs there when the app put something above it.
func TestAnAppMenuDoesNotOpenWithASeparator(t *testing.T) {
	desktop, sock, done := servedDesktop(t)
	defer done()

	conn := dialSocket(t, sock, "Plain App")
	if _, err := conn.Exec(`w=new window title="W" width=200 height=120`); err != nil {
		t.Fatalf("build: %v", err)
	}

	items := appMenuItems(t, desktop)
	if items[0].Separator {
		var rows []string
		for _, it := range items {
			if it.Separator {
				rows = append(rows, "----")
			} else {
				rows = append(rows, it.Text)
			}
		}
		t.Errorf("the menu opens with a rule: %v", rows)
	}

	// An app that declared its own items still gets the offset, because now
	// there is something to offset from.
	desktop2, sock2, done2 := servedDesktop(t)
	defer done2()
	conn2 := dialSocket(t, sock2, "Declaring App")
	if _, err := conn2.Exec(`w=new window title="W" width=200 height=120
mb=new menubar children={
	new menu caption="&Mine" wellknown="app" children={
		new menuitem caption="&New" action=x.new
	}
}`); err != nil {
		t.Fatalf("build: %v", err)
	}
	rows := appMenuItems(t, desktop2)
	if rows[0].Separator {
		t.Error("the declared menu opens with a rule")
	}
	if len(rows) < 2 || !rows[1].Separator {
		t.Error("the system's items are not offset from the app's own")
	}
}
