package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The first edit of a buffer whose file another editor holds a lock on prompts
// the user; "proceed" resolves it and a later edit does not re-prompt.
func TestEditLockPromptProceed(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "hello\n")
	w.Buffer.SetFilename("locked.txt")
	w.Buffer.SetModified(true)
	e.recordForeignLock(w.Buffer, foreignLockInfo{owner: "jeff@other.1", kind: "emacs"})

	e.checkEditLock() // the trackEdit hook
	fp := focusedPrompt(e)
	if fp == nil {
		t.Fatal("expected a lock prompt on the first edit of a locked buffer")
	}
	// Two rows: a top message bar describes who holds the lock; the input row's
	// label asks only the short question.
	if !strings.Contains(fp.MessageTopInner, "is being edited by jeff@other.1") {
		t.Fatalf("top message = %q, want the who-holds-the-lock description", fp.MessageTopInner)
	}
	if len(fp.RowMessages) == 0 || !strings.Contains(fp.RowMessages[0], "[S]teal lock") {
		t.Fatalf("prompt label = %v, want the short question", fp.RowMessages)
	}
	if fp.MessageTopInner != "" && fp.Height < 2 {
		t.Fatalf("a lock prompt with a top message should be 2 rows tall, got height %d", fp.Height)
	}
	answerPrompt(t, e, "p") // Proceed
	if focusedPrompt(e) != nil {
		t.Error("prompt should dismiss after answering")
	}
	if !e.lockResolved[w.Buffer] {
		t.Error("proceed should resolve the lock")
	}
	// A subsequent edit must not re-prompt.
	e.checkEditLock()
	if focusedPrompt(e) != nil {
		t.Error("must not re-prompt after the lock is resolved")
	}
}

// "Steal" overwrites a mew-native lock file with our owner.
func TestEditLockStealMew(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "x.lock")
	if err := os.WriteFile(lockPath, []byte("someone@otherhost.999\n/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, w, _ := newRenderedEditor(t, "hi\n")
	w.Buffer.SetFilename(filepath.Join(dir, "x"))
	w.Buffer.SetModified(true)
	e.recordForeignLock(w.Buffer, foreignLockInfo{owner: "someone@otherhost.999", kind: "mew", path: lockPath})

	e.checkEditLock()
	if focusedPrompt(e) == nil {
		t.Fatal("expected a lock prompt")
	}
	answerPrompt(t, e, "s") // Steal

	data, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("lock file gone: %v", err)
	}
	first := strings.SplitN(string(data), "\n", 2)[0]
	if first != e.lockOwnerString() {
		t.Errorf("lock should now be ours: got %q, want %q", first, e.lockOwnerString())
	}
	if e.mewLocks[w.Buffer] != lockPath {
		t.Error("stolen mew lock should be tracked as held")
	}
}

// "Quit" cancels the edit and leaves the lock unresolved, so the next edit
// prompts again.
func TestEditLockCancelReprompts(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "hello\n")
	w.Buffer.SetFilename("locked.txt")
	w.Buffer.SetModified(true)
	e.recordForeignLock(w.Buffer, foreignLockInfo{owner: "j@o.1", kind: "emacs"})
	e.checkEditLock()
	answerPrompt(t, e, "q") // Quit edit
	if e.lockResolved[w.Buffer] {
		t.Error("cancel must leave the lock unresolved so the next edit re-prompts")
	}
}

// An unlocked buffer never prompts.
func TestEditLockNoPromptWhenUnlocked(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "hi\n")
	w.Buffer.SetModified(true)
	e.checkEditLock()
	if focusedPrompt(e) != nil {
		t.Error("no foreign lock: must not prompt")
	}
}
