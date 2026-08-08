package editor

import (
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// liveEditor wires an editor on a live-capture session and returns the sink the
// host would relay purfecterm's structural events to. The buffer starts from
// content, with the launch caret at (line, 0).
func liveEditor(t *testing.T, content string, line int, format captureFormat) (*Editor, *viewport.Viewport, CaptureSink) {
	t.Helper()
	e, w := newTestEditor(t, content)
	e.Config.PTYProvider = func(PTYRequest) (PTYSession, error) { return newStubPTY(), nil }
	var sink CaptureSink
	e.Config.TerminalSurfaces = TerminalHooks{
		Open:           func(string, int, int) {},
		Feed:           func(string, []byte) []byte { return nil },
		Place:          func([]TerminalSurface) {},
		Close:          func(string) {},
		SetCaptureSink: func(_ string, s CaptureSink) { sink = s },
	}
	w.Caret.Seek(line, 0)
	if !e.execRequestArgsPolicy("bash", nil, "", ptySizePolicy{}, captureLive, format) {
		t.Fatal("exec failed")
	}
	if sink == nil {
		t.Fatal("live rung did not register a capture sink")
	}
	return e, w, sink
}

// A row of text, a newline, and a second row mirror straight down the buffer:
// the newline steps to the next line rather than pushing anything down.
func TestLiveStreamsRows(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "hello", "")
	sink.LiveNewline(0, 1)
	sink.LiveWrite(0, 1, "world", "")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "hello\nworld"; got != want {
		t.Fatalf("buffer = %q, want %q", got, want)
	}
}

// An absolute cursor move followed by a write overtypes in place: the cells
// under the caret are replaced, not inserted.
func TestLiveOvertypeInPlace(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "XXXX", "")
	sink.LiveCursorMove(1, 0)
	sink.LiveWrite(1, 0, "ab", "")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "XabX"; got != want {
		t.Fatalf("buffer = %q, want %q (overtype, not insert)", got, want)
	}
}

// A newline into an already-materialized row lands on it without inserting a
// blank line — the "skip past EOL" move — so a later overtype replaces that
// row's content.
func TestLiveNewlineSkipsEOL(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "AA", "")
	sink.LiveNewline(0, 1)
	sink.LiveWrite(0, 1, "BB", "")
	sink.LiveCursorMove(0, 0) // back up to row 0
	sink.LiveNewline(0, 1)    // must land on the existing row 1, inserting nothing
	sink.LiveWrite(0, 1, "XX", "")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "AA\nXX"; got != want {
		t.Fatalf("buffer = %q, want %q (newline must not push a line down)", got, want)
	}
}

// A short line is padded with spaces so a cursor move to a column past its end
// has a cell to land on.
func TestLivePadsToColumn(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "ab", "")
	sink.LiveCursorMove(5, 0) // three cells past end of "ab"
	sink.LiveWrite(5, 0, "Z", "")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "ab   Z"; got != want {
		t.Fatalf("buffer = %q, want %q (pad short line with spaces)", got, want)
	}
}

// One colour held across a write run and a newline emits exactly one SGR at the
// start: the pen carried into each row suppresses a redundant repeat.
func TestLiveMonochromeSingleSGR(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "ab", "\x1b[0;31m")
	sink.LiveNewline(0, 1)
	sink.LiveWrite(0, 1, "cd", "\x1b[0;31m") // same pen: no new SGR
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "\x1b[0;31mab\ncd"; got != want {
		t.Fatalf("buffer = %q, want %q (one SGR, carried across the newline)", got, want)
	}
}

// A pen change within a row emits one SGR at the change and nothing before it.
func TestLiveColorChange(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "a", "")
	sink.LiveWrite(1, 0, "b", "\x1b[0;31m")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "a\x1b[0;31mb"; got != want {
		t.Fatalf("buffer = %q, want %q (SGR only at the change)", got, want)
	}
}

// The plain format drops colour entirely: the geometry is mirrored, the SGR is
// not carried into the document.
func TestLivePlainDropsColor(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, capturePlain)
	sink.LiveWrite(0, 0, "ab", "\x1b[0;31m")
	sink.LiveWrite(2, 0, "cd", "\x1b[0;32m")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "abcd"; got != want {
		t.Fatalf("buffer = %q, want %q (plain keeps no SGR)", got, want)
	}
}

