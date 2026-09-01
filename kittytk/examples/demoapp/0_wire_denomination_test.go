package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// denomWindow builds the demo's main window over an in-process connection
// and returns the real Window behind it, so what the tab does can be read
// off the trinket rather than off the statement that was sent.
func denomWindow(t *testing.T) (*app, *window.Window) {
	t.Helper()
	conn := inprocess.New(nil)
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	a := &app{conn: conn, ui: ui}
	a.wireMainWindow()

	win, _ := ui.Object("w").Target().(*window.Window)
	if win == nil {
		t.Fatal("no window behind the main build")
	}
	return a, win
}

// press acts on the real trinket, because clicking and pressing Return are
// things a person does at the display, not calls the client veneer offers.
func (a *app) press(t *testing.T, name string) {
	t.Helper()
	b, ok := a.ui.Object(name).Target().(*trinkets.Button)
	if !ok {
		t.Fatalf("%q is not a button", name)
	}
	b.Click()
}

func (a *app) returnIn(t *testing.T, name string) {
	t.Helper()
	f, ok := a.ui.Object(name).Target().(*trinkets.TextInput)
	if !ok {
		t.Fatalf("%q is not a text field", name)
	}
	if !f.HandleKeyPress(core.KeyPressEvent{Key: "Return"}) {
		t.Fatalf("%q declined Return", name)
	}
}

// The two axes are independent, which is the whole reason this tab exists:
// the Selection tab's grid checkbox reaches the same machinery through the
// window's denomination property, and that one sets the row height alone.
func TestDenominationPresetsSetBothAxes(t *testing.T) {
	a, win := denomWindow(t)

	if ov := win.CellMetricsOverride(); ov != nil {
		t.Fatalf("the window starts with an override %+v; it should inherit", ov)
	}

	for _, c := range []struct {
		button string
		x, y   core.Unit
	}{
		{"dnh", 4, 8},
		{"dnt", 16, 32},
		{"dns", 16, 16},
		{"dnn", 8, 32},
		{"dnd", 8, 16},
	} {
		a.press(t, c.button)
		ov := win.CellMetricsOverride()
		if ov == nil {
			t.Fatalf("%s left no override", c.button)
		}
		if ov.UnitsPerCellWidth != c.x || ov.UnitsPerCellHeight != c.y {
			t.Errorf("%s gave %dx%d, want %dx%d",
				c.button, ov.UnitsPerCellWidth, ov.UnitsPerCellHeight, c.x, c.y)
		}
	}
}

// Typed values apply on complete -- Return in either field -- because that
// is the event that means the person is done with the value, rather than
// re-denominating the window on every keystroke of a two-digit number.
func TestDenominationAppliesTypedValuesOnComplete(t *testing.T) {
	a, win := denomWindow(t)

	_ = a.ui.TextInput("dnx").SetText("12")
	_ = a.ui.TextInput("dny").SetText("20")
	a.press(t, "dnap")

	ov := win.CellMetricsOverride()
	if ov == nil || ov.UnitsPerCellWidth != 12 || ov.UnitsPerCellHeight != 20 {
		t.Fatalf("override = %+v, want 12x20", ov)
	}

	// Return in the Y field commits the pair, not just its own half.
	_ = a.ui.TextInput("dnx").SetText("6")
	_ = a.ui.TextInput("dny").SetText("6")
	a.returnIn(t, "dny")
	if ov := win.CellMetricsOverride(); ov == nil || ov.UnitsPerCellWidth != 6 || ov.UnitsPerCellHeight != 6 {
		t.Errorf("Return in Y gave %+v, want 6x6 -- it must apply both fields", ov)
	}
}

// Zero is not a small denomination: the cell conversions divide by it. A
// value out of range is refused and the window keeps what it had, which is
// what leaves the tab able to put itself back.
func TestDenominationRefusesWhatWouldBreakTheWindow(t *testing.T) {
	a, win := denomWindow(t)

	a.press(t, "dnd") // a known good 8x16 to preserve
	for _, bad := range []struct{ x, y, why string }{
		{"0", "16", "zero divides"},
		{"8", "0", "zero divides"},
		{"-4", "16", "negative"},
		{"8", "99", "past the ceiling"},
		{"", "16", "not a number"},
		{"eight", "16", "not a number"},
	} {
		_ = a.ui.TextInput("dnx").SetText(bad.x)
		_ = a.ui.TextInput("dny").SetText(bad.y)
		a.press(t, "dnap")

		ov := win.CellMetricsOverride()
		if ov == nil || ov.UnitsPerCellWidth != 8 || ov.UnitsPerCellHeight != 16 {
			t.Errorf("%q x %q (%s) changed the window to %+v; it should have been refused",
				bad.x, bad.y, bad.why, ov)
		}
	}

	// One unit per cell is the floor, and it is accepted.
	_ = a.ui.TextInput("dnx").SetText("1")
	_ = a.ui.TextInput("dny").SetText("1")
	a.press(t, "dnap")
	if ov := win.CellMetricsOverride(); ov == nil || ov.UnitsPerCellWidth != 1 || ov.UnitsPerCellHeight != 1 {
		t.Errorf("1x1 gave %+v; one unit per cell is legal", ov)
	}
}
