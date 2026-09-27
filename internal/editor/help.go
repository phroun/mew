package editor

import (
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// There is ONE docked help viewport (Tag "help", top dock): help_toggle NAVIGATES
// it between help "locations" — the built-in Quick Help (the WordStar command
// reference) and help:/ wiki pages — building the viewport's own nav history, so
// its back button returns to where the reader came from. help_toggle on the
// location already showing closes the viewport. buffer_open_file, by contrast,
// opens ordinary UNtagged help viewports that can stack; only this slot is
// tagged, and only help_toggle manages it. The Quick Help checkmark tracks
// whether the slot is currently showing Quick Help.
const helpViewportTag = "help"

// The docked help slot carries one of two viewport CLASSES depending on its role,
// so per-class config (colors, mappings, options overlays) can target each: a
// regular help page is class "help", Quick Help is class "quickhelp".
// applyHelpViewportChrome flips it as the slot changes role. The Tag stays "help"
// either way, so helpViewport() still finds the slot.
const helpViewportClass = "help"

const quickHelpClass = "quickhelp"

// helpMaxHeightFraction caps a regular (non-Quick) help page at half the mew
// area height (less the 2 rows reserved for chrome): the layout resolves it live
// as floor((mewHeight-2) * 0.5), so the cap tracks the terminal size instead of a
// fixed row count. Quick Help ignores it and fits its content instead.
const helpMaxHeightFraction = 0.5

// quickHelpDocURL is the synthetic identity of the Quick Help buffer. It lives
// in mew's GENERATED "mew:" scheme (not the box: storage tree), so it is never
// read from disk, never resolves as a wiki page, and just gives the location a
// stable URL to compare against.
const quickHelpDocURL = "mew:/quickhelp"

// helpViewport returns the single docked help viewport (Tag "help"), or nil.
func (e *Editor) helpViewport() *viewport.Viewport {
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if w.Tag == helpViewportTag {
			return w
		}
	}
	return nil
}

// quickHelpViewportOpen reports whether the docked help viewport is open AND in
// Quick Help (dynamic, context-following) mode — what the "Quick Help"
// checkmark reflects. It reads the quickHelpMode flag rather than the buffer
// URL: Quick Help now shows real help pages, so URL alone can't tell it apart
// from a page reached by browsing. syncQuickHelpMode() first reconciles the
// flag with reality (the viewport may have closed, or the user may have browsed
// away) so the answer is always current.
func (e *Editor) quickHelpViewportOpen() bool {
	e.syncQuickHelpMode()
	return e.quickHelpMode && e.helpViewport() != nil
}

// syncQuickHelpMode reconciles quickHelpMode with the docked help viewport: quick
// mode ends the moment the viewport closes or its buffer drifts from the one
// Quick Help installed (the user followed a link, walked history, or opened the
// main help). Called before anything reads or acts on quick mode.
func (e *Editor) syncQuickHelpMode() {
	if !e.quickHelpMode {
		return
	}
	hw := e.helpViewport()
	if hw == nil {
		e.quickHelpMode = false
		e.quickHelpBuf = nil
		e.quickHelpShownTopic = ""
		return
	}
	if hw.Buffer != e.quickHelpBuf {
		// The user browsed away (link / history): the slot is a regular help
		// page now, so leave quick mode and restore the page chrome (title bar,
		// standard height) that Quick Help had stripped.
		e.quickHelpMode = false
		e.quickHelpBuf = nil
		e.quickHelpShownTopic = ""
		e.applyHelpViewportChrome(hw)
	}
}

// toggleHelp is the help_toggle command: open/navigate the docked help slot, or
// close it when it is already showing the target.
//
// A PAGE argument (help_toggle "keys") is a request to READ, so it focuses the
// help viewport — a reader who opened help to learn the keys cannot be expected
// to already know the key that would move focus there — and toggling it back off
// returns focus where the reader came from (see closeHelpViewport). With NO
// argument the target is Quick Help, which is a peek that must never steal the
// caret: it follows the key prefix you are typing, so taking focus would stop
// the very typing it is describing.
func (e *Editor) toggleHelp(arg string) bool {
	return e.openHelp(arg, true, strings.TrimSpace(arg) != "")
}

