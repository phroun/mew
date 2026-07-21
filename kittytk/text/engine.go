// Package text is KittyTK's shared text engine (plan decisions D5/D6):
// one KittyTK-owned module for text shaping, measurement, and
// rasterization, used identically by every graphical backend, so
// server-side layout is deterministic and measurement can never
// disagree with painting.
//
// The engine offers two tiers behind one roof (D6):
//
//   - a fast simple path - Measure and ShapeRun - for single-font,
//     single-direction glyph runs (button labels, menu items, titles);
//   - the full shaped-paragraph path - ShapeParagraph - with OpenType
//     shaping (GSUB/GPOS), bidirectional text (UAX #9), per-rune font
//     fallback, and UAX #14 line breaking.
//
// The contract altitude is the shaped paragraph: attributed text in;
// lines of positioned glyph runs plus cluster mapping out. Trinkets'
// graphical paint paths consume shaped lines and use the cluster-map
// operations (CaretX, RuneForX) - never per-rune arithmetic - which
// is what keeps caret movement, selection, and hit-testing correct in
// RTL text and inside ligatures. The shaping library is an
// implementation detail and does not appear in the API.
//
// Terminal-style regions (PurfecTerm) are a deliberate carve-out and
// do not go through this engine; TUI mode keeps its cell metrics
// (D6's accepted asymmetry).
package text

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/go-text/typesetting/di"
	gtfont "github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"
	xbidi "golang.org/x/text/unicode/bidi"

	"github.com/phroun/kittytk/core"
)

// Direction is the paragraph base direction.
type Direction uint8

const (
	// DirectionAuto derives the base direction from the first strong
	// character (UAX #9 rule P2), defaulting to left-to-right.
	DirectionAuto Direction = iota
	DirectionLTR
	DirectionRTL
)

// Span attributes a rune range of a paragraph with a font. Ranges are
// rune indices, half-open [Start, End).
type Span struct {
	Start, End int
	Font       *core.Font
}

// Paragraph is the input to the full shaping path: one paragraph of
// logical text (no newlines) with optional attribute spans.
type Paragraph struct {
	Text string
	// Font is the default font for text not covered by Spans.
	// nil means core.DefaultFont().
	Font *core.Font
	// Spans style sub-ranges; they must be sorted and non-overlapping.
	Spans []Span
	// Direction is the paragraph base direction.
	Direction Direction
}

// RuneRange is a half-open range [Start, End) of rune indices into
// the paragraph text.
type RuneRange struct {
	Start, End int
}

// Run is one directional stretch of shaped glyphs in a single face.
type Run struct {
	// Runes is the logical text range this run covers.
	Runes RuneRange
	// RTL reports the run's resolved direction.
	RTL bool
	// X is the run's left edge within its line, Width its advance.
	X, Width core.Unit

	raw shaping.Output // shaped glyphs; deliberately not exposed
}

// Line is one wrapped line: runs stored in visual order (leftmost
// first), with metrics for stacking.
type Line struct {
	Runs []Run
	// Runes is the logical text range of the whole line.
	Runes RuneRange
	// Width is the line's total advance.
	Width core.Unit
	// Ascent and Descent are distances above/below the baseline (both
	// positive); Gap is the leading below the descent.
	Ascent, Descent, Gap core.Unit
	// Baseline is the line's baseline Y relative to the paragraph top.
	Baseline core.Unit
}

// Height returns the vertical space the line occupies.
func (l *Line) Height() core.Unit { return l.Ascent + l.Descent + l.Gap }

// ShapedParagraph is the full-path output: wrapped, shaped, positioned.
type ShapedParagraph struct {
	Lines []Line
	// Text is the paragraph's logical text as runes (cluster indices
	// and RuneRanges index into it).
	Text []rune
}

// Height returns the total stacked height of all lines.
func (p *ShapedParagraph) Height() core.Unit {
	h := core.Unit(0)
	for i := range p.Lines {
		h += p.Lines[i].Height()
	}
	return h
}

// Width returns the widest line's advance.
func (p *ShapedParagraph) Width() core.Unit {
	w := core.Unit(0)
	for i := range p.Lines {
		if p.Lines[i].Width > w {
			w = p.Lines[i].Width
		}
	}
	return w
}

