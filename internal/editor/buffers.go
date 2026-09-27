package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// contentLocked reports whether the focused viewport currently forbids content
// mutations — it is read-only, or a link button is focused (the caret is inert
// inside it, its source text protected until nav_cancel) — and warns when it
// does. Every mutation's implementation calls this at the point it would change
// the buffer, so the lock holds no matter how the command was named, aliased,
// or chained in the script language. There is deliberately no command-name
// list: a name cannot tell you what a command does.
func (e *Editor) contentLocked() bool {
	return e.viewportEditLocked(e.ViewportManager.GetFocusedViewport())
}

// viewportEditLocked is contentLocked for a specific viewport — used where the
// mutating implementation names its target viewport directly (find/replace
// applies to the match's viewport, not necessarily the focused one).
func (e *Editor) viewportEditLocked(w *viewport.Viewport) bool {
	if w == nil {
		return false
	}
	// A generated surface (mew:/…) is read-only by its address, not by viewport
	// state — a hard guarantee independent of when options were last resolved.
	// viewportReadOnly is the SILENT predicate; keeping this branch on it means
	// the delete commands' silent read-only decline can never drift from what
	// warns here.
	if e.viewportReadOnly(w) {
		// Tagged so a burst of rejected edits (holding a key) collapses to one
		// warning instead of stacking a bar per keystroke.
		e.ShowWarningTagged("Buffer is read-only", "readonly_warning")
		e.RequestRender()
		return true
	}
	if e.focusedLinkButton(w) != nil {
		e.ShowWarning("Link is focused (^C to edit)")
		e.RequestRender()
		return true
	}
	return false
}

// openFile opens a file in a new buffer viewport. On the real OS the file is
// opened through Garland's lazy warm-storage path (huge files are paged, not
// slurped); virtualized file systems read through the host callbacks.
func (e *Editor) openFile(filename string) bool {
	// A registered wiki scheme ("help:/start") opens a PAGE, not a literal
	// file: route it through the same resolver a followed link uses, so the
	// real page file (~/.mew/help/start.txt) loads and the viewport is rooted
	// in the wiki. Without this the name fell through to a plain OS open of
	// "help:/start", which found nothing and came up blank.
	if _, handled := e.openWikiScheme(strings.TrimSpace(filename), true); handled {
		return true
	}

	// A generated mew: surface ("mew:/buffers") is produced on demand and
	// navigated to in place, not opened as a file in a new viewport.
	if name := genSurfaceName(strings.TrimSpace(filename)); name != "" {
		return e.openGeneratedSurface(name)
	}

	buf, err := e.loadBuffer(filename)
	if err != nil {
		return false
	}

	// Create the main buffer viewport UNfocused, then show it in the focused tile
	// (replacing it) rather than spawning a new tile beside it.
	id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          buf,
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: true,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
	})
	e.showInFocusedTile(e.ViewportManager.GetViewport(id))

	e.RequestRender()
	return true
}

// showInFocusedTile makes w the content of the currently- (or most recently-)
// focused main tile, replacing what was there, instead of letting the main-focus
// hook (tilerFollowFocus) spawn a NEW tile beside it. This is what buffer_new /
// buffer_open_file want: open in the current pane, not add one. With nothing
// tiled yet it falls back to a plain focus, letting the hook seat the first tile
// as usual. The viewport formerly in the tile stays open (in the buffer list),
// just untiled.
func (e *Editor) showInFocusedTile(w *viewport.Viewport) {
	if w == nil {
		return
	}
	vp := e.ensureTiler()
	tile := vp.GetFocus()
	if tile == 0 {
		if ts := vp.Tiles(); len(ts) > 0 {
			tile = ts[0].Tile
		}
	}
	if tile != 0 {
		vp.Set(tile, w.ID) // the focused tile now shows the new viewport
	}
	e.ViewportManager.SetFocus(w.ID) // the hook finds the reseated tile — no split
}

