package editor

import "testing"

// The host is told whether the focused buffer can undo and redo: once at the
// start, then only when the answer changes.
func TestTheHostLearnsWhatCanBeUndone(t *testing.T) {
	type pair struct{ undo, redo bool }
	var seen []pair
	e, w := newTestEditor(t, "hello\n")
	e.Config.UndoState = func(u, r bool) { seen = append(seen, pair{u, r}) }

	e.notifyUndoState()
	if len(seen) != 1 || seen[0] != (pair{}) {
		t.Fatalf("an untouched buffer should push one (false, false), got %v", seen)
	}

	w.Buffer.InsertText(0, 0, "X")
	w.Buffer.BakeUndo()
	e.notifyUndoState()
	if len(seen) != 2 || seen[1] != (pair{true, false}) {
		t.Fatalf("after an edit it should push (true, false), got %v", seen)
	}

	e.notifyUndoState()
	if len(seen) != 2 {
		t.Fatalf("an unchanged answer should push nothing, got %v", seen)
	}

	w.Buffer.Undo()
	e.notifyUndoState()
	if len(seen) != 3 || seen[2] != (pair{false, true}) {
		t.Fatalf("after undoing it should push (false, true), got %v", seen)
	}
}
