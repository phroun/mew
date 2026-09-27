package trinkets

import (
	"math"
	"sync"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The menu kit: the one place a menu bar, the dropdowns it opens and a
// context menu get their row height, their cell pitch and their face, all
// resolved at core.MenuScale. It is the title-bar kit's companion
// (objects/window/titlebar.go) and follows its rulings, so a host that sets
// both scales gets chrome that reads as one system.

// MenuSeparatorAlpha is how strongly a menu's separator rule inks over the
// background it sits on, on surfaces that can blend. A quarter: enough to
// read as a division between groups of items, not enough to read as a line
// drawn through the menu. Cell surfaces keep their dashed rule, having no
// alpha to spend.
const MenuSeparatorAlpha = 0.25

// MenuGutterAlpha is how strongly a menu's gutter lays its own colour over
// the menu background beneath it, on surfaces that can blend. Three
// quarters: the gutter reads as a shaded band of the menu rather than a
// separate panel butted against it. Cell surfaces fill it solid, having no
// alpha to spend.
const MenuGutterAlpha = 0.75

// MenuMetrics is the resolved geometry of one menu row at the current
// core.MenuScale: the row height, the cell pitch the gutters, checkmarks,
// arrows and pads lay out on, and the face the text draws with. All lengths
// are in the menu's own UNIT space -- a unit is the denomination's
// subdivision of a cell, so RowH and CellW are counted in whatever
// column_units/row_units the menu inherited. At scale 1.0 every field equals
// the unscaled original -- same row, same cells, same font pointer -- so the
// paint is bit-identical to the unscaled code. On cell (non-graphical)
// surfaces the scale is pinned to 1.0: a terminal cannot render nine tenths
// of a character cell.
type MenuMetrics struct {
	Scale float64
	RowH  core.Unit  // one item row (UnitsPerCellHeight at 1.0)
	CellW core.Unit  // the pitch the gutter, checkmark, arrow and pads count in
	Font  *core.Font // the menu's body face (the given font at 1.0, size-scaled otherwise)
	Mono  *core.Font // the cell-glyph face: ui-term at the scaled size, nil at 1.0
	YOff  core.Unit  // vertical centring of the scaled body glyphs in the row (0 at 1.0)

	Graphical bool

	base core.CellMetrics
	src  *core.Font
}

// MenuMetricsFor resolves one menu's geometry. metrics is the denomination
// the menu counts in and font its unscaled body face; graphical=false (a
// cell surface) pins the scale to 1.0.
//
// QUANTIZATION follows the title bar's ruling: the scaled row is ceiled onto
// the denomination's integer unit grid -- core.Unit is an integer, and a
// fraction in this system is a finer denomination, not a fractional value --
// so 0.9 of a 16-unit row lands on 15/16. The scale is therefore only as
// fine as the menu's denomination can say, and a menu counted more finely
// (row_units=32) realizes it more exactly.
func MenuMetricsFor(metrics core.CellMetrics, font *core.Font, graphical bool) MenuMetrics {
	scale := core.MenuScale()
	if !graphical {
		scale = 1
	}
	mm := MenuMetrics{
		Scale:     scale,
		RowH:      metrics.UnitsPerCellHeight,
		CellW:     metrics.UnitsPerCellWidth,
		Font:      font,
		Graphical: graphical,
		base:      metrics,
		src:       font,
	}
	if scale == 1 {
		return mm
	}
	mm.RowH = ceilUnit(scale, metrics.UnitsPerCellHeight)
	mm.CellW = ceilUnit(scale, metrics.UnitsPerCellWidth)
	mm.Font = menuFace(font, scale)
	mm.Mono = &core.Font{Name: "ui-term", Size: mm.Font.Size}
	// The body sits in the shortened row on the same terms as everything
	// beside it: its capitals centred, and the box rule where the surface
	// cannot say how tall a capital is.
	mm.YOff = mm.GlyphYOff(mm.Font)
	return mm
}

// GutterWidth is the band down the left of a dropdown that carries the
// checkmarks and item icons: three cell pitches -- the frame, the mark
// itself, and a space before the label -- with the divider rule on its last
// pixel column.
func (mm MenuMetrics) GutterWidth() core.Unit { return mm.CellW * 3 }

// ceilUnit is one scaled length on the unit grid, never shorter than a unit:
// a row that rounded to nothing would take the whole menu with it.
func ceilUnit(scale float64, u core.Unit) core.Unit {
	n := core.Unit(math.Ceil(scale * float64(u)))
	if n < 1 {
		n = 1
	}
	return n
}

// TextWidth measures a run in the menu's body face, counted in the menu's
// own denomination. The face is the SCALED one, so what is measured is what
// draws; the denomination is the real one, because a unit does not change
// size when the text inside it does.
func (mm MenuMetrics) TextWidth(text string) core.Unit {
	if mm.Font == nil {
		return 0
	}
	return mm.Font.MeasureTextIn(text, mm.base)
}

// Width measures a run in some other face the menu draws with -- a
// shortcut's, the clock's -- in the same terms.
func (mm MenuMetrics) Width(text string, f *core.Font) core.Unit {
	if f == nil {
		return 0
	}
	return f.MeasureTextIn(text, mm.base)
}

// glyphBox is how many units a face's line box occupies, which is what
// core.LineUnits answers: the UNSCALED cell's row times the face's share of
// the body point size.
//
// The unscaled row, not the scaled one. A shortened row holds a face
// shortened by the same fraction, so measuring the face against the row it
// is about to sit in applies the scale twice and reports a box smaller than
// the glyphs are -- which then reads as slack, and the text is pushed down
// out of the bottom of its own row.
func (mm MenuMetrics) glyphBox(f *core.Font) core.Unit {
	return core.LineUnits(f, mm.src, mm.base)
}

// GlyphYOff sits a face in a menu row beside the body face: the shortcut
// column in macOS-native mode, the menu bar's clock, a checkmark. Returned
// as an offset from the row's top, so the caller draws at itemY + this.
//
// The basis is the height of a CAPITAL, measured once for the face. That is
// the size of the type, and the three quantities that stand in for it are
// each wrong in the same direction: a line box carries leading the letters
// never use, a baseline says how far down the letters sit rather than how
// tall they are, and the ink of a particular string depends on whether that
// string happens to have a descender in it -- so a shortcut ending in "_"
// centres differently from one ending in "]". Sharing the body's baseline is
// wrong the same way, pinning a small face to the bottom half of the row
// because the body's ascenders fill the top and its own do not, which is how
// "^K _" came to hang under its own item.
//
// So: put the capital's block, which runs from a cap height above the
// baseline down to the baseline, in the middle of the row. Written as halves
// of a unit and floored once, a position never being ceiled.
//
// Falls back to centring the line box where the target cannot answer for
// these (a cell surface, a bare measurer), which is where it stood.
func (mm MenuMetrics) GlyphYOff(f *core.Font) core.Unit {
	if f == nil {
		return mm.YOff
	}
	d := core.DefaultCellMetrics()
	base := core.ExchangeY(core.FontBaseline(f), d, mm.base)
	capH := core.ExchangeY(core.FontCapHeight(f), d, mm.base)
	if base > 0 && capH > 0 {
		off := floorHalf(mm.RowH - 2*base + capH)
		if off < 0 {
			off = 0
		}
		if off > mm.RowH {
			off = mm.RowH
		}
		return off
	}
	if off := (mm.RowH - mm.glyphBox(f)) / 2; off > 0 {
		return off
	}
	return 0
}

// floorHalf halves a unit count downward, negatives included -- Go's integer
// division truncates toward zero, which rounds a negative position UP and is
// the bias this is here to avoid.
func floorHalf(u core.Unit) core.Unit {
	return core.Unit(math.Floor(float64(u) / 2))
}

// menuFaceKey identifies one (source font, scale) pair. core.Font is a plain
// comparable value, so it keys the cache directly and a caller handing over
// an equal font by a different pointer still hits.
type menuFaceKey struct {
	src   core.Font
	scale float64
}

var (
	menuFacesMu    sync.RWMutex
	menuFacesCache = map[menuFaceKey]*core.Font{}
)

// menuFacesCacheLimit bounds the cache against pathological churn (a caller
// varying font size every frame); past it the map starts over rather than
// growing without end. In practice a session holds one entry: the UI face at
// the host's scale.
const menuFacesCacheLimit = 64

// menuFace returns the body face for one (font, scale), building it once.
// MenuMetricsFor runs from the hit tests and the size calculations as well
// as the painters, several times per menu per frame, so a face allocated on
// every call would be garbage on the frame path.
func menuFace(font *core.Font, scale float64) *core.Font {
	src := core.Font{Name: "ui-text", Size: 12}
	if font != nil {
		src = *font
	}
	key := menuFaceKey{src: src, scale: scale}

	menuFacesMu.RLock()
	f, ok := menuFacesCache[key]
	menuFacesMu.RUnlock()
	if ok {
		return f
	}

	size := int(math.Round(scale * float64(src.Size)))
	if size < 1 {
		size = 1
	}
	scaled := src
	scaled.Size = size

	menuFacesMu.Lock()
	if len(menuFacesCache) >= menuFacesCacheLimit {
		menuFacesCache = map[menuFaceKey]*core.Font{}
	}
	menuFacesCache[key] = &scaled
	menuFacesMu.Unlock()
	return &scaled
}

// menuMetrics resolves this dropdown's row geometry. Popup menus are not
// parented into the trinket tree, so the denomination and font come from
// whatever the opener handed down (inheritDisplayContext) and the surface
// kind from the same hint graphicalSurface uses.
func (m *Menu) menuMetrics() MenuMetrics {
	return MenuMetricsFor(m.EffectiveCellMetrics(), m.EffectiveFont(), m.graphicalSurface())
}

// menuMetrics resolves the bar's row geometry, on the same terms as the
// dropdowns it opens so a title in the bar and the same text in the menu
// below it are the same size.
func (m *MenuBar) menuMetrics() MenuMetrics {
	return MenuMetricsFor(m.EffectiveCellMetrics(), m.EffectiveFont(), m.graphicalHere())
}

// graphicalHere reports whether the bar sits on a pixel surface. The bar's
// own graphicalCached is PAINT state -- it records what the last painter
// was -- and a row height is needed before that: a window reserves its
// chrome at layout, and a bar asked then answered for a surface it had not
// seen yet and gave back a full cell. Unlike a popup menu the bar is
// parented, so the tree can be asked directly, and the cached value only
// stands in once a paint has actually happened.
func (m *MenuBar) graphicalHere() bool {
	if m.graphicalCached {
		return true
	}
	return core.FindGraphicalFrames(m.Self())
}

// DrawGlyph paints one of a menu's cell glyphs -- a checkmark, an item
// icon, a submenu arrow -- at (x, y).
//
// At scale 1.0 that is DrawCell, the retro idiom where the cell font's own
// pitch IS the cell and the glyph fills it. Scaled, DrawCell would put a
// full-height glyph in a shortened row, so the glyph draws as a one-rune run
// in the MONOSPACED ui-term face at the row's size, centred in the row --
// the same two-path shape the title bar's controls take, and for the same
// reason.
func (mm MenuMetrics) DrawGlyph(p *core.Painter, x, y core.Unit, ch rune, st style.CellStyle) {
	if mm.Scale == 1 || mm.Mono == nil {
		p.DrawCell(x, y, ch, st)
		return
	}
	// The CELL's background first, edge to edge, so the next cell's fill
	// starts exactly where this one ends and a run of cells side by side
	// cannot show a crack between them. DrawCell does this for free; a text
	// run inks only its own advance, and a scaled face's advance is not the
	// cell it stands in -- which is what opened gaps down the middle of
	// "[<]" and made it read wider than it is.
	// A transparent background is the caller asking for the glyph over what
	// is already painted there, so there is no cell to lay first -- and
	// nothing can gap against it either, the fill beneath it being whole.
	if st.Bg != style.ColorTransparent {
		p.FillRect(core.UnitRect{X: x, Y: y, Width: mm.CellW, Height: mm.RowH}, ' ', st)
	}
	// Then the glyph, centred in the cell it belongs to, the way the cell
	// font's own pitch centres it at 1.0.
	w := mm.Width(string(ch), mm.Mono)
	if off := (mm.CellW - w) / 2; off > 0 {
		x += off
	}
	p.DrawText(x, y+mm.GlyphYOff(mm.Mono), string(ch), st, mm.Mono)
}

// MenuRowHeight implements core.MenuRowProvider: the bar's row at the
// current core.MenuScale, in the bar's own denomination. A window carrying
// this bar as chrome reserves exactly this much for it.
func (m *MenuBar) MenuRowHeight() core.Unit { return m.menuMetrics().RowH }
