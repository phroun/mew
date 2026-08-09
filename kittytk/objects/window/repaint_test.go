package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Window is the unit the GPU compositor caches a texture for, so it has
// to report a repaint revision at all.
func TestWindowIsASubtreeRepaintTracker(t *testing.T) {
	var _ core.SubtreeRepaintTracker = NewWindow("w")
}

// A change anywhere inside a window must move that window's revision, or
// the compositor keeps showing the texture it painted before the change.
func TestWindowRevisionMovesForContentChanges(t *testing.T) {
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{Width: 200, Height: 120})

	content := core.NewTrinketBase()
	content.Init(content)
	win.SetContent(content)

	for _, tc := range []struct {
		name   string
		mutate func()
	}{
		{"a trinket inside asks for a repaint", content.Update},
		{"a trinket inside moves", func() { content.SetPos(core.UnitPoint{X: 4, Y: 4}) }},
		{"a trinket inside is hidden", func() { content.SetVisible(false) }},
		{"the window is activated", func() { win.SetActive(true) }},
		{"the window is deactivated", func() { win.SetActive(false) }},
		{"the tear halo goes up", func() { win.SetTearHighlight(true) }},
		{"a resize edge is hovered", func() {
			win.SetResizeHoverRects([]core.UnitRect{{Width: 6, Height: 120}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := win.SubtreeRepaintRevision()
			tc.mutate()
			if win.SubtreeRepaintRevision() == before {
				t.Errorf("revision did not move; the compositor would keep a stale texture")
			}
		})
	}
}

// A window that nothing has touched must NOT move its revision — that is
// the whole point, and a revision that drifts on its own would repaint
// every window every frame exactly as before.
func TestWindowRevisionHoldsStillWhenNothingChanges(t *testing.T) {
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{Width: 200, Height: 120})

	rev := win.SubtreeRepaintRevision()

	// Reads, and a hover setter given the value it already holds.
	_ = win.Bounds()
	_ = win.Title()
	_ = win.IsMinimized()
	win.SetResizeHoverRects(nil)

	if got := win.SubtreeRepaintRevision(); got != rev {
		t.Errorf("revision moved from %d to %d with nothing changed", rev, got)
	}
}

// Moving a window is not a content change. The compositor rewrites every
// window's position uniforms each frame, so a drag should repaint no
// textures at all — the single biggest thing this cache buys.
func TestWindowMoveIsNotAContentChange(t *testing.T) {
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{X: 0, Y: 0, Width: 200, Height: 120})

	rev := win.SubtreeRepaintRevision()
	win.SetBounds(core.UnitRect{X: 40, Y: 25, Width: 200, Height: 120})
	if got := win.SubtreeRepaintRevision(); got != rev {
		t.Errorf("a pure move moved the revision from %d to %d; "+
			"every drag frame would repaint the window's whole texture", rev, got)
	}

	// A resize is a content change, though: the window draws different
	// pixels at a different size.
	win.SetBounds(core.UnitRect{X: 40, Y: 25, Width: 260, Height: 120})
	if win.SubtreeRepaintRevision() == rev {
		t.Error("a resize did not move the revision")
	}
}

// Moving a trinket INSIDE a window is a different matter: the window's
// own pixels include that trinket, so the window must repaint even
// though the trinket itself draws the same thing.
func TestInnerTrinketMoveRepaintsTheWindow(t *testing.T) {
	win := NewWindow("w")
	win.SetBounds(core.UnitRect{Width: 200, Height: 120})

	content := core.NewTrinketBase()
	content.Init(content)
	win.SetContent(content)

	rev := win.SubtreeRepaintRevision()
	content.SetPos(core.UnitPoint{X: 12, Y: 8})
	if win.SubtreeRepaintRevision() == rev {
		t.Error("a trinket moving inside the window did not move the window's revision")
	}
}
