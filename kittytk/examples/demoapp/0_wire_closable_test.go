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
