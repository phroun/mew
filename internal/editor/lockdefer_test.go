package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// A read-only open takes no mew-native editing lock — a viewer advertises
// nothing. The lock is acquired post-hoc at the intent-to-edit boundary: the
// moment read-only is turned off. Re-enabling read-only afterwards does NOT
// release it (editing intent was declared for the session), matching emacs
// (lock until save) and vim (swapfile until close).
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

	// Intent to edit: read-only off acquires the lock now.
	if !e.setOption(w, "readonly", "false") {
		t.Fatal("set_option readonly false failed")
	}
	if e.mewLocks[w.Buffer] == "" {
		t.Fatal("turning read-only off must acquire the deferred lock")
	}
	if _, err := os.Stat(lockFile); err != nil {
		t.Fatalf("lock file should exist after read-only turns off: %v", err)
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

// With a live foreign lock on the file, a read-only open stays silent — the
// warning belongs to editing intent, not viewing. Turning read-only off
// surfaces the "being edited by" notice and still respects the foreign lock.
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

	if !e.setOption(w, "readonly", "false") {
		t.Fatal("set_option readonly false failed")
	}
	if len(e.bufNotices[w.Buffer]) == 0 {
		t.Fatal("intent to edit should surface the foreign-lock notice")
	}
	if e.mewLocks[w.Buffer] != "" {
		t.Fatal("the live foreign lock must still be respected, not taken over")
	}
	if _, ok := e.foreignLocks[w.Buffer]; !ok {
		t.Fatal("the foreign lock should be recorded so the first edit prompts")
	}
}

// Turning the GLOBAL read-only option off acquires every deferred lock —
// each viewer became editable at once.
func TestGlobalReadOnlyOffAcquiresAllDeferred(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	os.WriteFile(a, []byte("a\n"), 0o644)
	os.WriteFile(b, []byte("b\n"), 0o644)

	e, _ := newTestEditor(t, "", "useEmacsLocks=false")
	e.Config.ReadOnly = true
	wa := openInEditor(t, e, a)
	wb := openInEditor(t, e, b)
	if e.mewLocks[wa.Buffer] != "" || e.mewLocks[wb.Buffer] != "" {
		t.Fatal("read-only opens must not take locks")
	}

	if !e.setOption(nil, "readonly", "false") {
		t.Fatal("global set_option readonly false failed")
	}
	if e.mewLocks[wa.Buffer] == "" || e.mewLocks[wb.Buffer] == "" {
		t.Fatal("global read-only off must acquire all deferred locks")
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
	e.executeCommand("buffer_close")
	if _, still := e.mewLockDeferred[buf]; still {
		t.Fatal("closing the buffer should drop the deferral record")
	}
}
