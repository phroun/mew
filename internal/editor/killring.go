package editor

import (
	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// killRingCapacity returns how many kill entries the ring retains — the
// killRingEntries option, floored at 1 (default 10).
func (e *Editor) killRingCapacity() int {
	if n := e.Config.KillRingEntries; n >= 1 {
		return n
	}
	return 10
}

// trimKillRing evicts (and closes) the oldest entries past the capacity —
// after a push, or after killRingEntries is lowered at runtime.
func (e *Editor) trimKillRing() {
	max := e.killRingCapacity()
	for len(e.killRing) > max {
		e.killRing[len(e.killRing)-1].Close()
		e.killRing = e.killRing[:len(e.killRing)-1]
	}
	if e.killPopIdx >= len(e.killRing) {
		e.killPopIdx = 0
	}
}

// yankRecord remembers the extent of the most recent kill_ring_yank (or
// kill_ring_pop) so kill_ring_pop can replace it with an older ring entry. It
// is only honored while the caret still sits exactly at the yank's end in the
// same window, and the command dispatcher invalidates it as soon as any other
// command runs — the emacs "previous command was not a yank" rule.
type yankRecord struct {
	windowID  string
	startByte int64
	endByte   int64
	valid     bool
}

// killCapture feeds one delete's capture into the global kill ring. Deletes
// belonging to the same edit accumulate into the same entry — appended for
// forward deletes, prepended for backward ones — where "same edit" reuses the
// cursor-ring plumbing: the caret has not deliberately moved off the last edit
// point (hasMoved false) and the previous tracked edit was also a kill.
// kill_ring_append forces the next capture to accumulate regardless. Anything
// else starts a fresh entry at the head of the ring.
func (e *Editor) killCapture(w *window.Window, cap buffer.Captured, forward bool) {
	if cap.Empty() {
		return
	}
	e.pendingKill = true
	if len(e.killRing) > 0 && (e.killAppendNext || (e.lastEditKill && !w.HasMovedSinceEdit())) {
		e.killAppendNext = false
		if forward {
			e.killRing[0].Append(cap)
		} else {
			e.killRing[0].Prepend(cap)
		}
		return
	}
	e.killAppendNext = false
	e.pushKillEntry(cap)
}

// pushKillEntry starts a new kill-ring entry at the head, evicting the oldest
// past the killRingEntries capacity.
func (e *Editor) pushKillEntry(cap buffer.Captured) {
	kb := e.lib.NewKillBuffer()
	if kb == nil {
		return
	}
	kb.Append(cap)
	e.killRing = append([]*buffer.KillBuffer{kb}, e.killRing...)
	e.trimKillRing()
	e.killPopIdx = 0
}

// killRingYank inserts the most recent kill entry at the caret, marks and all,
// like insert. Records the yanked extent so kill_ring_pop can replace it.
func (e *Editor) killRingYank() bool {
	if e.contentLocked() {
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil || w.Caret == nil {
		return false
	}
	if len(e.killRing) == 0 {
		e.ShowWarning("Kill ring is empty")
		return false
	}
	cap := e.killRing[0].Capture()
	if cap.Empty() {
		e.ShowWarning("Kill ring is empty")
		return false
	}

	e.clampCursorToBuffer(w)
	cur := w.CursorPos()
	w.Caret.Seek(cur.Line, cur.Rune)
	start := w.Caret.BytePos()
	w.Caret.InsertCaptured(cap)
	end := w.Caret.BytePos()

	e.lastYank = yankRecord{windowID: w.ID, startByte: start, endByte: end, valid: true}
	e.killPopIdx = 0

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.RequestRender()
	return true
}

// killRingPop replaces the text just yanked with the next-older ring entry,
// rotating around the ring (yank-pop). Only valid immediately after a
// kill_ring_yank or kill_ring_pop, with the caret still at the yank's end.
//
// The previous placement is removed by UNDOING it: the yank was one atomic
// revision (text insert + mark placement), so undo restores every mark to its
// pre-yank state — placed copies vanish and a relocated same-name mark returns
// to where it was — leaving no droppings from entries passed through while
// cycling. The rotated entry is then placed as a fresh yank.
func (e *Editor) killRingPop() bool {
	if e.contentLocked() {
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil || w.Caret == nil || len(e.killRing) == 0 {
		return false
	}
	if !e.lastYank.valid || e.lastYank.windowID != w.ID || w.Caret.BytePos() != e.lastYank.endByte {
		e.ShowWarning("Previous command was not a yank")
		return false
	}

	// Undo the previous yank/pop placement wholesale. This runs before any
	// mutation in this command, so no command transaction is open yet (garland
	// rejects an undo-seek mid-transaction).
	if !w.Buffer.Undo() {
		e.ShowWarning("Cannot undo the previous yank")
		return false
	}

	e.killPopIdx = (e.killPopIdx + 1) % len(e.killRing)
	cap := e.killRing[e.killPopIdx].Capture()

	// Re-yank the rotated entry where the undone placement began: the undo
	// also restored the caret to its pre-yank position, so it is already
	// sitting there.
	start := w.Caret.BytePos()
	w.Caret.InsertCaptured(cap)
	end := w.Caret.BytePos()

	e.lastYank = yankRecord{windowID: w.ID, startByte: start, endByte: end, valid: true}

	e.afterHorizontalMovement(w)
	e.ensureCursorVisible(w)
	e.RequestRender()
	return true
}

// blockCopyKill copies the marked block into a fresh kill-ring entry without
// deleting it (kill-ring-save). The copy is its own entry: it never merges
// with a delete accumulation, and it breaks any accumulation in progress.
func (e *Editor) blockCopyKill() bool {
	if e.contentLocked() {
		// The buffer's owning window is read-only, or a link button is
		// focused: reject the mutation at its source (name-agnostic).
		return false
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		e.ShowWarning("No active buffer")
		return false
	}
	startLine, startRune, endLine, endRune, exists := w.Buffer.GetBlockRange()
	if !exists {
		e.ShowWarning("No block marked")
		return false
	}
	cap := w.Buffer.CaptureRange(startLine, startRune, endLine, endRune)
	if cap.Empty() {
		e.ShowWarning("Block is empty")
		return false
	}
	e.pushKillEntry(cap)
	e.lastEditKill = false
	e.ShowNotification("Block copied to kill ring")
	return true
}
