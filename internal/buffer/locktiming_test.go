package buffer

import (
	"os"
	"path/filepath"
	"testing"
)

// garland v0.1.10 contract, proven through mew's Buffer layer: decoration-only
// changes (block marks) never create the emacs lock; a content edit does; and
// undoing the edit releases it even with mark revisions interleaved.
func TestMarksDoNotEmacsLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(dir, ".#doc.txt")

	b, err := OpenFile(path, OpenOptions{UseEmacsLocks: true, LockOwner: "tester@host.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	if _, err := os.Stat(lockPath); err == nil {
		t.Fatal("open must not create a lock file")
	}

	// Decoration-only: set block begin/end marks.
	if err := b.SetMark("block_begin", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := b.SetMark("block_end", 0, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); err == nil {
		t.Fatal("setting marks must not create the emacs lock")
	}

	// A real content edit locks.
	b.BeginUserCommand("insert")
	b.InsertText(0, 0, "X")
	b.EndUserCommand()
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatal("a content edit must create the emacs lock")
	}

	// Undo the edit: back to content-clean releases the lock, even though
	// decoration revisions sit between the clean point and here.
	if !b.Undo() {
		t.Fatal("undo failed")
	}
	if _, err := os.Stat(lockPath); err == nil {
		t.Fatal("undoing back to content-clean must release the lock")
	}
}
