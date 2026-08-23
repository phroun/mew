package trinkets

// The Event Viewer accessory, reached through Desktop Accessories in the
// system (Ψ) menu.
//
// Two things here are easy to get wrong and invisible if they are. Opening it
// twice must raise the one window rather than stacking a second; and because
// AddEventFilter has no counterpart, the filter it installs is permanent, so
// closing the window has to stop the logging by some other means than removing
// it.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// eventViewerItem finds the Event Viewer item in the system menu.
func eventViewerItem(t *testing.T, d *Desktop) *MenuItem {
	t.Helper()
	for _, it := range d.systemMenu.Items() {
		if strings.Contains(it.Text, "Event Viewer") {
			return it
		}
	}
	t.Fatal("Event Viewer not found in the system menu")
	return nil
}

// The accessories are a flat run under a disabled heading rather than a
// submenu. The heading must stay disabled - an enabled one would look like a
// command that does nothing - and the accessories under it must NOT be, which
// is the pairing this checks.
func TestDesktopAccessoriesHeadingAndItems(t *testing.T) {
	d := NewDesktop()

	items := d.systemMenu.Items()
	heading := -1
	for i, it := range items {
		if strings.Contains(it.Text, "Desktop Accessories") {
			heading = i
		}
	}
	if heading < 0 {
		t.Fatal("Desktop Accessories heading not found in the system menu")
	}
	if items[heading].Enabled {
		t.Error("the Desktop Accessories heading is enabled; it would read as a command")
	}
	if items[heading].SubMenu != nil {
		t.Error("the heading carries a submenu; the accessories are meant to be flat")
	}

	// The first accessory sits directly below the heading.
	if heading+1 >= len(items) {
		t.Fatal("nothing follows the Desktop Accessories heading")
	}
	first := items[heading+1]
	if !strings.Contains(first.Text, "Event Viewer") {
		t.Errorf("item below the heading is %q, want Event Viewer", first.Text)
	}
	if !first.Enabled {
		t.Error("Event Viewer is disabled")
	}
	if first.OnTriggered == nil {
		t.Error("Event Viewer item is not wired")
	}
}

func TestEventViewerOpensOnceAndLogs(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	viewer := eventViewerItem(t, d)

	viewer.OnTriggered()
	wins := d.windowManager.Windows()
	if len(wins) != 1 {
		t.Fatalf("opened %d windows, want 1", len(wins))
	}
	if got := wins[0].Title(); got != "Event Viewer" {
		t.Errorf("title = %q, want Event Viewer", got)
	}

	// Triggering again raises the same window rather than opening a second.
	viewer.OnTriggered()
	if got := len(d.windowManager.Windows()); got != 1 {
		t.Fatalf("second trigger left %d windows, want 1", got)
	}

	v := d.eventViewer
	if v == nil {
		t.Fatal("desktop is not tracking the open viewer")
	}

	// An event reaches the log through the desktop's filter, and is not
	// consumed on the way: the filter is an observer.
	if d.filterEvent(core.KeyPressEvent{Key: "a", Text: "a"}) {
		t.Error("the viewer's filter consumed the event")
	}
	if got := len(v.tree.RootItems()); got != 1 {
		t.Fatalf("logged %d rows, want 1", got)
	}

	// Mouse events are filtered out by default, so the noisy ones do not
	// bury the keystroke being looked for.
	d.filterEvent(core.MouseMoveEvent{X: 1, Y: 1})
	if got := len(v.tree.RootItems()); got != 1 {
		t.Errorf("mouse move logged with the filter off: %d rows", got)
	}
	v.showMouse = true
	d.filterEvent(core.MouseMoveEvent{X: 2, Y: 2})
	if got := len(v.tree.RootItems()); got != 2 {
		t.Errorf("mouse move not logged with the filter on: %d rows", got)
	}
}

// The log pans horizontally rather than squeezing, and every data column can
// be hidden from the [=] chooser.
//
// Both are about the same thing: the columns want more room than the window
// has. Fit mode would answer that by narrowing cells until they ellipsize,
// and "the field held something I cannot read" is the one answer a viewer
// that exists to report exactly what arrived must never give.
func TestEventViewerColumnsPanAndAreChoosable(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	eventViewerItem(t, d).OnTriggered()

	tree := d.eventViewer.tree
	if tree.fitWidth {
		t.Error("fit mode is on, so the log squeezes instead of panning")
	}

	// Natural widths have to exceed a plausible window, or panning is moot.
	natural := 0
	for _, c := range tree.Columns() {
		natural += c.Width
	}
	if natural <= 80 {
		t.Errorf("columns total %d cells; that fits, so nothing pans", natural)
	}

	// Every data column is in the chooser. The [=] button only appears when
	// at least one is, so this is also what makes it reachable at all.
	for _, c := range tree.Columns() {
		if !c.Optional {
			t.Errorf("column %q is not in the chooser and can never be hidden", c.ID)
		}
		if c.Hidden {
			t.Errorf("column %q starts hidden", c.ID)
		}
	}
	if _, ok := tree.chooserButtonRect(); !ok {
		t.Error("no [=] chooser button in the header")
	}
}

