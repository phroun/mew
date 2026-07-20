package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/phroun/mew/internal/buffer"
)

// foreignLockInfo describes a live lock held by someone else that mew respected
// when opening a file: the owner string, whether it is an "emacs" (.#name) or
// "mew" (native) lock, and — for mew locks — the lock file path so it can be
// stolen.
type foreignLockInfo struct {
	owner string
	kind  string // "emacs" | "mew"
	path  string // mew lock file (kind == "mew")
}

// recordForeignLock notes a foreign lock respected on open, so the first edit
// can prompt the user.
func (e *Editor) recordForeignLock(buf *buffer.Buffer, info foreignLockInfo) {
	if buf == nil {
		return
	}
	if e.foreignLocks == nil {
		e.foreignLocks = make(map[*buffer.Buffer]foreignLockInfo)
	}
	e.foreignLocks[buf] = info
}

// clearBufferLockState drops all edit-lock bookkeeping for a buffer (on close).
func (e *Editor) clearBufferLockState(buf *buffer.Buffer) {
	delete(e.foreignLocks, buf)
	delete(e.lockResolved, buf)
}

// checkEditLock is the trackEdit hook: on the first edit that modifies a buffer
// whose file another editor holds a lock on, prompt the user to Steal the lock,
// Proceed (edit anyway, leaving their lock), or Quit the edit (revert it). It
// runs at most once per buffer until the lock is resolved or the buffer is
// reverted clean.
func (e *Editor) checkEditLock() {
	if e.lockPrompting {
		return // a prompt is already up; ignore the edits it may cause
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		return
	}
	buf := w.Buffer
	if e.lockResolved[buf] || !buf.IsModified() {
		return
	}
	info, ok := e.foreignLocks[buf]
	if !ok || !e.foreignLockLive(info) {
		delete(e.foreignLocks, buf) // gone (they closed): nothing to prompt
		return
	}

	// Guard against re-entry while the prompt is up.
	if e.lockResolved == nil {
		e.lockResolved = make(map[*buffer.Buffer]bool)
	}
	e.lockResolved[buf] = true
	e.lockPrompting = true

	name := "this file"
	if fn := buf.GetFilename(); fn != "" {
		name = filepath.Base(fn)
	}
	// Two-row prompt: a top message bar describes who already holds the lock,
	// and the input row asks only the short question.
	desc := fmt.Sprintf("%s is being edited by %s", name, info.owner)
	question := "[S]teal lock, [P]roceed, [Q]uit edit: "
	e.PromptMgr.promptForInput(question, "", func(accepted bool, _, line string) {
		e.lockPrompting = false
		resp := strings.ToLower(strings.TrimSpace(line))
		first := ""
		if len(resp) > 0 {
			first = resp[:1]
		}
		switch first {
		case "s":
			e.stealLock(buf, info)
			e.ShowNotification("Lock stolen; you now hold it")
		case "p", "i":
			e.noteBuffer(buf, "lock", "Editing anyway; "+info.owner+" still holds the lock", false)
		default: // q, empty, Escape, anything else: cancel the edit
			if err := buf.RevertToLastSave(); err == nil {
				e.syncCursorAfterUndoRedo(w)
			}
			e.lockResolved[buf] = false // clean again: prompt on the next edit
			e.ShowNotification("Edit cancelled; " + name + " left untouched")
		}
		e.RequestRender()
	}, "lock", 1, desc)
}

// foreignLockLive reports whether the recorded foreign lock is still held.
func (e *Editor) foreignLockLive(info foreignLockInfo) bool {
	switch info.kind {
	case "mew":
		data, err := os.ReadFile(info.path)
		if err != nil {
			return false // lock file gone
		}
		holder := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
		return holder == info.owner && e.lockHolderAlive(holder)
	default: // emacs: garland tracks it
		return true
	}
}

// stealLock takes over a foreign lock: for emacs locks garland breaks and
// reacquires; for mew locks we overwrite the lock file and adopt it.
func (e *Editor) stealLock(buf *buffer.Buffer, info foreignLockInfo) {
	switch info.kind {
	case "mew":
		content := e.lockOwnerString() + "\n"
		if abs := buf.GetFilename(); abs != "" {
			if a, err := filepath.Abs(abs); err == nil {
				content += a + "\n"
			}
		}
		if err := os.WriteFile(info.path, []byte(content), 0o644); err == nil {
			if e.mewLocks == nil {
				e.mewLocks = make(map[*buffer.Buffer]string)
			}
			e.mewLocks[buf] = info.path
		}
	default:
		_ = buf.BreakSourceLock()
	}
	delete(e.foreignLocks, buf)
}