// createNewBuffer creates a new empty buffer, shown in the focused tile.
func (e *Editor) createNewBuffer() {
	buf := e.lib.New()

	// Created UNfocused: showInFocusedTile reseats the focused tile to it, so
	// focusing it finds that tile rather than spawning a new one.
	id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          buf,
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: true,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		AutoIndent:      e.Config.AutoIndent,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
	})
	e.showInFocusedTile(e.ViewportManager.GetViewport(id))

	e.RequestRender()
}

// duplicateCurrentBuffer opens a new buffer viewport containing a copy of the
// current buffer's content. The duplicate is unnamed (no filename) so saving it
// can't overwrite the original.
func (e *Editor) duplicateCurrentBuffer() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	buf := e.lib.NewFromString(w.Buffer.GetContent())

	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          buf,
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: true,
		TabSize:         e.Config.TabSize,
		ShowInvisibles:  e.Config.ShowInvisibles,
		ShowBidi:        e.Config.ShowBidi,
		ShowMarks:       e.Config.ShowMarks,
		OverwriteMode:   e.Config.OverwriteMode,
		ReadOnly:        e.Config.ReadOnly,
		ShowRuler:       e.Config.ShowColumnRuler,
		Scrollbar:       e.Config.Scrollbar,
		SetFocus:        true,
	})

	e.RequestRender()
	return true
}

// cloneCurrentViewport opens a second viewport onto the focused viewport's buffer
// (the same *buffer.Buffer, not a copy), starting at the same caret and
// viewport. Both viewports then edit and scroll independently — each owns its
// caret cursor and viewport anchor, and garland keeps both in sync with edits
// made through either viewport.
func (e *Editor) cloneCurrentViewport() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil || w.Type == viewport.PromptViewport {
		e.ShowWarning("No buffer to clone a viewport for")
		return false
	}

	srcPos := w.CursorPos()

	id := e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Type:            viewport.DocViewport,
		Buffer:          w.Buffer, // SAME buffer, not a copy
		Dock:            viewport.DockNone,
		Priority:        0,
		MinHeight:       1,
		ShowLineNumbers: w.ViewState.ShowLineNumbers,
		ProtectNewlines: w.ViewState.ProtectNewlines,
		TabSize:         w.ViewState.TabSize,
		ShowInvisibles:  w.ViewState.ShowInvisibles,
		ShowBidi:        w.ViewState.ShowBidi,
		ShowMarks:       w.ViewState.ShowMarks,
		OverwriteMode:   w.ViewState.OverwriteMode,
		ReadOnly:        w.ViewState.ReadOnly,
		ShowRuler:       w.ViewState.ShowRuler,
		// Inherit the scrollbar setting too, or the clone reserves no bar column
		// and comes up with no scrollbar beside the original that has one.
		Scrollbar: w.ViewState.Scrollbar,
		SetFocus:  true,
	})

	// Start the clone at the source's caret and viewport.
	if cw := e.ViewportManager.GetViewport(id); cw != nil {
		cw.SetCursorPos(srcPos)
		cw.SetViewTop(w.ViewState.ViewOffsetY)
	}

	e.RequestRender()
	return true
}