// openHelpFocused is the help_open command: like help_toggle but it only ever
// opens or replaces — it never closes — and it FOCUSES the help viewport, so the
// reader can scroll and follow links straight away.
func (e *Editor) openHelpFocused(arg string) bool { return e.openHelp(arg, false, true) }

// openHelp drives the single docked help slot (Tag "help", top dock). With no
// argument the target is Quick Help; with one it is the help wiki page
// help:/<arg> ("help:/..." is accepted whole, so `help:/` opens the index).
//
//	allowClose — when the slot is ALREADY showing the target, close it (toggle
//	             behavior); otherwise leave it open and merely re-assert it.
//	focus      — focus the help viewport after opening/navigating (help_open, and
//	             help_toggle for a page); Quick Help passes false so the peek
//	             never steals the caret.
//
// Navigating an existing slot grows its nav history, so its back button returns
// to where the reader came from.
func (e *Editor) openHelp(arg string, allowClose, focus bool) bool {
	arg = strings.TrimSpace(arg)
	e.syncQuickHelpMode()
	hw := e.helpViewport()

	if arg == "" {
		if e.quickHelpMode && hw != nil {
			// Already at Quick Help: toggle closes; open just re-asserts focus.
			if allowClose {
				e.closeHelpViewport(hw)
				e.quickHelpMode = false
				e.quickHelpBuf = nil
				e.quickHelpShownTopic = ""
				e.RequestRender()
				return true
			}
			e.focusHelpViewport(hw, focus)
			return true
		}
		// Resolve the topic for the current prefix now, so a menu-driven open
		// (no keypress pending) still lands on the root topic rather than a
		// stale one.
		e.quickHelpTopic = e.KeyProcessor.HelpTopic(e.ActiveSequence)
		buf, root, wikiName, browse, ok := e.quickHelpDestination()
		if !ok {
			// Fresh open with no page for the topic: nothing to keep, so show
			// the no-help notice.
			buf, root, wikiName, browse = e.quickHelpBuffer(), "", "", false
		}
		// Mark Quick Help mode BEFORE showing, so showHelpLocation's chrome pass
		// (applyHelpViewportChrome) drops the title bar and fits the height.
		e.quickHelpMode = true
		e.quickHelpBuf = buf
		e.quickHelpShownTopic = e.quickHelpTopic
		e.showHelpLocation(hw, buf, root, wikiName, browse, focus)
		return true
	}

	// A page argument opens/navigates the main help — this is browsing, so it
	// leaves Quick Help mode: the quickHelpTopic must never steer "Using mew".
	e.quickHelpMode = false
	e.quickHelpBuf = nil
	e.quickHelpShownTopic = ""

	ref := arg
	if !strings.HasPrefix(strings.ToLower(ref), "help:") {
		ref = "help:/" + strings.TrimPrefix(ref, "/")
	}
	res := e.resolveFollow(nil, ref)
	if res.url == "" {
		// Opening a help page that does not exist is an error, like opening a
		// missing file — not an invitation to author one. Report it as a
		// transient toast and leave any open help viewport untouched. (Authoring a
		// new help page is done deliberately through buffer_open, not here.)
		e.ShowError(res.message)
		e.RequestRender()
		return true
	}
	if hw != nil && e.bufferCanonicalURL(hw.Buffer) == res.url {
		// Already at this page: toggle closes; open just re-asserts focus.
		if allowClose {
			e.closeHelpViewport(hw)
			e.RequestRender()
			return true
		}
		e.focusHelpViewport(hw, focus)
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
	e.showHelpLocation(hw, buf, res.root, res.wikiName, true, focus)
	return true
}

// closeHelpViewport closes the docked help slot and, when the slot held the
// keyboard, hands focus back to the viewport last focused in the BLANK viewport
// set — the ordinary document group the reader came from, as opposed to the
// "help" set the slot itself belongs to. Reading help is a detour, so toggling
// it off must land the reader exactly where they left, not wherever
// RemoveViewport's generic fallback (nearest by creation order) would pick.
//
// The target is captured BEFORE removal: RemoveViewport re-focuses through that
// fallback, which would overwrite the blank set's last-focused record on its way
// out. Focus is restored only if the slot actually had it — closing help from
// the document leaves the caret in the document, untouched.
func (e *Editor) closeHelpViewport(hw *viewport.Viewport) {
	if hw == nil {
		return
	}
	fw := e.ViewportManager.GetFocusedViewport()
	hadFocus := fw != nil && fw.ID == hw.ID
	back := e.ViewportManager.LastFocusedInSet("")
	e.ViewportManager.RemoveViewport(hw.ID)
	if hadFocus && back != nil {
		e.ViewportManager.SetFocus(back.ID)
	}
}

// focusHelpViewport focuses hw when focus is set (help_open), repainting; a no-op
// otherwise (help_toggle re-asserting an already-open slot).
func (e *Editor) focusHelpViewport(hw *viewport.Viewport, focus bool) {
	if focus && hw != nil {
		e.ViewportManager.SetFocus(hw.ID)
	}
	e.RequestRender()
}

// showHelpLocation puts buf into the docked help viewport: it NAVIGATES an
// existing viewport (swap_buffer, so the previous location goes onto the back
// history), or creates the docked viewport when none exists. root/wikiName wire
// the viewport to the help wiki (empty for Quick Help), browse arms link browsing
// for a wiki page, and focus (help_open only) moves the caret to the slot.
func (e *Editor) showHelpLocation(hw *viewport.Viewport, buf *buffer.Buffer, root, wikiName string, browse, focus bool) {
	if hw == nil {
		hw = e.createHelpViewport(buf, focus)
	} else {
		e.swapBuffer(hw, buf)
		if focus {
			e.ViewportManager.SetFocus(hw.ID)
		}
	}
	hw.WikiRoot = root
	hw.WikiName = wikiName
	hw.BrowseActive = browse && hw.ViewState.LinkBrowsing
	e.applyHelpViewportChrome(hw) // title bar + height per Quick Help vs. page role
	e.ensureCursorVisible(hw)
	e.RequestRender()
}

// applyHelpViewportChrome sets the docked help viewport's chrome for its CURRENT
// role, read from quickHelpMode. Quick Help is a dynamic, chromeless page: it
// drops the top "Help" message bar entirely and sizes its max height to the
// loaded file so the viewport fits its content. Regular help keeps the "Help"
// title bar and the standard height envelope. Called whenever the viewport's role
// or loaded buffer changes — not per edit — so the message bar enables and
// disables as the same slot flips between Quick Help and a help page.
func (e *Editor) applyHelpViewportChrome(hw *viewport.Viewport) {
	if hw == nil {
		return
	}
	if e.quickHelpMode {
		hw.Class = quickHelpClass // distinct class for per-class config
		hw.MessageTopCenter = ""  // no title bar: all MessageTop* empty hides the row
		hw.CanFocus = false       // the focus switcher (^B N/^B P) skips the peek
		hw.MaxHeightFraction = 0  // Quick Help fits its content, not a proportion
		fit := quickHelpFitHeight(hw.Buffer)
		// Height is the PREFERRED height and clampHeight only caps it DOWN to
		// MaxHeight — it never grows a short preferred up to the max. So set the
		// preferred to the fit itself (not just the ceiling), or Quick Help sits at
		// its created default height instead of fitting its content.
		hw.Height = fit
		hw.MaxHeight = fit
		if hw.MinHeight > fit {
			hw.MinHeight = fit
		}
	} else {
		hw.Class = helpViewportClass
		hw.MessageTopCenter = "Help"
		hw.CanFocus = true
		hw.MinHeight = 4
		hw.MaxHeight = 0 // no literal ceiling; the proportional cap governs
		hw.MaxHeightFraction = helpMaxHeightFraction
	}
}

// quickHelpFitHeight is the height Quick Help sizes its viewport to: the visible
// line count of buf, less the phantom empty line a trailing newline would add,
// and at least 1.
func quickHelpFitHeight(buf *buffer.Buffer) int {
	if buf == nil {
		return 1
	}
	n := buf.GetLineCount()
	if strings.HasSuffix(buf.GetContent(), "\n") {
		n-- // GetLineCount counts the empty line after a trailing newline
	}
	if n < 1 {
		n = 1
	}
	return n
}

// createHelpViewport creates the single docked help viewport (Tag "help") holding
// buf. focus moves the caret to it (help_open, and help_toggle for a page); Quick
// Help passes false so the peek opens without stealing focus. Its chrome (title
// bar, height envelope) is set immediately afterward by applyHelpViewportChrome
// per the viewport's role; the values here are the regular-help defaults.
func (e *Editor) createHelpViewport(buf *buffer.Buffer, focus bool) *viewport.Viewport {
	opts := e.docViewportOptions()
	opts.Type = viewport.ToolViewport
	opts.ViewportSet = "help"
	opts.Class = helpViewportClass // applyHelpViewportChrome flips this to quickHelpClass in Quick Help
	opts.Tag = helpViewportTag
	opts.Dock = viewport.DockTop
	opts.Priority = 100
	opts.MinHeight = 4
	opts.MaxHeightFraction = helpMaxHeightFraction // proportional cap; no literal ceiling
	opts.MessageTopCenter = "Help"
	opts.Buffer = buf
	opts.SetFocus = focus
	return e.ViewportManager.GetViewport(e.ViewportManager.CreateViewport(opts))
}

// quickHelpDestination resolves the help wiki page for the current
// quickHelpTopic (help:/<topic>), plus its wiki wiring (so its links are
// followable). ok is false when the topic names no existing page — the caller
// decides what to do (keep the current help showing, or, on a fresh open, fall
// back to the no-help notice). HelpTopic already backs off to the longest
// shorter prefix, so !ok means neither the matched topic nor the root has a
// live, existing page.
func (e *Editor) quickHelpDestination() (buf *buffer.Buffer, root, wikiName string, browse, ok bool) {
	if e.quickHelpTopic != "" {
		ref := "help:/" + e.quickHelpTopic
		if res := e.resolveFollow(nil, ref); res.url != "" {
			b := e.findOpenBuffer(res.url)
			if b == nil {
				if loaded, err := e.loadBufferURL(res.url); err == nil {
					b = loaded
				}
			}
			if b != nil {
				return b, res.root, res.wikiName, true, true
			}
		}
	}
	return nil, "", "", false, false
}

// refreshQuickHelp re-renders the docked Quick Help viewport for the current
// quickHelpTopic, in place — no history entry, because Quick Help is a single
// dynamic slot. Called from the key loop when the context topic changes while
// Quick Help is open. When the new topic has no page it KEEPS the current help
// showing (rather than replacing it with the no-help notice), only recording
// the topic so it is not re-resolved every keystroke.
func (e *Editor) refreshQuickHelp(hw *viewport.Viewport) {
	buf, root, wikiName, browse, ok := e.quickHelpDestination()
	if !ok {
		e.quickHelpShownTopic = e.quickHelpTopic // keep the current page; don't retry each key
		return
	}
	if buf != hw.Buffer {
		e.replaceBuffer(hw, buf)
	}
	hw.WikiRoot = root
	hw.WikiName = wikiName
	hw.BrowseActive = browse && hw.ViewState.LinkBrowsing
	e.quickHelpBuf = buf
	e.quickHelpShownTopic = e.quickHelpTopic
	e.applyHelpViewportChrome(hw) // re-fit height to the newly loaded page
	e.ensureCursorVisible(hw)
	e.RequestRender()
}

// updateQuickHelp recomputes quickHelpTopic from the current key prefix and, if
// Quick Help is open in follow mode, re-navigates it when the topic changed.
// Called once per processed key, after the active sequence is settled. It never
// affects the main help — only the dedicated Quick Help slot.
func (e *Editor) updateQuickHelp() {
	e.quickHelpTopic = e.KeyProcessor.HelpTopic(e.ActiveSequence)
	e.syncQuickHelpMode()
	if !e.quickHelpMode {
		return
	}
	if e.quickHelpTopic == e.quickHelpShownTopic {
		return // already showing this topic — don't reload on every keypress
	}
	if hw := e.helpViewport(); hw != nil {
		e.refreshQuickHelp(hw)
	}
}

// quickHelpBuffer builds the placeholder shown when Quick Help has no page for
// the current key context — a single "no help" line, named by the synthetic
// quickHelpDocURL. Quick Help mode is tracked by quickHelpMode (not this URL),
// so the notice needs no special identity beyond a stable name.
func (e *Editor) quickHelpBuffer() *buffer.Buffer {
	buf := e.lib.NewFromString("No quick help is available.")
	buf.SetFilename(quickHelpDocURL)
	return buf
}
