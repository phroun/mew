package editor

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/phroun/mew/internal/buffer"
)

// This file wires garland's source-safety cluster into the editor: save
// prompts that close the overwrite data-loss holes, automatic backups,
// editing locks (emacs-interoperable with git-hygiene intelligence, mew-
// native fallback), and the per-buffer notice log behind buffer_status.

// ---------------------------------------------------------------------------
// Notices: garland-surfaced events captured per buffer. Each is shown as a
// transient when it occurs; buffer_status re-exposes the log for anything
// that timed out unseen. A future interface will let each notice be acted on
// or dismissed individually — the stored structure is the substrate for it.

type bufferNotice struct {
	When time.Time
	Kind string // "lock", "backup", "save", "source"
	Text string
}

// noteBuffer records a notice against a buffer and shows it as a transient
// (warning style when warn is set).
func (e *Editor) noteBuffer(buf *buffer.Buffer, kind, text string, warn bool) {
	if buf != nil {
		if e.bufNotices == nil {
			e.bufNotices = make(map[*buffer.Buffer][]bufferNotice)
		}
		e.bufNotices[buf] = append(e.bufNotices[buf], bufferNotice{When: time.Now(), Kind: kind, Text: text})
	}
	if warn {
		e.ShowWarning(text)
	} else {
		e.ShowNotification(text)
	}
}

// ---------------------------------------------------------------------------
// Save flows. The rules:
//   - Saving to a DIFFERENT file than the buffer's known source: prompt for
//     confirmation whenever that file already exists.
//   - Saving to the SAME filename: warn when the original file has changed
//     underneath us (metadata mismatch via garland's consistency check).
// Both prompts default to NO. A confirmed or un-conflicted save-as adopts
// the destination as the buffer's new source (history preserved).

// requestSave routes one buffer save through the safety prompts and performs
// it. target must be non-empty; done (optional) receives the outcome.
func (e *Editor) requestSave(buf *buffer.Buffer, target string, done func(bool)) {
	finish := func(ok bool) {
		if done != nil {
			done(ok)
		}
		e.RequestRender()
	}
	if buf == nil || target == "" {
		finish(false)
		return
	}

	if target == buf.GetFilename() {
		// Same filename: the only danger is the file changing underneath us.
		if buf.HasSource() {
			st := buf.SourceConsistency()
			if st.Changed() {
				msg := fmt.Sprintf("19: FILE CHANGED ON DISK (%s)", st.State)
				if st.LockedBy != "" {
					msg += " locked by " + st.LockedBy
				}
				e.noteBuffer(buf, "source", "Source "+st.State+" on disk before save: "+target, false)
				e.PromptMgr.PromptForConfirmation(msg+" — OVERWRITE?", false, func(accepted, confirmed bool) {
					if accepted && confirmed {
						finish(e.performSave(buf, target))
					} else {
						e.ShowNotification("Save cancelled")
						finish(false)
					}
				})
				return
			}
			finish(e.performSave(buf, target))
			return
		}
		// No tracked source (new file, pasted content): if the path has
		// appeared on disk since, it is someone else's file now — confirm.
		if e.fileExists(target) {
			e.PromptMgr.PromptForConfirmation(fmt.Sprintf("19: %s EXISTS ON DISK — OVERWRITE?", target), false, func(accepted, confirmed bool) {
				if accepted && confirmed {
					finish(e.performSave(buf, target))
				} else {
					e.ShowNotification("Save cancelled")
					finish(false)
				}
			})
			return
		}
		finish(e.performSave(buf, target))
		return
	}

	// Different file than the buffer's known source.
	if e.fileExists(target) {
		e.PromptMgr.PromptForConfirmation(fmt.Sprintf("13: OVERWRITE EXISTING FILE %s?", target), false, func(accepted, confirmed bool) {
			if accepted && confirmed {
				finish(e.performSave(buf, target))
			} else {
				e.ShowNotification("Save cancelled")
				finish(false)
			}
		})
		return
	}
	finish(e.performSave(buf, target))
}

