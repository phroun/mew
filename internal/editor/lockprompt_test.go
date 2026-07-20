package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
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

// The mew-native lock coordinates instances even when the buffer content is
// virtualized through a host FileSystem (usingOSFS false): a second instance
// opening the same path detects the first's lock and records it as foreign.
func TestMewLockDetectedWithoutOSFS(t *testing.T) {
	home := t.TempDir() // shared ~/.mew/locks between the two instances
	mk := func(pid int) *Editor {
		e, _ := newTestEditor(t, "")
		e.usingOSFS = false // content path is virtualized
		e.home = home
		e.Config.IdentityUser, e.Config.IdentityHost, e.Config.IdentityPID = "tester", "testhost", pid
		return e
	}

	e1 := mk(1) // pid 1 (init) always reads as alive
	b1 := buffer.NewFromString("x\n")
	if reason := e1.acquireMewLock(b1, "/proj/doc.txt"); reason != "" {
		t.Fatalf("first instance should take the lock, got %q", reason)
	}

	e2 := mk(2)
	b2 := buffer.NewFromString("x\n")
	if reason := e2.acquireMewLock(b2, "/proj/doc.txt"); reason != "" {
		t.Fatalf("second instance acquire: %q", reason)
	}
	info, ok := e2.foreignLocks[b2]
	if !ok {
		t.Fatal("second instance should record a foreign lock even without OSFS")
	}
	if info.owner != "tester@testhost.1" || info.kind != "mew" {
		t.Fatalf("foreign lock = %+v, want owner tester@testhost.1 kind mew", info)
	}
}

// A lock that cannot be taken is reported (a reason), so the caller can warn —
// no silent skip.
func TestMewLockReportsFailure(t *testing.T) {
	e, _ := newTestEditor(t, "")
	e.usingOSFS = false
	e.home = "" // no home and no project: nowhere to hold the lock
	e.LoadedConfig.ProjectDirs = nil
	b := buffer.NewFromString("x\n")
	reason := e.acquireMewLock(b, "/proj/doc.txt")
	if reason == "" {
		t.Fatal("acquireMewLock should report a reason when it cannot lock")
	}
}
