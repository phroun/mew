package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// Consecutive forward deletes in the same edit accumulate into one kill entry
// (appended in order), and kill_ring_yank re-inserts the whole entry.
func TestKillRingForwardAccumulation(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.executeCommand("del_char_next")
	e.executeCommand("del_char_next")
	e.executeCommand("del_char_next")
	if got := docContent(w); got != "def" {
		t.Fatalf("after 3 deletes: %q", got)
	}
	if len(e.killRing) != 1 {
		t.Fatalf("consecutive deletes should share one entry, ring has %d", len(e.killRing))
	}

	// Yank at end of the remaining text.
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "defabc" {
		t.Fatalf("yank should insert %q appended in kill order, got %q", "abc", got)
	}
}

// Consecutive backspaces prepend, preserving the original text order.
func TestKillRingBackwardPrepends(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})

	e.executeCommand("del_char_prior") // kills "c"
	e.executeCommand("del_char_prior") // prepends "b"
	e.executeCommand("del_char_prior") // prepends "a"
	if got := docContent(w); got != "def" {
		t.Fatalf("after 3 backspaces: %q", got)
	}
	if len(e.killRing) != 1 {
		t.Fatalf("backspace run should share one entry, ring has %d", len(e.killRing))
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "abcdef" {
		t.Fatalf("prepend accumulation should reconstruct %q, got %q", "abcdef", got)
	}
}

// Moving the caret between deletes starts a new kill entry; an intervening
// non-kill edit (typing) also breaks the accumulation.
func TestKillRingNewEntryOnMoveOrEdit(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\nxyz\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.executeCommand("del_char_next") // entry: "a"
	e.executeCommand("go_line_next")  // deliberate move
	e.executeCommand("del_char_next") // new entry: "x"
	if len(e.killRing) != 2 {
		t.Fatalf("a move between deletes should split entries, ring has %d", len(e.killRing))
	}

	e.executeCommand("insert 'Q'")    // non-kill edit at the same spot
	e.executeCommand("del_char_next") // must start a third entry
	if len(e.killRing) != 3 {
		t.Fatalf("typing between deletes should split entries, ring has %d", len(e.killRing))
	}
}

// A kill NEVER removes marks from the source buffer: garland collapses them
// to the kill point (the kill entry holds copies). Yanking in the same buffer
// then MOVES the collapsed remnant into the placement — a mark key is one
// identity per buffer, and the newest placement wins.
func TestKillRingMarksCollapseAndRideYank(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\nxyz\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.executeCommand("set_mark '5'") // mark "5" at (0,3)

	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("del_line") // kills "abcdef\n"; remnant collapses to (0,0)

	line, runePos, exists := w.Buffer.GetMark("5")
	if !exists || line != 0 || runePos != 0 {
		t.Fatalf("remnant should collapse at the kill point (0,0): line=%d rune=%d exists=%v",
			line, runePos, exists)
	}

	// Yank onto the now-first line: the remnant moves into the placement,
	// 3 runes in — one identity, no duplicate left at the collapse point.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "abcdef\nxyz" {
		t.Fatalf("yank should restore the line: %q", got)
	}
	line, runePos, exists = w.Buffer.GetMark("5")
	if !exists || line != 0 || runePos != 3 {
		t.Fatalf("mark should ride the yank to (0,3): line=%d rune=%d exists=%v", line, runePos, exists)
	}

	// Yanking the same entry again further down moves the mark again: the
	// newest placement wins within a buffer.
	w.SetCursorPos(window.Position{Line: 1, Rune: 3})
	e.executeCommand("kill_ring_yank")
	line, runePos, exists = w.Buffer.GetMark("5")
	if !exists || line != 1 || runePos != 6 {
		t.Fatalf("mark should follow the newest yank to (1,6): line=%d rune=%d exists=%v",
			line, runePos, exists)
	}
}

