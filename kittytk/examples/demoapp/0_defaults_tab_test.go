package main

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/inprocess"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
)

// defaultsSpecimens lays out the Defaults tab and returns the vbox holding one
// of each trinket, plus the denomination it was laid out in.
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

	// The tab holds a scroll area holding the vbox: the only panel down
	// there with a specimen for every trinket in it.
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

// The Defaults tab is where the fallback size is looked at, so it is also
// where it is pinned. A trinket with nothing to derive a size from lands at
// three cells -- deliberately too small to use, so a designer who forgot to
// set one is told. Change defaultSizeCells and this is the test that says the
// demo's canonical view of it moved.
func TestDefaultsTabShowsTheFallbackSize(t *testing.T) {
	specimens, m := defaultsSpecimens(t)

	// The trinkets that cannot size themselves from their content, and
	// whether the fallback covers the height too.
	wantHeight := map[string]bool{"*trinkets.ListView": true, "*trinkets.TreeView": true}
	seen := map[string]bool{}

	for _, k := range specimens {
		name := typeName(k)
		if _, ok := wantHeight[name]; !ok && !fallbackWidthOnly[name] {
			continue
		}
		seen[name] = true
		b := k.Bounds()
		if b.Width != m.CellWidth*3 {
			t.Errorf("%s is %d units wide, want %d (three cells)", name, b.Width, m.CellWidth*3)
		}
		if wantHeight[name] && b.Height != m.CellHeight*3 {
			t.Errorf("%s is %d units tall, want %d (three cells)", name, b.Height, m.CellHeight*3)
		}
	}

	for name := range fallbackWidthOnly {
		if !seen[name] {
			t.Errorf("the Defaults tab has no %s; it is meant to hold one of each", name)
		}
	}
	for name := range wantHeight {
		if !seen[name] {
			t.Errorf("the Defaults tab has no %s; it is meant to hold one of each", name)
		}
	}
}

// fallbackWidthOnly are the trinkets whose width falls back but whose height
// comes from somewhere else (a row, or a row count this change did not touch).
var fallbackWidthOnly = map[string]bool{
	"*trinkets.TextInput":   true,
	"*trinkets.ProgressBar": true,
	"*trinkets.ScrollArea":  true,
	"*trinkets.TabTrinket":  true,
	"*trinkets.Panel":       true,
}

func typeName(k core.Trinket) string {
	switch k.(type) {
	case *trinkets.TextInput:
		return "*trinkets.TextInput"
	case *trinkets.ProgressBar:
		return "*trinkets.ProgressBar"
	case *trinkets.ScrollArea:
		return "*trinkets.ScrollArea"
	case *trinkets.TabTrinket:
		return "*trinkets.TabTrinket"
	case *trinkets.Panel:
		return "*trinkets.Panel"
	case *trinkets.ListView:
		return "*trinkets.ListView"
	case *trinkets.TreeView:
		return "*trinkets.TreeView"
	}
	return ""
}
