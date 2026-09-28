package buffer

import "testing"

// CanUndo and CanRedo answer what Undo and Redo would find, and change
// nothing about them.
func TestABufferSaysWhetherItCanUndo(t *testing.T) {
	b := NewFromString("abc")
	defer b.Close()
	if b.CanUndo() || b.CanRedo() {
		t.Fatalf("a fresh buffer: CanUndo %v, CanRedo %v, want neither", b.CanUndo(), b.CanRedo())
	}
	b.InsertText(0, 0, "X")
	b.BakeUndo()
	if !b.CanUndo() || b.CanRedo() {
		t.Fatalf("after an edit: CanUndo %v, CanRedo %v, want undo only", b.CanUndo(), b.CanRedo())
	}
	if !b.Undo() {
		t.Fatal("Undo failed where CanUndo said it would not")
	}
	if b.CanUndo() || !b.CanRedo() {
		t.Errorf("after undoing: CanUndo %v, CanRedo %v, want redo only", b.CanUndo(), b.CanRedo())
	}
	if !b.Redo() {
		t.Fatal("Redo failed where CanRedo said it would not")
	}
	if !b.CanUndo() || b.CanRedo() {
		t.Errorf("after redoing: CanUndo %v, CanRedo %v, want undo only", b.CanUndo(), b.CanRedo())
	}
}

// A buffer with no history behind it answers no to both.
func TestABufferWithoutHistoryCannotUndo(t *testing.T) {
	var b Buffer
	if b.CanUndo() || b.CanRedo() {
		t.Error("a buffer with no garland claims it can undo or redo")
	}
}
