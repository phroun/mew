package window

import (
	"math"
	"sync"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The title-bar kit: the one place the system's title bars — a window's
// normal frame, its maximized frame, and the desktop's own themed bar —
// get their geometry, their text layout, their focused-title decoration,
// their keyboard-geometry vocabulary, and their double-click convention.
// Before this file each of those painters carried its own copy, and the
// copies drifted (a mis-set button origin, an approximated focus
// decoration); now a bar that consumes the kit is right by construction.
//
// The individual CONTROLS still paint through one function per button
// (PaintCloseButton, PaintMinimizeButton, ...): deliberately NOT collapsed
// into a table-driven loop, because theming may one day draw the three
// buttons genuinely differently. What they share today is only the
// three-cell mechanics underneath.

// TitleBarMetrics is the resolved geometry of one title bar row at the
// current core.TitleBarScale: the row height, the content cell pitch the
// buttons and text lay out on, and the font the text draws with. All
// lengths are in the caller's UNIT space — a unit is the denomination's
// subdivision of a cell, an arbitrary layout currency with no pixel
// identity of its own; only ppu says what a unit paints as. At scale 1.0
// every field equals the unscaled original — same row, same cells, same
// font pointer — so the paint is bit-identical to the pre-kit code. On
// cell (non-graphical) surfaces the scale is pinned to 1.0: a terminal
// cannot render seven tenths of a character cell.
type TitleBarMetrics struct {
	Scale   float64
	RowH    core.Unit  // title row height in frame units (CellHeight at 1.0)
	CellW   core.Unit  // cell pitch the controls/text lay out on
	ButtonW core.Unit  // one control slot: three of those cells
	YOff    core.Unit  // vertical centering of scaled glyphs in the row (0 at 1.0)
	Font    *core.Font // title TEXT font (the given font at 1.0, size-scaled otherwise)
	Mono    *core.Font // control-glyph font: the ui-term cell face at the scaled size — the buttons are MONOSPACED, the retro aesthetic DrawCell renders at 1.0

	Graphical bool

	base core.CellMetrics
}

// TitleBarMetricsFor resolves the title-bar geometry for one bar, in the
// FRAME's own terms: metrics is the frame denomination's cell metrics
// (frameCellMetrics for a window, the root metrics for the desktop). The
// title bar sits ABOVE the content area, so it is never sized in the
// content's interior denomination — units are an arbitrary subdivision of
// a cell, a layout tool of the area that declared them, and the chrome
// above owes them nothing. graphical=false (a cell surface) pins the
// scale to 1.0.
//
// QUANTIZATION, by explicit ruling ("do (c) for now"): the scaled row is
// ceiled on the frame denomination's integer unit grid — core.Unit is an
// integer, and a fraction in this system is a finer denomination, not a
// fractional value — so 0.7 of a 16-unit cell lands on 12/16. The scale
// is therefore only as fine as the frame's denomination can say, and a
// unit is NOT a pixel: at zooms where a unit spans several device pixels
// the realized fraction stays 12/16, not a pixel-exact 70%. A pixel-true
// bar (or a finer frame denomination) is a future, broader sweep.
func TitleBarMetricsFor(metrics core.CellMetrics, font *core.Font, graphical bool) TitleBarMetrics {
	scale := core.TitleBarScale()
	if !graphical {
		scale = 1
	}
	tm := TitleBarMetrics{
		Scale:     scale,
		RowH:      metrics.CellHeight,
		CellW:     metrics.CellWidth,
		Font:      font,
		Graphical: graphical,
		base:      metrics,
	}
	if scale != 1 {
		tm.RowH = core.Unit(math.Ceil(scale * float64(metrics.CellHeight)))
		tm.CellW = core.Unit(math.Ceil(scale * float64(metrics.CellWidth)))
	}
	// The two faces this bar draws with: the title text's, and ui-term for
	// the monospaced controls. Both are a pure function of (source font,
	// scale), so they come from a cache rather than being rebuilt — this
	// runs from contentBounds and the hit tests as well as the painters,
	// several times per window per frame, and allocating a face on each
	// call put that garbage on the frame path.
	tm.Font, tm.Mono = titleFaces(font, scale)
	if scale != 1 && font != nil && tm.Font != nil {
		// The glyph box is CellHeight (in units) at the base point size
		// and scales with it; center what remains of the row around it,
		// FLOORING the slack — rounding the half-gap up sat the text a
		// unit too low in the shortened row.
		glyphH := float64(metrics.CellHeight) * float64(tm.Font.Size) / float64(font.Size)
		if off := core.Unit((float64(tm.RowH) - glyphH) / 2); off > 0 {
			tm.YOff = off
		}
	}
	tm.ButtonW = tm.CellW * 3
	return tm
}

// titleFacesKey identifies one (source font, scale) pair. core.Font is a
// plain comparable value, so it keys the cache directly and a caller that
// hands over an equal font by a different pointer still hits.
type titleFacesKey struct {
	src   core.Font
	scale float64
}

type titleFacesPair struct{ text, mono *core.Font }

var (
	titleFacesMu    sync.RWMutex
	titleFacesCache = map[titleFacesKey]titleFacesPair{}
)

// titleFacesCacheLimit bounds the cache against pathological churn (a
// caller varying font size every frame); past it the map starts over
// rather than growing without end. In practice a session holds one or two
// entries: the UI face at the host's scale.
const titleFacesCacheLimit = 64

// titleFaces returns the title text face and the ui-term control face for
// one (font, scale), building them once. At scale 1.0 the text face IS the
// font passed in — no copy — and only the mono face is ever built.
func titleFaces(font *core.Font, scale float64) (text, mono *core.Font) {
	src := core.Font{Name: "ui-text", Size: 12}
	if font != nil {
		src = *font
	}
	key := titleFacesKey{src: src, scale: scale}

	titleFacesMu.RLock()
	pair, ok := titleFacesCache[key]
	titleFacesMu.RUnlock()
	if ok {
		if font != nil && scale == 1 {
			// Hand back the caller's own pointer at 1.0 (the cached text
			// face is an equal value; the pointer identity is what the
			// unscaled painters compare against).
			return font, pair.mono
		}
		return pair.text, pair.mono
	}

	text = font
	if scale != 1 {
		size := int(math.Round(scale * float64(src.Size)))
		if size < 1 {
			size = 1
		}
		scaled := src
		scaled.Size = size
		text = &scaled
	} else if text == nil {
		f := src
		text = &f
	}
	mono = &core.Font{Name: "ui-term", Size: text.Size}

	titleFacesMu.Lock()
	if len(titleFacesCache) >= titleFacesCacheLimit {
		titleFacesCache = map[titleFacesKey]titleFacesPair{}
	}
	titleFacesCache[key] = titleFacesPair{text: text, mono: mono}
	titleFacesMu.Unlock()
	return text, mono
}

// paintThreeCellButton is the shared mechanics of one [i] control: three
// content cells at x. At scale 1.0 it is the classic per-cell draw,
// bit-identical to the pre-kit painters; scaled, the slot is filled and
// the whole "[i]" draws as one run in the MONO (ui-term) face centered in
// the slot. Both halves of that matter and each was gotten wrong once:
// per-glyph centering by proportional widths let narrow ink sit
// differently against its brackets than wide ink (one run advances each
// character exactly from the edge the previous one ended on), and the
// proportional TITLE face lost the retro monospace the controls have at
// 1.0, where DrawCell renders the cell font and its pitch IS the cell.
func paintThreeCellButton(p *core.Painter, tm TitleBarMetrics, x core.Unit, icon rune, st style.CellStyle) {
	if tm.Scale == 1 {
		p.DrawCell(x, 0, '[', st)
		p.DrawCell(x+tm.CellW, 0, icon, st)
		p.DrawCell(x+tm.CellW*2, 0, ']', st)
		return
	}
	p.FillRect(core.UnitRect{X: x, Width: tm.ButtonW, Height: tm.RowH}, ' ', st)
	run := "[" + string(icon) + "]"
	rx := x + (tm.ButtonW-tm.Mono.MeasureText(run))/2
	if rx < x {
		rx = x
	}
	p.DrawText(rx, tm.YOff, run, st, tm.Mono)
}

// PaintCloseButton draws the close control [x].
func PaintCloseButton(p *core.Painter, tm TitleBarMetrics, x core.Unit, st style.CellStyle) {
	paintThreeCellButton(p, tm, x, 'x', st)
}

// PaintMinimizeButton draws the minimize control [.].
func PaintMinimizeButton(p *core.Painter, tm TitleBarMetrics, x core.Unit, st style.CellStyle) {
	paintThreeCellButton(p, tm, x, '.', st)
}

// PaintZoomButton draws the maximize/zoom control: [^] to fill, [o] to
// restore while filled.
func PaintZoomButton(p *core.Painter, tm TitleBarMetrics, x core.Unit, zoomed bool, st style.CellStyle) {
	icon := '^'
	if zoomed {
		icon = 'o'
	}
	paintThreeCellButton(p, tm, x, icon, st)
}

// PaintBlurButton draws the blur control [~] (exit the window's keyboard).
func PaintBlurButton(p *core.Painter, tm TitleBarMetrics, x core.Unit, st style.CellStyle) {
	paintThreeCellButton(p, tm, x, '~', st)
}

// PaintTearHandleSlot draws the tear-off handle's three-cell slot at x:
// bracketed like a control while keyboard-focused, otherwise the floating
// glyph with its flanking cells filled in the title-bar background (so
// the frame's top-border stroke does not peek through the gaps on either
// side).
func PaintTearHandleSlot(p *core.Painter, tm TitleBarMetrics, x core.Unit, glyph rune, focused bool, focusSt, titleSt, glyphSt style.CellStyle) {
	if focused {
		paintThreeCellButton(p, tm, x, glyph, focusSt)
		return
	}
	if tm.Scale == 1 {
		p.DrawCell(x, 0, ' ', titleSt)
		p.DrawCell(x+tm.CellW, 0, glyph, glyphSt)
		p.DrawCell(x+tm.CellW*2, 0, ' ', titleSt)
		return
	}
	p.FillRect(core.UnitRect{X: x, Width: tm.ButtonW, Height: tm.RowH}, ' ', titleSt)
	g := string(glyph)
	gx := x + (tm.ButtonW-tm.Mono.MeasureText(g))/2
	if gx < x {
		gx = x
	}
	p.DrawText(gx, tm.YOff, g, glyphSt, tm.Mono)
}

// PaintTitleBarText draws the (unfocused) title. Centered when a centered
// title fits between the left controls and the right limit; otherwise its
// left edge sits just past the controls and the text ellipsizes so the
// "..." butts against the right limit — the right side keeps no mirrored
// reserve. A span of zero or less clips the title entirely. (This is the
// former Window.paintTitleText, verbatim at scale 1.0.)
func PaintTitleBarText(p *core.Painter, tm TitleBarMetrics, title string, ts style.CellStyle, leftUsed, rightLimit, barWidth core.Unit) {
	leftEdge := leftUsed + tm.CellW
	avail := rightLimit - leftEdge
	if avail <= 0 || title == "" {
		return
	}
	display := title
	titleW := tm.Font.MeasureText(display)
	if titleW > avail {
		display = ellipsizeToWidth(title, avail, tm.Font)
		if display == "" {
			return
		}
		titleW = tm.Font.MeasureText(display)
	}
	x := (barWidth - titleW) / 2
	if x < leftEdge {
		x = leftEdge
	}
	if x+titleW > rightLimit {
		x = rightLimit - titleW
	}
	p.DrawText(x, tm.YOff, display, ts, tm.Font)
}

// PaintFocusedTitleDecoration draws the keyboard-focused title as
// "< title >" centered in innerWidth over a highlight foundation. It
// shapes the whole thing as ONE run rather than cell brackets abutting a
// proportional title: at a fractional font size the two rates diverge, so
// placing the closing bracket at the title's re-snapped unit end left it
// drifting right of where the glyphs actually finish. A single run ends
// the bracket exactly on the title. On a cell surface each character still
// occupies its own cell, so the classic look is unchanged. (The former
// Window.paintFocusedTitleDecoration, verbatim at scale 1.0; every title
// bar's focused title — a window's or the desktop's — draws through it.)
func PaintFocusedTitleDecoration(p *core.Painter, tm TitleBarMetrics, innerWidth core.Unit, title string, s style.CellStyle) {
	decorated := "< " + title + " >"
	totalWidth := tm.Font.MeasureText(decorated)
	startX := (innerWidth - totalWidth) / 2
	if startX < 0 {
		startX = 0
	}
	p.FillRect(core.UnitRect{X: startX, Width: totalWidth, Height: tm.RowH}, ' ', s)
	p.DrawText(startX, tm.YOff, decorated, s, tm.Font)
}

// DecodeTitleGeometry maps one of the window-geometry commands — what a
// FOCUSED TITLE answers — to its direction and kind: dir is an arrow name
// ("Left", "Right", "Up", "Down"), resize distinguishes the size commands
// from the moves, coarse the 10-column/4-row step from the fine one-cell
// step. ok is false for any other command. Shared by Window.handleTitleBarKey
// and the desktop's themed title bar, so the vocabulary cannot drift.
func DecodeTitleGeometry(cmd string) (dir string, resize, coarse, ok bool) {
	switch cmd {
	case core.CmdWindowMoveFineLeft:
		return "Left", false, false, true
	case core.CmdWindowMoveFineRight:
		return "Right", false, false, true
	case core.CmdWindowMoveFineUp:
		return "Up", false, false, true
	case core.CmdWindowMoveFineDown:
		return "Down", false, false, true
	case core.CmdWindowMoveLeft:
		return "Left", false, true, true
	case core.CmdWindowMoveRight:
		return "Right", false, true, true
	case core.CmdWindowMoveUp:
		return "Up", false, true, true
	case core.CmdWindowMoveDown:
		return "Down", false, true, true
	case core.CmdWindowSizeFineLeft:
		return "Left", true, false, true
	case core.CmdWindowSizeFineRight:
		return "Right", true, false, true
	case core.CmdWindowSizeFineUp:
		return "Up", true, false, true
	case core.CmdWindowSizeFineDown:
		return "Down", true, false, true
	case core.CmdWindowSizeLeft:
		return "Left", true, true, true
	case core.CmdWindowSizeRight:
		return "Right", true, true, true
	case core.CmdWindowSizeUp:
		return "Up", true, true, true
	case core.CmdWindowSizeDown:
		return "Down", true, true, true
	}
	return "", false, false, false
}

// TitleGeometryDelta turns a decoded direction into a unit delta at the
// standard steps: one cell fine, the 10-column/4-row step coarse.
func TitleGeometryDelta(dir string, coarse bool, metrics core.CellMetrics) (dx, dy core.Unit) {
	h, v := metrics.CellWidth, metrics.CellHeight
	if coarse {
		h *= 10
		v *= 4
	}
	switch dir {
	case "Left":
		return -h, 0
	case "Right":
		return h, 0
	case "Up":
		return 0, -v
	case "Down":
		return 0, v
	}
	return 0, 0
}

// The smallest an OS-hosted window may be resized to: a window still has
// to show something and stay grabbable. One rule for every surface we
// resize ourselves — a torn-off window, a solo window on the primary
// surface, and the desktop's own window — so none of them can be pulled
// down to nothing while its siblings stop.
const (
	MinHostCols = 12
	MinHostRows = 4
)

// MinHostSizePx is that minimum in device pixels at the given cell
// metrics and pixels-per-unit: what a resize gesture clamps to, and what
// the OS window itself is told so a resize we do NOT drive (a native
// title bar's edges, a window manager's keyboard resize) cannot undercut
// it either.
func MinHostSizePx(metrics core.CellMetrics, ppu float64) (w, h int) {
	if ppu <= 0 {
		ppu = 1
	}
	return int(math.Round(float64(metrics.CellWidth*MinHostCols) * ppu)),
		int(math.Round(float64(metrics.CellHeight*MinHostRows) * ppu))
}

// DoubleClickTracker is the title bars' double-click convention: a second
// press within 400ms and one cell of the first fires and consumes the
// memory (a third click starts fresh rather than tripling). Callers that
// track a target identity (the window manager, per window) Reset it when
// the target changes.
type DoubleClickTracker struct {
	at   time.Time
	x, y core.Unit
}

// Press records a press and reports whether it completed a double-click.
func (t *DoubleClickTracker) Press(x, y core.Unit, metrics core.CellMetrics) bool {
	now := time.Now()
	isDouble := !t.at.IsZero() &&
		now.Sub(t.at) < 400*time.Millisecond &&
		x-t.x < metrics.CellWidth && t.x-x < metrics.CellWidth &&
		y-t.y < metrics.CellHeight && t.y-y < metrics.CellHeight
	if isDouble {
		t.at = time.Time{}
		return true
	}
	t.at, t.x, t.y = now, x, y
	return false
}

// Reset forgets the pending click (the next press starts a fresh count).
func (t *DoubleClickTracker) Reset() {
	t.at = time.Time{}
}