// writeBufferCopy exports the whole buffer to a prompted-for file WITHOUT
// adopting it as the buffer's source (garland's SaveCopyTo, a streaming write):
// the buffer keeps working from its original source, and its filename, modified
// flag, and save history are all left untouched — this is not a save. The
// whole-buffer parallel to writeBlock. Like a block write, overwriting ANY
// existing file (the buffer's own source included) is confirmed first. Scars
// (data lost to placeholders) surface as buffer notices, same as a real save.
func (e *Editor) writeBufferCopy() bool {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	e.PromptMgr.PromptForFilename("Write buffer to", "", func(accepted bool, _, filename string) {
		if !accepted || filename == "" {
			e.RequestRender()
			return
		}
		write := func() {
			warnings, err := w.Buffer.SaveCopyTo(filename)
			for _, warn := range warnings {
				e.noteBuffer(w.Buffer, "save", warn, true)
			}
			if err != nil {
				e.ShowError("Failed to write buffer: " + err.Error())
			} else {
				e.ShowNotification("Buffer written: " + filename)
			}
			e.RequestRender()
		}
		// An export is never the buffer's own save, so overwriting ANY existing
		// file (the buffer's own source included) gets a prompt.
		if e.fileExists(filename) {
			e.PromptMgr.PromptForConfirmation(fmt.Sprintf("13: OVERWRITE EXISTING FILE %s?", filename), false, func(accepted, confirmed bool) {
				if accepted && confirmed {
					write()
				} else {
					e.ShowNotification("Buffer write cancelled")
					e.RequestRender()
				}
			})
			return
		}
		write()
	})
	return true
}

