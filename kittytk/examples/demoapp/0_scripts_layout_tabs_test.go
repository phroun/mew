package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// openTab builds the main window, selects the named tab and lays the window
// out, so what the tab holds has real bounds to read.
func openTab(t *testing.T, caption string) *trinkets.TabTrinket {
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
			return tabs
		}
	}
	t.Fatalf("the main window has no %q tab", caption)
	return nil
}

// panelsUnder collects every panel below a trinket, deepest last.
func panelsUnder(tr core.Trinket) []*trinkets.Panel {
	var out []*trinkets.Panel
	var dig func(core.Trinket)
	dig = func(w core.Trinket) {
		if p, ok := w.(*trinkets.Panel); ok {
			out = append(out, p)
		}
		if c, ok := w.(core.Container); ok {
			for _, k := range c.Children() {
				dig(k)
			}
		}
	}
	dig(tr)
	return out
}

// labelsIn returns the captions and bounds of the labels directly inside a
// panel.
func labelsIn(p *trinkets.Panel) map[string]core.UnitRect {
	out := map[string]core.UnitRect{}
	for _, k := range p.Children() {
		if l, ok := k.(*trinkets.Label); ok {
			out[l.Text()] = l.Bounds()
		}
	}
	return out
}

// The Grid tab's form comes out as a form: the labels share a column, each
// label is level with its own field, and the field column is the wider one.
func TestGridTabLaysOutAsAForm(t *testing.T) {
	tabs := openTab(t, "Grid")

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

	labels := labelsIn(form)
	name, address, notes := labels["Name:"], labels["Address:"], labels["Notes:"]
	// The labels ask for the trailing edge of their column, so what they share
	// is where they END -- captions of different lengths starting in the same
	// place would mean the alignment had not been applied.
	end := func(r core.UnitRect) core.Unit { return r.X + r.Width }
	if end(name) != end(address) || end(name) != end(notes) {
		t.Errorf("the three labels end at x=%d, x=%d and x=%d; they ask for one trailing edge",
			end(name), end(address), end(notes))
	}
	if name.X == address.X {
		t.Errorf("two captions of different lengths both start at x=%d, so the trailing alignment did nothing",
			name.X)
	}
	if !(name.Y < address.Y && address.Y < notes.Y) {
		t.Errorf("the rows run y=%d, y=%d, y=%d; they should run down the form",
			name.Y, address.Y, notes.Y)
	}

	var fields []core.UnitRect
	for _, k := range form.Children() {
		if f, ok := k.(*trinkets.TextInput); ok {
			fields = append(fields, f.Bounds())
		}
	}
	if len(fields) != 3 {
		t.Fatalf("the form holds %d fields, want 3", len(fields))
	}
	for i, f := range fields {
		if f.X < end(name) {
			t.Errorf("field %d starts at x=%d, inside the label column which ends at %d", i, f.X, end(name))
		}
		if f.Width <= name.Width {
			t.Errorf("field %d is %d wide against the label column's %d; the field column takes the leftover",
				i, f.Width, name.Width)
		}
	}
	// Each label is level with its own field.
	rows := []core.UnitRect{name, address, notes}
	for i := range rows {
		if rows[i].Y != fields[i].Y {
			t.Errorf("row %d has its label at y=%d and its field at y=%d", i, rows[i].Y, fields[i].Y)
		}
	}
}

// The Flex tab's wrapping run actually wraps: the eight buttons do not all sit
// on one line, and the run starts each line over at the left.
func TestFlexTabWraps(t *testing.T) {
	tabs := openTab(t, "Flex")

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
		t.Fatal("the Flex tab has no panel of eight buttons")
	}

	rows := map[core.Unit]int{}
	var firstX core.Unit = -1
	for _, k := range run.Children() {
		b, ok := k.(*trinkets.Button)
		if !ok {
			continue
		}
		r := b.Bounds()
		rows[r.Y]++
		if firstX < 0 {
			firstX = r.X
		}
	}
	if len(rows) < 2 {
		t.Errorf("eight buttons came out on %d line(s); the run should wrap in the space it has", len(rows))
	}

	// Every line starts where the first one does.
	starts := map[core.Unit]core.Unit{}
	for _, k := range run.Children() {
		b, ok := k.(*trinkets.Button)
		if !ok {
			continue
		}
		r := b.Bounds()
		if x, seen := starts[r.Y]; !seen || r.X < x {
			starts[r.Y] = r.X
		}
	}
	for y, x := range starts {
		if x != firstX {
			t.Errorf("the line at y=%d starts at x=%d, want the run's x=%d", y, x, firstX)
		}
	}
}
