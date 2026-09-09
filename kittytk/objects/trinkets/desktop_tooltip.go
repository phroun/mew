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
	"time"

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

// tooltipDwellDefault is how long the pointer rests before a note is raised
// over the screen, and tooltipFollowOn is how long after one goes the next
// comes without waiting -- long enough to read along a row of trinkets, short
// enough that coming back later starts the wait again.
const (
	tooltipDwellDefault = 500 * time.Millisecond
	tooltipFollowOn     = 1500 * time.Millisecond
	// tooltipFadeDur is how long a note takes to arrive and to leave. The
	// compositor runs it at the surface's own rate; nothing here ticks it.
	tooltipFadeDur = 250 * time.Millisecond
)

// desktopTooltip is what is on the screen for whom.
type desktopTooltip struct {
	from  core.Trinket
	lines []string
	inBar bool   // the status bar is showing it
	saved string // and this is what it was saying before
	// layer is the popup layer showing it, remembered so it is withdrawn
	// from the same one it was raised into: a torn-out window has a layer
	// of its own, and the desktop's would not know about this note.
	layer core.PopupController
	// shown is the request the layer is holding, kept so a fade-out can
	// hand back the very same note with nothing but its fade changed.
	shown *core.PopupRequest
}

// ShowTooltip implements core.TooltipHandler: the desktop takes anything that
// reached it, which is everything nothing else wanted.
//
// A note raised over the screen WAITS for the pointer to settle. It is a
// second window's worth of ink appearing under the hand, and a pointer merely
// crossing the screen should not leave a trail of them. The status bar has no
// such cost -- it is a row that is already there, saying something already --
// so it answers at once.
func (d *Desktop) ShowTooltip(req core.TooltipRequest) bool {
	if strings.TrimSpace(req.Text) == "" {
		return false
	}
	d.HideTooltip(nil)

	if d.graphicalSurface() && d.popupHost(req.From) != nil {
		if d.tooltipComesAtOnce() {
			return d.revealTooltip(req)
		}
		d.awaitTooltipRest(req)
		return true
	}
	// No popup layer, or a cell surface: the status bar says it instead.
	if bar := d.StatusBar(); bar != nil {
		mm := d.tooltipFace()
		d.tooltip = &desktopTooltip{
			from:  req.From,
			lines: tooltipLines(req.Text, d.tooltipWrapWidth(mm), mm),
			inBar: true,
			saved: bar.Text(),
		}
		bar.SetText(tooltipStatusText(req.Text))
		d.RequestUpdate()
		return true
	}
	return false
}

// revealTooltip puts the note on the screen now.
func (d *Desktop) revealTooltip(req core.TooltipRequest) bool {
	mm := d.tooltipFace()
	lines := tooltipLines(req.Text, d.tooltipWrapWidth(mm), mm)
	// A note still fading out is replaced rather than left to expire: the
	// layer holds one tooltip, and this is now that one.
	d.cancelTooltipDeparture()
	layer, popup := d.raiseTooltipPopup(req, lines, mm)
	if layer == nil {
		return false
	}
	d.tooltip = &desktopTooltip{from: req.From, lines: lines, layer: layer, shown: popup}
	d.RequestUpdate()
	return true
}

// awaitTooltipRest holds the offer until the pointer has been still long
// enough, and drops it if the pointer moves on to something else first.
func (d *Desktop) awaitTooltipRest(req core.TooltipRequest) {
	d.dropPendingTooltip()
	d.tooltipPending = &req
	d.armTooltipDwell(d.tooltipDelay())
}

// armTooltipDwell asks to be woken in `in`, replacing any earlier wait.
func (d *Desktop) armTooltipDwell(in time.Duration) {
	if in <= 0 {
		d.wakeTooltipDwell()
		return
	}
	d.tooltipTimer = d.StartTimer(in, d.wakeTooltipDwell)
}