// insertFile inserts the contents of a file at the cursor position, as a single
// undo revision. Line endings are normalized to '\n' like paste.
func (e *Editor) insertFile(filename string) bool {
	if e.contentLocked() {
		// The buffer's owning viewport is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Buffer == nil {
		return false
	}
	data, err := e.FS.ReadFile(filename)
	if err != nil {
		e.ShowError("Failed to read file: " + err.Error())
		return false
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	if text == "" {
		return true
	}

	w.Buffer.BeginUserCommand("buffer_insert_file")
	e.insertText(text)
	w.Buffer.EndUserCommand()
	w.TrackEdit()
	e.lastEditKill = false // an insert, not a kill: breaks delete accumulation
	return true
}

// closeCurrentBuffer closes the current buffer viewport, reporting whether the
// close is UNDERWAY: a modified buffer's close is underway while its prompt is
// up, and its real outcome arrives later. Callers that need the outcome use
// closeCurrentBufferThen.
func (e *Editor) closeCurrentBuffer() bool {
	underway := true
	e.closeCurrentBufferThen(func(closed bool) { underway = closed })
	return underway
}

// closeCurrentBufferThen closes the focused viewport and calls then with the
// outcome: true when the viewport closed, false when it did not — nothing
// closable, or the user answered the lose-changes prompt with no. then runs
// before this returns EXCEPT when a prompt intervenes, which is the whole
// reason the callback exists: closing is a question, not always an act.
func (e *Editor) closeCurrentBufferThen(then func(closed bool)) {
	w := e.ViewportManager.GetFocusedViewport()
	if w == nil || w.Type == viewport.PromptViewport {
		then(false)
		return
	}

	// Check for changes that would be lost. The viewport's active buffer is
	// always at stake. Its stacked buffers are only at stake when the WINDOW
	// itself will close — with a non-empty graveyard, viewport_close instead
	// resurrects the most recent burial into this viewport, so the history and
	// graveyard survive intact.
	resurrecting := len(w.GraveyardBuffers()) > 0
	var atRisk []*buffer.Buffer
	if w.Buffer != nil && w.Buffer.IsModified() {
		atRisk = append(atRisk, w.Buffer)
	}
	if !resurrecting {
		for _, b := range w.StackedBuffers() {
			if b != w.Buffer && b.IsModified() && !e.bufferReferencedElsewhere(b, w) {
				atRisk = append(atRisk, b)
			}
		}
	}
	if len(atRisk) > 0 {
		// Get the name for the prompt
		viewportName := atRisk[0].GetFilename()
		if viewportName == "" {
			viewportName = "Untitled"
		}
		if len(atRisk) > 1 {
			viewportName = fmt.Sprintf("%s (+%d more in history)", viewportName, len(atRisk)-1)
		}

		// Store viewport ID to close later
		viewportID := w.ID

		// Prompt for confirmation using PromptManager
		e.PromptMgr.PromptForConfirmation(fmt.Sprintf("04: LOSE CHANGES TO %s?", viewportName), true, func(accepted bool, confirmed bool) {
			closed := false
			if accepted && confirmed {
				// User confirmed - close the buffer
				closed = e.finishCloseBuffer(viewportID)
			} else {
				e.ShowNotification("Close cancelled")
			}
			e.RequestRender()
			then(closed)
		})
		return
	}

	// Not modified - close directly
	then(e.finishCloseBuffer(w.ID))
}

// closeSessionThen closes EVERY viewport in this session, one at a time, and
// ends the session when the last one goes; it calls then with true when the
// session is finished and false when a close was declined and the rest were
// abandoned. Each modified buffer asks its own lose-changes question, and an
// answer of no stops the sweep where it stands — a window that holds unsaved
// work does not close, and nothing already agreed to is put back.
//
// This is what an embedding host's window-close runs: closing the window means
// closing what is in it, on the editor's own terms and with the editor's own
// prompts, rather than the frame deciding for the session inside it.
func (e *Editor) closeSessionThen(then func(done bool)) {
	// A backstop, not a policy: every step either closes a viewport or
	// resurrects a buried buffer into one, both of which are finite, so the
	// sweep converges. The counter is here so that a future step which does
	// neither ends the loop instead of spinning the editor.
	steps := 0
	const maxSteps = 4096

	var step func()
	step = func() {
		if len(e.contentViewports()) == 0 {
			e.Running = false
			then(true)
			return
		}
		if steps++; steps > maxSteps {
			then(false)
			return
		}
		e.focusAContentViewport()
		e.closeCurrentBufferThen(func(closed bool) {
			switch {
			case !closed:
				then(false) // declined: abandon the sweep
			case !e.Running:
				then(true) // that was the last one; the session ended with it
			default:
				step()
			}
		})
	}
	step()
}

// focusAContentViewport puts the focus on a content viewport when it is
// somewhere else (a prompt), so a sweep that closes "the current viewport"
// always has one to close.
func (e *Editor) focusAContentViewport() {
	w := e.ViewportManager.GetFocusedViewport()
	if w != nil && w.Type != viewport.PromptViewport {
		return
	}
	for _, c := range e.contentViewports() {
		if c.Type != viewport.PromptViewport {
			e.ViewportManager.FocusViewportAsCycle(c)
			return
		}
	}
}

// bufferDisplayName is a short human name for a buffer: its filename's base,
// or "Untitled".
func bufferDisplayName(b *buffer.Buffer) string {
	if b != nil {
		if fn := b.GetFilename(); fn != "" {
			return filepath.Base(fn)
		}
	}
	return "Untitled"
}

// finishCloseBuffer performs the actual buffer close.
func (e *Editor) finishCloseBuffer(viewportID string) bool {
	// Resurrection first: with buried bindings waiting, closing the buffer
	// does NOT close the viewport — the most recently buried binding surfaces
	// as the viewport's current one (full caret/scroll state), and the closed
	// buffer is retired only if nothing else still holds it open.
	if w := e.ViewportManager.GetViewport(viewportID); w != nil {
		closed := w.Buffer
		if w.ResurrectLastBuried() {
			e.unburyEverywhere(w.Buffer)
			if closed != nil {
				stillOpen := false
				for _, b := range e.openDocViewports() {
					if b == closed {
						stillOpen = true
						break
					}
				}
				if !stillOpen {
					e.forgetBufferSafety(closed)
				}
			}
			e.ensureCursorVisible(w)
			e.ShowNotification("Closed; resurfaced " + bufferDisplayName(w.Buffer))
			e.RequestRender()
			return true
		}
	}

	// Get all main buffers
	mainBuffers := e.contentViewports()
	if len(mainBuffers) <= 1 {
		// Last buffer - exit instead
		e.Running = false
		return true
	}

	closing := e.ViewportManager.GetViewport(viewportID)

	// Remove the viewport, and dismiss the tiler tile that held it.
	e.ViewportManager.RemoveViewport(viewportID)
	e.dismissTileFor(viewportID)

	// Drop safety state (mew lock, notices) when no other viewport still holds
	// this buffer open — actively or stacked in a nav history (viewport_clone
	// can share one buffer; link-follow histories reference buffers too).
	if closing != nil && closing.Buffer != nil {
		shared := false
		for _, b := range e.openDocViewports() {
			if b == closing.Buffer {
				shared = true
				break
			}
		}
		if !shared {
			e.forgetBufferSafety(closing.Buffer)
		}
	}
	e.RequestRender()
	return true
}

// viewportTiled reports whether a main-area tile currently references the
// viewport id — i.e. it is shown on screen. False when there is no tiler.
func (e *Editor) viewportTiled(id string) bool {
	if e.tiler == nil {
		return false
	}
	for _, b := range e.tiler.Tiles() {
		if b.Ref == id {
			return true
		}
	}
	return false
}

// cycleBuffer brings a NON-VISIBLE buffer into the focused pane. buffer_next /
// buffer_prior walk the content viewports in `direction` and land on the first
// one NOT currently shown in any tile — a buffer visible in a pane is reached
// with viewport_next/prior instead, so the two commands partition the set with
// no overlap. adoptFocusInPlace reseats the focused tile onto the buffer, rather
// than splitting a new one. Reports false when nothing is hidden to bring in.
func (e *Editor) cycleBuffer(direction int) bool {
	mains := e.contentViewports()
	if len(mains) <= 1 {
		return false
	}

	currentID := ""
	if w := e.ViewportManager.GetFocusedViewport(); w != nil {
		currentID = w.ID
	}
	currentIndex := -1
	for i, w := range mains {
		if w.ID == currentID {
			currentIndex = i
			break
		}
	}
	if currentIndex == -1 {
		currentIndex = 0
	}

	n := len(mains)
	for step := 1; step <= n; step++ {
		w := mains[((currentIndex+direction*step)%n+n)%n]
		if w.ID == currentID || e.viewportTiled(w.ID) {
			continue // self or an on-screen pane — skip
		}
		e.adoptFocusInPlace = true
		e.ViewportManager.SetFocus(w.ID)
		e.adoptFocusInPlace = false
		e.RequestRender()
		return true
	}
	return false
}

// contentViewports returns every content viewport — documents and tool surfaces
// (help, listings) — but not prompts or chrome (the modebar). These are the
// viewports the data-safety and navigation enumerations act on (buffer close,
// save-all, DEADCAT, nav-history liveness, buffer cycling).
func (e *Editor) contentViewports() []*viewport.Viewport {
	var result []*viewport.Viewport
	for _, w := range e.ViewportManager.AllViewports() {
		if w.FocusEligible() {
			result = append(result, w)
		}
	}
	return result
}

// loadBuffer loads a file into a buffer: through Garland's own lazy
// warm-storage path on the real OS, or through the host's FileSystem bridged
// into garland when virtualized (so host buffers get the same save engine,
// history preservation, and revert). Opening also arms the buffer's safety
// net: automatic backups, and an editing lock — emacs-interoperable when
// git hygiene allows it, mew-native otherwise.
func (e *Editor) loadBuffer(filename string) (*buffer.Buffer, error) {
	// mew:-scheme names load through the URL path: the real file in local
	// mode, the virtualized support tree otherwise. Everything else is
	// normalized (tilde-expanded, absolutized) so the buffer's filename
	// survives saves and working-directory changes.
	if isBoxPath(filename) {
		return e.loadBufferURL(filename)
	}
	filename = e.normalizeDocPath(filename)
	if !e.usingOSFS {
		buf, err := e.lib.NewFromHostFile(e.FS, filename)
		if err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
			// A filename that does not exist yet is a NEW document under that
			// name, not an error — open an empty buffer carrying the name (save
			// creates it). Without this, launching mew on a new filename exits.
			// A ~/.mew/... page absent from the user tree but SHIPPED in the
			// read-only system/embedded layers opens with the shipped content
			// (a shadow it becomes the moment it is edited and saved) — the same
			// fallback the OS path does below, so a host wiring its own OS-backed
			// FS (the KittyTK trinket) surfaces the help manual too.
			if data, ok := e.mew.fallbackForLocal(filename); ok {
				buf = e.lib.NewFromString(string(data))
				buf.SetFilename(filename)
				// TODO: Later we will defer this note to the point of saving rather than here:
				// e.noteBuffer(buf, "resource", "Shipped page (edits save to your ~/.mew copy)", false)
			} else {
				buf = e.lib.New()
				buf.SetFilename(filename)
				e.noteBuffer(buf, "new", "New file", false)
			}
		}
		// The content is virtualized through the host FileSystem, but a mew-native
		// editing lock still coordinates multiple mew instances editing the same
		// path (it is an OS-level advisory lock under ~/.mew or the project, not
		// written through the host FS). Emacs locks need the real file's directory
		// and so are not available on this path. The mew lock is LAZY, like
		// garland's emacs locks: opening takes none — the lock (and any foreign /
		// stale-lock handling) is acquired on the first edit (see trackEdit), so a
		// viewer that never edits advertises nothing and never orphans a lock file.
		e.deferMewLock(buf, filename)
		return buf, nil
	}
	emacsLock, lockWarning := e.emacsLockDecision(filename)
	buf, err := e.lib.OpenFile(filename, buffer.OpenOptions{
		UseEmacsLocks: emacsLock,
		LockOwner:     e.lockOwnerString(), // one identity for both emacs and mew-native locks
	})
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		// A filename that does not exist yet is a NEW document under that name,
		// as in every editor — start an empty buffer carrying the name; save
		// creates the file, with the same backup + lock machinery armed below.
		// (Without this, `mew newfile` exited straight back to the shell.) No
		// emacs lock for a path with no directory entry: use the mew-native
		// lock only.
		emacsLock, lockWarning = false, ""
		if data, ok := e.mew.fallbackForLocal(filename); ok {
			// A ~/.mew/... page the user has no local copy of, but which ships
			// in the read-only system/embedded layers: open its content as an
			// unmodified buffer named for the user path. It reads like the
			// shipped page; the moment it is edited and saved it becomes the
			// user's own ~/.mew shadow (garland arms backups/locks then and on
			// every reopen thereafter).
			buf = e.lib.NewFromString(string(data))
			buf.SetFilename(filename)
			e.noteBuffer(buf, "resource", "Shipped page (edits save to your ~/.mew copy)", false)
		} else {
			buf = e.lib.New()
			buf.SetFilename(filename)
			e.noteBuffer(buf, "new", "New file", false)
		}
	}
	if lockWarning != "" {
		e.noteBuffer(buf, "lock", lockWarning, true)
	}
	if !emacsLock {
		// No emacs lock (config or git hygiene): fall back to a mew-native
		// lock in the nearest .mew directory. Its most common catch is the
		// user opening the same file in another mew viewport. Like the emacs
		// lock it is LAZY — acquired on the first edit (trackEdit), not at open —
		// so a viewer advertises nothing and never orphans a lock file; foreign /
		// stale-lock handling happens at that first edit too.
		e.deferMewLock(buf, filename)
	}
	if owner, ok := buf.SourceLockOwner(); ok && owner != "" {
		e.noteBuffer(buf, "lock", fmt.Sprintf("%s is being edited by %s", filepath.Base(filename), owner), true)
		e.recordForeignLock(buf, foreignLockInfo{owner: owner, kind: "emacs"})
	}
	e.armSourceSafety(buf)
	return buf, nil
}
