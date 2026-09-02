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
	// The body glyph box is UnitsPerCellHeight at the base point size and
	// scales with it; centre what remains of the row around it, FLOORING the
	// slack the way the title bar does -- rounding the half-gap up sits the
	// text a unit low in the shortened row.
	if off := (mm.RowH - mm.glyphBox(mm.Font)) / 2; off > 0 {
		mm.YOff = off
	}
	return mm
}

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
// column in macOS-native mode, the menu bar's clock. Returned as an offset
// from the row's top, so the caller draws at itemY + this.
//
// What a smaller face beside a bigger one has to match is where its INK
// sits, not where its baseline or its line box does, and the two smaller
// faces here are labels standing in a row rather than text continuing a
// line. Sharing the body's baseline pins their ink to the bottom half of the
// row -- the body's ascenders fill the top and theirs do not -- which is
// exactly how "^K _" came to hang under its own item. Centring their line
// BOX is nearer but still low, since a box carries descent the string may
// not use.
//
// A face's ink runs from near the top of its ascent down to its baseline, so
// the centre of that block is half its baseline below the top. Two faces
// share an ink centre when the smaller starts half the difference between
// their baselines lower -- which is this, and which lands the clock exactly
// on the label's ink centre and the shortcut within half a pixel of it.
//
// Falls back to centring the box where the target cannot answer for a
// baseline (a cell surface, a bare measurer), which is where it stood.
func (mm MenuMetrics) GlyphYOff(f *core.Font) core.Unit {
	if f == nil {
		return mm.YOff
	}
	body, other := core.FontBaseline(mm.Font), core.FontBaseline(f)
	if body > 0 && other > 0 {
		off := mm.YOff + core.ExchangeY((body-other)/2, core.DefaultCellMetrics(), mm.base)
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
	p.DrawText(x, y+mm.GlyphYOff(mm.Mono), string(ch), st, mm.Mono)
}

// MenuRowHeight implements core.MenuRowProvider: the bar's row at the
// current core.MenuScale, in the bar's own denomination. A window carrying
// this bar as chrome reserves exactly this much for it.
func (m *MenuBar) MenuRowHeight() core.Unit { return m.menuMetrics().RowH }
