package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A side-named key carries only side-named commands, so a trinket whose
// sequence runs down the screen no longer answers a horizontal arrow it never
// asked for. It used to: Left fell past trinket_item_left, which a list does
// not declare, onto trinket_item_prior, which it does -- and no direction
// could ever have corrected that, because a column does not turn over.
func TestASideNamedKeyOnlyReachesASideNamedCommand(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys interface {
			KeyCommand(string) string
			AbandonKeySequence()
		}
	}{
		{"a list", NewListView()},
		{"a combo box", NewComboBox()},
	} {
		for _, key := range []string{"Left", "Right"} {
			tc.keys.AbandonKeySequence()
			if got := tc.keys.KeyCommand(key); got != "" {
				t.Errorf("%s answers %s with %q; it declares no side-named command",
					tc.name, key, got)
			}
		}
		// The keys its sequence does run along are untouched.
		for key, want := range map[string]string{
			"Up":   core.CmdTrinketItemUp,
			"Down": core.CmdTrinketItemDown,
		} {
			tc.keys.AbandonKeySequence()
			if got := tc.keys.KeyCommand(key); got != want {
				t.Errorf("%s answers %s with %q, want %q", tc.name, key, got, want)
			}
		}
	}
}

// A dock's entries run the way it reads, so the left arrow steps back along
// them in a dock reading left to right and on along them in one reading the
// other way.
func TestADockWalksTheWayItReads(t *testing.T) {
	for _, tc := range []struct {
		dir      core.Direction
		on, back string
	}{
		{core.DirLTR, "Right", "Left"},
		{core.DirRTL, "Left", "Right"},
	} {
		d := NewDockRow()
		d.SetDirection(tc.dir)
		for _, name := range []string{"one", "two", "three"} {
			d.AddEntry(&DockEntry{Title: name, WindowID: core.NextObjectID()})
		}
		d.selectedIndex = 1

		key := func(k string) {
			d.AbandonKeySequence()
			d.HandleKeyPress(core.KeyPressEvent{Key: k})
		}
		key(tc.on)
		if d.selectedIndex != 2 {
			t.Errorf("%v: %s left the dock on entry %d, want the next one",
				tc.dir, tc.on, d.selectedIndex)
		}
		key(tc.back)
		key(tc.back)
		if d.selectedIndex != 0 {
			t.Errorf("%v: %s twice left the dock on entry %d, want the first",
				tc.dir, tc.back, d.selectedIndex)
		}
	}
}

// A radio group laid out in a row walks the same way, and its group is the
// sequence -- so what the arrow means is asked of the FORM the buttons sit in.
func TestARadioGroupWalksTheWayItsFormReads(t *testing.T) {
	for _, tc := range []struct {
		dir      core.Direction
		on, back string
	}{
		{core.DirLTR, "Right", "Left"},
		{core.DirRTL, "Left", "Right"},
	} {
		form := NewPanel()
		form.SetDirection(tc.dir)
		g := NewRadioGroup()
		var buttons []*RadioButton
		for _, name := range []string{"one", "two", "three"} {
			b := NewRadioButton(name)
			g.AddButton(b)
			form.AddChild(b)
			buttons = append(buttons, b)
		}
		buttons[1].SetChecked(true)

		key := func(k string) {
			buttons[1].AbandonKeySequence()
			buttons[1].HandleKeyPress(core.KeyPressEvent{Key: k})
		}
		key(tc.on)
		if !buttons[2].IsChecked() {
			t.Errorf("%v: %s did not move on to the next button", tc.dir, tc.on)
		}
		buttons[1].SetChecked(true)
		key(tc.back)
		if !buttons[0].IsChecked() {
			t.Errorf("%v: %s did not move back to the previous button", tc.dir, tc.back)
		}
	}
}

// Up and down cross a column, which no direction turns over, so they land in
// the same place in either form -- whatever the dock's own row arithmetic
// makes that.
func TestAColumnDoesNotTurnOverForTheVerticalArrows(t *testing.T) {
	after := func(dir core.Direction, key string) int {
		d := NewDockRow()
		d.SetDirection(dir)
		for _, name := range []string{"one", "two", "three", "four"} {
			d.AddEntry(&DockEntry{Title: name, WindowID: core.NextObjectID()})
		}
		d.selectedIndex = 2
		d.AbandonKeySequence()
		d.HandleKeyPress(core.KeyPressEvent{Key: key})
		return d.selectedIndex
	}
	for _, key := range []string{"Up", "Down"} {
		ltr, rtl := after(core.DirLTR, key), after(core.DirRTL, key)
		if ltr != rtl {
			t.Errorf("%s lands on entry %d in one form and %d in the other", key, ltr, rtl)
		}
		if ltr == 2 {
			t.Fatalf("%s moved nothing, so this proves nothing about turning over", key)
		}
	}
}
