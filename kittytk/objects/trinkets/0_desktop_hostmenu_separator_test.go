package trinkets

// addHostWindowMenuItems puts Minimize and Zoom above Exit Desktop as a group
// of their own, which means dividing them from the group above AND from Exit
// below - but only where a rule is actually missing.
//
// Both edges have their own way of going wrong. Above: createSystemMenu
// already closes its last group with a rule, so inserting one unconditionally
// draws two in a row, and with Exit first in the menu it opens the menu with a
// rule above everything. Below: the rule the menu already carries gets used up
// above these items, so Exit is left joined onto Zoom with nothing between.

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// hostMenuDesktop is a desktop with the themed frame active - the gate on
// addHostWindowMenuItems - carrying the given system menu.
func hostMenuDesktop(items ...*MenuItem) *Desktop {
	d := NewDesktop()
	d.graphicalFrames = true
	d.surface = &fakeNativeSurface{size: core.UnitSize{Width: 800, Height: 600}, pxW: 800, pxH: 600}
	if len(items) > 0 {
		menu := NewMenu("Ψ")
		for _, it := range items {
			menu.AddItem(it)
		}
		d.systemMenu = menu
	}
	return d
}

// exitItem is an Exit Desktop item, which is what the insert anchors to.
func exitItem() *MenuItem {
	return NewMenuItem("E&xit Desktop").SetCommand(core.CmdDesktopExit)
}

// menuShape renders a menu as a list, separators as "---".
func menuShape(m *Menu) []string {
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

func equalShape(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHostWindowMenuItemsDivideTheGroupOnBothSides(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []*MenuItem
		want  []string
	}{
		{
			// The group above is already closed by its own rule, so these
			// items join on below it - and Exit still needs dividing.
			name:  "a rule already closes the group above",
			items: []*MenuItem{NewMenuItem("About"), NewSeparator(), exitItem()},
			want:  []string{"About", "---", "Minimize", "Zoom", "---", "Exit Desktop"},
		},
		{
			// A live item directly above IS a group to divide from, so a
			// rule is wanted on both sides.
			name:  "a live item sits directly above",
			items: []*MenuItem{NewMenuItem("About"), exitItem()},
			want:  []string{"About", "---", "Minimize", "Zoom", "---", "Exit Desktop"},
		},
		{
			// Nothing above at all: no group, so no rule there. One would
			// open the menu with a divider above everything.
			name:  "Exit is the first item",
			items: []*MenuItem{exitItem()},
			want:  []string{"Minimize", "Zoom", "---", "Exit Desktop"},
		},
		{
			// No Exit item: the insert falls to the end, so there is
			// nothing below to divide from.
			name:  "no Exit item to anchor to",
			items: []*MenuItem{NewMenuItem("About")},
			want:  []string{"About", "---", "Minimize", "Zoom"},
		},
		{
			// Same, onto a menu that already ends in a rule: neither edge
			// wants one.
			name:  "no Exit item, menu already ends in a rule",
			items: []*MenuItem{NewMenuItem("About"), NewSeparator()},
			want:  []string{"About", "---", "Minimize", "Zoom"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := hostMenuDesktop(tc.items...)
			d.addHostWindowMenuItems()
			if got := menuShape(d.systemMenu); !equalShape(got, tc.want) {
				t.Errorf("got  %v\nwant %v", got, tc.want)
			}
		})
	}
}

// The real system menu, which is the case that was actually wrong.
func TestHostWindowMenuItemsOnTheRealSystemMenu(t *testing.T) {
	d := hostMenuDesktop()
	if !d.themedFrameActive() {
		t.Fatal("themed frame is not active; the insert would not run")
	}
	d.addHostWindowMenuItems()

	want := []string{
		"About Desktop",
		"---",
		"Desktop Accessories",
		"Event Viewer",
		"---",
		"Minimize",
		"Zoom",
		"---",
		"Exit Desktop",
	}
	if got := menuShape(d.systemMenu); !equalShape(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}
