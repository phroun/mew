package editor

import (
	"github.com/phroun/mew/internal/render"
	"github.com/phroun/mew/internal/window"
)

// Hyperlink browse mode (rendering pass — no navigation yet).
//
// When a buffer's grammar recognizes links (dokuwiki, including .txt files
// the path-conditional [formats] rules route there), each link renders in one
// of two styles per window:
//
//   - caret mode (BrowseActive off): the raw [[target|Title]] text, painted
//     in the "link" color over its syntax colors;
//   - browse mode (BrowseActive on): every link on screen becomes a button —
//     cap + resolved title + cap + shadow cell — via the renderer's
//     substitution layer; the button the caret sits in takes the focused
//     style, its destination shows in the modebar context slot, and accept
//     activates it (a transient notification for now).
//
// Browse mode arms automatically when the caret enters a link span it was
// not previously inside, and disarms via nav_cancel (^C's first stop). The
// "previously inside" identity is a garland tracking anchor at the span's
// start — it slides with edits like every other position; no line numbers
// are kept between operations. The caret stays inert inside a button: it
// still knows its rune position within the link's source text, but the
// button paints (and parks the terminal cursor) as one unit.

// linkSpansOnLine returns the grammar-derived link spans for one line of w's
// buffer, or nil when the grammar has none (not linkable, line out of range).
func (e *Editor) linkSpansOnLine(w *window.Window, docLine int) []linkSpan {
	if w == nil || w.Buffer == nil || w.Type != window.MainBuffer {
		return nil
	}
	c := e.ensureSynCache(w.Buffer, docLine)
	if c == nil || !c.linkable || docLine >= len(c.links) {
		return nil
	}
	return c.links[docLine]
}

// caretLinkSpan returns the link span the window's caret is strictly inside
// (Start < caret < End — the boundaries just outside the brackets don't
// count), or nil. The line number is read fresh from the caret each call.
func (e *Editor) caretLinkSpan(w *window.Window) *linkSpan {
	if w == nil {
		return nil
	}
	pos := w.CursorPos()
	spans := e.linkSpansOnLine(w, pos.Line)
	for i := range spans {
		if spans[i].Start < pos.Rune && pos.Rune < spans[i].End {
			return &spans[i]
		}
	}
	return nil
}

// updateBrowseState runs after every executed command (and paste): when the
// caret has entered a link span it was not previously inside, browse mode
// arms for the window. The occupied span's identity is its start position,
// held in a tracking anchor so edits slide it rather than staling it.
// Leaving all links clears the anchor but does NOT disarm browse mode —
// only nav_cancel does that.
func (e *Editor) updateBrowseState() {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Type != window.MainBuffer || w.Buffer == nil {
		return
	}
	if !w.ViewState.LinkBrowsing {
		return // hyperlink layer off: never arms
	}
	span := e.caretLinkSpan(w)
	if span == nil {
		w.ClearLinkAnchor()
		return
	}
	line := w.CursorPos().Line
	if al, ar, ok := w.LinkAnchorPos(); ok && al == line && ar == span.Start {
		return // still inside the same span: no re-entry
	}
	w.SetLinkAnchor(line, span.Start)
	if !w.BrowseActive {
		w.BrowseActive = true
		e.RequestRender()
	}
}

// navCancel turns browse mode off on the focused window (links revert to
// caret-mode link styling until the caret enters another link). Reports
// false when browse mode was not active, so a nav_cancel|cancel|... chain
// falls through to the next command.
func (e *Editor) navCancel() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || !w.BrowseActive {
		return false
	}
	w.BrowseActive = false
	e.RequestRender()
	return true
}

// focusedLinkButton returns the span rendered as the FOCUSED button in w: w
// must be the focused window, in browse mode, with the caret strictly inside
// the span. nil otherwise.
func (e *Editor) focusedLinkButton(w *window.Window) *linkSpan {
	if w == nil || !w.BrowseActive || !w.ViewState.LinkBrowsing || e.WindowManager.GetFocusedWindow() != w {
		return nil
	}
	return e.caretLinkSpan(w)
}

// navFollow (the nav_follow command) activates the focused button: a
// transient notification naming the destination (navigation itself comes
// later). Reports false when not in active browse mode with a focused button,
// so a nav_follow|accept|insert chain falls through.
func (e *Editor) navFollow() bool {
	w := e.WindowManager.GetFocusedWindow()
	span := e.focusedLinkButton(w)
	if span == nil {
		return false
	}
	e.ShowNotification("Link: " + span.Target)
	return true
}

// navLink (nav_next / nav_prior) moves the caret from the focused button to
// the next (dir +1) or previous (dir -1) link in the document, cycling at the
// ends. It captures (returns true) only when a button is currently focused —
// so in a fallthrough chain (tab = nav_next|completion|insert) it yields to
// editing whenever the caret is not inside a link. The move re-arms browse
// mode on the new span via the main loop's updateBrowseState.
func (e *Editor) navLink(dir int) bool {
	w := e.WindowManager.GetFocusedWindow()
	cur := e.focusedLinkButton(w)
	if cur == nil {
		return false
	}
	line, span, ok := e.siblingLink(w, cur, dir)
	if !ok {
		return false
	}
	// Land strictly inside the target (Start+1) so it focuses. Dokuwiki link
	// spans are always >= 4 runes ("[[]]"), so Start+1 < End holds.
	e.setCursorForNav(w, line, span.Start+1)
	e.RequestRender()
	return true
}

