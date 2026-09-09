package trinkets

// Where the classic tooltip is drawn, and in whose coordinates.

import (
	"testing"
	"time"

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
	// These tests are about what a note looks like and where it goes, not
	// about how long the pointer waits for it: the dwell is its own test.
	d.tooltipDwell = time.Nanosecond
	d.tooltipFade = time.Nanosecond

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
	d.ProcessTimers()
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
	d.ProcessTimers()
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

// The compositor never calls the popup layer: it is handed the overlays with
// the rest of the frame and draws each on a layer of its own. A tooltip has to
// be in that hand-off, or a composited host shows nothing however well the
// software path draws it.
func TestATooltipReachesTheCompositor(t *testing.T) {
	d, _, label := onScreen(t)

	d.ShowTooltip(core.TooltipRequest{
		Text: "the whole fingerprint",
		From: label,
		At:   core.UnitRect{Width: label.Bounds().Width, Height: label.Bounds().Height},
	})
	d.ProcessTimers()

	list := d.GetChildWindows()
	if list == nil {
		t.Fatal("the desktop offered the compositor no frame at all")
	}
	var found *window.PopupOverlay
	for _, p := range list.Popups {
		if o, ok := p.(*window.PopupOverlay); ok && o.ID == tooltipPopupID {
			found = o
		}
	}
	if found == nil {
		t.Fatal("the frame the compositor is given holds no tooltip")
	}
	// The compositor skips an overlay with no area and one with no paint,
	// and sizes the layer's texture from these bounds.
	if found.Bounds.Width <= 0 || found.Bounds.Height <= 0 {
		t.Errorf("the tooltip layer is %dx%d, which the compositor drops", found.Bounds.Width, found.Bounds.Height)
	}
	if found.Paint == nil {
		t.Error("the tooltip layer has nothing to paint it")
	}
}

// The compositor unions a popup's anchor into its drop shadow, so a drop-down
// and the control it opened from cast one shape. A tooltip is not one piece
// with anything: an anchor would draw a shadow around the very text it is
// explaining.
func TestATooltipCastsNoShadowOverTheTextItExplains(t *testing.T) {
	_, wm, label := onScreen(t)

	overlay := showAndCatch(t, wm, label)
	if !overlay.Anchor.IsEmpty() {
		t.Errorf("the tooltip named %+v as its anchor, which the shadow would take in", overlay.Anchor)
	}
}

// A tooltip is read among the dropdowns and context menus, so it is drawn at
// their size: [window] menu_scale, not the full body face.
func TestATooltipIsDrawnAtTheMenuSize(t *testing.T) {
	_, wm, label := onScreen(t)
	full := showAndCatch(t, wm, label).Bounds

	core.SetMenuScale(0.5)
	t.Cleanup(func() { core.SetMenuScale(1) })
	small := showAndCatch(t, wm, label).Bounds

	if small.Width >= full.Width || small.Height >= full.Height {
		t.Errorf("at half the menu scale the note is %dx%d, no smaller than %dx%d",
			small.Width, small.Height, full.Width, full.Height)
	}
}

// showAndCatch raises one tooltip over the label and hands back the overlay.
func showAndCatch(t *testing.T, wm *window.WindowManager, from *Label) *window.PopupOverlay {
	t.Helper()
	d := from.Parent().Parent().Parent().(*Desktop)
	d.HideTooltip(nil)
	d.ShowTooltip(core.TooltipRequest{
		Text: "the whole fingerprint",
		From: from,
		At:   core.UnitRect{Width: from.Bounds().Width, Height: from.Bounds().Height},
	})
	d.ProcessTimers()
	o := tooltipOverlay(wm)
	if o == nil {
		t.Fatal("a graphical desktop raised no popup")
	}
	return o
}

// ownLayer is a popup layer of a window's own, standing in for the surface a
// torn-out window is drawn on.
type ownLayer struct {
	got    *core.PopupRequest
	gone   []string
	screen core.UnitRect
}

func (c *ownLayer) RegisterPopup(r *core.PopupRequest) { c.got = r }
func (c *ownLayer) UnregisterPopup(id string)          { c.gone = append(c.gone, id) }
func (c *ownLayer) ScreenBounds() core.UnitRect {
	if c.screen.Width > 0 {
		return c.screen
	}
	return core.UnitRect{Width: 900, Height: 600}
}
func (c *ownLayer) MapToScreen(t core.Trinket, local core.UnitPoint) core.UnitPoint {
	return local
}

// A window torn out onto a surface of its own carries its own popup layer. A
// note about something inside it belongs on that surface -- drawing it on the
// desktop's layer puts it on a different screen from the text it explains.
func TestATornOutWindowKeepsItsTooltipOnItsOwnSurface(t *testing.T) {
	d, wm, label := onScreen(t)

	// Tearing a window out stamps the new surface's layer onto the window
	// and everything in it, which is what makes the trinket the thing to
	// ask -- the desktop's own manager is the wrong answer for it now.
	layer := &ownLayer{}
	wm.Windows()[0].SetPopupController(layer)
	label.Parent().(*Panel).SetPopupController(layer)
	label.SetPopupController(layer)

	d.ShowTooltip(core.TooltipRequest{
		Text: "the whole fingerprint",
		From: label,
		At:   core.UnitRect{Width: label.Bounds().Width, Height: label.Bounds().Height},
	})
	d.ProcessTimers()

	if layer.got == nil {
		t.Fatal("the note went somewhere other than the window's own layer")
	}
	if tooltipOverlay(wm) != nil {
		t.Error("the note was also put on the desktop the window was torn out of")
	}

	// And it is withdrawn from the layer it was raised into.
	d.HideTooltip(label)
	d.ProcessTimers()
	if len(layer.gone) != 1 || layer.gone[0] != tooltipPopupID {
		t.Errorf("the window's layer was told to drop %v", layer.gone)
	}
}

// And it leaves the popup layer when it is withdrawn.
func TestATooltipLeavesThePopupLayer(t *testing.T) {
	d, wm, label := onScreen(t)

	d.ShowTooltip(core.TooltipRequest{Text: "the whole fingerprint", From: label})
	d.ProcessTimers()
	if tooltipOverlay(wm) == nil {
		t.Fatal("a graphical desktop raised no popup")
	}
	d.HideTooltip(label)
	d.ProcessTimers()
	if tooltipOverlay(wm) != nil {
		t.Error("the tooltip was withdrawn and the popup layer is still holding it")
	}
}
