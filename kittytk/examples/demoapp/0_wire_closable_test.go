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