// Yanking into a DIFFERENT buffer sets that buffer's own mark: buffers never
// share mark identities, so the source buffer's mark stays put.
func TestKillRingMarksCrossBuffer(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\nxyz\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.executeCommand("set_mark '5'")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("del_line")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("kill_ring_yank") // source buffer's mark now at (0,3)

	id := e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc2", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buffer.NewFromString("second\n"), SetFocus: true,
	})
	w2 := e.WindowManager.GetWindow(id)
	w2.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("kill_ring_yank")

	if l, r, ok := w2.Buffer.GetMark("5"); !ok || l != 0 || r != 3 {
		t.Fatalf("second buffer should get its own mark at (0,3): l=%d r=%d ok=%v", l, r, ok)
	}
	if l, r, ok := w.Buffer.GetMark("5"); !ok || l != 0 || r != 3 {
		t.Fatalf("source buffer's mark must be untouched at (0,3): l=%d r=%d ok=%v", l, r, ok)
	}
}

// kill_ring_pop undoes the previous placement wholesale before placing the
// rotated entry, so cycling through entries leaves no mark droppings: the
// passed-through entry's mark returns to wherever it was before the yank.
func TestKillRingPopNoDroppings(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nddd\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 1})
	e.executeCommand("set_mark '3'") // inside "aaa"
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("del_line")     // entry A: "aaa\n" + mark 3; remnant at (0,0)
	e.executeCommand("go_line_next") // move: next kill is a new entry
	e.executeCommand("del_line")     // entry B: "ccc\n" (ring: [B, A])

	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	e.executeCommand("kill_ring_yank") // places B ("ccc\n")
	e.executeCommand("kill_ring_pop")  // undo B's placement, place A with mark
	if got := docContent(w); got != "bbb\naaa\nddd" {
		t.Fatalf("pop should swap in entry A: %q", got)
	}
	if l, r, ok := w.Buffer.GetMark("3"); !ok || l != 1 || r != 1 {
		t.Fatalf("A's mark should sit in its placement at (1,1): l=%d r=%d ok=%v", l, r, ok)
	}

	e.executeCommand("kill_ring_pop") // wrap back to B: A's placement undone
	if got := docContent(w); got != "bbb\nccc\nddd" {
		t.Fatalf("second pop should swap B back in: %q", got)
	}
	// A's mark returned to its pre-yank spot (the remnant collapse point at
	// the top of the buffer) — no dropping left at the pop site.
	if l, r, ok := w.Buffer.GetMark("3"); !ok || l != 0 || r != 0 {
		t.Fatalf("A's mark should be restored to the collapse point (0,0): l=%d r=%d ok=%v", l, r, ok)
	}
}

// kill_ring_pop replaces the just-yanked text with the next-older entry, and
// is refused when the previous command was not a yank.
func TestKillRingPop(t *testing.T) {
	e, w := newTestEditor(t, "aaa\nbbb\nccc\nddd\n")

	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("del_line") // entry A: "aaa\n"
	e.executeCommand("go_line_next")
	e.executeCommand("del_line") // entry B: "ccc\n" (ring: [B, A])
	if got := docContent(w); got != "bbb\nddd" {
		t.Fatalf("setup: %q", got)
	}

	e.executeCommand("kill_ring_yank") // inserts "ccc\n"
	if got := docContent(w); got != "bbb\nccc\nddd" {
		t.Fatalf("yank: %q", got)
	}
	e.executeCommand("kill_ring_pop") // replaces with "aaa\n"
	if got := docContent(w); got != "bbb\naaa\nddd" {
		t.Fatalf("pop should swap in the older kill: %q", got)
	}
	// Another pop wraps back to the newest entry.
	e.executeCommand("kill_ring_pop")
	if got := docContent(w); got != "bbb\nccc\nddd" {
		t.Fatalf("second pop should wrap to the newest kill: %q", got)
	}

	// After a non-yank command, pop is refused and changes nothing.
	e.executeCommand("go_line_prior")
	before := docContent(w)
	e.executeCommand("kill_ring_pop")
	if got := docContent(w); got != before {
		t.Fatalf("pop after a movement should be refused: %q -> %q", before, got)
	}
	if !hasWarning(e, "Previous command was not a yank") {
		t.Fatal("expected the not-a-yank warning")
	}
}

