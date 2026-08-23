package tui

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
)

// The present writes its diff by addressing cells all over the screen, and a
// terminal renders the stream as it parses it — a visible hardware cursor
// would skate along with the repaint. EndFrame brackets a non-empty diff: hide
// first, paint, address the cursor, and only then show it again.
func TestEndFrameHidesCursorAcrossRepaint(t *testing.T) {
	b, out := newTestTUI(20, 2)
	s := style.DefaultStyle()

	// Frame 1: the caret is shown for the first time. The host asks from
	// inside its Frame, so the reveal must wait for the present.
	b.BeginFrame()
	b.DrawText(0, 0, "hello", s, nil)
	b.SetCursorPosition(core.Unit(cellX(b, 3)), 0)
	b.SetCursorVisible(true)
	if strings.Contains(out.String(), "\033[?25h") {
		t.Fatal("SetCursorVisible(true) must not reveal the cursor before it is placed")
	}
	b.EndFrame()

	frame := out.String()
	show := strings.Index(frame, "\033[?25h")
	if show < 0 {
		t.Fatal("the present must reveal the cursor at the end")
	}
	if text := strings.Index(frame, "hello"); text < 0 || text > show {
		t.Fatalf("the repaint must land BEFORE the reveal: %q", frame)
	}
	// It was hidden going in (the initial state), so no redundant hide.
	if strings.Contains(frame[:show], "\033[?25l") {
		t.Errorf("no hide is needed when the cursor is already hidden: %q", frame)
	}

	// Frame 2: the cursor IS showing now, so the next repaint must bracket.
	out.Reset()
	b.BeginFrame()
	b.DrawText(0, b.metrics.CellToUnitsY(1), "second line", s, nil)
	b.SetCursorPosition(core.Unit(cellX(b, 5)), b.metrics.CellToUnitsY(1))
	b.SetCursorVisible(true)
	b.EndFrame()

	frame = out.String()
	hide := strings.Index(frame, "\033[?25l")
	text := strings.Index(frame, "second") // the diff may split the run at unchanged cells
	show = strings.LastIndex(frame, "\033[?25h")
	if hide < 0 || text < 0 || show < 0 {
		t.Fatalf("expected hide, repaint and show in one present: %q", frame)
	}
	if !(hide < text && text < show) {
		t.Errorf("order must be hide -> repaint -> show; got hide=%d text=%d show=%d in %q",
			hide, text, show, frame)
	}
}

// An idle present writes no cells, so it must not blink the cursor off and on
// — that would flicker on every frame the host repaints for other reasons.
func TestEndFrameQuietWhenNothingChanged(t *testing.T) {
	b, out := newTestTUI(20, 2)
	s := style.DefaultStyle()

	b.BeginFrame()
	b.DrawText(0, 0, "hello", s, nil)
	b.SetCursorPosition(core.Unit(cellX(b, 3)), 0)
	b.SetCursorVisible(true)
	b.EndFrame()

	out.Reset()
	b.BeginFrame()
	b.DrawText(0, 0, "hello", s, nil) // identical content
	b.SetCursorPosition(core.Unit(cellX(b, 3)), 0)
	b.SetCursorVisible(true)
	b.EndFrame()

	frame := out.String()
	if strings.Contains(frame, "\033[?25l") {
		t.Errorf("an unchanged present must not hide the cursor: %q", frame)
	}
	if strings.Contains(frame, "\033[?25h") {
		t.Errorf("the cursor is already shown; no redundant reveal: %q", frame)
	}
}

// Hiding is immediate: a host that drops the caret mid-frame (focus lost, a
// modal taking over) should not keep showing it until the next present.
func TestSetCursorVisibleHidesImmediately(t *testing.T) {
	b, out := newTestTUI(20, 2)
	s := style.DefaultStyle()

	b.BeginFrame()
	b.DrawText(0, 0, "hello", s, nil)
	b.SetCursorVisible(true)
	b.EndFrame()

	out.Reset()
	b.SetCursorVisible(false)
	if out.String() != "\033[?25l" {
		t.Errorf("hiding should write exactly the hide, got %q", out.String())
	}
	// And a second hide is a no-op on the wire.
	out.Reset()
	b.SetCursorVisible(false)
	if out.String() != "" {
		t.Errorf("an already-hidden cursor should stay off the wire, got %q", out.String())
	}
}
