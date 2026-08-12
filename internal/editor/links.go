package editor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/phroun/key-sequence-processor/keyseq"
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/render"
	"github.com/phroun/mew/internal/textwidth"
	"github.com/phroun/mew/internal/viewport"
)

// Hyperlink browse mode (rendering pass — no navigation yet).
//
// When a buffer's grammar recognizes links (dokuwiki, including .txt files
// the path-conditional [formats] rules route there), each link renders in one
// of two styles per viewport:
//
//   - caret mode (BrowseActive off): the raw [[target|Title]] text, painted
//     in the "link" color over its syntax colors;
//   - browse mode (BrowseActive on): every link on screen becomes a button —
//     cap + resolved title + cap + shadow cell — via the renderer's
//     substitution layer; the button the caret sits in takes the focused
//     style, its destination shows in the modebar context slot, and accept
//     activates it (a transient notification for now).
//
// Browse mode is entered explicitly — the navigationMode option (set_option /
// ^O N) — and disarmed via nav_cancel or by turning navigationMode off;
// following a link carries it into the destination page. It does NOT arm on
// its own when the caret lands on a link. The caret stays inert inside a
// button: it still knows its rune position within the link's source text, but
// the button paints (and parks the terminal cursor) as one unit.

// linkSpansOnLine returns the grammar-derived link spans for one line of w's
// buffer, or nil when the grammar has none (not linkable, line out of range).
func (e *Editor) linkSpansOnLine(w *viewport.Viewport, docLine int) []linkSpan {
	if w == nil || w.Buffer == nil || w.Type == viewport.PromptViewport {
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
func (e *Editor) markupSpansOnLine(w *viewport.Viewport, docLine int) []markupSpan {
	if w == nil || w.Buffer == nil || w.Type == viewport.PromptViewport {
		return nil
	}
	c := e.ensureSynCache(w.Buffer, docLine)
	if c == nil || !c.linkable || docLine >= len(c.markup) {
		return nil
	}
	return c.markup[docLine]
}

// caretLinkSpan returns the link span the viewport's caret is on, treating the
// range as half-open [Start, End): the caret is "on" the link the moment it
// reaches the first character (the left edge counts), and leaves it only past
// the last (End does not count). The line number is read fresh each call.
func (e *Editor) caretLinkSpan(w *viewport.Viewport) *linkSpan {
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

// navCancel turns browse mode off on the focused viewport (links revert to
// caret-mode link styling until the caret enters another link). Reports
// false when browse mode was not active, so a nav_cancel|cancel|... chain
// falls through to the next command.
func (e *Editor) navCancel() bool {
	w := e.ViewportManager.GetFocusedViewport()
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
// this viewport's nav structures) is buried in the viewport's graveyard for the
// eventual save decision, instead of being released with the invalidated
// forward trail. The newly bound buffer, conversely, leaves every graveyard.
func (e *Editor) swapBuffer(w *viewport.Viewport, buf *buffer.Buffer) {
	w.SwapBuffer(buf, func(b *buffer.Buffer) bool {
		return e.bufferReferencedElsewhere(b, w)
	})
	e.unburyEverywhere(buf)
	// SwapBuffer mints a fresh binding but leaves ViewState as the departed buffer
	// left it. Re-resolve read-only for the new buffer (unless the viewport has an
	// explicit override) so a mew:/ surface's read-only — which it sets on the
	// viewport for the indicator, editing itself being blocked by address — can't
	// stick to an ordinary, editable buffer the reader follows to. Global
	// read-only mode and any class/grammar/overlay read-only re-resolve to true,
	// so this only clears a value nothing configured.
	if w != nil && !w.IsOptionOverridden("readonly") {
		e.applyResolvedOption(w, "readonly")
	}
}

// replaceBuffer swaps w to buf in place WITHOUT growing the nav history (see
// Viewport.ReplaceBuffer). Used for Quick Help's dynamic re-render as the key
// context changes: the departing page is ephemeral and is released outright, so
// successive context changes never pile buffers into the viewport's graveyard.
func (e *Editor) replaceBuffer(w *viewport.Viewport, buf *buffer.Buffer) {
	w.ReplaceBuffer(buf)
	e.unburyEverywhere(buf)
}

// unburyEverywhere releases every graveyard binding of buf across all
// viewports: actively bound again, the buffer is no longer at risk of
// orphaning and the graveyards have no claim to it.
func (e *Editor) unburyEverywhere(buf *buffer.Buffer) {
	if buf == nil {
		return
	}
	for _, w := range e.ViewportManager.AllViewports() {
		w.Unbury(buf)
	}
}

// navHistory walks the focused viewport's buffer-swap history: dir < 0 returns
// to the binding the viewport last swapped away from (nav_history_prior),
// dir > 0 re-advances (nav_history_next). Reports false when there is no
// history in that direction, so command chains fall through.
func (e *Editor) navHistory(dir int, always bool) bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type == viewport.PromptViewport {
		return false
	}
	if !always && !e.navHistoryGatePasses(w) {
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

// navHistoryGatePasses is the always=false gate for nav_history_prior. It passes
// in two cases: the caret is on the actively focused link button (browse mode,
// caret on a link) — mirroring nav_follow's focused-button gate, so a fallthrough
// chain yields to editing off a link — OR the document is read-only. There is
// nothing to edit in a read-only document, so the key is free to mean "go back"
// regardless of browse mode or caret position.
//
// The focused-button check is focusedLinkButton, which itself requires w to BE
// the focused viewport (plus browse mode and the caret on a link), so a focused
// link in some other open document can never satisfy this.
func (e *Editor) navHistoryGatePasses(w *viewport.Viewport) bool {
	if e.viewportReadOnly(w) {
		return true
	}
	return e.focusedLinkButton(w) != nil
}

// viewportReadOnly reports whether edits through w are refused — its ReadOnly
// state or a generated (mew:/) surface that is read-only by address. Silent,
// unlike viewportEditLocked, so it can gate without emitting a warning.
func (e *Editor) viewportReadOnly(w *viewport.Viewport) bool {
	return w != nil && (w.ViewState.ReadOnly ||
		(w.Buffer != nil && isGenPath(w.Buffer.GetFilename())))
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
// viewport's entire back/forward history, releasing the stacked bindings —
// except any holding the LAST reference to a buffer (nothing else, active or
// stacked in any viewport, still holds it): those move to the viewport's
// GRAVEYARD, kept alive solely for the eventual "save its changes?"
// reckoning rather than being orphaned. Reports false when there is no
// history.
func (e *Editor) navHistoryClear() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type == viewport.PromptViewport {
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
// must be the focused viewport, in browse mode, with the caret strictly inside
// the span. nil otherwise.
func (e *Editor) focusedLinkButton(w *viewport.Viewport) *linkSpan {
	if w == nil || !w.BrowseActive || !w.ViewState.LinkBrowsing || e.ViewportManager.GetFocusedViewport() != w {
		return nil
	}
	return e.caretLinkSpan(w)
}

// linkButtonAt returns the rendered link button covering a document position
// of w, independent of focus and of w's caret: w must be in browse mode with
// the link layer on, and the position must fall in a link span (half-open,
// left edge counts). Mouse routing uses it to recognize a button press in an
// UNFOCUSED viewport.
func (e *Editor) linkButtonAt(w *viewport.Viewport, docLine, runePos int) *linkSpan {
	if w == nil || !w.BrowseActive || !w.ViewState.LinkBrowsing {
		return nil
	}
	spans := e.linkSpansOnLine(w, docLine)
	for i := range spans {
		if spans[i].Start <= runePos && runePos < spans[i].End {
			return &spans[i]
		}
	}
	return nil
}

// navFollow (the nav_follow command) NAVIGATES to the link at the caret: the
// target resolves through the dokuwiki reference layers (wikiref.go) to the
// canonical URL of a real file, which is opened IN PLACE — an already-open
// buffer (active or stacked anywhere) is reused, a fresh one is loaded
// otherwise, and the viewport's previous binding goes onto its nav history
// (nav_history_prior returns). Non-wiki targets (schemes, interwiki) and
// unresolved pages show a notification instead.
//
// always=true (the default; ^B F) follows whenever the caret is within/at the
// left edge of a link's source, even in ordinary edit mode — the link layer
// (linkBrowsing) must still be enabled. always=false follows only the focused
// button of navigation mode, and reports false otherwise so a
// `nav_follow false|accept|insert_newline` chain falls through to plain Enter.
func (e *Editor) navFollow(always bool) bool {
	w := e.ViewportManager.GetFocusedViewport()
	var span *linkSpan
	if always {
		// Follow from ordinary edit mode too: the link the caret is within
		// (or at the left edge of), independent of navigation mode. The link
		// layer must still be enabled (linkBrowsing) — the whole feature is
		// off otherwise.
		if w != nil && w.Type != viewport.PromptViewport && w.ViewState.LinkBrowsing {
			span = e.caretLinkSpan(w)
		}
	} else {
		// Navigation-mode only: the focused button (nil unless browse mode is
		// on with the caret on a link), so a fallthrough chain yields to Enter.
		span = e.focusedLinkButton(w)
	}
	if span == nil {
		return false
	}
	return e.followLinkSpan(w, span)
}

// recordLinkVisit marks target (as resolved) visited editor-wide and primes
// the resolution memo keyed by the SOURCE buffer's URL — the buffer the link
// lives in, passed explicitly so it is captured before any buffer swap.
func (e *Editor) recordLinkVisit(srcBuf *buffer.Buffer, target string, res followResolution) {
	key := visitKey(res, target)
	e.markLinkVisited(key, time.Now())
	e.linkResolveCache[e.bufferCanonicalURL(srcBuf)+"\x00"+target] = key
}

// followLinkSpan performs the follow of one link span in viewport w — the
// shared tail of navFollow and the mouse press/release follow, which may act
// on an UNFOCUSED (but visible) viewport. w navigates in place (or spawns per
// the resolution), never gaining or losing focus here.
func (e *Editor) followLinkSpan(w *viewport.Viewport, span *linkSpan) bool {
	// A link inside a generated mew: surface is not an ordinary document
	// reference: hand its target to the surface's follow handler, which turns it
	// into an operation (go to a buffer, switch a tile's viewport, …).
	if handled, isSurface := e.followGeneratedSurfaceLink(w, span.Target); isSurface {
		e.RequestRender()
		return handled
	}
	res := e.resolveFollow(w, span.Target)

	if res.url == "" {
		// The target does not resolve to a real page yet: DON'T mark the link
		// visited — it hasn't been followed. A writable missing page offers
		// creation; the visit is recorded only if the user actually creates it
		// (promptCreatePage). An unresolvable target just reports.
		if res.createURL != "" && res.writable {
			e.promptCreatePage(w.ID, w.Buffer, span, res)
		} else {
			e.ShowNotification(res.message)
		}
		e.RequestRender()
		return true
	}

	// A real destination: record the visit under its RESOLVED identity
	// (editor-wide), so any other spelling of it, in any buffer, now paints in
	// the "recent" style. The paint memo is primed with the fresh resolution.
	e.recordLinkVisit(w.Buffer, span.Target, res)

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
	if res.newViewport {
		// A viewport's root never changes, so a destination under a DIFFERENT
		// root surfaces in a fresh viewport — sharing the underlying buffer
		// when it is already open elsewhere. A full-scheme escape gets a
		// rootless main-area viewport; a registered-wiki destination (help:/...)
		// gets a viewport rooted at that wiki, in the Type/dock the wiki
		// declares, arriving in browse mode.
		var nw *viewport.Viewport
		if def, ok := wikiRegistry[res.wikiName]; ok {
			nw = e.createWikiViewport(buf, def, true)
		} else {
			nw = e.createMainViewport(buf, nil, true)
		}
		nw.WikiRoot = res.root
		nw.WikiName = res.wikiName
		if res.root != "" {
			nw.BrowseActive = nw.ViewState.LinkBrowsing
		}
		e.ShowNotificationTagged("→ "+displayPath(res.url), "navigate")
		e.RequestRender()
		return true
	}
	if buf == w.Buffer {
		// Self-link: nothing to swap (and no history entry to create).
		e.ShowNotificationTagged("Already here: "+span.Target, "navigate")
		e.RequestRender()
		return true
	}
	// Same root by construction (in-wiki resolution): swap in place.
	e.swapBuffer(w, buf)
	// Stay in browse mode: following a link is browsing, and the reader keeps
	// tabbing onward in the destination page.
	w.BrowseActive = true
	e.ensureCursorVisible(w)
	e.ShowNotificationTagged("→ "+displayPath(res.url), "navigate")
	e.RequestRender()
	return true
}

// wikiDisplayName renders a wiki-rooted viewport's page as its extensionless
// scheme form ("help:/start", "help:/sample/widget") — what the user typed
// and what they should SEE — instead of the underlying file base
// ("start.txt"). Returns "" for a non-wiki viewport, a buffer outside the
// viewport's root, or an unknown registry, so the modebar falls back to the
// ordinary filename.
func (e *Editor) wikiDisplayName(w *viewport.Viewport) string {
	if w == nil || w.Buffer == nil || w.WikiName == "" || w.WikiRoot == "" {
		return ""
	}
	def, ok := wikiRegistry[w.WikiName]
	if !ok {
		return ""
	}
	url := e.bufferCanonicalURL(w.Buffer)
	if url == "" || !urlWithin(url, w.WikiRoot) {
		return ""
	}
	rel := strings.TrimPrefix(strings.TrimPrefix(url, w.WikiRoot), "/")
	if def.Ext != "" && strings.HasSuffix(strings.ToLower(rel), strings.ToLower(def.Ext)) {
		rel = rel[:len(rel)-len(def.Ext)]
	}
	return def.Name + ":/" + rel
}

// openWikiScheme opens a registered wiki-scheme reference ("help:/start")
// typed at the Open prompt (or launched from the command line), resolving it
// through the same wiki machinery a followed link uses and surfacing the page
// in a fresh viewport rooted at the wiki — in the viewport Type and dock the wiki
// declares (help is a top-docked ToolViewport), in browse mode. A trailing wiki
// extension is tolerated and stripped ("help:/start.txt" == "help:/start"),
// since the page id is extensionless (the .Ext is implied internally).
//
// The viewport takes focus when focus is set. Returns the opened viewport (nil
// when the page could not be loaded or was only reported not-found) and
// whether the ref was a wiki scheme (handled here regardless of outcome); a
// non-wiki name returns (nil, false) to fall through to the ordinary file
// open.
func (e *Editor) openWikiScheme(ref string, focus bool) (*viewport.Viewport, bool) {
	def, rest, ok := wikiSchemeRef(ref)
	if !ok {
		return nil, false
	}
	// Tolerate (and hide) the page-file extension in what the user typed.
	if def.Ext != "" && strings.HasSuffix(strings.ToLower(rest), strings.ToLower(def.Ext)) {
		rest = rest[:len(rest)-len(def.Ext)]
		ref = def.Name + ":/" + rest
	}

	res := e.resolveFollow(nil, ref)
	if res.url != "" {
		buf := e.findOpenBuffer(res.url)
		if buf == nil {
			loaded, err := e.loadBufferURL(res.url)
			if err != nil {
				e.ShowError("Open " + displayPath(res.url) + ": " + err.Error())
				e.RequestRender()
				return nil, true
			}
			buf = loaded
		}
		nw := e.createWikiViewport(buf, def, focus)
		nw.WikiRoot = res.root
		nw.WikiName = res.wikiName
		nw.BrowseActive = nw.ViewState.LinkBrowsing
		e.ShowNotificationTagged("→ "+displayPath(res.url), "navigate")
		e.RequestRender()
		return nw, true
	}

	// The page does not exist: offer to create it when the wiki is writable,
	// else report it (never a blank buffer under the literal scheme name).
	if res.createURL != "" && res.writable {
		title := rest
		if title == "" {
			title = def.Start
		}
		buf, err := e.createBufferURL(res.createURL, "=== "+title+" ===\n\n")
		if err != nil {
			e.ShowError("Create: " + err.Error())
			e.RequestRender()
			return nil, true
		}
		nw := e.createWikiViewport(buf, def, focus)
		nw.WikiRoot = res.root
		nw.WikiName = res.wikiName
		nw.SetCursorPos(viewport.Position{Line: buf.GetLineCount() - 1, Rune: 0})
		e.ensureCursorVisible(nw)
		e.ShowNotification("New page: " + displayPath(e.canonicalDocURL(res.createURL)) + " (save to create the file)")
		e.RequestRender()
		return nw, true
	}
	e.ShowNotification(res.message)
	e.RequestRender()
	return nil, true
}

// promptCreatePage offers to create an unresolved wiki page, lock-prompt
// style: the description on the top row, the short question on the input
// row, with the prompt buffer offering "y" and "n" above the blank default
// line. Creating mints a buffer named for the page's would-be file, seeded
// with a heading carrying the link's title and the caret parked at the end,
// ready to write — the file itself appears on first save. The page surfaces
// exactly as a successful follow would: in place for the viewport's own wiki,
// a fresh viewport for a cross-root destination.
func (e *Editor) promptCreatePage(viewportID string, srcBuf *buffer.Buffer, span *linkSpan, res followResolution) {
	title := span.Title
	if title == "" {
		title = span.Target
	}
	target := span.Target
	e.PromptMgr.PromptForConfirmationTop("Page not found: "+title, "Create it? [y/N]: ", false,
		func(accepted, yes bool) {
			if !accepted || !yes {
				// Declined: the link stays UNfollowed, painted as unvisited.
				e.RequestRender()
				return
			}
			buf, err := e.createBufferURL(res.createURL, "=== "+title+" ===\n\n")
			if err != nil {
				e.ShowError("Create: " + err.Error())
				e.RequestRender()
				return
			}
			// The page now exists (in memory; it hits disk on save): NOW the
			// link is genuinely followed — record the visit against the source
			// buffer, captured before the swap below.
			e.recordLinkVisit(srcBuf, target, res)
			var target *viewport.Viewport
			if res.newViewport {
				if def, ok := wikiRegistry[res.wikiName]; ok {
					target = e.createWikiViewport(buf, def, true)
				} else {
					target = e.createMainViewport(buf, nil, true)
				}
				target.WikiRoot = res.root
				target.WikiName = res.wikiName
			} else if w := e.ViewportManager.GetViewport(viewportID); w != nil {
				e.swapBuffer(w, buf)
				target = w
			}
			if target != nil {
				// Caret at the end of the seeded page, ready to write.
				target.SetCursorPos(viewport.Position{Line: buf.GetLineCount() - 1, Rune: 0})
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
func (e *Editor) linkTargetVisited(w *viewport.Viewport, target string) bool {
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
// editing whenever the caret is not inside a link. The move keeps browse mode
// active, landing the caret on the new button.
func (e *Editor) navLink(dir int, always bool) bool {
	w := e.ViewportManager.GetFocusedViewport()
	cur := e.focusedLinkButton(w)
	if cur == nil {
		// Gated (always=false): only a FOCUSED button counts, so a chain like
		// tab = nav_next false|completion|insert '\t' yields to editing when
		// the caret is not on a link. This is the shape the tab/S-tab chains
		// depend on and must keep.
		if !always {
			return false
		}
		// Ungated: work from the caret instead. The link layer still has to be
		// on - the whole feature is off otherwise - but browse mode does not,
		// which is what lets ^B tab step links straight out of edit mode.
		if w == nil || w.Type == viewport.PromptViewport || !w.ViewState.LinkBrowsing || w.Buffer == nil {
			return false
		}
		if cur = e.caretLinkSpan(w); cur == nil {
			// Caret is not in a link at all: enter at the first one from here
			// rather than reporting failure, so the command always moves when
			// the document has any link to move to.
			line, span, ok := e.firstLinkFromCaret(w)
			if !ok {
				return false
			}
			w.BrowseActive = true
			e.setCursorForNav(w, line, span.Start+1)
			e.RequestRender()
			return true
		}
		w.BrowseActive = true
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
func (e *Editor) siblingLink(w *viewport.Viewport, cur *linkSpan, dir int) (int, linkSpan, bool) {
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
func (e *Editor) setCursorForNav(w *viewport.Viewport, line, runePos int) {
	w.SetCursorPos(viewport.Position{Line: line, Rune: runePos})
	w.HasGhostCursor = false
	w.IdealVisualColumn = 0
	w.NavIdealSet = false // a non-vertical nav move re-anchors the vertical ideal
	e.ensureCursorVisible(w)
}

// navStart enters nav (browse) mode programmatically. If the caret is already
// in a link it just arms; otherwise it moves to the first link at/after the
// caret (cycling). Fails when the layer is off or the buffer has no links.
func (e *Editor) navStart() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type == viewport.PromptViewport || !w.ViewState.LinkBrowsing || w.Buffer == nil {
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
func (e *Editor) firstLinkFromCaret(w *viewport.Viewport) (int, linkSpan, bool) {
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
	w := e.ViewportManager.GetFocusedViewport()
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
		w.SetCursorPos(viewport.Position{Line: L, Rune: chosen.Start + 1})
		w.HasGhostCursor = false
		e.ensureCursorVisible(w)
		e.RequestRender()
		return true
	}
	// No further link on the current screen. If the page can turn — there is
	// content beyond the visible window in the travel direction — turn it and land
	// on the newly revealed content. If it cannot (at the document edge, or the
	// whole buffer fits on screen), just nudge the caret ONE line in the travel
	// direction from where it is, off any link: a single opposite press then
	// brings it straight back to the button it left, instead of flinging it into a
	// void that takes many presses to climb out of.
	lastLine := w.Buffer.GetLineCount() - 1
	if canTurn := (dir > 0 && bottom < lastLine) || (dir < 0 && top > 0); !canTurn {
		cur := w.CursorPos()
		nl := cur.Line + dir
		if nl < 0 {
			nl = 0
		}
		if nl > lastLine {
			nl = lastLine
		}
		nr := cur.Rune
		if ll := e.getEffectiveLineLen(w.Buffer, nl); nr > ll {
			nr = ll
		}
		w.SetCursorPos(viewport.Position{Line: nl, Rune: nr})
		w.HasGhostCursor = false
		e.ensureCursorVisible(w)
		e.RequestRender()
		return true
	}

	// The page can turn. Paging clears NavIdealSet (via afterVerticalMovement),
	// so save/restore it — a page is part of the same vertical run.
	oldTop, oldBottom := top, bottom
	saved, wasSet := w.NavIdealCol, w.NavIdealSet
	if dir > 0 {
		e.pageDown()
	} else {
		e.pageUp()
	}
	e.trackMove()
	w.NavIdealCol, w.NavIdealSet = saved, wasSet

	// The visible window after the page turn.
	newTop := w.ViewState.ViewOffsetY
	newBottom := newTop + w.ContentHeight - 1
	if newBottom > lastLine {
		newBottom = lastLine
	}
	// The first line that was NOT visible before the page turn: just past the old
	// bottom going down, just before the old top going up. Clamped into the new
	// window (a page that could not scroll — already at an end — pins it there).
	boundary := oldBottom + 1
	if dir < 0 {
		boundary = oldTop - 1
	}
	if boundary < newTop {
		boundary = newTop
	}
	if boundary > newBottom {
		boundary = newBottom
	}
	if boundary < 0 {
		boundary = 0
	}

	// Prefer a link on the newly revealed content — scan in the travel direction
	// starting from the FIRST line that was not visible before the turn
	// (inclusive), so a partial page turn (overlap) never backtracks to a button
	// above the previous caret. Land on the first link line by the run's ideal
	// column, so vertical nav continues from a focused button. With no link in
	// the revealed content, land on that first revealed line itself. target is
	// the run's ideal column, established at the top of navVert.
	land, landRune := boundary, 0
	if dir > 0 {
		for L := boundary; L <= newBottom; L++ {
			if spans := e.linkSpansOnLine(w, L); len(spans) > 0 {
				land = L
				landRune = e.pickLinkByDisplayColumn(w, L, spans, target, dir, tabSize).Start + 1
				break
			}
		}
	} else {
		for L := boundary; L >= newTop; L-- {
			if spans := e.linkSpansOnLine(w, L); len(spans) > 0 {
				land = L
				landRune = e.pickLinkByDisplayColumn(w, L, spans, target, dir, tabSize).Start + 1
				break
			}
		}
	}
	w.SetCursorPos(viewport.Position{Line: land, Rune: landRune})
	w.HasGhostCursor = false
	e.ensureCursorVisible(w)
	e.RequestRender()
	return true
}

// pickLinkByDisplayColumn chooses the link on line L nearest the display-space
// column target: a link whose painted column range contains target wins; else
// the nearest by distance, with ties broken toward the first link moving down
// (dir >= 0) and the last moving up. Columns are measured with buttons
// substituted and bidi applied, so the choice matches what is on screen.
func (e *Editor) pickLinkByDisplayColumn(w *viewport.Viewport, L int, spans []linkSpan, target, dir, tabSize int) linkSpan {
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
func (e *Editor) displayVisualColumn(w *viewport.Viewport, docLine, docRune, tabSize int) int {
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
func (e *Editor) visualColumnNoMarks(w *viewport.Viewport, line string, runePos, tabSize int) int {
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
	w := e.ViewportManager.GetFocusedViewport()
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
// unless the viewport is in browse mode over a linkable (dokuwiki) buffer.
// Computed fresh per paint; nothing based on line numbers survives the frame.
func (e *Editor) lineDisplaySpans(w *viewport.Viewport, docLine int) ([]render.DisplaySpan, bool) {
	if w == nil || !w.BrowseActive || !w.ViewState.LinkBrowsing || w.Type == viewport.PromptViewport || w.Buffer == nil {
		return nil, false
	}
	cls, typ := w.EffectiveClass(), w.Type.Name()
	col := func(name string) string { return e.LoadedConfig.Colors.Resolve(cls, typ, name) }
	raw := strings.TrimRight(w.Buffer.GetLine(docLine), "\n\r")
	runes := []rune(raw)

	var spans []render.DisplaySpan
	doubleWide := false

	// Link buttons.
	ind := e.LoadedConfig.Indicators
	pos := w.CursorPos()
	focusedHere := e.ViewportManager.GetFocusedViewport() == w && pos.Line == docLine
	for _, s := range e.linkSpansOnLine(w, docLine) {
		focused := focusedHere && s.Start <= pos.Rune && pos.Rune < s.End
		pressed := e.mousePressed.active && e.mouseOnCaptured && e.mousePressed.winID == w.ID &&
			e.mousePressed.line == docLine && e.mousePressed.start == s.Start
		hovered := e.mouseHovered.active && e.mouseHovered.winID == w.ID &&
			e.mouseHovered.line == docLine && e.mouseHovered.start == s.Start
		// Key badge: a [[keys#action|alias]] reference resolves to the live
		// binding and renders as a tight badge (no caps, no shadow) in the
		// "key" color — "keyFocused" when the caret is on it. Everything else
		// falls through to the normal link-button styling below.
		if action, verbose, ok := keysRefAction(s.Target); ok {
			name := "key"
			if focused || pressed {
				name = "keyFocused"
			}
			disp := e.keyBindingDisplay(action, s.Title)
			if verbose {
				disp = e.verboseKeys(disp)
			}
			spans = append(spans, render.ButtonSpan{
				Start: s.Start, End: s.End,
				Runes: []rune(render.SanitizeButtonTitle(disp)),
				Color: col(name),
			}.Span())
			continue
		}
		capL, capR, shadow := ind.ButtonLeft, ind.ButtonRight, ind.ButtonShadow
		colorName, shadowName := "button", "buttonShadow"
		switch {
		case pressed:
			// Held down by the mouse: the pressed style, until release or a
			// drag off the button.
			capL, capR, shadow = ind.FocusedButtonLeft, ind.FocusedButtonRight, ind.FocusedButtonShadow
			colorName, shadowName = "buttonPressed", "buttonShadowPressed"
		case focused:
			capL, capR, shadow = ind.FocusedButtonLeft, ind.FocusedButtonRight, ind.FocusedButtonShadow
			colorName, shadowName = "buttonFocused", "buttonShadowFocused"
		case hovered:
			// Pointer over the button (all-motion hosts): the hover style.
			colorName, shadowName = "buttonHover", "buttonShadowHover"
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
			// Selected cells take the button's own selected scheme rather than
			// the plain selection bar, whatever state the button is otherwise
			// in. Shaping is untouched: same caps, same shadow glyph.
			SelColor:       col("buttonSelected"),
			SelShadowColor: col("buttonShadowSelected"),
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
func (e *Editor) displayCaretLine(w *viewport.Viewport, line string, runePos int) (string, int) {
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

// displayCaretDoc is the inverse of displayCaretLine for the caret's line: it
// maps a DISPLAY rune index back to the document rune it came from. Identity
// when the line paints verbatim. Column math for the sticky ideal column runs
// in display space (that is where the caret is painted, and where a heading's
// "======" markers no longer take columns), so the landing position has to
// come back through here before it can be a cursor position.
func (e *Editor) displayCaretDoc(w *viewport.Viewport, line string, dispRune int) int {
	spans, dw := e.lineDisplaySpans(w, w.CursorPos().Line)
	if len(spans) == 0 && !dw {
		return dispRune
	}
	_, dispToDoc := render.SubstituteDisplay(line, spans, dw)
	if dispToDoc == nil {
		return dispRune
	}
	if dispRune < 0 {
		dispRune = 0
	}
	return displayToDoc(dispToDoc, dispRune)
}

// keysRefAction extracts the action name from a [[keys#action|alias]] link
// target ("keys#go_page_prior" -> "go_page_prior"), or reports false. The
// "keys_verbose#" prefix requests the same live binding spelled out in prose
// (Ctrl+B then C) for beginner-facing help — verbose is true then. This is the
// help system's live-keybinding reference: a plain dokuwiki internal link on
// the web (to the "keys" page anchor), a live key badge in mew.
//
// A dokuwiki anchor cannot contain "|" (it would separate the target from the
// title) or "&" (entity trouble), yet a fallback-chain command name literally
// contains "|" (e.g. "buffer_redo|buffer_undo"). So the author writes "." where
// a "|" belongs and "," where an "&" belongs; we decode them back here before
// matching against the keymap.
func keysRefAction(target string) (action string, verbose, ok bool) {
	prefix := ""
	switch {
	case strings.HasPrefix(target, "keys_verbose#"):
		prefix, verbose = "keys_verbose#", true
	case strings.HasPrefix(target, "keys#"):
		prefix = "keys#"
	default:
		return "", false, false
	}
	a := strings.TrimSpace(target[len(prefix):])
	if a == "" {
		return "", verbose, false
	}
	a = strings.ReplaceAll(a, ".", "|")
	a = strings.ReplaceAll(a, ",", "&")
	return a, verbose, true
}

// keyBindingDisplay resolves the SINGLE key a badge shows for an action. The
// candidate set is every key bound EXACTLY to the action — a binding is an
// explicit action (or an "a|b" fallback chain), and a badge never shows a key
// that runs something else, so the command match stays exact (keys#buffer_redo
// .buffer_undo answers the ^Z chain; keys#buffer_redo alone matches no key
// whose command IS just that). The CHOICE among candidates is by how well each
// key SEQUENCE matches the author's given binding (preferred, the link alias):
// an exact key, else the closest shared beginning, else the closest shared end,
// else the last-configured key. When there are no candidates it falls back to
// the documented alias, or the bare action name.
func (e *Editor) keyBindingDisplay(action, preferred string) string {
	if e.KeyProcessor == nil {
		return preferred
	}
	var seqs []keyCandidate
	for raw, cmd := range e.KeyProcessor.GetAllMappings() {
		if cmd != action {
			continue
		}
		// Show the keys as pressed: capture/override prefixes are precedence,
		// not keystrokes, and a wildcard names no key at all. The RAW spelling
		// travels alongside, because that is what provenance is filed under.
		if seq, ok := keyseq.DisplayKey(raw); ok {
			seqs = append(seqs, keyCandidate{seq: seq, raw: raw})
		}
	}
	if len(seqs) == 0 {
		if preferred != "" {
			return preferred
		}
		return action
	}
	return e.chooseKeyBinding(seqs, preferred)
}

// keyCandidate is one key bound to the action a badge is resolving: the
// spelling to SHOW, and the RAW mapping key it came from.
//
// The two differ whenever a binding carries a capture/override prefix, and the
// difference matters here: the prefix is precedence rather than a keystroke, so
// it must not be shown — but provenance is filed under the raw spelling, which
// is what "last configured" is decided from. Looking provenance up by the
// display spelling missed every prefixed binding, which then read as a built-in
// (System, precedence 0) and could lose the tie-break to the very binding it
// was written to outrank.
type keyCandidate struct {
	seq string // the key as pressed, shown in the badge
	raw string // the mapping key as written, which provenance is keyed by
}

// chooseKeyBinding picks one key from seqs (all bound to the same action) to
// display, ranking each key SEQUENCE against the author's given binding
// (preferred): an exact key wins; else the key whose beginning matches
// preferred most closely; else whose end does; else — no alias, or nothing
// shares a start or end — the last-configured key. Precedence (then the
// sequence text) breaks every tie: "the last one configured, or the last among
// ties."
func (e *Editor) chooseKeyBinding(seqs []keyCandidate, preferred string) string {
	// better reports whether a is the stronger "last configured" than b —
	// higher precedence, and the greater sequence text as a deterministic
	// stand-in for "last" when precedence ties (a mapping that never came
	// through the config stream at all sits at 0).
	better := func(a, b keyCandidate) bool {
		oa, ob := e.originFor(a.raw), e.originFor(b.raw)
		if oa.Precedence != ob.Precedence {
			return oa.Precedence > ob.Precedence
		}
		return a.seq > b.seq
	}
	if preferred != "" {
		for _, s := range seqs {
			if s.seq == preferred {
				return s.seq // exact key match
			}
		}
		// Longest shared beginning, then (only if none) longest shared end.
		for _, suffix := range []bool{false, true} {
			var best keyCandidate
			bestScore := 0
			for _, s := range seqs {
				sc := sharedAffixLen(s.seq, preferred, suffix)
				if sc == 0 {
					continue
				}
				if best.seq == "" || sc > bestScore || (sc == bestScore && better(s, best)) {
					best, bestScore = s, sc
				}
			}
			if best.seq != "" {
				return best.seq
			}
		}
	}
	// No alias, or nothing shared its start or end: last configured.
	best := seqs[0]
	for _, s := range seqs[1:] {
		if better(s, best) {
			best = s
		}
	}
	return best.seq
}

// sharedAffixLen counts the runes a and b share from the front (suffix=false)
// or the back (suffix=true).
func sharedAffixLen(a, b string, suffix bool) int {
	ra, rb := []rune(a), []rune(b)
	n := len(ra)
	if len(rb) < n {
		n = len(rb)
	}
	count := 0
	for i := 0; i < n; i++ {
		ca, cb := ra[i], rb[i]
		if suffix {
			ca, cb = ra[len(ra)-1-i], rb[len(rb)-1-i]
		}
		if ca != cb {
			break
		}
		count++
	}
	return count
}

// originFor returns the provenance of a RAW mapping key (the spelling as
// written, capture/override prefixes and all — that is how the origins map is
// keyed, in lockstep with the keymap itself), or the built-in
// default (AuthorSystem, precedence 0) when the key carries no recorded origin.
func (e *Editor) originFor(raw string) config.MappingOrigin {
	if o, ok := e.mappingOrigins[raw]; ok {
		return o
	}
	return config.MappingOrigin{Author: config.AuthorSystem}
}
