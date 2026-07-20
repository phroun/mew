package render

import (
	"strings"

	"github.com/phroun/mew/internal/window"
)

// Browse-mode display transforms. In browse mode the editor rewrites how a
// document line is PAINTED without changing the document: link spans become
// buttons, wiki markup markers (**bold**, //italic//, __underline__, heading
// ===) are hidden and their text restyled, and a top-level heading line may be
// drawn double-width. All of this is expressed as a list of DisplaySpans plus a
// per-line double-width flag; the substitution below turns them into a
// "display line" the whole render/measure pipeline then treats as the content,
// so tabs, bidi, truncation, and cursor math all just work. With no provider
// (or an empty result) every path is the identity and rendering is unchanged.
//
// A DisplaySpan replaces the document runes [Start,End) with Runes for display.
// Each display rune carries the source doc index it came from (Doc[i], or -1
// for synthetic chrome like a button cap/shadow) and a forced SGR (Style[i], or
// "" to colour normally from its doc rune). Collapse marks a span whose interior
// is a single inert unit — a button: every doc position inside it parks at the
// span's first display cell. A non-collapse span (markup/heading) keeps its
// content traversable: a preserved doc rune maps to its own display cell, and a
// hidden marker collapses onto the next visible cell.
type DisplaySpan struct {
	Start, End int
	Runes      []rune
	Doc        []int
	Style      []string
	Collapse   bool
}

// ButtonSpan is the convenience shape the link code builds; it becomes a
// Collapse DisplaySpan (cap+title+cap, then a shadow cell).
type ButtonSpan struct {
	Start, End  int
	Runes       []rune // display cells: left cap + title + right cap
	Shadow      rune   // trailing shadow cell glyph (0 = none)
	Color       string // SGR for the button cells
	ShadowColor string // SGR for the shadow cell
}

// asDisplaySpan expands a ButtonSpan to the general form.
func (b ButtonSpan) asDisplaySpan() DisplaySpan {
	runes := append([]rune(nil), b.Runes...)
	styles := make([]string, len(b.Runes))
	docs := make([]int, len(b.Runes))
	for i := range styles {
		styles[i] = b.Color
		docs[i] = -1
	}
	if b.Shadow != 0 {
		runes = append(runes, b.Shadow)
		styles = append(styles, b.ShadowColor)
		docs = append(docs, -1)
	}
	return DisplaySpan{Start: b.Start, End: b.End, Runes: runes, Doc: docs, Style: styles, Collapse: true}
}

// DisplayProvider returns the browse-mode display transform for one line of a
// window: the ordered, non-overlapping spans and whether the line is drawn
// double-width. Nil spans + false is the identity (the common case).
type DisplayProvider func(w *window.Window, docLine int) (spans []DisplaySpan, doubleWidth bool)

// lineDisplay is one document line rewritten for display, with the position
// maps the renderer needs to keep cursor, selection, and syntax colours honest
// against the original document positions.
type lineDisplay struct {
	Text  string
	Runes []rune
	// Forced holds, per display rune, a fixed SGR ("" = colour normally from
	// its doc rune). Forced cells take no selection tint or whitespace marker.
	Forced []string
	// DocToDisp maps every doc-rune boundary (0..len(docRunes)) to a display
	// boundary; DispToDoc maps each display rune back to its doc rune (-1 for
	// chrome). See DisplaySpan for the per-span mapping rules.
	DocToDisp []int
	DispToDoc []int
	// DoubleWide draws the line double-width (DEC DECDWL): each cell counts as
	// two columns for every measurement, and the painter emits the mode.
	DoubleWide bool
}

// substituteSpans builds the display form of a line. A nil/empty transform (no
// spans and not double-width) returns nil (identity).
func substituteSpans(docRunes []rune, spans []DisplaySpan, doubleWidth bool) *lineDisplay {
	if len(spans) == 0 && !doubleWidth {
		return nil
	}
	d := &lineDisplay{DocToDisp: make([]int, len(docRunes)+1), DoubleWide: doubleWidth}
	// collapseAt[docStart] = display index for a Collapse span's interior.
	collapse := map[int][2]int{} // Start -> {End, dispStart}
	doc := 0
	appendDoc := func(upto int) {
		for ; doc < upto && doc < len(docRunes); doc++ {
			d.Runes = append(d.Runes, docRunes[doc])
			d.Forced = append(d.Forced, "")
			d.DispToDoc = append(d.DispToDoc, doc)
		}
	}
	for _, s := range spans {
		if s.Start < doc {
			continue // overlap/garbage: skip
		}
		appendDoc(s.Start)
		if doc > len(docRunes) {
			break
		}
		dispStart := len(d.Runes)
		for i, r := range s.Runes {
			d.Runes = append(d.Runes, r)
			st := ""
			if i < len(s.Style) {
				st = s.Style[i]
			}
			d.Forced = append(d.Forced, st)
			src := -1
			if i < len(s.Doc) {
				src = s.Doc[i]
			}
			d.DispToDoc = append(d.DispToDoc, src)
		}
		end := s.End
		if end > len(docRunes) {
			end = len(docRunes)
		}
		if s.Collapse {
			collapse[s.Start] = [2]int{end, dispStart}
		}
		doc = end
	}
	appendDoc(len(docRunes))

	// Build DocToDisp: preserved doc runes map to their own display cell.
	for i := range d.DocToDisp {
		d.DocToDisp[i] = -1
	}
	for di, src := range d.DispToDoc {
		if src >= 0 && src < len(docRunes) {
			d.DocToDisp[src] = di
		}
	}
	// Collapse spans: their whole interior parks at the span's first cell.
	for start, ce := range collapse {
		for p := start; p < ce[0]; p++ {
			d.DocToDisp[p] = ce[1]
		}
	}
	d.DocToDisp[len(docRunes)] = len(d.Runes)
	// Fill any still-unmapped positions (hidden markers) with the next mapped
	// display index, scanning from the end.
	next := len(d.Runes)
	for p := len(docRunes); p >= 0; p-- {
		if d.DocToDisp[p] < 0 {
			d.DocToDisp[p] = next
		} else {
			next = d.DocToDisp[p]
		}
	}
	d.Text = string(d.Runes)
	return d
}