// block_copy_kill copies the marked block into a new kill entry without
// deleting anything.
func TestBlockCopyKill(t *testing.T) {
	e, w := newTestEditor(t, "hello world\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("set_block_begin")
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	e.executeCommand("set_block_end")

	e.executeCommand("block_copy_kill")
	if got := docContent(w); got != "hello world" {
		t.Fatalf("copy must not modify the buffer: %q", got)
	}
	if len(e.killRing) != 1 {
		t.Fatalf("ring should hold the copied block, has %d entries", len(e.killRing))
	}

	// Yank onto the trailing empty line.
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "hello world\nhello" {
		t.Fatalf("yank of copied block: %q", got)
	}
}

// kill_ring_append forces the next kill to accumulate into the newest entry
// even after the caret has moved away.
func TestKillRingAppendNext(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	e.executeCommand("del_char_next") // entry: "a" (content "bcdef")
	e.executeCommand("go_char_next")  // move away: would normally split
	e.executeCommand("kill_ring_append")
	e.executeCommand("del_char_next") // kills "c", appends to the entry
	if len(e.killRing) != 1 {
		t.Fatalf("kill_ring_append should merge into one entry, ring has %d", len(e.killRing))
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "acbdef" {
		t.Fatalf("merged entry should yank %q first: got %q", "ac", got)
	}
}

// The kill ring is global: text killed in one buffer yanks into another.
func TestKillRingCrossBuffer(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("del_word_end") // kills "abcdef"

	id := e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc2", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buffer.NewFromString("second\n"), SetFocus: true,
	})
	w2 := e.WindowManager.GetWindow(id)
	w2.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w2); got != "abcdefsecond" {
		t.Fatalf("cross-buffer yank: %q", got)
	}
}

// The ring retains killRingEntries entries, evicting the oldest past that.
func TestKillRingCapacity(t *testing.T) {
	e, w := newTestEditor(t, "aa\nbb\ncc\ndd\nee\n", "killRingEntries=2")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})

	// Three separate line kills, split by a deliberate move between each.
	e.executeCommand("del_line")     // entry: "aa\n"
	e.executeCommand("go_line_next") // move away
	e.executeCommand("del_line")     // entry: "cc\n"
	e.executeCommand("go_line_next")
	e.executeCommand("del_line") // entry: "ee\n" -> evicts "aa\n"

	if len(e.killRing) != 2 {
		t.Fatalf("ring should be capped at killRingEntries=2, has %d", len(e.killRing))
	}
	// The two retained entries are the newest two: "ee\n" then (pop) "cc\n".
	e.executeCommand("kill_ring_yank") // "ee\n" onto the trailing empty line
	e.executeCommand("kill_ring_pop")  // -> "cc\n" (the older retained entry)
	e.executeCommand("kill_ring_pop")  // wraps back -> "ee\n"
	if got := docContent(w); got != "bb\ndd\nee" {
		t.Fatalf("double pop should wrap back to the newest entry: %q", got)
	}
}

