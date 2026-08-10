package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// Opening takes no mew-native editing lock — a viewer advertises nothing. The
// lock is claimed lazily on the FIRST EDIT (like garland's emacs locks), not at
// open and not when read-only is toggled off. Re-enabling read-only afterwards
// does NOT release it (editing intent was declared for the session), matching
// emacs (lock until save) and vim (swapfile until close).
func TestReadOnlyOpenDefersMewLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("x\n"), 0o644)

	e, _ := newTestEditor(t, "", "useEmacsLocks=false")
	e.Config.ReadOnly = true
	w := openInEditor(t, e, path)

	lockFile := filepath.Join(home, ".mew", "locks", pathHash(canonicalPath(path))+".lock")
	if e.mewLocks[w.Buffer] != "" {
		t.Fatal("a read-only open must not take the mew lock")
	}
	if _, err := os.Stat(lockFile); err == nil {
		t.Fatal("a read-only open must not create a lock file")
	}
	if e.mewLockDeferred[w.Buffer] == "" {
		t.Fatal("the deferred lock should be recorded for post-hoc acquisition")
	}

	// Turning read-only OFF no longer acquires the lock — under lazy locking only
	// a real edit does; the deferral stays pending.
	if !e.setOption(w, "readonly", "false") {
		t.Fatal("set_option readonly false failed")
	}
	if e.mewLocks[w.Buffer] != "" {
		t.Fatal("turning read-only off must NOT acquire the lock (only the first edit does)")
	}
	if e.mewLockDeferred[w.Buffer] == "" {
		t.Fatal("the deferral should still be pending until the first edit")
	}

	// The first edit claims the deferred lock.
	typeText(t, e, "Z")
	if e.mewLocks[w.Buffer] == "" {
		t.Fatal("the first edit must acquire the deferred lock")
	}
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatalf("lock file should exist after the first edit: %v", err)
	}
	if _, still := e.mewLockDeferred[w.Buffer]; still {
		t.Fatal("the deferral record should be consumed")
	}

	// Back to read-only: the lock stays for the session.
	if !e.setOption(w, "readonly", "true") {
		t.Fatal("set_option readonly true failed")
	}
	if e.mewLocks[w.Buffer] == "" {
		t.Fatal("re-enabling read-only must not release the session lock")
	}
}

// With a live foreign lock on the file, opening stays silent — the warning
// belongs to editing, not viewing. The first edit surfaces the "being edited
// by" notice and still respects the foreign lock (no takeover).
func TestReadOnlyOpenForeignLockNoticeDeferred(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("x\n"), 0o644)

	foreign := filepath.Join(home, ".mew", "locks", pathHash(canonicalPath(path))+".lock")
	os.MkdirAll(filepath.Dir(foreign), 0o755)
	os.WriteFile(foreign, []byte("someone@elsewhere.4242\n"+path+"\n"), 0o644)

	e, _ := newTestEditor(t, "", "useEmacsLocks=false")
	e.Config.ReadOnly = true
	w := openInEditor(t, e, path)

	if len(e.bufNotices[w.Buffer]) != 0 {
		t.Fatalf("a read-only open should raise no lock notice; got %v", e.bufNotices[w.Buffer])
	}

	// The first edit (lazy acquisition) surfaces the foreign-lock notice and
	// still respects the live lock.
	e.ensureDeferredMewLock(w.Buffer)
	if len(e.bufNotices[w.Buffer]) == 0 {
		t.Fatal("the first edit should surface the foreign-lock notice")
	}
	if e.mewLocks[w.Buffer] != "" {
		t.Fatal("the live foreign lock must still be respected, not taken over")
	}
	if _, ok := e.foreignLocks[w.Buffer]; !ok {
		t.Fatal("the foreign lock should be recorded so the edit prompts")
	}
}

// Opening takes no lock; the first real edit (through the command path, firing
// trackEdit) claims the deferred mew lock end-to-end.
func TestFirstEditClaimsMewLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("x\n"), 0o644)

	e, _ := newTestEditor(t, "", "useEmacsLocks=false")
	w := openInEditor(t, e, path)
	if e.mewLocks[w.Buffer] != "" {
		t.Fatal("no mew lock should be held at open (lazy)")
	}
	typeText(t, e, "Z")
	if e.mewLocks[w.Buffer] == "" {
		t.Fatal("the first edit should claim the mew lock")
	}
}

// Closing a read-only buffer discards its deferral record along with the rest
// of its safety state — nothing lingers to acquire later.
func TestCloseDropsDeferredLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("x\n"), 0o644)

	e, _ := newTestEditor(t, "keep\n", "useEmacsLocks=false")
	e.Config.ReadOnly = true
	w := openInEditor(t, e, path)
	buf := w.Buffer
	if e.mewLockDeferred[buf] == "" {
		t.Fatal("deferral should be recorded")
	}
	e.executeCommand("viewport_close")
	if _, still := e.mewLockDeferred[buf]; still {
		t.Fatal("closing the buffer should drop the deferral record")
	}
}