// performSave executes the actual write: in place when target is the
// buffer's tracked source, else a save-as that adopts target as the new
// source. Scars and backup failures surface as notices.
func (e *Editor) performSave(buf *buffer.Buffer, target string) bool {
	var warnings []string
	var err error
	adopted := false
	if buf.HasSource() && target == buf.GetFilename() {
		warnings, err = buf.Save()
	} else {
		warnings, err = buf.SaveAsAdopt(target)
		adopted = err == nil
	}
	for _, w := range warnings {
		e.noteBuffer(buf, "save", w, true)
	}
	if err != nil {
		e.ShowError("Failed to save: " + err.Error())
		e.noteBuffer(buf, "save", "Save failed: "+err.Error(), false)
		return false
	}
	if bs := buf.BackupStatus(); bs.State == "failed" {
		e.noteBuffer(buf, "backup", "Backup failed: "+bs.Err, true)
	}
	if adopted {
		// The buffer has a (possibly new) source now: arm its safety net.
		e.armSourceSafety(buf)
	}
	e.ShowNotification("Saved: " + target)
	return true
}

// fileExists probes whether a path currently names a file, through the
// buffer's world: a real stat on the OS, a read probe through the host FS
// (hosts expose no stat; the read is the only honest signal).
func (e *Editor) fileExists(path string) bool {
	if e.usingOSFS {
		_, err := os.Stat(path)
		return err == nil
	}
	_, err := e.FS.ReadFile(path)
	return err == nil
}

// ---------------------------------------------------------------------------
// Backups: garland streams a pre-session copy of the source on the first
// edit; configuring the location is all mew has to do. The directory comes
// from the project cascade's [storage] backups (trusted only when the
// configuration was loaded from local disk — never from a host-supplied
// config string), else ~/.mew/backups.

// armSourceSafety configures automatic backups for a source-tracked buffer.
// Call after open and after a save-as adoption.
func (e *Editor) armSourceSafety(buf *buffer.Buffer) {
	if buf == nil || !buf.HasSource() || !e.usingOSFS {
		return
	}
	dir := ""
	if e.configFromDisk {
		dir = e.LoadedConfig.Storage.Backups
	}
	name := ""
	if dir == "" {
		if e.home == "" {
			return
		}
		dir = filepath.Join(e.home, ".mew", "backups")
		// The global directory pools every project's files: disambiguate
		// the emacs-style "<basename>~" name with a path hash.
		name = fmt.Sprintf("%s.%s~", filepath.Base(buf.GetFilename()), pathHash(buf.GetFilename()))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		e.noteBuffer(buf, "backup", "Backup location unavailable: "+err.Error(), true)
		return
	}
	if err := buf.ConfigureBackups(dir, name); err != nil {
		e.noteBuffer(buf, "backup", "Backups not configured: "+err.Error(), true)
	}
}

// pathHash returns a short stable identifier for a file path (used to keep
// same-named files from different projects apart in shared directories).
func pathHash(path string) string {
	abs := path
	if a, err := filepath.Abs(path); err == nil {
		abs = a
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:4])
}