// wakeTooltipDwell reveals the offer being held, or waits out the rest of the
// dwell if the pointer moved while it was waiting: the clock is on the
// POINTER being still, not on how long the offer has been held, so a hand
// travelling across a row of trinkets never reaches the end of it.
func (d *Desktop) wakeTooltipDwell() {
	d.tooltipTimer = nil
	req := d.tooltipPending
	if req == nil {
		return
	}
	if rest := d.tooltipDelay() - time.Since(d.pointerMovedAt); rest > 0 {
		d.armTooltipDwell(rest)
		return
	}
	d.tooltipPending = nil
	d.revealTooltip(*req)
}

// dropPendingTooltip forgets an offer that was still waiting to be shown.
func (d *Desktop) dropPendingTooltip() {
	d.tooltipPending = nil
	if d.tooltipTimer != nil {
		d.StopTimer(d.tooltipTimer)
		d.tooltipTimer = nil
	}
}

// tooltipComesAtOnce reports whether the reader is already reading tooltips,
// in which case the next one is not made to wait: having asked for one, moving
// along a row to ask about its neighbour is one continued question, and a
// fresh half-second on each would answer none of it.
func (d *Desktop) tooltipComesAtOnce() bool {
	return !d.tooltipShownAt.IsZero() && time.Since(d.tooltipShownAt) < tooltipFollowOn
}

// tooltipDelay is how long the pointer rests before a note appears.
func (d *Desktop) tooltipDelay() time.Duration {
	if d.tooltipDwell > 0 {
		return d.tooltipDwell
	}
	return tooltipDwellDefault
}

// tooltipFadeFor is how long a note takes to arrive and to leave.
func (d *Desktop) tooltipFadeFor() time.Duration {
	if d.tooltipFade > 0 {
		return d.tooltipFade
	}
	return tooltipFadeDur
}

// notePointerMoved restarts the dwell: the pointer is what the clock is on.
func (d *Desktop) notePointerMoved() { d.pointerMovedAt = time.Now() }

// HideTooltip implements core.TooltipHandler. A nil asker means every tooltip,
// which is what a keystroke asks for.
func (d *Desktop) HideTooltip(from core.Trinket) {
	if p := d.tooltipPending; p != nil && (from == nil || p.From == from) {
		d.dropPendingTooltip()
	}
	t := d.tooltip
	if t == nil || (from != nil && t.from != from) {
		return
	}
	d.tooltip = nil
	// A note that was on the screen starts the follow-on clock. An offer
	// still waiting its turn never gets here -- it is dropped above, and
	// d.tooltip was never set for it -- so a pointer crossing a row of
	// trinkets does not buy itself an instant note at the far end.
	d.tooltipShownAt = time.Now()
	if t.inBar {
		if bar := d.StatusBar(); bar != nil {
			bar.SetText(t.saved)
		}
	}
	if t.layer != nil {
		d.fadeTooltipOut(t)
	}
	d.RequestUpdate()
}

// fadeTooltipOut leaves the note on the layer for as long as it takes to go,
// turned around from wherever its arrival had got to -- a note taken down
// half way in leaves from half in rather than snapping to solid first.
//
// The layer is holding the same request throughout: only its fade changes,
// so nothing is re-measured or re-placed on the way out.
func (d *Desktop) fadeTooltipOut(t *desktopTooltip) {
	if t.shown == nil || t.shown.Fade == nil {
		t.layer.UnregisterPopup(tooltipPopupID)
		return
	}
	now := time.Now()
	out := t.shown.Fade.Reverse(now, 0, d.tooltipFadeFor())
	t.shown.Fade = &out
	t.layer.RegisterPopup(t.shown)

	d.cancelTooltipDeparture()
	d.tooltipLeaving = t
	d.tooltipLeavingTimer = d.StartTimer(d.tooltipFadeFor(), func() {
		d.tooltipLeavingTimer = nil
		if d.tooltipLeaving != t {
			return
		}
		d.tooltipLeaving = nil
		t.layer.UnregisterPopup(tooltipPopupID)
		d.RequestUpdate()
	})
}

