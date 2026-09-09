package trinkets

// The desktop is the last handler in the tooltip chain: whatever nothing else
// took, it shows.
//
// What it shows depends on the surface. A cell target lends its status bar --
// the one row on the screen already meant for a word about what the pointer is
// on -- and puts back what was there when the tooltip goes. A pixel target
// raises the classic tooltip: a small note in its own colors, laid over the
// screen near the text it stands for.
//
// Either way it goes the moment the pointer moves off, and any keystroke takes
// it down: a tooltip is an answer to a pointer resting somewhere, and a reader
// who has started typing is no longer resting.

import (
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// tooltipPopupID names the desktop's one tooltip in the popup layer. There is
// only ever one: a second would be a second answer to the same pointer.
const tooltipPopupID = "kittytk.tooltip"

// tooltipWrapCells is how wide a tooltip is allowed to grow before its text
// wraps. Long enough for a path or a fingerprint, short enough that a
// paragraph does not become one line across the screen.
const tooltipWrapCells = 60

// desktopTooltip is what is on the screen for whom.
type desktopTooltip struct {
	from  core.Trinket
	lines []string
	inBar bool   // the status bar is showing it
	saved string // and this is what it was saying before
	popup bool   // the popup layer is showing it
}

// ShowTooltip implements core.TooltipHandler: the desktop takes anything that
// reached it, which is everything nothing else wanted.
func (d *Desktop) ShowTooltip(req core.TooltipRequest) bool {
	if strings.TrimSpace(req.Text) == "" {
		return false
	}
	d.HideTooltip(nil)

	lines := tooltipLines(req.Text, d.tooltipWrapWidth())
	d.tooltip = &desktopTooltip{from: req.From, lines: lines}

	if d.graphicalSurface() {
		if d.raiseTooltipPopup(req, lines) {
			d.tooltip.popup = true
			d.RequestUpdate()
			return true
		}
	}
	// No popup layer, or a cell surface: the status bar says it instead.
	if bar := d.StatusBar(); bar != nil {
		d.tooltip.saved = bar.Text()
		d.tooltip.inBar = true
		bar.SetText(tooltipStatusText(req.Text))
		d.RequestUpdate()
		return true
	}
	d.tooltip = nil
	return false
}

// HideTooltip implements core.TooltipHandler. A nil asker means every tooltip,
// which is what a keystroke asks for.
func (d *Desktop) HideTooltip(from core.Trinket) {
	t := d.tooltip
	if t == nil || (from != nil && t.from != from) {
		return
	}
	d.tooltip = nil
	if t.inBar {
		if bar := d.StatusBar(); bar != nil {
			bar.SetText(t.saved)
		}
	}
	if t.popup {
		if pc := d.popupHost(); pc != nil {
			pc.UnregisterPopup(tooltipPopupID)
		}
	}
	d.RequestUpdate()
}

// forgetTooltipPopup is what the desktop does when the popup layer discards
// the tooltip for it: let go of it without asking the layer to drop something
// it has already dropped.
func (d *Desktop) forgetTooltipPopup() {
	t := d.tooltip
	if t == nil || !t.popup {
		return
	}
	d.tooltip = nil
	d.RequestUpdate()
}

// TooltipShowing is what the desktop is currently saying, for a test or a host
// that wants to know.
func (d *Desktop) TooltipShowing() string {
	if d.tooltip == nil {
		return ""
	}
	return strings.Join(d.tooltip.lines, "\n")
}

// graphicalSurface reports whether this desktop draws glyphs rather than
// cells, which is what decides between a popup and the status bar.
func (d *Desktop) graphicalSurface() bool {
	gm, ok := d.backend.(core.GraphicalModer)
	return ok && gm.GraphicalMode()
}

// popupHost is the layer a tooltip is raised into: the window manager, which
// is what holds every other popup on the screen.
func (d *Desktop) popupHost() core.PopupController {
	if d.windowManager == nil {
		return nil
	}
	if pc, ok := any(d.windowManager).(core.PopupController); ok {
		return pc
	}
	return nil
}

// tooltipWrapWidth is the widest a tooltip line may be, in units.
func (d *Desktop) tooltipWrapWidth() core.Unit {
	metrics := d.EffectiveCellMetrics()
	w := core.Unit(tooltipWrapCells) * metrics.UnitsPerCellWidth
	if b := d.Bounds(); b.Width > 0 && w > b.Width-metrics.UnitsPerCellWidth*4 {
		w = b.Width - metrics.UnitsPerCellWidth*4
	}
	return w
}

// tooltipLines is the text as it will be shown: broken where it says to break,
// and wrapped where a line is longer than a tooltip may be.
func tooltipLines(text string, width core.Unit) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapText(line, width, core.DefaultFont(), core.DefaultCellMetrics())...)
	}
	if len(out) == 0 {
		out = []string{""}
	}
	return out
}

