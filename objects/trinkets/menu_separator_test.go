package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
)

// gfxFrameParent is a minimal Container that reports graphical window
// frames, so a child Menu takes the pixel-surface separator path.
type gfxFrameParent struct{ core.TrinketBase }

func (g *gfxFrameParent) GraphicalWindowFrames() bool         { return true }
func (g *gfxFrameParent) Children() []core.Trinket            { return nil }
func (g *gfxFrameParent) AddChild(core.Trinket)               {}
func (g *gfxFrameParent) RemoveChild(core.Trinket)            {}
func (g *gfxFrameParent) ChildAt(core.UnitPoint) core.Trinket { return nil }
func (g *gfxFrameParent) Layout()                             {}
func (g *gfxFrameParent) LayoutManager() core.LayoutManager   { return nil }
func (g *gfxFrameParent) SetLayoutManager(core.LayoutManager) {}

func newGraphicalMenu() *Menu {
	gp := &gfxFrameParent{}
	gp.TrinketBase = *core.NewTrinketBase()
	gp.Init(gp)
	m := NewMenu("t")
	m.SetParent(gp)
	m.AddItem(NewMenuItem("New"))
	m.AddItem(NewMenuItem("Open"))
	m.AddSeparator()
	m.AddItem(NewMenuItem("Quit"))
	m.Show(0, 0)
	return m
}

// On a graphical surface a separator row is a thin band, so the menu is
// shorter than four full text rows, and Y coordinates still map to the
// right items across the thin separator.
func TestMenuGraphicalSeparatorLayout(t *testing.T) {
	m := newGraphicalMenu()
	cellH := m.EffectiveCellMetrics().CellHeight

	// Height = 3 text rows + 1 thin separator band, not 4 full rows.
	if got, full := m.calculateSize().Height, cellH*4; got >= full {
		t.Errorf("graphical menu height = %d, want < %d (thin separator)", got, full)
	}
	if got, want := m.calculateSize().Height, cellH*3+separatorBandUnits; got != want {
		t.Errorf("graphical menu height = %d, want %d", got, want)
	}

	// hitRow maps Y to the right item across the thin separator band.
	// Rows: New [0,cellH), Open [cellH,2cellH), sep [2cellH,2cellH+band),
	// Quit [2cellH+band, ...).
	quitTop := cellH*2 + separatorBandUnits
	if kind, idx := m.hitRow(quitTop + 2); kind != 3 || idx != 3 {
		t.Errorf("hitRow at Quit = (%d,%d), want (3,3)", kind, idx)
	}
	if kind, idx := m.hitRow(cellH*2 + 1); kind != 3 || idx != 2 {
		t.Errorf("hitRow in separator band = (%d,%d), want (3,2 separator)", kind, idx)
	}
	if kind, idx := m.hitRow(cellH + 1); kind != 3 || idx != 1 {
		t.Errorf("hitRow at Open = (%d,%d), want (3,1)", kind, idx)
	}
}

// A real menu-bar dropdown is NOT parented into the trinket tree, so
// FindGraphicalFrames can't see the surface. Painting on a pixel
// backend must still switch it to the thin-separator layout (regression
// for the dropdown that kept rendering full-height dashed separators).
func TestMenuUnparentedGraphicalAfterPaint(t *testing.T) {
	b, err := raster.New(320, 320)
	if err != nil {
		t.Fatal(err)
	}
	core.SetTextMeasurer(b)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	m := NewMenu("t") // no parent, like a dropdown
	m.AddItem(NewMenuItem("New"))
	m.AddItem(NewMenuItem("Open"))
	m.AddSeparator()
	m.AddItem(NewMenuItem("Quit"))
	m.Show(0, 0)
	cellH := m.EffectiveCellMetrics().CellHeight

	// Before any paint the orphan can't tell it's graphical.
	if got := m.calculateSize().Height; got != cellH*4 {
		t.Fatalf("pre-paint height = %d, want %d (cell layout)", got, cellH*4)
	}

	m.Paint(core.NewPainter(b))

	// After painting on the pixel backend the separator is a thin band.
	if got, want := m.calculateSize().Height, cellH*3+separatorBandUnits; got != want {
		t.Errorf("post-paint height = %d, want %d (thin separator)", got, want)
	}
}
