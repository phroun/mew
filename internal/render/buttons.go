package render

import (
	"strings"

	"github.com/phroun/mew/internal/window"
)

// Link-as-button rendering (browse mode). The editor supplies, per window and
// document line, a set of ButtonSpans: doc-rune ranges to be painted as
// buttons (a cap, the resolved link title, a cap, and a trailing shadow cell)
// instead of their raw text. The substitution happens at the very top of the
// render/measure pipeline — the walk, tabs, bidi reordering, truncation, and
// cursor math all operate on the substituted "display line" — so RTL titles,
// wide runes, and mirroring inside buttons work exactly as they do in body
// text. With no provider, or a provider returning nil (browse mode off, no
// links), every path below is the identity and rendering is byte-for-byte
// unchanged.
type ButtonSpan struct {
	Start, End  int    // [Start, End) doc-rune range replaced on the line
	Runes       []rune // display cells: left cap + title + right cap
	Shadow      rune   // trailing shadow cell glyph (0 = none)
	Color       string // SGR for the button cells
	ShadowColor string // SGR for the shadow cell
}

// ButtonProvider returns the button replacements for one line of a window,
// ordered by Start and non-overlapping, or nil when none apply.
type ButtonProvider func(w *window.Window, docLine int) []ButtonSpan

// lineDisplay is one document line with its button spans substituted, plus
// the position maps the renderer needs to keep cursor, selection, and syntax
// colors honest against the original document positions.
type lineDisplay struct {
	Text  string
	Runes []rune
	// Forced holds, per display rune, the fixed SGR of a button/shadow cell
	// ("" for cells that came from the document and color normally). Forced
	// cells never take selection tint or invisible-whitespace markers.
	Forced []string
	// DocToDisp maps every doc-rune boundary (0..len(docRunes)) to a display
	// boundary. Boundaries inside a span map to the span's display start (the
	// caret parks on the button); the boundary at a span's End lands after its
	// shadow cell.
	DocToDisp []int
	// DispToDoc maps each display rune to the doc rune it came from, or -1
	// for chrome (button/shadow) cells.
	DispToDoc []int
}

// substituteButtons builds the display form of a line from its button spans.
// Spans out of range are clamped; a nil/empty span list returns nil (identity).
func substituteButtons(docRunes []rune, spans []ButtonSpan) *lineDisplay {
	if len(spans) == 0 {
		return nil
	}
	d := &lineDisplay{DocToDisp: make([]int, len(docRunes)+1)}
	doc := 0
	appendDoc := func(upto int) {
		for ; doc < upto && doc < len(docRunes); doc++ {
			d.DocToDisp[doc] = len(d.Runes)
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
		if doc >= len(docRunes) {
			break
		}
		dispStart := len(d.Runes)
		for _, r := range s.Runes {
			d.Runes = append(d.Runes, r)
			d.Forced = append(d.Forced, s.Color)
			d.DispToDoc = append(d.DispToDoc, -1)
		}
		if s.Shadow != 0 {
			d.Runes = append(d.Runes, s.Shadow)
			d.Forced = append(d.Forced, s.ShadowColor)
			d.DispToDoc = append(d.DispToDoc, -1)
		}
		end := s.End
		if end > len(docRunes) {
			end = len(docRunes)
		}
		// Boundaries within the span park on the button; the End boundary
		// falls after the shadow (set by the next appendDoc / final fill).
		for ; doc < end; doc++ {
			d.DocToDisp[doc] = dispStart
		}
	}
	appendDoc(len(docRunes))
	d.DocToDisp[len(docRunes)] = len(d.Runes)
	d.Text = string(d.Runes)
	return d
}

// SetButtonProvider installs the editor's per-line button source. A nil
// provider (the default) disables button substitution entirely.
func (sr *ScreenRenderer) SetButtonProvider(p ButtonProvider) {
	sr.buttonProvider = p
}

// displayFor returns the substituted display form of a document line, with a
// display-aligned syntax color array spliced from the normal colorizer, or
// nil when no buttons apply to the line (the common case).
func (sr *ScreenRenderer) displayFor(w *window.Window, docLine int, line string) (*lineDisplay, []string) {
	if sr.buttonProvider == nil {
		return nil, nil
	}
	spans := sr.buttonProvider(w, docLine)
	if len(spans) == 0 {
		return nil, nil
	}
	d := substituteButtons([]rune(line), spans)
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

// displayCaretLine substitutes buttons on the window's caret line and maps
// the given doc-rune caret position into display space. Identity (the inputs
// unchanged) when no buttons apply. Used by every cursor-side measurement so
// the caret, ghost, and ruler land on the cells the line was painted with.
func (sr *ScreenRenderer) displayCaretLine(w *window.Window, line string, runePos int) (string, int) {
	if sr.buttonProvider == nil {
		return line, runePos
	}
	spans := sr.buttonProvider(w, w.CursorPos().Line)
	if len(spans) == 0 {
		return line, runePos
	}
	d := substituteButtons([]rune(line), spans)
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

// lineHasButtons reports whether button substitution applies to a line of w.
// The showMarks cell walks consult it: mark cells are suppressed on
// substituted lines (their doc positions have no cells of their own there).
func (sr *ScreenRenderer) lineHasButtons(w *window.Window, docLine int) bool {
	return sr.buttonProvider != nil && len(sr.buttonProvider(w, docLine)) > 0
}

// SubstituteButtons is the exported form of the display substitution for the
// editor's own scroll/visibility math: the display text of a line and the map
// from doc-rune boundaries to display boundaries. The editor and renderer
// must agree on this geometry, so both call the one implementation.
func SubstituteButtons(line string, spans []ButtonSpan) (string, []int) {
	d := substituteButtons([]rune(line), spans)
	if d == nil {
		return line, nil
	}
	return d.Text, d.DocToDisp
}

// sanitizeButtonTitle is a helper for providers: control characters and tabs
// inside a button title become plain spaces so a title can never re-enter the
// tab/control rendering paths inside a chrome cell.
func SanitizeButtonTitle(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r < 0x20 || r == 0x7F {
			return ' '
		}
		return r
	}, s)
}