// Jumping back into differently-coloured content and overtyping keeps the
// trailing cells their own colour: the run gets a leading pen and a restore SGR
// at its tail, both in one op.
func TestLiveJumpRecolorRestoresTrailing(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	// A red row of six cells.
	sink.LiveWrite(0, 0, "aaabbb", "\x1b[0;31m")
	// Jump back to column 0 and overtype the first three in green.
	sink.LiveCursorMove(0, 0)
	sink.LiveWrite(0, 0, "XXX", "\x1b[0;32m")
	e.ptyEnded(w.Buffer, nil)
	// green XXX, then red restored for the trailing bbb.
	if got, want := w.Buffer.GetContent(), "\x1b[0;32mXXX\x1b[0;31mbbb"; got != want {
		t.Fatalf("buffer = %q, want %q (trailing colour restored)", got, want)
	}
}

// Landing on an existing SGR and correcting it replaces that SGR rather than
// stacking a second one beside it.
func TestLiveJumpReplacesSGRNoStacking(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "ab", "\x1b[0;31m") // red "ab"
	// Jump to column 0 (which sits right after the red SGR) and rewrite it green.
	sink.LiveCursorMove(0, 0)
	sink.LiveWrite(0, 0, "a", "\x1b[0;32m")
	e.ptyEnded(w.Buffer, nil)
	// The green SGR replaces the red one; the trailing "b" is restored to red.
	if got, want := w.Buffer.GetContent(), "\x1b[0;32ma\x1b[0;31mb"; got != want {
		t.Fatalf("buffer = %q, want %q (replace SGR, no back-to-back stack)", got, want)
	}
}

// Overtyping into trailing content with the SAME pen needs no correction at all.
func TestLiveOvertypeSamePenNoSGR(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "aaabbb", "\x1b[0;31m")
	sink.LiveCursorMove(0, 0)
	sink.LiveWrite(0, 0, "XXX", "\x1b[0;31m") // same red: just overtype
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "\x1b[0;31mXXXbbb"; got != want {
		t.Fatalf("buffer = %q, want %q (same pen: no extra SGR)", got, want)
	}
}

// A scroll preserves every prior line — the departed row stays above the window
// as history, nothing is deleted — and writing continues below it.
func TestLiveScrollPreservesHistory(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "L0", "")
	sink.LiveNewline(0, 1)
	sink.LiveWrite(0, 1, "L1", "")
	// The screen scrolls: row 0 departs the top; purfecterm then lands the
	// newline at the bottom row and the next write fills it.
	sink.LiveScrollLineOff(1)
	sink.LiveNewline(0, 1)
	sink.LiveWrite(0, 1, "L2", "")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "L0\nL1\nL2"; got != want {
		t.Fatalf("buffer = %q, want %q (history preserved, nothing deleted)", got, want)
	}
}

// A clear preserves the screen as history and starts a fresh region below it.
func TestLiveClearKeepsHistory(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	sink.LiveWrite(0, 0, "old", "")
	sink.LiveClearScreen()
	sink.LiveWrite(0, 0, "new", "")
	e.ptyEnded(w.Buffer, nil)
	if got, want := w.Buffer.GetContent(), "old\nnew"; got != want {
		t.Fatalf("buffer = %q, want %q (clear keeps the old screen above)", got, want)
	}
}

// A stream of frontier writes and newlines folds in as adjacent, same-kind
// garland mutations, so with coalescing on it collapses to a SINGLE undo step —
// the write caret is never perturbed by the mirror's peeking. One Undo reverts
// the whole stream.
func TestLiveWritesCoalesce(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	w.Buffer.SetUndoCoalescing(true, 0) // 0 autoBake: no time-based break in a tight test
	for i := 0; i < 4; i++ {
		sink.LiveWrite(0, i, "row", "")
		if i < 3 {
			sink.LiveNewline(0, i+1)
		}
	}
	if got := w.Buffer.GetContent(); got != "row\nrow\nrow\nrow" {
		t.Fatalf("buffer = %q before undo, want four rows", got)
	}
	if !w.Buffer.Undo() {
		t.Fatal("Undo returned false")
	}
	if got := w.Buffer.GetContent(); got != "" {
		t.Fatalf("after one Undo buffer = %q, want empty (stream should be one coalesced step)", got)
	}
	e.ptyEnded(w.Buffer, nil)
}

// Content folded in by the live mirror marks the buffer modified — captured
// output is a real edit, not a free ride.
func TestLiveMarksModified(t *testing.T) {
	e, w, sink := liveEditor(t, "", 0, captureFull)
	if w.Buffer.IsModified() {
		t.Fatal("buffer modified before any capture")
	}
	sink.LiveWrite(0, 0, "x", "")
	if !w.Buffer.IsModified() {
		t.Fatal("live write did not mark the buffer modified")
	}
	e.ptyEnded(w.Buffer, nil)
}