// SetDisplayProvider installs the editor's per-line browse-mode transform. A
// nil provider (the default) disables all substitution.
func (sr *ScreenRenderer) SetDisplayProvider(p DisplayProvider) {
	sr.displayProvider = p
}

// SetCaretHiddenFn installs a predicate that hides the hardware caret for a
// window even when it is on screen (the caret is inert inside a focused
// button). nil never hides for this reason.
func (sr *ScreenRenderer) SetCaretHiddenFn(fn func(w *window.Window) bool) {
	sr.caretHiddenFn = fn
}

// lineTransform fetches a line's display transform, or (nil,false) when none
// applies.
func (sr *ScreenRenderer) lineTransform(w *window.Window, docLine int) ([]DisplaySpan, bool) {
	if sr.displayProvider == nil {
		return nil, false
	}
	return sr.displayProvider(w, docLine)
}

// displayFor returns the substituted display form of a document line, with a
// display-aligned syntax colour array spliced from the normal colorizer, or
// nil when no transform applies to the line (the common case).
func (sr *ScreenRenderer) displayFor(w *window.Window, docLine int, line string) (*lineDisplay, []string) {
	spans, dw := sr.lineTransform(w, docLine)
	if len(spans) == 0 && !dw {
		return nil, nil
	}
	d := substituteSpans([]rune(line), spans, dw)
	if d == nil {
		return nil, nil
	}
	var syn []string
	if sr.syntaxColorizer != nil {
		if orig := sr.syntaxColorizer(w, docLine); orig != nil {
			syn = make([]string, len(d.Runes))
			for i, di := range d.DispToDoc {
				if di >= 0 && di < len(orig) {
					syn[i] = orig[di]
				}
			}
		}
	}
	return d, syn
}

// displayCaretLine substitutes the window's caret line and maps the given
// doc-rune caret position into display space. Identity when no transform
// applies. Used by every cursor-side measurement so the caret, ghost, and
// ruler land on the cells the line was painted with.
func (sr *ScreenRenderer) displayCaretLine(w *window.Window, line string, runePos int) (string, int) {
	spans, dw := sr.lineTransform(w, w.CursorPos().Line)
	if len(spans) == 0 && !dw {
		return line, runePos
	}
	d := substituteSpans([]rune(line), spans, dw)
	if d == nil {
		return line, runePos
	}
	if runePos < 0 {
		runePos = 0
	}
	if runePos >= len(d.DocToDisp) {
		runePos = len(d.DocToDisp) - 1
	}
	return d.Text, d.DocToDisp[runePos]
}

// lineIsSubstituted reports whether any display transform applies to a line of
// w. The showMarks cell walks consult it: mark cells are suppressed on
// substituted lines (their doc positions have no cells of their own there).
func (sr *ScreenRenderer) lineIsSubstituted(w *window.Window, docLine int) bool {
	spans, dw := sr.lineTransform(w, docLine)
	return len(spans) > 0 || dw
}

// caretLineDoubleWide reports whether the window's caret line is drawn
// double-width, so the cursor-positioning math can count its columns by two.
func (sr *ScreenRenderer) caretLineDoubleWide(w *window.Window) bool {
	_, dw := sr.lineTransform(w, w.CursorPos().Line)
	return dw
}

// SubstituteButtons is the exported form of the display substitution for the
// editor's own scroll/visibility math: the display text of a line and the map
// from doc-rune boundaries to display boundaries. The editor and renderer must
// agree on this geometry, so both call the one implementation.
func SubstituteButtons(line string, spans []DisplaySpan, doubleWidth bool) (string, []int) {
	d := substituteSpans([]rune(line), spans, doubleWidth)
	if d == nil {
		return line, nil
	}
	return d.Text, d.DocToDisp
}

// sanitizeButtonTitle is a helper for providers: control characters and tabs
// inside substituted content become plain spaces so they never re-enter the
// tab/control rendering paths inside a chrome cell.
func SanitizeButtonTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r < 0x20 || r == 0x7F {
			return ' '
		}
		return r
	}, s)
}

// Button builds a Collapse DisplaySpan from a ButtonSpan (link buttons).
func (b ButtonSpan) Span() DisplaySpan { return b.asDisplaySpan() }
