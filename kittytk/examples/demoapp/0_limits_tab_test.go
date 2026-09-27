package main

import (
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// openLimits builds the main window and selects the Limits tab, handing back
// the client handle so a test can drive the tab's controls the way a person
// clicking them would -- by setting properties over the wire.
func openLimits(t *testing.T) (*client.UI, *window.Window) {
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
		if tabs.TabText(i) == "Limits" {
			tabs.SetCurrentIndex(i)
			win.Layout()
			return ui, win
		}
	}
	t.Fatal("the main window has no Limits tab")
	return nil, nil
}

// The Limits tab's run: three boxes sharing the width, the middle one taking
// whatever bound the controls put on it.
func TestLimitsTabShowsAMaximumBiting(t *testing.T) {
	ui, win := openLimits(t)
	mid, ok := ui.Object("lmid").Target().(*trinkets.Panel)
	if !ok {
		t.Fatal("the Limits tab has no middle box behind key lmid")
	}
	run, ok := mid.Parent().(*trinkets.Panel)
	if !ok {
		t.Fatal("the middle box is not in a panel")
	}
	widths := func() [3]int {
		win.Layout()
		var out [3]int
		for i, k := range run.Children() {
			out[i] = int(k.Bounds().Width)
		}
		return out
	}

	// As built nothing is bounded, so the three share the run about equally.
	built := widths()
	if built[1] <= 0 {
		t.Fatalf("the run came out %v", built)
	}

	// A maximum stops the middle one, and the others take what it turned down.
	if err := ui.Object("lmid").Set("max_width=40"); err != nil {
		t.Fatalf("max_width: %v", err)
	}
	capped := widths()
	if capped[1] != 40 {
		t.Errorf("with max_width=40 the middle box is %d wide", capped[1])
	}
	if capped[0] <= built[0] || capped[2] <= built[2] {
		t.Errorf("the boxes beside it are %v, no wider than the %v they were", capped, built)
	}

	// And a minimum beats it.
	if err := ui.Object("lmid").Set("min_width=160"); err != nil {
		t.Fatalf("min_width: %v", err)
	}
	if got := widths(); got[1] != 160 {
		t.Errorf("min_width=160 against max_width=40 gave %d, want the minimum", got[1])
	}
}

// The other half of the tab: a maximum stops a child filling its cell, and
// where it stops short its own alignment places it.
func TestLimitsTabShowsWhereACappedChildSits(t *testing.T) {
	ui, win := openLimits(t)
	capped, ok := ui.Object("lfill").Target().(*trinkets.Button)
	if !ok {
		t.Fatal("the Limits tab has no capped button behind key lfill")
	}
	at := func() (x, w int) {
		win.Layout()
		b := capped.Bounds()
		return int(b.X), int(b.Width)
	}

	// It fills its cell as far as its maximum and no further.
	_, width := at()
	if width != 120 {
		t.Fatalf("the capped button is %d wide, want its maximum of 120", width)
	}

	// Where it sits in what is left is its alignment's answer, and changing
	// that after the build reaches the layout.
	_ = ui.Object("lfill").Set("halign=textnatural")
	begin, _ := at()
	_ = ui.Object("lfill").Set("halign=textopposite")
	end, _ := at()
	_ = ui.Object("lfill").Set("halign=center")
	middle, _ := at()

	if !(begin < middle && middle < end) {
		t.Errorf("textnatural, center and textopposite put it at x=%d, %d and %d", begin, middle, end)
	}

	// Filling is what the maximum interrupted: turned off, the button falls
	// back to its own width, which is narrower than the cap.
	_ = ui.Object("lfill").Set("fill=none")
	_, natural := at()
	if natural >= 120 {
		t.Errorf("without filling the button is %d wide, want less than its 120 cap", natural)
	}
}