// tooltipStatusText is the tooltip as one row. The status bar has a single
// line, so a break in the text becomes a separator rather than disappearing,
// and a run of them -- the blank line between two paragraphs -- reads as the
// one break it looks like.
func tooltipStatusText(text string) string {
	var parts []string
	for _, part := range strings.Split(text, "\n") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " · ")
}

// raiseTooltipPopup puts the classic tooltip on the popup layer, placed
// against the text it stands for and shifted to stay on the screen.
func (d *Desktop) raiseTooltipPopup(req core.TooltipRequest, lines []string) bool {
	pc := d.popupHost()
	if pc == nil || req.From == nil {
		return false
	}
	metrics := d.EffectiveCellMetrics()
	font := d.EffectiveFont()

	var textW core.Unit
	for _, line := range lines {
		if w := font.MeasureTextIn(line, metrics); w > textW {
			textW = w
		}
	}
	pad := metrics.UnitsPerCellWidth
	box := core.UnitRect{
		Width:  textW + pad*2,
		Height: core.Unit(len(lines))*metrics.UnitsPerCellHeight + metrics.UnitsPerCellHeight,
	}

	// The anchor in screen space: the rect the text occupies, mapped the way
	// every other popup maps its opener. Its SIZE crosses denominations with
	// it -- the asker measured it in its own container's units, and the
	// popup layer is the screen's.
	origin := pc.MapToScreen(req.From, core.UnitPoint{X: req.At.X, Y: req.At.Y})
	local := core.FindEffectiveCellMetrics(req.From)
	anchor := core.UnitRect{
		X: origin.X, Y: origin.Y,
		Width:  core.ExchangeX(req.At.Width, local, metrics),
		Height: core.ExchangeY(req.At.Height, local, metrics),
	}
	box.X, box.Y = tooltipOrigin(req.Side, anchor, box, pc.ScreenBounds(), metrics)

	scheme := d.GetScheme()
	face := scheme.GetTooltip()
	border := scheme.GetTooltipBorder()
	pc.RegisterPopup(&core.PopupRequest{
		ID:     tooltipPopupID,
		Bounds: box,
		Anchor: anchor,
		// A tooltip is drawn and nothing else. It sits ON the text it
		// stands for, so a click there is a click on the text.
		Inert: true,
		Paint: func(p *core.Painter) {
			paintTooltip(p, box, lines, face, border, metrics, font)
		},
		// The layer clears every popup on a press outside them. Without
		// this the desktop would go on believing it is showing one.
		OnDismiss: func() { d.forgetTooltipPopup() },
	})
	return true
}

// tooltipOrigin places the box against the anchor the way the asker asked, and
// then shifts it back onto the screen -- the same compromise a context menu
// makes near an edge: a preference is not worth half a tooltip.
func tooltipOrigin(side core.TooltipSide, anchor, box, screen core.UnitRect, metrics core.CellMetrics) (x, y core.Unit) {
	gap := metrics.UnitsPerCellHeight / 2
	switch side {
	case core.TooltipOver:
		x, y = anchor.X, anchor.Y
	case core.TooltipAbove:
		x, y = anchor.X, anchor.Y-box.Height-gap
	case core.TooltipBefore:
		x, y = anchor.X-box.Width-gap, anchor.Y
	case core.TooltipAfter:
		x, y = anchor.X+anchor.Width+gap, anchor.Y
	default: // auto and below both start under the text
		x, y = anchor.X, anchor.Y+anchor.Height+gap
	}

	// Below and above flip rather than hang off the edge, since the other side
	// of the same text is where the room is.
	if y+box.Height > screen.Y+screen.Height && side != core.TooltipOver {
		if above := anchor.Y - box.Height - gap; above >= screen.Y {
			y = above
		}
	}
	if y+box.Height > screen.Y+screen.Height {
		y = screen.Y + screen.Height - box.Height
	}
	if y < screen.Y {
		y = screen.Y
	}
	if x+box.Width > screen.X+screen.Width {
		x = screen.X + screen.Width - box.Width
	}
	if x < screen.X {
		x = screen.X
	}
	return x, y
}

// paintTooltip draws the note: its own face, a rule around it, and the text
// inside. Rounded where the surface can round, square where it cannot.
//
// box is in SCREEN units, and so is everything drawn here: the popup layer
// hands every overlay the screen's own painter rather than one moved to the
// overlay, so a popup that drew from zero would draw in the corner.
func paintTooltip(p *core.Painter, box core.UnitRect, lines []string, face, border style.CellStyle, metrics core.CellMetrics, font *core.Font) {
	radius := metrics.UnitsPerCellHeight / 2
	if !p.DrawRoundedRect(box, radius, style.BorderSingle, face.WithFg(border.Fg)) {
		p.FillRect(box, ' ', face)
		p.DrawRect(box, style.BorderSingle, face.WithFg(border.Fg))
	}
	x := box.X + metrics.UnitsPerCellWidth
	y := box.Y + metrics.UnitsPerCellHeight/2
	for _, line := range lines {
		p.DrawText(x, y, line, face, font)
		y += metrics.UnitsPerCellHeight
	}
}
