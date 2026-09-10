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
