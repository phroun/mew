package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
)

// Through a whole drag, the pointer stays on the part of the divider it
// grabbed.
//
// Under the kitty protocol the pointer reports where it is INSIDE a cell. A
// cell surface can only put the divider on a cell boundary, so the sub-cell
// part of the pointer and of the grab is carried into the split ratio and
// then rounded away again when the divider is placed -- and the divider
// trails the pointer by a fraction that changes as it crosses cells.
func TestASplitterKeepsItsGrip(t *testing.T) {
	for _, c := range []struct {
		name        string
		orientation core.Orientation
	}{
		{"horizontal", core.Horizontal},
		{"vertical", core.Vertical},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := NewSplitter(c.orientation)
			s.SetBounds(core.UnitRect{Width: 640, Height: 480})
			s.SetFirst(NewPanel())
			s.SetSecond(NewPanel())
			s.SetPosition(0.5)

			m := s.EffectiveCellMetrics()
			cell := m.UnitsPerCellWidth
			if c.orientation != core.Horizontal {
				cell = m.UnitsPerCellHeight
			}

			// Grab the divider partway into its own cell -- a place only a
			// sub-cell pointer can report.
			d := s.dividerBounds()
			at := core.UnitPoint{X: d.X + 3, Y: d.Y + 8}
			if c.orientation != core.Horizontal {
				at = core.UnitPoint{X: d.X + 8, Y: d.Y + 3}
			}
			s.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
			if !s.dragging {
				t.Fatal("the press did not grab the divider")
			}

			grip := func(p core.UnitPoint) int {
				b := s.dividerBounds()
				if c.orientation == core.Horizontal {
					return m.UnitsToCellX(p.X) - m.UnitsToCellX(b.X)
				}
				return m.UnitsToCellY(p.Y) - m.UnitsToCellY(b.Y)
			}
			want := grip(at)

			// Along three cells, a unit at a time.
			for i := core.Unit(1); i <= 3*cell; i++ {
				p := core.UnitPoint{X: at.X + i, Y: at.Y}
				if c.orientation != core.Horizontal {
					p = core.UnitPoint{X: at.X, Y: at.Y + i}
				}
				s.HandleMouseMove(core.MouseMoveEvent{X: p.X, Y: p.Y, Buttons: core.LeftButton})
				if got := grip(p); got != want {
					t.Fatalf("%d units in, the pointer is %d cells from the divider's edge; it grabbed %d",
						i, got, want)
				}
			}
			s.HandleMouseRelease(core.MouseReleaseEvent{Button: core.LeftButton})
		})
	}
}