// cancelTooltipDeparture stops waiting for a note to finish leaving. It does
// NOT take the note off the layer: either a new one is about to replace it
// under the same id, or it has gone already.
func (d *Desktop) cancelTooltipDeparture() {
	if d.tooltipLeavingTimer != nil {
		d.StopTimer(d.tooltipLeavingTimer)
		d.tooltipLeavingTimer = nil
	}
	d.tooltipLeaving = nil
}

// forgetTooltipPopup is what the desktop does when the popup layer discards
// the tooltip for it: let go of it without asking the layer to drop something
// it has already dropped.
func (d *Desktop) forgetTooltipPopup() {
	t := d.tooltip
	if t == nil || t.layer == nil {
		return
	}
	d.tooltip = nil
	d.tooltipShownAt = time.Now()
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

// popupHost is the layer a tooltip is raised into.
//
// It is asked of the TRINKET, not of the desktop: a window torn out onto a
// surface of its own carries its own popup layer, and a note about something
// in it belongs on that surface rather than back on the desktop the window
// came from. The desktop's own manager is the answer for everything still on
// the desktop, and the fallback for a trinket that names no layer.
func (d *Desktop) popupHost(from core.Trinket) core.PopupController {
	for t, depth := from, 0; t != nil && depth < maxPopupHostWalk; depth++ {
		if getter, ok := t.(interface {
			PopupController() core.PopupController
		}); ok {
			if pc := getter.PopupController(); pc != nil {
				return pc
			}
		}
		t = popupWalkParent(t)
	}
	if d.windowManager == nil {
		return nil
	}
	if pc, ok := any(d.windowManager).(core.PopupController); ok {
		return pc
	}
	return nil
}

// maxPopupHostWalk stops the walk for a chain that somehow leads back into
// itself, the same guard the rest of the chain walks carry.
const maxPopupHostWalk = 64

// popupWalkParent is the next trinket up, following an embedded trinket's host
// where it has no parent of its own -- a tree's cell editor is drawn by the
// tree, and its popups belong to the same layer as the tree's.
func popupWalkParent(t core.Trinket) core.Trinket {
	if p := t.Parent(); p != nil {
		return p
	}
	if e, ok := t.(core.EmbedHosted); ok {
		return e.EmbedHost()
	}
	return nil
}

// tooltipFace is the size a tooltip is drawn at, which is a menu's: a note
// that appears beside the pointer belongs with the dropdowns and context
// menus it appears among, and [window] menu_scale is what sets that size.
func (d *Desktop) tooltipFace() MenuMetrics {
	return MenuMetricsFor(d.EffectiveCellMetrics(), d.EffectiveFont(), d.graphicalSurface())
}

// tooltipWrapWidth is the widest a tooltip line may be, in units.
func (d *Desktop) tooltipWrapWidth(mm MenuMetrics) core.Unit {
	w := core.Unit(tooltipWrapCells) * mm.CellW
	if b := d.Bounds(); b.Width > 0 && w > b.Width-mm.CellW*4 {
		w = b.Width - mm.CellW*4
	}
	return w
}

// tooltipLines is the text as it will be shown: broken where it says to break,
// and wrapped where a line is longer than a tooltip may be.
func tooltipLines(text string, width core.Unit, mm MenuMetrics) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapText(line, width, mm.Font, mm.base)...)
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
func (d *Desktop) raiseTooltipPopup(req core.TooltipRequest, lines []string, mm MenuMetrics) (core.PopupController, *core.PopupRequest) {
	if req.From == nil {
		return nil, nil
	}
	pc := d.popupHost(req.From)
	if pc == nil {
		return nil, nil
	}
	metrics := d.EffectiveCellMetrics()

	var textW core.Unit
	for _, line := range lines {
		if w := mm.TextWidth(line); w > textW {
			textW = w
		}
	}
	padX, padY := tooltipPadding(mm)
	box := core.UnitRect{
		Width:  textW + padX*2,
		Height: core.Unit(len(lines))*mm.RowH + padY*2,
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
	box.X, box.Y = tooltipOrigin(req.Side, anchor, box, pc.ScreenBounds(), metrics, padX, padY)

	scheme := d.GetScheme()
	face := scheme.GetTooltip()
	border := scheme.GetTooltipBorder()
	popup := &core.PopupRequest{
		ID:     tooltipPopupID,
		Bounds: box,
		// No anchor. A drop-down and the control it opened from cast one
		// shadow because they read as one piece; a tooltip is a note laid
		// over the screen, and giving it an anchor would draw a shadow
		// around the very text it is explaining.
		//
		// A tooltip is drawn and nothing else. It sits ON the text it
		// stands for, so a click there is a click on the text.
		Inert: true,
		Paint: func(p *core.Painter) {
			paintTooltip(p, box, lines, face, border, mm)
		},
		// The layer clears every popup on a press outside them. Without
		// this the desktop would go on believing it is showing one.
		OnDismiss: func() { d.forgetTooltipPopup() },
		// It arrives rather than appearing. The fade is a value the
		// compositor reads at each frame, so nothing here has to tick it.
		Fade: &core.Fade{Start: time.Now(), Dur: d.tooltipFadeFor(), From: 0, To: 1},
	}
	pc.RegisterPopup(popup)
	return pc, popup
}

// tooltipOrigin places the box against the anchor the way the asker asked, and
// then shifts it back onto the screen -- the same compromise a context menu
// makes near an edge: a preference is not worth half a tooltip.
func tooltipOrigin(side core.TooltipSide, anchor, box, screen core.UnitRect, metrics core.CellMetrics, padX, padY core.Unit) (x, y core.Unit) {
	gap := metrics.UnitsPerCellHeight / 2
	// Over the text, what has to line up is the note's TEXT and the text it
	// expands -- not the note's box and the text's corner. The box is pulled
	// back by its own padding on the left and centred on the anchor
	// vertically, so the words sit where the words were.
	overX := anchor.X - padX
	overY := anchor.Y + (anchor.Height-box.Height)/2
	switch side {
	case core.TooltipOver:
		x, y = overX, overY
	case core.TooltipAbove:
		x, y = overX, anchor.Y-box.Height-gap
	case core.TooltipBefore:
		x, y = anchor.X-box.Width-gap, overY
	case core.TooltipAfter:
		x, y = anchor.X+anchor.Width+gap, overY
	case core.TooltipBelow:
		x, y = overX, anchor.Y+anchor.Height+gap
	default:
		// Auto reads on IN PLACE. A note is an answer about the words under
		// the pointer, and putting it a row below them leaves the reader's
		// eye to travel and lands it on whatever stands underneath.
		x, y = overX, overY
	}

	// Below and above flip rather than hang off the edge, since the other side
	// of the same text is where the room is.
	if y+box.Height > screen.Y+screen.Height && side != core.TooltipOver && side != core.TooltipAuto {
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
func paintTooltip(p *core.Painter, box core.UnitRect, lines []string, face, border style.CellStyle, mm MenuMetrics) {
	radius := mm.RowH / 2
	if !p.DrawRoundedRect(box, radius, style.BorderSingle, face.WithFg(border.Fg)) {
		p.FillRect(box, ' ', face)
		p.DrawRect(box, style.BorderSingle, face.WithFg(border.Fg))
	}
	padX, padY := tooltipPadding(mm)
	x := box.X + padX
	y := box.Y + padY
	for _, line := range lines {
		p.DrawText(x, y+mm.YOff, line, face, mm.Font)
		y += mm.RowH
	}
}

// tooltipPadding is the room between the note's text and its rule: half a
// pitch beside the text and a quarter row above and below it. A tooltip is
// read at a glance and wants to be small.
func tooltipPadding(mm MenuMetrics) (x, y core.Unit) {
	x, y = mm.CellW/2, mm.RowH/4
	if x < 1 {
		x = 1
	}
	if y < 1 {
		y = 1
	}
	return x, y
}
