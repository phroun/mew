package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// defaultsSpecimens lays out the Defaults tab and returns the vbox holding one
// of each trinket, with the denomination it was laid out in.
func defaultsSpecimens(t *testing.T) ([]core.Trinket, core.CellMetrics) {
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

	idx := -1
	for i := 0; i < tabs.Count(); i++ {
		if tabs.TabText(i) == "Defaults" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatal("the main window has no Defaults tab")
	}
	tabs.SetCurrentIndex(idx)
	win.Layout()

	// The tab holds a scroll area holding the vbox: the only panel down there
	// with a specimen for every trinket in it.
	var vbox core.Container
	var dig func(core.Trinket)
	dig = func(tr core.Trinket) {
		if vbox != nil {
			return
		}
		if p, ok := tr.(*trinkets.Panel); ok && len(p.Children()) > 10 {
			vbox = p
			return
		}
		if c, ok := tr.(core.Container); ok {
			for _, k := range c.Children() {
				dig(k)
			}
		}
	}
	dig(tabs)
	if vbox == nil {
		t.Fatal("the Defaults tab has no vbox of specimens")
	}
	return vbox.Children(), core.FindEffectiveCellMetrics(vbox)
}

// fallbackSize is what a trinket that cannot size itself from its content asks
// for, in cells. A zero height means the height is not a fallback -- a field
// and a bar are one row, from their content.
type fallbackSize struct{ widthCells, heightCells core.Unit }

// The Defaults tab is where these are looked at, so it is where they are
// pinned. They are deliberately too small to use: a trinket laid out at one of
// them is a designer being told they forgot to give it a size. A tree asks for
// twice the width because it draws expand and collapse hardware beside the
// text; a tab strip asks for more again.
func TestDefaultsTabShowsTheFallbackSizes(t *testing.T) {
	want := map[string]fallbackSize{
		"*trinkets.TextInput":   {3, 0},
		"*trinkets.ProgressBar": {3, 0},
		"*trinkets.ListView":    {3, 3},
		"*trinkets.TreeView":    {6, 3},
		"*trinkets.ScrollArea":  {3, 5},
		"*trinkets.Panel":       {3, 5},
		"*trinkets.TabTrinket":  {12, 5},
		"*trinkets.MDIPane":     {3, 5},
		"*trinkets.Editor":      {12, 5},
		"*trinkets.PurfecTerm":  {12, 5},
	}

	specimens, m := defaultsSpecimens(t)
	seen := map[string]bool{}
	for _, k := range specimens {
		name := specimenName(k)
		size, ok := want[name]
		if !ok {
			continue
		}
		seen[name] = true

		b := k.Bounds()
		if b.Width != m.UnitsPerCellWidth*size.widthCells {
			t.Errorf("%s is %d units wide, want %d (%d cells)",
				name, b.Width, m.UnitsPerCellWidth*size.widthCells, size.widthCells)
		}
		if size.heightCells > 0 && b.Height != m.UnitsPerCellHeight*size.heightCells {
			t.Errorf("%s is %d units tall, want %d (%d cells)",
				name, b.Height, m.UnitsPerCellHeight*size.heightCells, size.heightCells)
		}
	}

	for name := range want {
		if !seen[name] {
			t.Errorf("the Defaults tab has no %s; it is meant to hold one of each", name)
		}
	}
}

// specimenName names the trinkets whose sizes this pins. Anything else in the
// tab -- the labels, and everything that sizes itself from its content -- is
// not one of them.
func specimenName(k core.Trinket) string {
	switch k.(type) {
	case *trinkets.TextInput:
		return "*trinkets.TextInput"
	case *trinkets.ProgressBar:
		return "*trinkets.ProgressBar"
	case *trinkets.ListView:
		return "*trinkets.ListView"
	case *trinkets.TreeView:
		return "*trinkets.TreeView"
	case *trinkets.ScrollArea:
		return "*trinkets.ScrollArea"
	case *trinkets.Panel:
		return "*trinkets.Panel"
	case *trinkets.TabTrinket:
		return "*trinkets.TabTrinket"
	case *trinkets.MDIPane:
		return "*trinkets.MDIPane"
	case *trinkets.Editor:
		return "*trinkets.Editor"
	case *trinkets.PurfecTerm:
		return "*trinkets.PurfecTerm"
	}
	return ""
}