// Engine is the shared text engine. One Engine per display service;
// safe for concurrent use.
type Engine struct {
	mu      sync.Mutex
	db      *fontDB
	shaper  shaping.HarfbuzzShaper
	seg     shaping.Segmenter
	wrapper shaping.LineWrapper

	// Shaping is deterministic (D5), so identical inputs are shaped
	// once and reused: UI frames repeat the same strings, and without
	// this every repaint re-runs segmentation and HarfBuzz shaping
	// for every visible string.
	cache shapeCache
	epoch uint64 // bumped when the font set changes
}

// NewEngine creates an engine with the embedded default fonts:
// "Noto Sans" (default UI face) and "Noto Sans Mono", with the
// TUI-era names "Monday" and "Tuesday" aliased onto them and the Go
// families kept as fallbacks.
func NewEngine() *Engine {
	return &Engine{db: newFontDB(), cache: newShapeCache(2048)}
}

// RegisterFont adds a font variant (TTF/OTF bytes) under a family
// name, extending the fallback chain in registration order. Data is
// parsed once; registering the same family+aspect again replaces it.
// Cached shapes are invalidated: fallback resolution may change.
func (e *Engine) RegisterFont(familyName string, a Aspect, ttf []byte) error {
	if err := e.db.register(familyName, a, ttf); err != nil {
		return err
	}
	e.mu.Lock()
	e.cache.clear()
	e.epoch++
	e.mu.Unlock()
	return nil
}

// Epoch identifies the engine's font set: it changes whenever
// RegisterFont does. External caches keyed on shaped output (e.g.
// rendered-text images) compare epochs to know when to flush.
func (e *Engine) Epoch() uint64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.epoch
}

// SetFontAlias re-points a font alias (e.g. "ui-term", "ui-fraktur") at an
// ordered list of target families; resolve uses the first that is registered,
// so the list is a fallback chain. No targets deletes the alias. Bumps the
// epoch so caches keyed on the font set flush. The named families must already
// be registered (RegisterFontFile / RegisterFontByName, or UseFont which loads
// on demand).
func (e *Engine) SetFontAlias(alias string, targets ...string) {
	e.db.setAlias(alias, targets)
	e.bumpEpoch()
}

// sharedEngine is the process-wide engine a graphical backend publishes so
// every surface — UI chrome and each embedded terminal — resolves fonts from
// one font set. A live font change then reaches them all through one handle.
var (
	sharedMu     sync.RWMutex
	sharedEngine *Engine
)

// SetShared publishes e as the process-wide engine (called by the raster
// backend when it builds its engine). Pass nil to clear.
func SetShared(e *Engine) {
	sharedMu.Lock()
	sharedEngine = e
	sharedMu.Unlock()
}

// Shared returns the process-wide engine, or nil if none is published (e.g. the
// pure-TUI path, where fonts are the outer terminal's concern).
func Shared() *Engine {
	sharedMu.RLock()
	defer sharedMu.RUnlock()
	return sharedEngine
}

// shapeCache is a two-generation cache: inserts land in cur; when cur
// fills, it becomes prev and a fresh cur starts. Anything untouched
// for two generations is dropped - no per-entry bookkeeping, bounded
// memory, and the hot working set (the strings on screen) stays warm.
type shapeCache struct {
	cur, prev map[string]*ShapedParagraph
	max       int
}

func newShapeCache(max int) shapeCache {
	return shapeCache{
		cur:  make(map[string]*ShapedParagraph),
		prev: make(map[string]*ShapedParagraph),
		max:  max,
	}
}

func (c *shapeCache) get(k string) (*ShapedParagraph, bool) {
	if v, ok := c.cur[k]; ok {
		return v, true
	}
	if v, ok := c.prev[k]; ok {
		c.cur[k] = v // promote so it survives the next rotation
		return v, true
	}
	return nil, false
}

func (c *shapeCache) put(k string, v *ShapedParagraph) {
	if len(c.cur) >= c.max {
		c.prev = c.cur
		c.cur = make(map[string]*ShapedParagraph)
	}
	c.cur[k] = v
}

func (c *shapeCache) clear() {
	c.cur = make(map[string]*ShapedParagraph)
	c.prev = make(map[string]*ShapedParagraph)
}

// shapeKey identifies a shaping request: font identity (colors are
// irrelevant to shaping), direction, wrap width, and the text.
func shapeKey(f *core.Font, d Direction, w core.Unit, text string) string {
	if f == nil {
		f = core.DefaultFont()
	}
	var b strings.Builder
	b.Grow(len(f.Name) + len(text) + 24)
	b.WriteString(f.Name)
	b.WriteByte(0)
	b.WriteString(strconv.Itoa(int(f.Style)))
	b.WriteByte(0)
	b.WriteString(strconv.Itoa(f.Size))
	b.WriteByte(0)
	b.WriteString(strconv.Itoa(int(d)))
	b.WriteByte(0)
	b.WriteString(strconv.Itoa(int(w)))
	b.WriteByte(0)
	b.WriteString(text)
	return b.String()
}

