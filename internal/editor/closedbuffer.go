package editor

import (
	"fmt"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// The closed-buffer tombstone. When buffer_close closes a buffer everywhere it
// is referenced, its nav-history slots are not purged — they are REPLACED with a
// read-only placeholder buffer that says the buffer was closed and, when it had
// a real file on disk, offers a Re-open link back to it. The placeholder carries
// the mew:/closed address, so it renders in the dokuwiki grammar and is read-only
// by address like the other generated surfaces; its Re-open link routes through
// the "closed" surface's follow handler below.

// closedPlaceholderURL is the address every tombstone carries.
const closedPlaceholderURL = "mew:/closed"

// reopenableName reports whether a closed buffer's name can be re-opened: it
// must have a name, and not be one of mew's own generated (mew:) surfaces —
// there is nothing on disk behind those to reload.
func reopenableName(name string) bool {
	return name != "" && !isGenPath(name)
}

// newClosedPlaceholder builds the read-only tombstone shown wherever a
// globally-closed buffer used to be referenced. It names the closed buffer and,
// when that buffer had a real file, offers a Re-open link whose target is the
// filename (followClosedReopen loads it back in place).
func (e *Editor) newClosedPlaceholder(name string) *buffer.Buffer {
	label := name
	if label == "" {
		label = "Untitled"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The buffer %s has been closed.\n", label)
	if reopenableName(name) {
		fmt.Fprintf(&b, "\n[[%s|Re-open]]\n", name)
	}
	buf := e.lib.NewFromString(b.String())
	buf.SetFilename(closedPlaceholderURL)
	return buf
}

// renderClosed is the "closed" surface's render function. Tombstones are baked
// with their buffer's name at close time (newClosedPlaceholder), so this generic
// form is only reached if mew:/closed is opened directly, which nothing does.
func (e *Editor) renderClosed() string {
	return "This buffer has been closed.\n"
}

// followClosedReopen is the "closed" surface's follow handler: the Re-open link's
// target is the closed buffer's filename, so re-load it (reusing an open copy if
// one exists) and swap it into the placeholder's viewport in place — the
// tombstone becomes the reopened document.
func (e *Editor) followClosedReopen(target string) bool {
	target = strings.TrimSpace(target)
	w := e.surfaceTargetViewport()
	if w == nil || target == "" {
		return false
	}
	tomb := w.Buffer // the tombstone we are re-opening from
	buf := e.findOpenBuffer(target)
	if buf == nil {
		loaded, err := e.loadBuffer(target)
		if err != nil {
			e.ShowError("Re-open " + displayPath(target) + ": " + err.Error())
			e.RequestRender()
			return true
		}
		buf = loaded
	}
	// Replace the tombstone with the reopened buffer in this viewport in place —
	// no history hop, the tombstone is being undone, not navigated away from —
	// then re-resolve view state for an ordinary document (replaceBuffer, unlike
	// swapBuffer, leaves the tombstone's read-only/browse state behind).
	e.replaceBuffer(w, buf)
	w.BrowseActive = false
	if !w.IsOptionOverridden("readonly") {
		e.applyResolvedOption(w, "readonly")
	}
	if !w.IsOptionOverridden("linkbrowsing") {
		e.applyResolvedOption(w, "linkbrowsing")
	}
	// Un-tombstone every OTHER slot that was this same tombstone (one tombstone is
	// shared across all of a name's closed references), so re-opening restores the
	// buffer everywhere it used to be, not only where the reader clicked.
	if tomb != nil && isGenPath(tomb.GetFilename()) {
		e.reopenTombstoneEverywhere(tomb, buf, w.ViewState.ReadOnly, w.ViewState.LinkBrowsing, w.BrowseActive)
	}
	e.ensureCursorVisible(w)
	e.ShowNotificationTagged("→ "+displayPath(target), "navigate")
	e.RequestRender()
	return true
}

// reopenTombstoneEverywhere restores buf over every nav-history slot still
// holding the shared tombstone, across all viewports, then unburies buf (the
// sweep may have planted a graveyard binding on it). Reports how many slots were
// restored.
func (e *Editor) reopenTombstoneEverywhere(tomb, buf *buffer.Buffer, readOnly, linkBrowsing, browse bool) int {
	n := 0
	for _, v := range e.ViewportManager.AllViewports() {
		n += v.ReplaceHistoryBuffer(tomb, buf, readOnly, linkBrowsing, browse)
	}
	e.unburyEverywhere(buf)
	return n
}

// closeBufferEverywhere is the buffer_close command: close the focused viewport's
// buffer from EVERY place it is referenced. Unlike viewport_close (which closes
// one viewport), this retires the buffer itself — the active views showing it
// mirror viewport_close, and its nav-history references everywhere become
// mew:/closed tombstones. A modified buffer prompts once before anything is torn
// down; the whole thing is one buffer, so one prompt covers it.
func (e *Editor) closeBufferEverywhere() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type == viewport.PromptViewport || w.Buffer == nil {
		return false
	}
	target := w.Buffer
	// A generated surface (a buffers list, a tombstone) is not a document to
	// retire everywhere — leave those to viewport_close.
	if isGenPath(target.GetFilename()) {
		return false
	}
	if target.IsModified() {
		name := target.GetFilename()
		if name == "" {
			name = "Untitled"
		}
		e.PromptMgr.PromptForConfirmation(fmt.Sprintf("04: LOSE CHANGES TO %s?", name), true, func(accepted, confirmed bool) {
			if accepted && confirmed {
				e.doCloseBufferEverywhere(target)
			} else {
				e.ShowNotification("Close cancelled")
			}
			e.RequestRender()
		})
		return true
	}
	return e.doCloseBufferEverywhere(target)
}

// doCloseBufferEverywhere carries out the global close after any prompt: active
// views mirror viewport_close, then every remaining nav-history reference to the
// buffer becomes a mew:/closed tombstone, then its safety state is dropped.
func (e *Editor) doCloseBufferEverywhere(target *buffer.Buffer) bool {
	name := target.GetFilename()

	// 1. Active views: mirror viewport_close for every viewport whose active
	// buffer is the target (the focused one, plus any clone). Snapshot the ids
	// first — finishCloseBuffer removes viewports as it goes.
	var activeIDs []string
	for _, v := range e.ViewportManager.AllViewports() {
		if v.Buffer == target {
			activeIDs = append(activeIDs, v.ID)
		}
	}
	for _, id := range activeIDs {
		if v := e.ViewportManager.GetViewport(id); v != nil && v.Buffer == target {
			// Mirror viewport_close for this active view. It may resurrect the
			// viewport's graveyard, remove the viewport, or — closing the last
			// content viewport — set the editor to exit; planting tombstones after
			// that is harmless, so there is nothing to bail out for.
			e.finishCloseBuffer(id)
		}
	}

	// 2. History references anywhere → a single shared tombstone. Built after the
	// active pass so a viewport_close resurrection never surfaces the placeholder.
	tomb := e.newClosedPlaceholder(name)
	planted := 0
	for _, v := range e.ViewportManager.AllViewports() {
		// Tombstone bindings open read-only in link-browse mode, so the Re-open
		// link is a focusable button when navigated back to.
		planted += v.ReplaceHistoryBuffer(target, tomb, true, true, true)
	}

	// 3. Nothing references the buffer now (its active views closed, its history
	// slots are tombstones) — drop its lock and captured notices.
	if !e.bufferStillReferenced(target) {
		e.forgetBufferSafety(target)
	}
	e.RequestRender()
	return true
}

// bufferStillReferenced reports whether any content viewport still holds b, as
// its active buffer or stacked in a nav history.
func (e *Editor) bufferStillReferenced(b *buffer.Buffer) bool {
	for _, ob := range e.openBuffers() {
		if ob == b {
			return true
		}
	}
	return false
}
