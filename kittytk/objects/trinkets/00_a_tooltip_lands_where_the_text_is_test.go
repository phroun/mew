package trinkets

// Where the classic tooltip is drawn, and in whose coordinates.

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/layout"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/style"
)

// drawnRects remembers where a painter was asked to put a box and its text.
type drawnRects struct {
	core.RenderBackend
	rects []core.UnitRect
	texts []core.UnitPoint
}

func (d *drawnRects) FillRect(r core.UnitRect, ch rune, s style.CellStyle) {
	d.rects = append(d.rects, r)
	d.RenderBackend.FillRect(r, ch, s)
}

func (d *drawnRects) DrawRect(r core.UnitRect, b style.BorderStyle, s style.CellStyle) {
	d.rects = append(d.rects, r)
	d.RenderBackend.DrawRect(r, b, s)
}

func (d *drawnRects) DrawRoundedRect(r core.UnitRect, radius core.Unit, b style.BorderStyle, s style.CellStyle) {
	d.rects = append(d.rects, r)
	if rd, ok := d.RenderBackend.(core.RoundedRectDrawer); ok {
		rd.DrawRoundedRect(r, radius, b, s)
	}
}

func (d *drawnRects) StrokeRoundedRect(r core.UnitRect, radius core.Unit, b style.BorderStyle, s style.CellStyle) {
	if rd, ok := d.RenderBackend.(core.RoundedRectDrawer); ok {
		rd.StrokeRoundedRect(r, radius, b, s)
	}
}

func (d *drawnRects) DrawText(x, y core.Unit, text string, s style.CellStyle, font *core.Font) core.Unit {
	d.texts = append(d.texts, core.UnitPoint{X: x, Y: y})
	return d.RenderBackend.DrawText(x, y, text, s, font)
}

// onScreen is a graphical desktop with a window in it, and the label inside
// that window that a tooltip will be asked for.
func onScreen(t *testing.T) (*Desktop, *window.WindowManager, *Label) {
	t.Helper()
	px, err := raster.New(900, 600)
	if err != nil {
		t.Skip("no raster backend:", err)
	}
	core.SetTextMeasurer(px)
	t.Cleanup(func() { core.SetTextMeasurer(nil) })

	d := NewDesktop()
	d.SetBackend(px)
	d.SetBounds(core.UnitRect{Width: 900, Height: 600})
	d.windowManager = window.NewWindowManager()
	d.windowManager.SetDesktop(d)
	d.windowManager.SetScreenBounds(core.UnitRect{Width: 900, Height: 600})

	win := window.NewWindow("Connections")
	panel := NewPanel()
	panel.SetLayoutManager(layout.NewVBoxLayout())
	label := NewLabel("x")
	panel.AddChild(label)
	win.SetContent(panel)
	win.SetBounds(core.UnitRect{X: 300, Y: 200, Width: 400, Height: 300})
	d.windowManager.AddWindow(win)
	win.Layout()
	return d, d.windowManager, label
}

// tooltipOverlay is the tooltip the popup layer is holding, if any.
func tooltipOverlay(wm *window.WindowManager) *window.PopupOverlay {
	for _, p := range wm.GetPopups() {
		if o, ok := p.(*window.PopupOverlay); ok && o.ID == tooltipPopupID {
			return o
		}
	}
	return nil
}

// The popup layer hands every overlay the SCREEN's painter, not one moved to
// the overlay -- so a tooltip that drew from zero would draw in the corner
// instead of beside the text it stands for.
func TestATooltipIsDrawnWhereItWasPlaced(t *testing.T) {
	d, wm, label := onScreen(t)

	if !d.ShowTooltip(core.TooltipRequest{
		Text: "the whole fingerprint",
		From: label,
		At:   core.UnitRect{Width: label.Bounds().Width, Height: label.Bounds().Height},
	}) {
		t.Fatal("the desktop took nothing")
	}
	overlay := tooltipOverlay(wm)
	if overlay == nil {
		t.Fatal("a graphical desktop raised no popup")
	}
	box := overlay.Bounds
	if box.X < 300 || box.Y < 200 {
		t.Fatalf("the tooltip sits at %d,%d, above and left of the window it belongs to", box.X, box.Y)
	}

	rec := &drawnRects{RenderBackend: d.backend}
	overlay.Paint(core.NewPainter(rec))

	if len(rec.rects) == 0 {
		t.Fatal("the tooltip drew no box at all")
	}
	if got := rec.rects[0]; got.X != box.X || got.Y != box.Y {
		t.Errorf("the box was drawn at %d,%d but placed at %d,%d", got.X, got.Y, box.X, box.Y)
	}
	if len(rec.texts) == 0 {
		t.Fatal("the tooltip drew no text")
	}
	if at := rec.texts[0]; at.X < box.X || at.Y < box.Y {
		t.Errorf("the text was drawn at %d,%d, outside the box at %d,%d", at.X, at.Y, box.X, box.Y)
	}
}

// A tooltip lies over the very text it stands for, so the pointer passes
// through it: pressing there presses the text, not the note about it. And the
// press takes the note down, on both sides of the bookkeeping.
func TestPressingThroughATooltipReachesWhatItLiesOver(t *testing.T) {
	d, wm, label := onScreen(t)

	// A button standing where the tooltip will be drawn, asking for the note
	// directly over itself the way a tree cell does.
	pressed := false
	btn := NewButton("Forget")
	btn.SetOnClick(func() { pressed = true })
	btn.SetTooltipSide(core.TooltipOver)
	label.Parent().(*Panel).AddChild(btn)
	wm.Windows()[0].Layout()

	d.ShowTooltip(core.TooltipRequest{
		Text: "the whole fingerprint",
		From: btn,
		At:   core.UnitRect{Width: btn.Bounds().Width, Height: btn.Bounds().Height},
		Side: core.TooltipOver,
	})
	overlay := tooltipOverlay(wm)
	if overlay == nil {
		t.Fatal("a graphical desktop raised no popup")
	}
	at := core.UnitPoint{X: overlay.Bounds.X + 4, Y: overlay.Bounds.Y + 4}

	wm.HandleMousePress(core.MousePressEvent{X: at.X, Y: at.Y, Button: core.LeftButton})
	wm.HandleMouseRelease(core.MouseReleaseEvent{X: at.X, Y: at.Y, Button: core.LeftButton})

	if !pressed {
		t.Error("the tooltip swallowed a press meant for the button under it")
	}
	if tooltipOverlay(wm) != nil {
		t.Error("the press landed and the tooltip is still on the screen")
	}
	if showing := d.TooltipShowing(); showing != "" {
		t.Errorf("the layer dropped the tooltip and the desktop still believes it shows %q", showing)
	}
}

// And it leaves the popup layer when it is withdrawn.
func TestATooltipLeavesThePopupLayer(t *testing.T) {
	d, wm, label := onScreen(t)

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})
	if tooltipOverlay(wm) == nil {
		t.Fatal("a graphical desktop raised no popup")
	}
	d.HideTooltip(label)
	if tooltipOverlay(wm) != nil {
		t.Error("the tooltip was withdrawn and the popup layer is still holding it")
	}
}
