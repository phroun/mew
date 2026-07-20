package editor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/render"
	"github.com/phroun/mew/internal/textwidth"
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

// markupSpansOnLine returns the grammar-derived markup runs (bold/italic/
// underline/heading) for one line, or nil.
func (e *Editor) markupSpansOnLine(w *window.Window, docLine int) []markupSpan {
	if w == nil || w.Buffer == nil || w.Type != window.MainBuffer {
		return nil
	}
	c := e.ensureSynCache(w.Buffer, docLine)
	if c == nil || !c.linkable || docLine >= len(c.markup) {
		return nil
	}
	return c.markup[docLine]
}

// caretLinkSpan returns the link span the window's caret is on, treating the
// range as half-open [Start, End): the caret is "on" the link the moment it
// reaches the first character (the left edge counts), and leaves it only past
// the last (End does not count). The line number is read fresh each call.
func (e *Editor) caretLinkSpan(w *window.Window) *linkSpan {
	if w == nil {
		return nil
	}
	pos := w.CursorPos()
	spans := e.linkSpansOnLine(w, pos.Line)
	for i := range spans {
		if spans[i].Start <= pos.Rune && pos.Rune < spans[i].End {
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
	w.NavIdealSet = false
	e.RequestRender()
	return true
}

// swapBuffer swaps w to buf with orphan protection: a forward-history
// binding whose buffer would lose its last reference (held nowhere outside
// this window's nav structures) is buried in the window's graveyard for the
// eventual save decision, instead of being released with the invalidated
// forward trail. The newly bound buffer, conversely, leaves every graveyard.
func (e *Editor) swapBuffer(w *window.Window, buf *buffer.Buffer) {
	w.SwapBuffer(buf, func(b *buffer.Buffer) bool {
		return e.bufferReferencedElsewhere(b, w)
	})
	e.unburyEverywhere(buf)
}

// unburyEverywhere releases every graveyard binding of buf across all
// windows: actively bound again, the buffer is no longer at risk of
// orphaning and the graveyards have no claim to it.
func (e *Editor) unburyEverywhere(buf *buffer.Buffer) {
	if buf == nil {
		return
	}
	for _, w := range e.WindowManager.AllWindows() {
		w.Unbury(buf)
	}
}

// navHistory walks the focused window's buffer-swap history: dir < 0 returns
// to the binding the window last swapped away from (nav_history_prior),
// dir > 0 re-advances (nav_history_next). Reports false when there is no
// history in that direction, so command chains fall through.
func (e *Editor) navHistory(dir int) bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Type != window.MainBuffer {
		return false
	}
	var ok bool
	if dir < 0 {
		ok = w.NavHistoryPrior()
	} else {
		ok = w.NavHistoryNext()
	}
	if !ok {
		return false
	}
	// The restored buffer is actively bound again: no graveyard holds a
	// claim on it now.
	e.unburyEverywhere(w.Buffer)
	// The restored binding carries its own viewport, but edits made while it
	// was stacked may have slid the caret out of it: re-ensure visibility on
	// the restored geometry.
	e.ensureCursorVisible(w)
	e.RequestRender()
	return true
}

// navClearVisited (the nav_clear command) forgets every visited link,
// editor-wide: the presence set and the chronological log reset, so all
// links repaint in their unvisited style. Reports false when there was
// nothing to clear, so chains fall through.
func (e *Editor) navClearVisited() bool {
	if len(e.linkVisitSeen) == 0 && len(e.linkVisitLog) == 0 {
		return false
	}
	e.linkVisitSeen = make(map[string]bool)
	e.linkVisitLog = nil
	e.ShowNotification("Visited links cleared")
	e.RequestRender()
	return true
}

// navHistoryClear (the nav_history_clear command) empties the focused
// window's entire back/forward history, releasing the stacked bindings —
// except any holding the LAST reference to a buffer (nothing else, active or
// stacked in any window, still holds it): those move to the window's
// GRAVEYARD, kept alive solely for the eventual "save its changes?"
// reckoning rather than being orphaned. Reports false when there is no
// history.
func (e *Editor) navHistoryClear() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Type != window.MainBuffer {
		return false
	}
	if prior, next := w.NavHistoryDepths(); prior+next == 0 {
		return false
	}
	dropped, buried := w.ClearNavHistory(func(b *buffer.Buffer) bool {
		return e.bufferReferencedElsewhere(b, w)
	})
	switch {
	case buried > 0:
		e.ShowNotification(fmt.Sprintf("History cleared: %d dropped, %d moved to graveyard", dropped, buried))
	default:
		e.ShowNotification("History cleared")
	}
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

// navFollow (the nav_follow command) activates the focused button and
// NAVIGATES: the target resolves through the dokuwiki reference layers
// (wikiref.go) to the canonical URL of a real file, which is opened IN PLACE —
// an already-open buffer (active or stacked anywhere) is reused, a fresh one
// is loaded otherwise, and the window's previous binding goes onto its nav
// history (nav_history_prior returns). Non-wiki targets (schemes, interwiki)
// and unresolved pages show a notification instead. Reports false when not in
// active browse mode with a focused button, so a nav_follow|accept|insert
// chain falls through.
func (e *Editor) navFollow() bool {
	w := e.WindowManager.GetFocusedWindow()
	span := e.focusedLinkButton(w)
	if span == nil {
		return false
	}
	// Resolve, then record the visit under its RESOLVED identity (editor-wide):
	// any other spelling of the same destination, in any buffer, now paints in
	// the "recent" style. The paint memo is primed with the fresh resolution.
	res := e.resolveFollow(w, span.Target)
	key := visitKey(res, span.Target)
	e.markLinkVisited(key, time.Now())
	e.linkResolveCache[e.bufferCanonicalURL(w.Buffer)+"\x00"+span.Target] = key

	if res.url == "" {
		if res.createURL != "" && res.writable {
			e.promptCreatePage(w.ID, span, res)
		} else {
			e.ShowNotification(res.message)
		}
		e.RequestRender()
		return true
	}

	buf := e.findOpenBuffer(res.url)
	if buf == nil {
		loaded, err := e.loadBufferURL(res.url)
		if err != nil {
			e.ShowError("Open " + displayPath(res.url) + ": " + err.Error())
			e.RequestRender()
			return true
		}
		buf = loaded
	}
	if res.newWindow {
		// A window's root never changes, so a destination under a DIFFERENT
		// root surfaces in a fresh window — sharing the underlying buffer
		// when it is already open elsewhere. A full-scheme escape gets a
		// rootless window; a registered-wiki destination (help:/...) gets a
		// window rooted at that wiki, arriving in browse mode.
		nw := e.createMainWindow(buf, nil, true)
		nw.WikiRoot = res.root
		nw.WikiName = res.wikiName
		if res.root != "" {
			nw.BrowseActive = nw.ViewState.LinkBrowsing
		}
		e.ShowNotification("→ " + displayPath(res.url))
		e.RequestRender()
		return true
	}
	if buf == w.Buffer {
		// Self-link: nothing to swap (and no history entry to create).
		e.ShowNotification("Already here: " + span.Target)
		e.RequestRender()
		return true
	}
	// Same root by construction (in-wiki resolution): swap in place.
	e.swapBuffer(w, buf)
	// Stay in browse mode: following a link is browsing, and the reader keeps
	// tabbing onward in the destination page.
	w.BrowseActive = true
	e.ensureCursorVisible(w)
	e.ShowNotification("→ " + displayPath(res.url))
	e.RequestRender()
	return true
}

// promptCreatePage offers to create an unresolved wiki page, lock-prompt
// style: the description on the top row, the short question on the input
// row, with the prompt buffer offering "y" and "n" above the blank default
// line. Creating mints a buffer named for the page's would-be file, seeded
// with a heading carrying the link's title and the caret parked at the end,
// ready to write — the file itself appears on first save. The page surfaces
// exactly as a successful follow would: in place for the window's own wiki,
// a fresh window for a cross-root destination.
func (e *Editor) promptCreatePage(windowID string, span *linkSpan, res followResolution) {
	title := span.Title
	if title == "" {
		title = span.Target
	}
	e.PromptMgr.PromptForConfirmationTop("Page not found: "+title, "Create it? [y/N]: ", false,
		func(accepted, yes bool) {
			if !accepted || !yes {
				e.RequestRender()
				return
			}
			buf, err := e.createBufferURL(res.createURL, "=== "+title+" ===\n\n")
			if err != nil {
				e.ShowError("Create: " + err.Error())
				e.RequestRender()
				return
			}
			var target *window.Window
			if res.newWindow {
				target = e.createMainWindow(buf, nil, true)
				target.WikiRoot = res.root
				target.WikiName = res.wikiName
			} else if w := e.WindowManager.GetWindow(windowID); w != nil {
				e.swapBuffer(w, buf)
				target = w
			}
			if target != nil {
				// Caret at the end of the seeded page, ready to write.
				target.SetCursorPos(window.Position{Line: buf.GetLineCount() - 1, Rune: 0})
				e.ensureCursorVisible(target)
			}
			e.ShowNotification("New page: " + displayPath(e.canonicalDocURL(res.createURL)) + " (save to create the file)")
			e.RequestRender()
		})
}

// displayPath renders a canonical URL for a human: the path part of a
// file:/// URL, the URL itself otherwise.
func displayPath(url string) string {
	if p := strings.TrimPrefix(url, "file://"); p != url {
		return p
	}
	return url
}

// linkVisit records one hyperlink follow: the resolved visit key and when.
type linkVisit struct {
	Key string
	At  time.Time
}

// markLinkVisited records a visit under its resolved identity: the presence
// set answers "visited?" in O(1) and the log keeps the chronological record.
func (e *Editor) markLinkVisited(key string, at time.Time) {
	if key == "" {
		return
	}
	e.linkVisitSeen[key] = true
	e.linkVisitLog = append(e.linkVisitLog, linkVisit{Key: key, At: at})
}

// visitKey is a link target's visit identity: the canonical URL it resolves
// to, or the trimmed raw target when the resolution yields none (external
// schemes, interwiki, missing pages — those are destinations too). Two
// spellings in two buffers that resolve to one file share one identity.
func visitKey(res followResolution, target string) string {
	if res.url != "" {
		return res.url
	}
	return strings.TrimSpace(target)
}

// linkTargetVisited answers the PAINT-TIME "draw this link recent?" question:
// the target's visit key, memoized per (source document, raw target) so the
// renderer never re-walks the filesystem per frame, checked against the
// editor-wide visited set. The memo can go stale when a previously missing
// page appears on disk mid-session — a cosmetic staleness only; navFollow
// always resolves fresh.
func (e *Editor) linkTargetVisited(w *window.Window, target string) bool {
	cacheKey := e.bufferCanonicalURL(w.Buffer) + "\x00" + target
	key, ok := e.linkResolveCache[cacheKey]
	if !ok {
		key = visitKey(e.resolveFollow(w, target), target)
		e.linkResolveCache[cacheKey] = key
	}
	return e.linkVisitSeen[key]
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
	w.NavIdealSet = false // a non-vertical nav move re-anchors the vertical ideal
	e.ensureCursorVisible(w)
}

// navStart enters nav (browse) mode programmatically. If the caret is already
// in a link it just arms; otherwise it moves to the first link at/after the
// caret (cycling). Fails when the layer is off or the buffer has no links.
func (e *Editor) navStart() bool {
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Type != window.MainBuffer || !w.ViewState.LinkBrowsing || w.Buffer == nil {
		return false
	}
	if e.caretLinkSpan(w) != nil {
		if !w.BrowseActive {
			w.BrowseActive = true
			e.RequestRender()
		}
		return true
	}
	line, span, ok := e.firstLinkFromCaret(w)
	if !ok {
		return false
	}
	w.BrowseActive = true
	e.setCursorForNav(w, line, span.Start+1)
	e.RequestRender()
	return true
}

// firstLinkFromCaret finds the first link at/after the caret in document
// order, wrapping to the top. ok=false when the buffer has no links.
func (e *Editor) firstLinkFromCaret(w *window.Window) (int, linkSpan, bool) {
	pos := w.CursorPos()
	n := w.Buffer.GetLineCount()
	for L := pos.Line; L < n; L++ {
		for _, s := range e.linkSpansOnLine(w, L) {
			if L > pos.Line || s.Start >= pos.Rune {
				return L, s, true
			}
		}
	}
	for L := 0; L < n; L++ {
		if spans := e.linkSpansOnLine(w, L); len(spans) > 0 {
			return L, spans[0], true
		}
	}
	return 0, linkSpan{}, false
}

// navVert (nav_down / nav_up) moves to the nearest link on the next / previous
// link-bearing line, but never scrolls past the current screen: when there is
// no further link line on screen it pages instead (go_page_next / go_page_prior)
// and still reports success. On the target line the link is chosen by the ideal
// caret column — the one that overlaps it, else the nearest, with the first
// (down) / last (up) link as the tiebreak. Acts only when a button is focused
// at activation.
func (e *Editor) navVert(dir int) bool {
	w := e.WindowManager.GetFocusedWindow()
	// Act only when a button is already focused at activation (like the other
	// directional nav commands). After the paging fallback lands the caret off
	// a link, a further nav_up/down does nothing until a link is focused again.
	if e.focusedLinkButton(w) == nil {
		return false
	}
	tabSize := e.tabSize(w)
	// Establish the target column once per vertical run (display space, so it
	// matches where the caret actually sits on screen), then preserve it across
	// the run so repeated up/down keep a consistent column like normal caret
	// up/down does — regardless of where each link lands.
	if !w.NavIdealSet {
		w.NavIdealCol = e.displayVisualColumn(w, w.CursorPos().Line, w.CursorPos().Rune, tabSize)
		w.NavIdealSet = true
	}
	target := w.NavIdealCol

	top := w.ViewState.ViewOffsetY
	bottom := top + w.ContentHeight - 1
	if n := w.Buffer.GetLineCount() - 1; bottom > n {
		bottom = n
	}
	for L := w.CursorPos().Line + dir; L >= top && L <= bottom; L += dir {
		spans := e.linkSpansOnLine(w, L)
		if len(spans) == 0 {
			continue
		}
		chosen := e.pickLinkByDisplayColumn(w, L, spans, target, dir, tabSize)
		// Move without disturbing the vertical ideal (keep the run's target).
		w.SetCursorPos(window.Position{Line: L, Rune: chosen.Start + 1})
		w.HasGhostCursor = false
		e.ensureCursorVisible(w)
		e.RequestRender()
		return true
	}
	// No further link on the current screen: page, and treat that as success.
	// Paging clears NavIdealSet (via afterVerticalMovement), so save/restore it
	// — a page is part of the same vertical run.
	saved, wasSet := w.NavIdealCol, w.NavIdealSet
	if dir > 0 {
		e.pageDown()
	} else {
		e.pageUp()
	}
	e.trackMove()
	w.NavIdealCol, w.NavIdealSet = saved, wasSet
	e.RequestRender()
	return true
}

// pickLinkByDisplayColumn chooses the link on line L nearest the display-space
// column target: a link whose painted column range contains target wins; else
// the nearest by distance, with ties broken toward the first link moving down
// (dir >= 0) and the last moving up. Columns are measured with buttons
// substituted and bidi applied, so the choice matches what is on screen.
func (e *Editor) pickLinkByDisplayColumn(w *window.Window, L int, spans []linkSpan, target, dir, tabSize int) linkSpan {
	best := spans[0]
	bestDist := -1
	for _, s := range spans {
		c0 := e.displayVisualColumn(w, L, s.Start, tabSize)
		c1 := e.displayVisualColumn(w, L, s.End, tabSize)
		lo, hi := c0, c1
		if lo > hi { // RTL: the button's start cell sits at the higher column
			lo, hi = hi, lo
		}
		if target >= lo && target < hi {
			return s // overlaps the target column
		}
		d := lo - target
		if d < 0 {
			d = target - (hi - 1)
		}
		if d < 0 {
			d = 0
		}
		if bestDist < 0 || d < bestDist || (dir < 0 && d == bestDist) {
			best, bestDist = s, d
		}
	}
	return best
}

// displayVisualColumn returns the on-screen visual column of a document rune on
// line docLine, with browse-mode buttons substituted (so a link measures where
// its button paints) and bidi applied. A doc rune inside a link maps to its
// button's start cell — the same place the caret parks.
func (e *Editor) displayVisualColumn(w *window.Window, docLine, docRune, tabSize int) int {
	raw := strings.TrimRight(w.Buffer.GetLine(docLine), "\n\r")
	text := raw
	dr := docRune
	if spans, dw := e.lineDisplaySpans(w, docLine); len(spans) > 0 || dw {
		t, docToDisp := render.SubstituteButtons(raw, spans, dw)
		if docToDisp != nil {
			text = t
			if dr < 0 {
				dr = 0
			}
			if dr >= len(docToDisp) {
				dr = len(docToDisp) - 1
			}
			dr = docToDisp[dr]
		}
	}
	return e.visualColumnNoMarks(w, text, dr, tabSize)
}

// visualColumnNoMarks is the bidi-aware visual column of a rune position on an
// arbitrary (not necessarily caret) line, without showMarks cells — safe to
// call for any line during vertical link picking.
func (e *Editor) visualColumnNoMarks(w *window.Window, line string, runePos, tabSize int) int {
	runes := []rune(line)
	if runePos < 0 {
		runePos = 0
	}
	if runePos > len(runes) {
		runePos = len(runes)
	}
	if layout := e.layoutFor(w, runes); layout != nil {
		cols, total := e.bidiColumns(runes, layout, nil, tabSize)
		if runePos >= len(runes) {
			return total
		}
		return cols[runePos]
	}
	return e.plainVisualColumn(line, runePos, tabSize)
}

// navHoriz (nav_left / nav_right) moves to the link optically left (dir -1) or
// right (dir +1) of the focused link, on the same line only. It orders links
// by visual column (bidi-aware), so under RTL "left" moves toward higher rune
// numbers. Requires a focused button; no wrap. Returns false when there is no
// link on that side.
func (e *Editor) navHoriz(dir int) bool {
	w := e.WindowManager.GetFocusedWindow()
	cur := e.focusedLinkButton(w)
	if cur == nil {
		return false
	}
	line := w.CursorPos().Line
	raw := strings.TrimRight(w.Buffer.GetLine(line), "\n\r")
	tabSize := e.tabSize(w)
	curCol := e.runeToVisualColumn(w, raw, cur.Start, tabSize)

	found := false
	var best linkSpan
	var bestCol int
	for _, s := range e.linkSpansOnLine(w, line) {
		if s.Start == cur.Start {
			continue
		}
		c := e.runeToVisualColumn(w, raw, s.Start, tabSize)
		if dir > 0 { // optical right: the nearest link at a higher visual column
			if c > curCol && (!found || c < bestCol) {
				found, best, bestCol = true, s, c
			}
		} else { // optical left: the nearest at a lower visual column
			if c < curCol && (!found || c > bestCol) {
				found, best, bestCol = true, s, c
			}
		}
	}
	if !found {
		return false
	}
	e.setCursorForNav(w, line, best.Start+1)
	e.RequestRender()
	return true
}

// plainVisualColumn is a bidi-agnostic, mark-free visual column of a rune
// position on any line — tabs expand to the next stop, control chars take two
// cells, other runes their terminal width. Used for vertical link picking,
// where a consistent proxy across non-caret lines matters more than exact
// bidi placement.
func (e *Editor) plainVisualColumn(line string, runePos, tabSize int) int {
	runes := []rune(line)
	if runePos > len(runes) {
		runePos = len(runes)
	}
	if tabSize <= 0 {
		tabSize = 8
	}
	col := 0
	for i := 0; i < runePos; i++ {
		r := runes[i]
		switch {
		case r == '\t':
			col += tabSize - (col % tabSize)
		case r < 0x20 || r == 0x7f:
			col += 2
		default:
			if wd := textwidth.Rune(r); wd > 0 {
				col += wd
			}
		}
	}
	return col
}

// lineDisplaySpans is the renderer's DisplayProvider: the browse-mode display
// transform for one line — link buttons, dokuwiki markup marker-hiding and
// heading restyle — plus whether the line is drawn double-width. nil/false
// unless the window is in browse mode over a linkable (dokuwiki) buffer.
// Computed fresh per paint; nothing based on line numbers survives the frame.
func (e *Editor) lineDisplaySpans(w *window.Window, docLine int) ([]render.DisplaySpan, bool) {
	if w == nil || !w.BrowseActive || !w.ViewState.LinkBrowsing || w.Type != window.MainBuffer || w.Buffer == nil {
		return nil, false
	}
	cls, typ := w.Class, w.Type.Name()
	col := func(name string) string { return e.LoadedConfig.Colors.Resolve(cls, typ, name) }
	raw := strings.TrimRight(w.Buffer.GetLine(docLine), "\n\r")
	runes := []rune(raw)

	var spans []render.DisplaySpan
	doubleWide := false

	// Link buttons.
	ind := e.LoadedConfig.Indicators
	pos := w.CursorPos()
	focusedHere := e.WindowManager.GetFocusedWindow() == w && pos.Line == docLine
	for _, s := range e.linkSpansOnLine(w, docLine) {
		focused := focusedHere && s.Start <= pos.Rune && pos.Rune < s.End
		capL, capR, shadow := ind.ButtonLeft, ind.ButtonRight, ind.ButtonShadow
		colorName, shadowName := "button", "buttonShadow"
		switch {
		case focused:
			capL, capR, shadow = ind.FocusedButtonLeft, ind.FocusedButtonRight, ind.FocusedButtonShadow
			colorName, shadowName = "buttonFocused", "buttonShadowFocused"
		case e.linkTargetVisited(w, s.Target):
			colorName, shadowName = "buttonRecent", "buttonShadowRecent"
		}
		var shadowRune rune
		if sr := []rune(shadow); len(sr) > 0 {
			shadowRune = sr[0]
		}
		spans = append(spans, render.ButtonSpan{
			Start: s.Start, End: s.End,
			Runes:       []rune(capL + render.SanitizeButtonTitle(s.Title) + capR),
			Shadow:      shadowRune,
			Color:       col(colorName),
			ShadowColor: col(shadowName),
		}.Span())
	}

	// Markup: hide markers, keep/restyle the content between them.
	for _, m := range e.markupSpansOnLine(w, docLine) {
		if m.MarkLeft+m.MarkRight == 0 {
			continue // no markers to hide
		}
		cs, ce := m.Start+m.MarkLeft, m.End-m.MarkRight
		if cs < 0 || ce > len(runes) || cs >= ce {
			continue
		}
		content := runes[cs:ce]
		docs := make([]int, len(content))
		styles := make([]string, len(content))
		var forced string
		if m.Kind == markupHeading {
			forced = headingSGR(col("heading"), m.Level)
			doubleWide = doubleWide || m.Level <= 2
		}
		for i := range content {
			docs[i] = cs + i
			styles[i] = forced // "" for inline: keep the grammar's bold/italic/underline
		}
		spans = append(spans, render.DisplaySpan{
			Start: m.Start, End: m.End, Runes: append([]rune(nil), content...),
			Doc: docs, Style: styles,
		})
	}

	spans = mergeDisplaySpans(spans)
	return spans, doubleWide
}

// mergeDisplaySpans sorts spans by Start and drops any that overlap an earlier
// one (a link inside a heading, say): the first-registered wins. The result is
// ordered and non-overlapping, as the substitution requires.
func mergeDisplaySpans(spans []render.DisplaySpan) []render.DisplaySpan {
	if len(spans) < 2 {
		return spans
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	out := spans[:0:0]
	end := -1
	for _, s := range spans {
		if s.Start < end {
			continue
		}
		out = append(out, s)
		end = s.End
	}
	return out
}

// headingSGR builds the per-level heading SGR from the base heading color:
// L1 bold+underline, L2 underline, L3 bold+underline, L4 underline, L5 plain.
// (Double-width is a line attribute, applied separately.) The base color
// starts with a reset, so appended \e[1m/\e[4m add attributes without clearing.
func headingSGR(base string, level int) string {
	bold := level == 1 || level == 3
	underline := level >= 1 && level <= 4
	if bold {
		base += "\x1b[1m"
	}
	if underline {
		base += "\x1b[4m"
	}
	return base
}

// displayCaretLine mirrors the renderer's substitution for the editor's own
// scroll/visibility math: the caret line as it is actually painted and the
// caret position mapped onto it. Identity when no transform applies.
func (e *Editor) displayCaretLine(w *window.Window, line string, runePos int) (string, int) {
	spans, dw := e.lineDisplaySpans(w, w.CursorPos().Line)
	if len(spans) == 0 && !dw {
		return line, runePos
	}
	text, docToDisp := render.SubstituteButtons(line, spans, dw)
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