// lineBudget is the vertical space a font's line occupies, in fixed
// units: Size * 4/3. This is the toolkit's size denomination - a
// 12pt font has a 16-unit line height, exactly one default cell row,
// so graphical text lines up with the TUI-era grid and text always
// fits the chrome that hosts it. The em size is derived from this
// budget per face (below), not the other way around.
func lineBudget(f *core.Font) fixed.Int26_6 {
	if f == nil {
		f = core.DefaultFont()
	}
	size := f.Size
	if size <= 0 {
		size = 12
	}
	return fixed.Int26_6(size * 256 / 3)
}

// emFor computes the em size (the shaping Size) that makes face's
// ascent + descent + line gap fill exactly the font's line budget.
// Deterministic per face + size (D5).
func emFor(face *gtfont.Face, f *core.Font) fixed.Int26_6 {
	budget := lineBudget(f)
	ext, ok := face.FontHExtents()
	total := float64(ext.Ascender) - float64(ext.Descender) + float64(ext.LineGap)
	if !ok || total <= 0 {
		return budget
	}
	em := float64(budget) * float64(face.Upem()) / total
	return fixed.Int26_6(math.Round(em))
}

// resolveDirection applies UAX #9 P2/P3: first strong character wins.
func resolveDirection(d Direction, runes []rune) di.Direction {
	switch d {
	case DirectionLTR:
		return di.DirectionLTR
	case DirectionRTL:
		return di.DirectionRTL
	}
	for _, r := range runes {
		props, _ := xbidi.LookupRune(r)
		switch props.Class() {
		case xbidi.L:
			return di.DirectionLTR
		case xbidi.R, xbidi.AL:
			return di.DirectionRTL
		}
	}
	return di.DirectionLTR
}

// spanPiece is a normalized attribute run covering the paragraph.
type spanPiece struct {
	start, end int
	font       *core.Font
}

// normalizeSpans covers [0, n) completely: gaps get the default font.
func normalizeSpans(spans []Span, def *core.Font, n int) []spanPiece {
	if def == nil {
		def = core.DefaultFont()
	}
	var pieces []spanPiece
	pos := 0
	for _, s := range spans {
		start, end := s.Start, s.End
		if start < 0 {
			start = 0
		}
		if end > n {
			end = n
		}
		if start >= end || start < pos {
			continue
		}
		if start > pos {
			pieces = append(pieces, spanPiece{pos, start, def})
		}
		f := s.Font
		if f == nil {
			f = def
		}
		pieces = append(pieces, spanPiece{start, end, f})
		pos = end
	}
	if pos < n {
		pieces = append(pieces, spanPiece{pos, n, def})
	}
	return pieces
}

// shapeOutputs runs segmentation (bidi + script + face fallback) and
// shaping over the paragraph, returning shaped runs in logical order.
func (e *Engine) shapeOutputs(runes []rune, pieces []spanPiece, base di.Direction) []shaping.Output {
	var outs []shaping.Output
	for _, pc := range pieces {
		face := e.db.resolve(pc.font)
		in := shaping.Input{
			Text:      runes,
			RunStart:  pc.start,
			RunEnd:    pc.end,
			Direction: base,
			Face:      face,
			Size:      emFor(face, pc.font),
		}
		fbRoot, fbStyle := scriptContext(pc.font.Name)
		fb := fallbackMap{db: e.db, primary: face, scriptRoot: fbRoot, scriptStyle: fbStyle}
		for _, si := range e.seg.Split(in, fb) {
			outs = append(outs, e.shaper.Shape(si))
		}
	}
	return outs
}