// siblingLink finds the link to move to from the currently focused span cur,
// in direction dir, cycling through the document. The reference position is
// cur's own start on the caret line, so the current link is skipped. ok is
// false only when the buffer somehow has no links (cur guarantees at least
// one, so cycling always finds a target — possibly cur itself).
func (e *Editor) siblingLink(w *window.Window, cur *linkSpan, dir int) (int, linkSpan, bool) {
	refLine := w.CursorPos().Line
	refStart := cur.Start
	n := w.Buffer.GetLineCount()

	if dir >= 0 {
		// Forward from the current line: first span past the reference, then
		// the first span on any later line.
		for L := refLine; L < n; L++ {
			for _, s := range e.linkSpansOnLine(w, L) {
				if L > refLine || s.Start > refStart {
					return L, s, true
				}
			}
		}
		// Wrap: the first link in the document.
		for L := 0; L < n; L++ {
			if spans := e.linkSpansOnLine(w, L); len(spans) > 0 {
				return L, spans[0], true
			}
		}
		return 0, linkSpan{}, false
	}

	// Backward: last span before the reference on the current line, then the
	// last span on any earlier line.
	for L := refLine; L >= 0; L-- {
		spans := e.linkSpansOnLine(w, L)
		for i := len(spans) - 1; i >= 0; i-- {
			if L < refLine || spans[i].Start < refStart {
				return L, spans[i], true
			}
		}
	}
	// Wrap: the last link in the document.
	for L := n - 1; L >= 0; L-- {
		if spans := e.linkSpansOnLine(w, L); len(spans) > 0 {
			return L, spans[len(spans)-1], true
		}
	}
	return 0, linkSpan{}, false
}

// setCursorForNav moves the caret to a link target and brings it on screen,
// mirroring the bookkeeping a movement command does (ideal column reset, no
// ghost, viewport follow).
func (e *Editor) setCursorForNav(w *window.Window, line, runePos int) {
	w.SetCursorPos(window.Position{Line: line, Rune: runePos})
	w.HasGhostCursor = false
	w.IdealVisualColumn = 0
	e.ensureCursorVisible(w)
}

// lineButtons is the renderer's ButtonProvider: the button replacements for
// one line of a window, nil unless the window is in browse mode and the line
// has links. Everything here is computed fresh per paint — nothing based on
// line numbers survives the frame.
func (e *Editor) lineButtons(w *window.Window, docLine int) []render.ButtonSpan {
	if w == nil || !w.BrowseActive || !w.ViewState.LinkBrowsing || w.Type != window.MainBuffer {
		return nil
	}
	spans := e.linkSpansOnLine(w, docLine)
	if len(spans) == 0 {
		return nil
	}
	ind := e.LoadedConfig.Indicators
	cls, typ := w.Class, w.Type.Name()
	col := func(name string) string { return e.LoadedConfig.Colors.Resolve(cls, typ, name) }

	pos := w.CursorPos()
	focusedHere := e.WindowManager.GetFocusedWindow() == w && pos.Line == docLine

	out := make([]render.ButtonSpan, 0, len(spans))
	for _, s := range spans {
		focused := focusedHere && s.Start < pos.Rune && pos.Rune < s.End
		capL, capR, shadow := ind.ButtonLeft, ind.ButtonRight, ind.ButtonShadow
		colorName, shadowName := "button", "buttonShadow"
		if focused {
			capL, capR, shadow = ind.FocusedButtonLeft, ind.FocusedButtonRight, ind.FocusedButtonShadow
			colorName, shadowName = "buttonFocused", "buttonShadowFocused"
		}
		var shadowRune rune
		if sr := []rune(shadow); len(sr) > 0 {
			shadowRune = sr[0]
		}
		out = append(out, render.ButtonSpan{
			Start:       s.Start,
			End:         s.End,
			Runes:       []rune(capL + render.SanitizeButtonTitle(s.Title) + capR),
			Shadow:      shadowRune,
			Color:       col(colorName),
			ShadowColor: col(shadowName),
		})
	}
	return out
}

// displayCaretLine mirrors the renderer's substitution for the editor's own
// scroll/visibility math: the caret line as it is actually painted (buttons
// substituted) and the caret position mapped onto it. Identity when the line
// has no buttons.
func (e *Editor) displayCaretLine(w *window.Window, line string, runePos int) (string, int) {
	spans := e.lineButtons(w, w.CursorPos().Line)
	if len(spans) == 0 {
		return line, runePos
	}
	text, docToDisp := render.SubstituteButtons(line, spans)
	if docToDisp == nil {
		return line, runePos
	}
	if runePos < 0 {
		runePos = 0
	}
	if runePos >= len(docToDisp) {
		runePos = len(docToDisp) - 1
	}
	return text, docToDisp[runePos]
}