// block_move carries the marks inside the block along with it — the in-range
// user marks AND the block markers themselves, so the block stays marked at
// its destination.
func TestBlockMoveCarriesMarks(t *testing.T) {
	e, w := newTestEditor(t, "AAA\nBBB\nCCC\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 1, 0) // block is "AAA\n"
	w.Buffer.SetMark("4", 0, 2)          // user mark inside the block, on the last A

	w.SetCursorPos(window.Position{Line: 2, Rune: 0}) // start of CCC
	e.executeCommand("block_move")

	if got := docContent(w); got != "BBB\nAAA\nCCC" {
		t.Fatalf("moved content: %q", got)
	}
	// The user mark traveled with its text: line 1, rune 2.
	line, runePos, exists := w.Buffer.GetMark("4")
	if !exists || line != 1 || runePos != 2 {
		t.Fatalf("mark 4 should ride to (1,2): line=%d rune=%d exists=%v", line, runePos, exists)
	}
	// The block markers moved too: the block is still marked, now spanning the
	// moved text.
	sl, sr, el, er, exists := w.Buffer.GetBlockRange()
	if !exists {
		t.Fatal("block markers should survive the move")
	}
	if sl != 1 || sr != 0 || el != 2 || er != 0 {
		t.Fatalf("block should now span (1,0)-(2,0), got (%d,%d)-(%d,%d)", sl, sr, el, er)
	}
}

// A mark exactly on the block's end boundary stays put (it is outside the
// captured range), matching the kill capture's filtering decision.
func TestBlockMoveLeavesBoundaryMark(t *testing.T) {
	e, w := newTestEditor(t, "AAA\nBBB\nCCC\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 1, 0)
	w.Buffer.SetMark("9", 1, 1) // outside the block (inside BBB)

	w.SetCursorPos(window.Position{Line: 2, Rune: 0})
	e.executeCommand("block_move")

	// BBB slid up to line 0; the mark rode BBB, not the moved block.
	line, runePos, exists := w.Buffer.GetMark("9")
	if !exists || line != 0 || runePos != 1 {
		t.Fatalf("outside mark should stay with BBB at (0,1): line=%d rune=%d exists=%v",
			line, runePos, exists)
	}
}

// block_delete is a kill-region: the deleted block (with its in-range user
// marks, but NOT the block markers) lands on the kill ring and can be yanked.
func TestBlockDeleteKills(t *testing.T) {
	e, w := newTestEditor(t, "AAA\nBBB\nCCC\n")
	w.Buffer.SetMark("_block_begin", 0, 0)
	w.Buffer.SetMark("_block_end", 1, 0) // block is "AAA\n"
	w.Buffer.SetMark("6", 0, 1)          // user mark inside the block

	w.SetCursorPos(window.Position{Line: 2, Rune: 0})
	e.executeCommand("block_delete")
	if got := docContent(w); got != "BBB\nCCC" {
		t.Fatalf("after block_delete: %q", got)
	}
	if len(e.killRing) != 1 {
		t.Fatalf("block_delete should push one kill entry, ring has %d", len(e.killRing))
	}
	if _, _, _, _, exists := w.Buffer.GetBlockRange(); exists {
		t.Fatal("block marks should be cleared by block_delete")
	}

	// Yank at the end restores the text and the user mark rides along; the
	// block markers do not reappear.
	w.SetCursorPos(window.Position{Line: 1, Rune: 3})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "BBB\nCCCAAA" {
		t.Fatalf("yank of killed block: %q", got)
	}
	line, runePos, exists := w.Buffer.GetMark("6")
	if !exists || line != 1 || runePos != 4 {
		t.Fatalf("user mark should ride to (1,4): line=%d rune=%d exists=%v", line, runePos, exists)
	}
	if _, _, _, _, exists := w.Buffer.GetBlockRange(); exists {
		t.Fatal("yank must not resurrect the block markers")
	}
}

// Mixed directions in one accumulation: forward deletes append, backspaces
// prepend, all in the same entry while the edit stays put.
func TestKillRingMixedDirections(t *testing.T) {
	e, w := newTestEditor(t, "abcdef\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})

	e.executeCommand("del_char_next")  // kills "d"        entry: "d"
	e.executeCommand("del_char_prior") // prepends "c"     entry: "cd"
	e.executeCommand("del_char_next")  // appends "e"      entry: "cde"
	if got := docContent(w); got != "abf" {
		t.Fatalf("buffer after mixed deletes: %q", got)
	}
	if len(e.killRing) != 1 {
		t.Fatalf("mixed-direction run should stay one entry, ring has %d", len(e.killRing))
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 2})
	e.executeCommand("kill_ring_yank")
	if got := docContent(w); got != "abcdef" {
		t.Fatalf("yank should reconstruct the original: %q", got)
	}
}