// ShapeParagraph runs the full path: attributed text + available
// width in, wrapped lines of positioned shaped runs out. width <= 0
// means unbounded (single line unless the text forces breaks).
func (e *Engine) ShapeParagraph(p Paragraph, width core.Unit) *ShapedParagraph {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Spanless paragraphs (the hot path: every label, title, and
	// measurement) are cached; shaped output is immutable to callers.
	cacheable := len(p.Spans) == 0
	var key string
	if cacheable {
		key = shapeKey(p.Font, p.Direction, width, p.Text)
		if sp, ok := e.cache.get(key); ok {
			return sp
		}
	}

	runes := []rune(p.Text)
	base := resolveDirection(p.Direction, runes)
	pieces := normalizeSpans(p.Spans, p.Font, len(runes))
	outs := e.shapeOutputs(runes, pieces, base)

	maxW := fixed.Int26_6(math.MaxInt32)
	if width > 0 {
		maxW = fixed.I(int(width))
	}
	cfg := shaping.WrapConfig{Direction: base}
	wrapped, _ := e.wrapper.WrapParagraphF(cfg, maxW, runes, shaping.NewSliceIterator(outs))

	sp := &ShapedParagraph{Text: runes}
	y := core.Unit(0)
	for _, lineRuns := range wrapped {
		line := buildLine(lineRuns)
		line.Baseline = y + line.Ascent
		y += line.Height()
		sp.Lines = append(sp.Lines, line)
	}
	if len(sp.Lines) == 0 {
		// Empty text still has one empty line with font metrics.
		face := e.db.resolve(p.Font)
		ext, _ := face.FontHExtents()
		size := emFor(face, p.Font)
		scale := float32(size) / float32(face.Upem()) / 64
		asc := core.Unit(math.Ceil(float64(ext.Ascender * scale)))
		desc := core.Unit(math.Ceil(float64(-ext.Descender * scale)))
		gap := core.Unit(math.Ceil(float64(ext.LineGap * scale)))
		sp.Lines = []Line{{Ascent: asc, Descent: desc, Gap: gap, Baseline: asc}}
	}
	if cacheable {
		e.cache.put(key, sp)
	}
	return sp
}

// buildLine converts one wrapped line to the contract type: runs in
// visual order with x positions assigned left to right.
func buildLine(runs shaping.Line) Line {
	line := Line{Runes: RuneRange{Start: math.MaxInt, End: 0}}
	// Sort into visual order (leftmost first).
	visual := make([]shaping.Output, len(runs))
	copy(visual, runs)
	for i := 1; i < len(visual); i++ {
		for j := i; j > 0 && visual[j].VisualIndex < visual[j-1].VisualIndex; j-- {
			visual[j], visual[j-1] = visual[j-1], visual[j]
		}
	}

	pen := fixed.Int26_6(0)
	for _, out := range visual {
		r := Run{
			Runes: RuneRange{Start: out.Runes.Offset, End: out.Runes.Offset + out.Runes.Count},
			RTL:   out.Direction.Progression() == di.TowardTopLeft,
			X:     core.Unit(pen.Round()),
			raw:   out,
		}
		pen += out.Advance
		r.Width = core.Unit(pen.Round()) - r.X

		if r.Runes.Start < line.Runes.Start {
			line.Runes.Start = r.Runes.Start
		}
		if r.Runes.End > line.Runes.End {
			line.Runes.End = r.Runes.End
		}
		if a := core.Unit(out.LineBounds.Ascent.Ceil()); a > line.Ascent {
			line.Ascent = a
		}
		if d := core.Unit((-out.LineBounds.Descent).Ceil()); d > line.Descent {
			line.Descent = d
		}
		if g := core.Unit(out.LineBounds.Gap.Ceil()); g > line.Gap {
			line.Gap = g
		}
		line.Runs = append(line.Runs, r)
	}
	line.Width = core.Unit(pen.Round())
	if line.Runes.Start == math.MaxInt {
		line.Runes = RuneRange{}
	}
	return line
}

// ShapeRun is the fast simple tier: one font, base direction from the
// text, no wrapping. Fallback still applies per rune. The result is a
// single-line ShapedParagraph.
func (e *Engine) ShapeRun(f *core.Font, s string) *ShapedParagraph {
	return e.ShapeParagraph(Paragraph{Text: s, Font: f}, 0)
}

// Measure is the fast simple tier's measurement: the advance width of
// s in font f, by real shaping (so it always agrees with painting).
func (e *Engine) Measure(f *core.Font, s string) core.Unit {
	return e.ShapeRun(f, s).Width()
}

// LineHeight returns the line height of font f: exactly the font's
// line budget (Size * 4/3 units) by construction - the em size is
// derived to fill it, so 12pt = 16 units = one default cell row.
func (e *Engine) LineHeight(f *core.Font) core.Unit {
	return core.Unit(lineBudget(f).Ceil())
}

// MeasureText implements core.TextMeasurer (G1: measurement comes
// from the render target). The same engine paints, so measurement
// and rendering can never disagree.
func (e *Engine) MeasureText(f *core.Font, s string) core.Unit {
	return e.Measure(f, s)
}