// The sequence is a data column rather than the key column, so it can declare
// Numeric and sort as a number. The key column cannot: it is not a TreeColumn,
// so it has nowhere to carry the flag (nor a SortProxy), and would order the
// log 1, 10, 11, 2.
func TestEventViewerSequenceSortsNumerically(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()
	eventViewerItem(t, d).OnTriggered()

	v := d.eventViewer
	tree := v.tree
	if tree.showKey {
		t.Error("the key column is shown; the log is flat and has no hierarchy for it")
	}

	var seq *TreeColumn
	for _, c := range tree.Columns() {
		if c.ID == "seq" {
			seq = c
		}
	}
	if seq == nil {
		t.Fatal("no seq column")
	}
	if !seq.Numeric {
		t.Error("seq is not numeric, so it sorts 1, 10, 11, 2")
	}
	if !seq.Sortable {
		t.Error("seq is not sortable, so the numeric flag never applies")
	}

	// Twelve rows, so a text sort would put 10, 11, 12 between 1 and 2.
	for i := 0; i < 12; i++ {
		v.log(core.KeyPressEvent{Key: "a", Text: "a"})
	}
	tree.SetSorted(true, tree.columnIndex(seq), false)

	var got []string
	for _, it := range tree.visualSiblings(tree.RootItems()) {
		got = append(got, it.Value("seq"))
	}
	want := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sorted ascending as %v, want %v", got, want)
		}
	}
}

// The viewer is opened OVER the program being watched, so a window that
// covers the desktop defeats the point. Its preferred size is what the tree's
// columns want, which is wider than most desktops - the cap is what decides
// the size in practice, and it is the part worth pinning.
func TestEventViewerFitsInsideTheDesktop(t *testing.T) {
	for _, size := range []core.UnitSize{
		{Width: 400, Height: 300},   // smaller than the preferred size
		{Width: 4000, Height: 3000}, // larger than it
	} {
		d := NewDesktop()
		d.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
		d.windowManager = window.NewWindowManager()
		d.windowManager.SetDesktop(d)

		area := d.windowManager.ClientArea()
		if area.Width <= 0 || area.Height <= 0 {
			t.Fatalf("client area is %dx%d; the cap would not be exercised",
				area.Width, area.Height)
		}

		eventViewerItem(t, d).OnTriggered()

		b := d.windowManager.Windows()[0].Bounds()
		if b.Width > area.Width || b.Height > area.Height {
			t.Errorf("in a %dx%d client area the viewer is %dx%d",
				area.Width, area.Height, b.Width, b.Height)
		}
		// And it stays on screen rather than being centred off the edge.
		if b.X < area.X || b.Y < area.Y ||
			b.X+b.Width > area.X+area.Width || b.Y+b.Height > area.Y+area.Height {
			t.Errorf("in a %dx%d client area the viewer sits at %d,%d %dx%d",
				area.Width, area.Height, b.X, b.Y, b.Width, b.Height)
		}
	}
}

// On a cell surface a window must sit on the grid AND be a whole number of
// cells across. The cap above is arithmetic on the client area and knows
// nothing about the cell size, so the sizes are the half that goes wrong -
// the origin is snapped explicitly and looks right while the extents do not.
func TestEventViewerIsCellAligned(t *testing.T) {
	// Sizes chosen so three quarters of the resulting client area is NOT a
	// whole number of cells on at least one axis.
	for _, size := range []core.UnitSize{
		{Width: 640, Height: 400},
		{Width: 1000, Height: 700},
		{Width: 1234, Height: 567},
	} {
		d := NewDesktop()
		d.SetBounds(core.UnitRect{Width: size.Width, Height: size.Height})
		d.windowManager = window.NewWindowManager()
		d.windowManager.SetDesktop(d)
		if d.windowManager.SmoothPositioning() {
			t.Skip("this manager positions smoothly; the grid rule does not apply")
		}
		m := d.EffectiveCellMetrics()

		eventViewerItem(t, d).OnTriggered()

		b := d.windowManager.Windows()[0].Bounds()
		for _, c := range []struct {
			name string
			v    core.Unit
			cell core.Unit
		}{
			{"width", b.Width, m.CellWidth},
			{"height", b.Height, m.CellHeight},
			{"x", b.X, m.CellWidth},
			{"y", b.Y, m.CellHeight},
		} {
			if c.v%c.cell != 0 {
				t.Errorf("on a %dx%d desktop the viewer's %s is %d, which is %d "+
					"past a %d-unit cell boundary",
					size.Width, size.Height, c.name, c.v, c.v%c.cell, c.cell)
			}
		}
	}
}

