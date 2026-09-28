package main

import (
	"testing"

	"github.com/phroun/kittytk/objects/trinkets"
)

// The closable switch reaches all four tab strips and turns them back off:
// the window's own strip, the Bottom Tabs strip, and both side strips.
func TestTheClosableSwitchReachesEveryTabStrip(t *testing.T) {
	ui, win, _ := openTabWithUI(t, "Vertical Tabs")
	(&app{ui: ui}).wireClosable()

	strips := map[string]*trinkets.TabTrinket{}
	for _, name := range []string{"tabs", "btabs", "vtside", "vtopp"} {
		s, _ := ui.Object(name).Target().(*trinkets.TabTrinket)
		if s == nil {
			t.Fatalf("%s is not surfaced as a tab strip", name)
		}
		strips[name] = s
	}
	box, _ := ui.Object("vtclose").Target().(*trinkets.Checkbox)
	if box == nil {
		t.Fatal("nothing behind vtclose is a checkbox")
	}

	for _, want := range []bool{true, false} {
		box.Toggle()
		win.Layout()
		for name, s := range strips {
			if s.IsClosable() != want {
				t.Errorf("after toggling to %v, %s closable=%v", want, name, s.IsClosable())
			}
		}
	}
}

// The trailing switch moves the same four strips' buttons after their labels,
// and back before them.
func TestTheTrailingSwitchReachesEveryTabStrip(t *testing.T) {
	ui, win, _ := openTabWithUI(t, "Vertical Tabs")
	(&app{ui: ui}).wireClosable()
	box, _ := ui.Object("vttrail").Target().(*trinkets.Checkbox)
	if box == nil {
		t.Fatal("nothing behind vttrail is a checkbox")
	}
	for _, leading := range []bool{false, true} {
		box.Toggle()
		win.Layout()
		for _, name := range []string{"tabs", "btabs", "vtside", "vtopp"} {
			s, _ := ui.Object(name).Target().(*trinkets.TabTrinket)
			if s == nil || s.CloseLeading() != leading {
				t.Errorf("after toggling, %s leading=%v, want %v", name, s != nil && s.CloseLeading(), leading)
			}
		}
	}
}

// The side strip's First tab says !closable, so the closable switch leaves it
// without a button while every other tab on the strip gets one.
func TestTheFirstSideTabStaysWithoutAButton(t *testing.T) {
	ui, win, _ := openTabWithUI(t, "Vertical Tabs")
	(&app{ui: ui}).wireClosable()
	box, _ := ui.Object("vtclose").Target().(*trinkets.Checkbox)
	side, _ := ui.Object("vtside").Target().(*trinkets.TabTrinket)
	if box == nil || side == nil {
		t.Fatal("no closable switch or side strip")
	}
	box.Toggle()
	win.Layout()
	if side.TabClosable(0) != trinkets.ClosableOff || side.TabClosable(1) != trinkets.ClosableDefault {
		t.Errorf("the side strip's tabs say %v and %v", side.TabClosable(0), side.TabClosable(1))
	}
}

// The movable switch reaches the same four strips and turns them back off.
func TestTheMovableSwitchReachesEveryTabStrip(t *testing.T) {
	ui, win, _ := openTabWithUI(t, "Vertical Tabs")
	(&app{ui: ui}).wireClosable()
	box, _ := ui.Object("vtmove").Target().(*trinkets.Checkbox)
	if box == nil {
		t.Fatal("nothing behind vtmove is a checkbox")
	}
	for _, want := range []bool{true, false} {
		box.Toggle()
		win.Layout()
		for _, name := range []string{"tabs", "btabs", "vtside", "vtopp"} {
			s, _ := ui.Object(name).Target().(*trinkets.TabTrinket)
			if s == nil || s.IsMovable() != want {
				t.Errorf("after toggling to %v, %s movable=%v", want, name, s != nil && s.IsMovable())
			}
		}
	}
}

// A tab stands where a move leaves it: the one moved goes where it was put,
// and the ones between close up behind it.
func TestFollowMoveFindsATabAfterAMove(t *testing.T) {
	for _, tc := range []struct{ i, from, to, want int }{
		{15, 15, 3, 3}, {15, 3, 16, 14}, {15, 16, 3, 16}, {15, 2, 15, 14}, {15, 16, 15, 16}, {15, 0, 1, 15},
	} {
		if got := followMove(tc.i, tc.from, tc.to); got != tc.want {
			t.Errorf("followMove(%d, %d, %d) = %d, want %d", tc.i, tc.from, tc.to, got, tc.want)
		}
	}
}

// A tab stands where a close leaves it: the ones after the closed tab move up
// one, and the closed tab itself stands nowhere.
func TestFollowCloseFindsATabAfterAClose(t *testing.T) {
	for _, tc := range []struct{ i, closed, want int }{
		{15, 15, -1}, {15, 3, 14}, {15, 16, 15},
	} {
		if got := followClose(tc.i, tc.closed); got != tc.want {
			t.Errorf("followClose(%d, %d) = %d, want %d", tc.i, tc.closed, got, tc.want)
		}
	}
}
