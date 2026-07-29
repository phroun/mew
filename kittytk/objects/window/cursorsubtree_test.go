package window

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// A CONTAINER that resolves the cursor from its own state must still be asked
// when it is the descent's ENTRY trinket. The entry-skip exists only to keep a
// delegating window from recursing forever; applied blindly it silently
// discards every shape such a container wanted, because the descent falls
// through to the position-less CursorShape fallback instead.
type subtreeShaper struct {
	core.TrinketBase
	asked bool
	shape core.CursorShape
}

func (s *subtreeShaper) CursorShapeAt(x, y core.Unit) core.CursorShape {
	s.asked = true
	return s.shape
}
func (s *subtreeShaper) CursorShapesSubtree() {}

// CursorShape is the fallback the descent would reach if CursorShapeAt were
// skipped — deliberately a DIFFERENT shape, so the test can tell them apart.
func (s *subtreeShaper) CursorShape() core.CursorShape { return core.CursorText }

// Container methods: this is the shape of trinket the guard mis-caught.
func (s *subtreeShaper) ChildAt(core.UnitPoint) core.Trinket   { return nil }
func (s *subtreeShaper) Children() []core.Trinket              { return nil }
func (s *subtreeShaper) AddChild(core.Trinket)                 {}
func (s *subtreeShaper) RemoveChild(core.Trinket)              {}
func (s *subtreeShaper) Layout()                               {}
func (s *subtreeShaper) LayoutManager() core.LayoutManager     { return nil }
func (s *subtreeShaper) SetLayoutManager(l core.LayoutManager) {}

func TestEntryContainerOwningItsSubtreeIsAsked(t *testing.T) {
	s := &subtreeShaper{shape: core.CursorDefault}
	if _, ok := interface{}(s).(core.Container); !ok {
		t.Skip("harness is not a Container; the guard under test would not apply")
	}
	got := cursorShapeAtTrinket(s, core.UnitPoint{X: 5, Y: 5})
	if !s.asked {
		t.Fatal("the entry container was never asked for its cursor")
	}
	if got != core.CursorDefault {
		t.Fatalf("cursor = %v, want the arrow the container resolved (not its fallback)", got)
	}
}