func TestEventViewerStopsLoggingWhenClosed(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	viewer := eventViewerItem(t, d)
	viewer.OnTriggered()
	first := d.eventViewer
	win := first.win

	win.Close()
	if d.eventViewer != nil {
		t.Fatal("closing the window did not clear the desktop's reference")
	}

	// The filter is still installed - there is no way to remove one - so the
	// thing being tested is that it now does nothing rather than logging into
	// a window nobody can see.
	before := len(first.tree.RootItems())
	d.filterEvent(core.KeyPressEvent{Key: "b", Text: "b"})
	if got := len(first.tree.RootItems()); got != before {
		t.Errorf("closed viewer still logging: %d rows, was %d", got, before)
	}

	// Opening it again builds a fresh viewer and logs into that one, without
	// installing a second filter.
	viewer.OnTriggered()
	second := d.eventViewer
	if second == nil || second == first {
		t.Fatal("reopening did not build a new viewer")
	}
	d.filterEvent(core.KeyPressEvent{Key: "c", Text: "c"})
	if got := len(second.tree.RootItems()); got != 1 {
		t.Errorf("reopened viewer logged %d rows, want 1", got)
	}
	if got := len(first.tree.RootItems()); got != before {
		t.Errorf("the closed viewer logged again: %d rows", got)
	}
}

// The whole content tree, not just the window frame.
//
// A cell surface draws through UnitsToCellX/Y, which integer-divides, so an
// off-grid trinket is DRAWN snapped while Contains still hit-tests it at its
// raw bounds. The two then disagree by up to a cell, and a click near the
// boundary resolves to the wrong trinket. Nothing enforces the rule (see the
// task on that), so this pins the viewer's own tree.
func TestEventViewerTreeIsCellAligned(t *testing.T) {
	d := NewDesktop()
	d.SetBounds(core.UnitRect{Width: 1000, Height: 700})
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetDesktop(d)
	m := d.EffectiveCellMetrics()

	eventViewerItem(t, d).OnTriggered()
	win := d.windowManager.Windows()[0]
	win.SetBounds(win.Bounds()) // settle the layout

	var walk func(tr core.Trinket, path string)
	walk = func(tr core.Trinket, path string) {
		if tr == nil {
			return
		}
		b := tr.Bounds()
		if b.X%m.CellWidth != 0 || b.Y%m.CellHeight != 0 ||
			b.Width%m.CellWidth != 0 || b.Height%m.CellHeight != 0 {
			t.Errorf("%s (%T) is off the %dx%d cell grid at %+v",
				path, tr, m.CellWidth, m.CellHeight, b)
		}
		if ct, ok := tr.(core.Container); ok {
			for i, c := range ct.Children() {
				walk(c, fmt.Sprintf("%s/%d", path, i))
			}
		}
	}
	walk(win.Content(), "content")
}

// ...and the snapping is a CELL-SURFACE rule only. A pixel surface places
// windows at unit granularity on purpose, so rounding there would throw away
// precision the graphical path exists to have. The gate is the manager's
// SmoothPositioning, which the desktop sets from the backend.
func TestEventViewerIsNotSnappedOnASmoothSurface(t *testing.T) {
	// A size whose three-quarters cap is deliberately NOT a whole cell.
	d := NewDesktop()
	d.SetBounds(core.UnitRect{Width: 1000, Height: 700})
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetDesktop(d)
	d.windowManager.SetSmoothPositioning(true)

	area := d.windowManager.ClientArea()
	m := d.EffectiveCellMetrics()

	eventViewerItem(t, d).OnTriggered()
	b := d.windowManager.Windows()[0].Bounds()

	// The cap is taken exactly, not rounded down to a cell.
	if want := area.Width * 3 / 4; b.Width != want {
		t.Errorf("width = %d, want the exact cap %d - a smooth surface must "+
			"not be snapped", b.Width, want)
	}
	if b.Width%m.CellWidth == 0 {
		t.Errorf("width %d happens to be cell-aligned, so this proves nothing; "+
			"pick a desktop size whose cap is not a multiple of %d",
			b.Width, m.CellWidth)
	}
}
