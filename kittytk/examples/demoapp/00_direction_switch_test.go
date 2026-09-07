package main

import (
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// openTabWithUI builds the main window, selects a tab and hands back the
// client handle, so a test can drive that tab's controls the way the wiring
// does -- by setting properties over the wire.
func openTabWithUI(t *testing.T, caption string) (*client.UI, *window.Window, *trinkets.TabTrinket) {
	t.Helper()
	conn := inprocess.New(nil)
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	win, _ := ui.Object("w").Target().(*window.Window)
	tabs, _ := ui.Object("tabs").Target().(*trinkets.TabTrinket)
	if win == nil || tabs == nil {
		t.Fatal("no window or tab strip behind the main build")
	}
	for i := 0; i < tabs.Count(); i++ {
		if tabs.TabText(i) == caption {
			tabs.SetCurrentIndex(i)
			win.Layout()
			return ui, win, tabs
		}
	}
	t.Fatalf("the main window has no %q tab", caption)
	return nil, nil, nil
}

// The Grid tab's checkbox turns the form over: the label column and the field
// column swap sides, and the labels keep asking for the edge of their own
// column rather than for the left of the screen.
//
// The panel it is set on is the one holding the demonstrations, not the tab:
// the checkbox itself sits above that panel and must stay where the reader
// left it.
func TestTheGridTabTurnsOver(t *testing.T) {
	ui, win, tabs := openTabWithUI(t, "Grid")

	var form *trinkets.Panel
	for _, p := range panelsUnder(tabs) {
		if l := labelsIn(p); len(l) == 3 {
			if _, ok := l["Name:"]; ok {
				form = p
			}
		}
	}
	if form == nil {
		t.Fatal("the Grid tab has no form panel with the three field labels in it")
	}

	field := func() core.UnitRect {
		for _, k := range form.Children() {
			if f, ok := k.(*trinkets.TextInput); ok {
				return f.Bounds()
			}
		}
		t.Fatal("the form holds no fields")
		return core.UnitRect{}
	}

	label, fld := labelsIn(form)["Name:"], field()
	if label.X >= fld.X {
		t.Fatalf("as built the label is at x=%d and the field at x=%d; the form is not left to right",
			label.X, fld.X)
	}
	control, _ := ui.Object("grtl").Target().(core.Trinket)
	if control == nil {
		t.Fatal("the Grid tab's direction checkbox is not surfaced")
	}
	controlAt := control.Bounds()

	if err := ui.Object("grc").Set("direction=rtl"); err != nil {
		t.Fatalf("direction=rtl: %v", err)
	}
	win.Layout()

	label, fld = labelsIn(form)["Name:"], field()
	if label.X <= fld.X {
		t.Errorf("turned over, the label is at x=%d and the field at x=%d; the columns did not swap",
			label.X, fld.X)
	}
	if got := control.Bounds(); got.X != controlAt.X {
		t.Errorf("the checkbox moved from x=%d to x=%d; it sits outside what it turns over",
			controlAt.X, got.X)
	}

	// And back, so the control is a switch rather than a one-way door.
	if err := ui.Object("grc").Set("direction=ltr"); err != nil {
		t.Fatalf("direction=ltr: %v", err)
	}
	win.Layout()
	if label, fld = labelsIn(form)["Name:"], field(); label.X >= fld.X {
		t.Errorf("switched back, the label is at x=%d and the field at x=%d", label.X, fld.X)
	}
}

// Ticking the checkbox is what turns the tab over. The test above sets
// direction on the content panel outright, which says the demo's SHAPE is
// right; this one runs the app's own wiring and toggles the real trinket, so a
// handler pointed at the wrong object is caught rather than looking inert.
func TestTickingTheCheckboxTurnsTheGridTabOver(t *testing.T) {
	ui, win, tabs := openTabWithUI(t, "Grid")
	(&app{ui: ui}).wireDirection()

	var form *trinkets.Panel
	for _, p := range panelsUnder(tabs) {
		if l := labelsIn(p); len(l) == 3 {
			if _, ok := l["Name:"]; ok {
				form = p
			}
		}
	}
	if form == nil {
		t.Fatal("the Grid tab has no form panel with the three field labels in it")
	}
	before := labelsIn(form)["Name:"]

	box, _ := ui.Object("grtl").Target().(*trinkets.Checkbox)
	if box == nil {
		t.Fatal("nothing behind grtl is a checkbox")
	}
	box.Toggle()
	win.Layout()

	if after := labelsIn(form)["Name:"]; after.X <= before.X {
		t.Errorf("ticked, the label is at x=%d against %d before; the tab did not turn over",
			after.X, before.X)
	}

	// And untick, which is a second branch of the handler and not the first
	// one run backwards.
	box.Toggle()
	win.Layout()
	if back := labelsIn(form)["Name:"]; back.X != before.X {
		t.Errorf("unticked, the label is at x=%d; it was at %d before the tick", back.X, before.X)
	}
}

// Every tab that shows something turning over has a switch, and each one
// reaches the demonstrations rather than itself.
//
// The two tests above read the geometry, which is what says the Grid and Flex
// tabs really turn over. This one is about the switches: that each is wired,
// that it is pointed at the content and not at some neighbour, and that the
// control itself stays where the reader left it.
func TestEveryDirectionSwitchReachesItsOwnTab(t *testing.T) {
	for _, c := range []struct{ tab, box, content string }{
		{"Grid", "grtl", "grc"},
		{"Flex", "fxrtl", "fxc"},
		{"Selection", "sertl", "serc"},
		{"Scroll Selection", "ssrtl", "ssc"},
		{"Scroll Lists", "slrtl", "slc"},
		{"Progress", "pgrtl", "pgc"},
		{"Lists", "lirtl", "lilv"},
		{"Details", "drtl", "dtree"},
		{"Vertical Tabs", "vtrtl", "vtc"},
	} {
		t.Run(c.tab, func(t *testing.T) {
			ui, win, _ := openTabWithUI(t, c.tab)
			(&app{ui: ui}).wireDirection()

			box, _ := ui.Object(c.box).Target().(*trinkets.Checkbox)
			content, _ := ui.Object(c.content).Target().(core.Trinket)
			if box == nil || content == nil {
				t.Fatalf("the %s tab has no switch behind %q or no content behind %q",
					c.tab, c.box, c.content)
			}
			if got := core.FindEffectiveDirection(content); got != core.DirLTR {
				t.Fatalf("as built the content reads %v, want %v", got, core.DirLTR)
			}

			box.Toggle()
			win.Layout()
			if got := core.FindEffectiveDirection(content); got != core.DirRTL {
				t.Errorf("ticked, the content reads %v, want %v", got, core.DirRTL)
			}
			if got := core.FindEffectiveDirection(box); got != core.DirLTR {
				t.Errorf("the switch turned over with what it switches; a control stays where the reader left it, by sitting outside what turns or by naming its own direction")
			}

			box.Toggle()
			win.Layout()
			if got := core.FindEffectiveDirection(content); got != core.DirLTR {
				t.Errorf("unticked, the content reads %v, want %v back", got, core.DirLTR)
			}
		})
	}
}

// The Flex tab's checkbox turns its runs over: the wrapping run starts each
// line at the right instead of the left. Ticked through the app's own wiring,
// so the Flex tab's handler is checked as well as the Grid tab's.
func TestTheFlexTabTurnsOver(t *testing.T) {
	ui, win, tabs := openTabWithUI(t, "Flex")
	(&app{ui: ui}).wireDirection()

	var run *trinkets.Panel
	for _, p := range panelsUnder(tabs) {
		buttons := 0
		for _, k := range p.Children() {
			if _, ok := k.(*trinkets.Button); ok {
				buttons++
			}
		}
		if buttons == 8 {
			run = p
		}
	}
	if run == nil {
		t.Fatal("the Flex tab has no panel holding the eight wrapping cards")
	}

	first := func() core.UnitRect { return run.Children()[0].Bounds() }
	right := func() core.Unit { return run.Bounds().X + run.Bounds().Width }

	built := first()
	if built.X > right()/2 {
		t.Fatalf("as built the first card is at x=%d, past the middle of the run", built.X)
	}

	box, _ := ui.Object("fxrtl").Target().(*trinkets.Checkbox)
	if box == nil {
		t.Fatal("nothing behind fxrtl is a checkbox")
	}
	box.Toggle()
	win.Layout()

	if turned := first(); turned.X <= built.X {
		t.Errorf("turned over, the first card is at x=%d against %d before; the run did not turn over",
			turned.X, built.X)
	}
}