// canonicalPath resolves a file path to a stable, canonical absolute form so the
// same file always maps to the same lock — no matter which working directory a
// relative name (mew go.work) was typed from, nor symlinks/firmlinks along the
// way (macOS routes /Users through a firmlink, so filepath.Abs alone can leave
// two references to one file looking different). Symlink resolution needs the
// file to exist; for a not-yet-created file it falls back to a plain absolute
// path.
func canonicalPath(path string) string {
	abs := path
	if a, err := filepath.Abs(path); err == nil {
		abs = a
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// ---------------------------------------------------------------------------
// Locks. mew respects and maintains emacs locks first and foremost — but
// skips them inside git repositories whose .gitignore does not cover the
// ".#*" lock names (a lock symlink appearing in `git status` is worse than
// no lock), with a transient telling the user the one-line fix. When emacs
// locks are off or skipped, a mew-native lock in the nearest .mew directory
// (project first, else ~/.mew) covers the most common collision of all:
// opening the same file twice yourself.

// emacsLockDecision decides whether to enable emacs locks for a file and
// returns a warning to surface when they were deliberately skipped.
func (e *Editor) emacsLockDecision(path string) (enable bool, warning string) {
	if !e.Config.UseLocks || !e.Config.UseEmacsLocks || !e.usingOSFS {
		return false, ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return true, ""
	}
	dir := filepath.Dir(abs)
	root := findGitRoot(dir)
	if root == "" {
		return true, "" // not in a git repo: lock freely
	}
	if gitIgnoresEmacsLocks(dir, root) {
		return true, ""
	}
	return false, fmt.Sprintf("Editing lock skipped; add '.#*' to %s to enable.",
		filepath.Join(root, ".gitignore"))
}

// findGitRoot walks up from dir looking for a .git entry (directory or
// worktree file), returning the containing directory or "".
func findGitRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// gitIgnoresEmacsLocks reports whether any .gitignore from dir up to (and
// including) the repo root carries a pattern covering the ".#<name>" lock
// files. The check is deliberately pragmatic — the handful of spellings
// people actually use — not a full gitignore engine.
func gitIgnoresEmacsLocks(dir, root string) bool {
	for {
		if gitignoreCoversLocks(filepath.Join(dir, ".gitignore")) {
			return true
		}
		if dir == root {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

func gitignoreCoversLocks(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch strings.TrimSpace(line) {
		case ".#*", "/.#*", "**/.#*", ".#**":
			return true
		}
	}
	return false
}

// acquireMewLock takes the mew-native lock for a file, placing it under the
// nearest .mew directory's locks/ folder (project first, else ~/.mew). A
// live foreign lock is respected: mew warns and proceeds without the lock
// (advisory, like the emacs protocol); a stale lock from a dead process on
// this host is replaced silently.
// It works regardless of whether the buffer's CONTENT is virtualized through a
// host FileSystem: the lock itself is an OS-level advisory file under ~/.mew (or
// the project), keyed to the file's path, so it coordinates any mew instances
// that share that lock directory. It returns "" on success (or when a live
// foreign lock is respected, or when locking is disabled) and a human reason
// when a lock was wanted but could not be taken, so the caller can warn.
func (e *Editor) acquireMewLock(buf *buffer.Buffer, path string) (skipReason string) {
	if !e.Config.UseLocks || buf == nil {
		return "" // locking not requested — not a failure
	}
	abs := canonicalPath(path) // stable key: same file -> same lock
	dir := e.mewLockDir(abs)   // resolve the project relative to the FILE
	if dir == "" {
		return "no project or home directory to hold the lock"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "lock directory is not writable (" + err.Error() + ")"
	}
	lockPath := filepath.Join(dir, pathHash(abs)+".lock")
	owner := e.lockOwnerString()

	if data, err := os.ReadFile(lockPath); err == nil {
		lines := strings.SplitN(string(data), "\n", 3)
		holder := strings.TrimSpace(lines[0])
		if holder != "" && holder != owner {
			if e.lockHolderAlive(holder) {
				e.noteBuffer(buf, "lock", fmt.Sprintf("%s is being edited by %s (mew lock)", filepath.Base(abs), holder), true)
				e.recordForeignLock(buf, foreignLockInfo{owner: holder, kind: "mew", path: lockPath})
				return "" // respect the live lock; open stays advisory
			}
			// Stale lock from a dead process: fall through and take it.
		}
	}
	content := owner + "\n" + abs + "\n"
	if err := os.WriteFile(lockPath, []byte(content), 0o644); err != nil {
		return "could not write the lock file (" + err.Error() + ")"
	}
	if e.mewLocks == nil {
		e.mewLocks = make(map[*buffer.Buffer]string)
	}
	e.mewLocks[buf] = lockPath
	return ""
}

// releaseMewLock drops the mew-native lock held for a buffer, if any.
func (e *Editor) releaseMewLock(buf *buffer.Buffer) {
	if lockPath, ok := e.mewLocks[buf]; ok {
		os.Remove(lockPath)
		delete(e.mewLocks, buf)
	}
}

// releaseAllMewLocks drops every held mew lock (editor shutdown).
func (e *Editor) releaseAllMewLocks() {
	for buf, lockPath := range e.mewLocks {
		os.Remove(lockPath)
		delete(e.mewLocks, buf)
	}
}

// mewLockDir returns the locks directory for a file: inside the nearest .mew
// project directory ABOVE THE FILE, else the user's ~/.mew. The project root is
// resolved relative to the file being edited — not where mew was launched — so
// two mew instances opening the same file (from any working directory) agree on
// the same lock, which is what lets them see each other.
func (e *Editor) mewLockDir(path string) string {
	if mew := e.nearestProjectMewDir(path); mew != "" {
		return filepath.Join(mew, "locks")
	}
	if e.home == "" {
		return ""
	}
	return filepath.Join(e.home, ".mew", "locks")
}

// nearestProjectMewDir walks up from a file's directory to the filesystem root
// and returns the nearest ".mew" project directory (a real directory), excluding
// the user's own ~/.mew (which is config, not a project). Returns "" when the
// file sits in no project or the project cascade is disabled. This is the
// file-relative counterpart of the config manager's launch-time project walk.
func (e *Editor) nearestProjectMewDir(path string) string {
	if !e.LoadedConfig.General.ProjectConfig || path == "" {
		return ""
	}
	dir, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	dir = filepath.Dir(dir)
	localMew := ""
	if e.home != "" {
		if a, err := filepath.Abs(filepath.Join(e.home, ".mew")); err == nil {
			localMew = a
		}
	}
	for {
		mew := filepath.Join(dir, ".mew")
		if mew != localMew {
			if fi, err := os.Stat(mew); err == nil && fi.IsDir() {
				return mew
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// lockOwnerString identifies this process in the emacs owner convention
// (user@host.pid), honoring the host's identity overrides.
func (e *Editor) lockOwnerString() string {
	user := e.Config.IdentityUser
	if user == "" {
		if user = os.Getenv("USER"); user == "" {
			user = "unknown"
		}
	}
	host := e.Config.IdentityHost
	if host == "" {
		var err error
		if host, err = os.Hostname(); err != nil {
			host = "localhost"
		}
	}
	pid := e.Config.IdentityPID
	if pid == 0 {
		pid = os.Getpid()
	}
	return fmt.Sprintf("%s@%s.%d", user, host, pid)
}

// identityHost is the host component of this session's identity, for stale-lock
// liveness checks.
func (e *Editor) identityHost() string {
	if e.Config.IdentityHost != "" {
		return e.Config.IdentityHost
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "localhost"
}

// lockHolderAlive reports whether a lock owner string names a process that
// is still running on THIS host. A different host (or any doubt) counts as
// alive — stale-lock takeover is only safe when we can actually check.
func (e *Editor) lockHolderAlive(owner string) bool {
	at := strings.LastIndex(owner, "@")
	dot := strings.LastIndex(owner, ".")
	if at < 0 || dot < at {
		return true
	}
	if owner[at+1:dot] != e.identityHost() {
		return true
	}
	pid, err := strconv.Atoi(owner[dot+1:])
	if err != nil || pid <= 0 {
		return true
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 probes existence without touching the process. On platforms
	// where the probe is unsupported the error is not ESRCH, and we stay
	// conservative (alive).
	err = proc.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return !strings.Contains(err.Error(), "process already finished") &&
		!strings.Contains(err.Error(), "no such process")
}

// ---------------------------------------------------------------------------
// buffer_status: re-expose everything captured for the focused buffer.

// bufferStatusText builds the status report for one buffer.
func (e *Editor) bufferStatusText(buf *buffer.Buffer) string {
	var sb strings.Builder
	name := buf.GetFilename()
	if name == "" {
		name = "(unnamed)"
	}
	sb.WriteString("Buffer: " + name + "\n")
	if buf.IsModified() {
		sb.WriteString("Modified: yes\n")
	} else {
		sb.WriteString("Modified: no\n")
	}

	if buf.HasSource() {
		st := buf.SourceConsistency()
		sb.WriteString("Source: " + st.State)
		if st.LockedBy != "" {
			sb.WriteString(" (locked by " + st.LockedBy + ")")
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("Source: none (buffer not backed by a tracked file)\n")
	}

	switch {
	case buf.HoldsSourceLock():
		sb.WriteString("Lock: held (emacs-style)\n")
	case e.mewLocks[buf] != "":
		sb.WriteString("Lock: held (mew): " + e.mewLocks[buf] + "\n")
	default:
		if owner, ok := buf.SourceLockOwner(); ok && owner != "" {
			sb.WriteString("Lock: foreign, held by " + owner + "\n")
		} else {
			sb.WriteString("Lock: none\n")
		}
	}

	bs := buf.BackupStatus()
	sb.WriteString("Backup: " + bs.State)
	if bs.Path != "" {
		sb.WriteString(" -> " + bs.Path)
	}
	if bs.Err != "" {
		sb.WriteString(" (" + bs.Err + ")")
	}
	sb.WriteString("\n")

	notices := e.bufNotices[buf]
	if len(notices) == 0 {
		sb.WriteString("\nNo captured notices.\n")
	} else {
		sb.WriteString("\nNotices (oldest first):\n")
		for _, n := range notices {
			sb.WriteString(fmt.Sprintf("  %s [%s] %s\n", n.When.Format("15:04:05"), n.Kind, n.Text))
		}
	}
	return sb.String()
}

// forgetBufferSafety clears safety state when a buffer leaves the editor.
func (e *Editor) forgetBufferSafety(buf *buffer.Buffer) {
	e.releaseMewLock(buf)
	delete(e.bufNotices, buf)
	e.clearBufferLockState(buf)
}
