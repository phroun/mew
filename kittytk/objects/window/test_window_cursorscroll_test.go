package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// scrollMock is a container that scrolls its single child by a unit
// offset, like a ScrollArea (implements core.ScrollOffsetUnitsProvider).
type scrollMock struct {
	*core.TrinketBase
	child      core.Trinket
	offX, offY core.Unit
}

func (m *scrollMock) Children() []core.Trinket            { return []core.Trinket{m.child} }
func (m *scrollMock) AddChild(core.Trinket)               {}
func (m *scrollMock) RemoveChild(core.Trinket)            {}
func (m *scrollMock) ChildAt(core.UnitPoint) core.Trinket { return m.child }
func (m *scrollMock) Layout()                             {}
func (m *scrollMock) LayoutManager() core.LayoutManager   { return nil }
func (m *scrollMock) SetLayoutManager(core.LayoutManager) {}
func (m *scrollMock) ScrollOffsetUnits() (core.Unit, core.Unit) {
	return m.offX, m.offY
}

// ibeamMock reports an I-beam cursor when hovered within its content
// coordinates (a text field only wants the I-beam over its glyph row).
type ibeamMock struct {
	*core.TrinketBase
	rect core.UnitRect
}

func (m *ibeamMock) CursorShape() core.CursorShape {
	return core.CursorText
}

// The cursor-shape descent must add a scroll container's offset exactly
// as the mouse-event descent does, so the I-beam region tracks the
// content as it scrolls instead of drifting.
func TestCursorShapeFollowsScroll(t *testing.T) {
	ib := &ibeamMock{TrinketBase: core.NewTrinketBase(), rect: core.UnitRect{Width: 200, Height: 16}}
	sc := &scrollMock{TrinketBase: core.NewTrinketBase(), child: ib}

	// Unscrolled: a hover at viewport y=0 lands on the child's top row.
	if got := cursorShapeAtTrinket(sc, core.UnitPoint{X: 10, Y: 0}); got != core.CursorText {
		t.Fatalf("unscrolled: cursor = %v, want I-beam", got)
	}

	// Scroll the content down by 40 units: the same viewport hover now
	// maps to content y=40. The descent must add the offset (mirroring
	// ScrollArea.HandleMouseMove), so the child still receives the hover.
	sc.offX, sc.offY = 0, 40
	if got := cursorShapeAtTrinket(sc, core.UnitPoint{X: 10, Y: 0}); got != core.CursorText {
		t.Errorf("scrolled: cursor = %v, want I-beam (offset must be added)", got)
	}
}

// plainContainer is a minimal container whose ChildAt returns its single
// child (no special coordinate transform).
type plainContainer struct {
	*core.TrinketBase
	child core.Trinket
}

func (m *plainContainer) Children() []core.Trinket            { return []core.Trinket{m.child} }
func (m *plainContainer) AddChild(core.Trinket)               {}
func (m *plainContainer) RemoveChild(core.Trinket)            {}
func (m *plainContainer) ChildAt(core.UnitPoint) core.Trinket { return m.child }
func (m *plainContainer) Layout()                             {}
func (m *plainContainer) LayoutManager() core.LayoutManager   { return nil }
func (m *plainContainer) SetLayoutManager(core.LayoutManager) {}

// shaperMock is a container that answers the cursor query itself
// (core.CursorShaper), like a nested window or MDI pane.
type shaperMock struct {
	*core.TrinketBase
	gotX, gotY core.Unit
	called     bool
}

func (m *shaperMock) CursorShapeAt(x, y core.Unit) core.CursorShape {
	m.called = true
	m.gotX, m.gotY = x, y
	return core.CursorText
}

// When the descent reaches a container that routes events specially
// (a nested window, an MDI pane), it must delegate the cursor query to
// that container's own CursorShapeAt with the child-local coordinate,
// rather than continue the generic descent (which can't reproduce the
// window/pane coordinate transform).
func TestCursorShapeDelegatesToShaper(t *testing.T) {
	sh := &shaperMock{TrinketBase: core.NewTrinketBase()}
	sh.SetBounds(core.UnitRect{X: 20, Y: 10, Width: 100, Height: 50})
	root := &plainContainer{TrinketBase: core.NewTrinketBase(), child: sh}

	got := cursorShapeAtTrinket(root, core.UnitPoint{X: 30, Y: 14})
	if !sh.called {
		t.Fatal("descent did not delegate to the CursorShaper child")
	}
	if got != core.CursorText {
		t.Errorf("cursor = %v, want the shaper's I-beam", got)
	}
	// The shaper received the point in ITS local space (minus its bounds).
	if sh.gotX != 10 || sh.gotY != 4 {
		t.Errorf("shaper got (%d,%d), want (10,4) (child-local)", sh.gotX, sh.gotY)
	}
}
