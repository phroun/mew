package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/window"
)

// Replicates the demo's Selection-tab hierarchy: window → tabtrinket →
// panel(label + splitter). Toggling the window's denomination must not
// move anything in device space.
func TestWindowDenominationLayoutInvariance(t *testing.T) {
	win := window.NewWindow("t")

	tabs := NewTabTrinket()

	split := NewVSplitter()
	split.SetPosition(0.4)
	split.SetSizePolicy(core.NewSizePolicy(core.SizeExpanding, core.SizeExpanding))
	top := NewPanel()
	top.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	bottom := NewPanel()
	bottom.SetLayoutManager(layout.NewBoxLayout(core.Vertical))
	split.SetFirst(top)
	split.SetSecond(bottom)

	outer := NewPanel()
	ol := layout.NewBoxLayout(core.Vertical)
	lbl := NewLabel("a wrapped label occupying the top row of the page contents area")
	lbl.SetWordWrap(true)
	outer.AddChild(lbl)
	outer.AddChild(split)
	outer.SetLayoutManager(ol)
	ol.ItemAt(0).WithAlign(core.AlignFill)
	ol.ItemAt(1).WithAlign(core.AlignFill)

	tabs.AddTab("Sel", outer)
	win.SetContent(tabs)
	win.SetBounds(core.UnitRect{Width: 8 * 100, Height: 16 * 40})

	// Device-space geometry of the splitter's top pane: rows, in the
	// currency the trinkets actually live in.
	paneRows := func() (topRows, splitRows core.Unit) {
		split.Layout()
		m := core.FindEffectiveCellMetrics(split)
		return top.Bounds().Height / m.CellHeight,
			split.Bounds().Height / m.CellHeight
	}

	topBefore, splitBefore := paneRows()
	if splitBefore == 0 || topBefore == 0 {
		t.Fatalf("degenerate baseline: top=%d split=%d rows", topBefore, splitBefore)
	}

	dense := core.CellMetrics{CellWidth: 8, CellHeight: 32}
	win.SetCellMetrics(&dense)
	topAfter, splitAfter := paneRows()

	if splitAfter != splitBefore {
		t.Errorf("splitter height changed: %d -> %d rows", splitBefore, splitAfter)
	}
	if topAfter != topBefore {
		t.Errorf("top pane height changed: %d -> %d rows", topBefore, topAfter)
	}

	// And back again: exact restoration.
	win.SetCellMetrics(nil)
	topBack, splitBack := paneRows()
	if topBack != topBefore || splitBack != splitBefore {
		t.Errorf("toggle-off did not restore: top %d->%d, split %d->%d",
			topBefore, topBack, splitBefore, splitBack)
	}
}

// MDIPane boundary machinery: child-window geometry lives in the
// pane's interior denomination. Hit-testing takes outer coordinates
// and must agree with where windows actually sit; ClientArea (the
// window-positioning space) is interior; and toggling an override
// off restores the identity mapping.
func TestMDIPaneDenominationBoundary(t *testing.T) {
	pane := NewMDIPane()
	pane.SetBounds(core.UnitRect{Width: 8 * 80, Height: 16 * 20}) // outer: 640x320

	child := window.NewWindow("doc")
	child.SetBounds(core.UnitRect{X: 64, Y: 64, Width: 160, Height: 96})
	pane.AddWindow(child)
	// AddWindow may cascade-position; pin the geometry we assert on.
	child.SetBounds(core.UnitRect{X: 64, Y: 64, Width: 160, Height: 96})

	// Baseline (no override): outer == interior, identity mapping.
	if got := pane.ChildAt(core.UnitPoint{X: 70, Y: 70}); got != child {
		t.Fatalf("baseline hit: got %T", got)
	}
	if got := pane.ChildAt(core.UnitPoint{X: 70, Y: 200}); got == child {
		t.Fatal("baseline miss expected below the window")
	}
	baseArea := pane.ClientArea()
	if baseArea.Width != 640 || baseArea.Height != 320 {
		t.Fatalf("baseline client area = %+v", baseArea)
	}

	// Override: 32 units per row inside the pane (outer stays 16).
	dense := core.CellMetrics{CellWidth: 8, CellHeight: 32}
	pane.SetCellMetrics(&dense)

	// ClientArea re-denominates: same physical area, interior units.
	area := pane.ClientArea()
	if area.Width != 640 || area.Height != 640 {
		t.Errorf("override client area = %+v, want 640x640 interior units", area)
	}

	// The window sits at interior y=64 (2 rows of 32) = outer y=32
	// (2 rows of 16). Hit-testing must find it where it PAINTS:
	// outer y in [32, 80) maps into the window's interior y-range.
	if got := pane.ChildAt(core.UnitPoint{X: 70, Y: 40}); got != child {
		t.Errorf("override hit at outer y=40 failed")
	}
	// Outer y=70 is interior y=140 - past the window's bottom (160
	// ends at... 64+96=160, so interior 140 IS inside; outer y=90 ->
	// interior 180 is below).
	if got := pane.ChildAt(core.UnitPoint{X: 70, Y: 90}); got == child {
		t.Errorf("override miss at outer y=90 failed (interior 180 is below the window)")
	}

	// Maximized windows fill the INTERIOR client area.
	pane.MaximizeWindow(child)
	if b := child.Bounds(); b.Width != 640 || b.Height != 640 {
		t.Errorf("maximized bounds = %+v, want interior 640x640", b)
	}
	pane.RestoreWindow(child)

	// Toggle off: identity mapping again, geometry untouched.
	pane.SetCellMetrics(nil)
	child.SetBounds(core.UnitRect{X: 64, Y: 64, Width: 160, Height: 96})
	if got := pane.ChildAt(core.UnitPoint{X: 70, Y: 70}); got != child {
		t.Errorf("post-toggle hit failed")
	}
	backArea := pane.ClientArea()
	if backArea != baseArea {
		t.Errorf("client area did not restore: %+v vs %+v", backArea, baseArea)
	}
}
