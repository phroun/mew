package plugins

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// The modebar nav-history buttons render only as wide as needed and collapse
// when there is no history: "[<]" for back only, "[<][>]" for both, and
// "   [>]" (a 3-space placeholder holding the back slot) for forward only. The
// recorded column ranges hit-test the right button, and the placeholder is
// inert.
func TestModebarNavRenderAndHitTest(t *testing.T) {
	keep := func(*buffer.Buffer) bool { return false } // nothing held outside
	wm := window.NewManager()
	id := wm.CreateWindow(window.WindowOptions{
		Type: window.DocWindow, Dock: window.DockNone, Visible: true, SetFocus: true,
		Buffer: buffer.NewFromString("A\n"),
	})
	w := wm.GetWindow(id)
	m := NewModebar(wm)
	plain := func(string) string { return "" } // strip colors: assert the glyphs
	const start = 5

	// No history: collapses to nothing, no button hits.
	if got := m.renderNav(plain, "", w, start); got != "" {
		t.Fatalf("no history: got %q, want empty", got)
	}
	if m.NavButtonAtColumn(start) != ModebarNavNone {
		t.Fatal("no button should hit with no history")
	}

	// Back only.
	w.SwapBuffer(buffer.NewFromString("B\n"), keep) // A -> B: back=[A]
	if got := m.renderNav(plain, "", w, start); got != "[<]" {
		t.Fatalf("back-only: got %q, want [<]", got)
	}
	if m.NavButtonAtColumn(start) != ModebarNavBack || m.NavButtonAtColumn(start+2) != ModebarNavBack {
		t.Fatal("back button range wrong")
	}
	if m.NavButtonAtColumn(start+3) != ModebarNavNone {
		t.Fatal("no forward button expected in back-only")
	}

	// Back and forward.
	w.SwapBuffer(buffer.NewFromString("C\n"), keep) // B -> C: back=[A,B]
	w.NavHistoryPrior()                             // back to B: back=[A], fwd=[C]
	if got := m.renderNav(plain, "", w, start); got != "[<][>]" {
		t.Fatalf("back+fwd: got %q, want [<][>]", got)
	}
	if m.NavButtonAtColumn(start) != ModebarNavBack ||
		m.NavButtonAtColumn(start+3) != ModebarNavFwd ||
		m.NavButtonAtColumn(start+5) != ModebarNavFwd {
		t.Fatal("back+fwd ranges wrong")
	}

	// Forward only: the 3-space placeholder keeps [>] in the same column.
	w.NavHistoryPrior() // back to A: back=[], fwd=[C,B]
	if got := m.renderNav(plain, "", w, start); got != "   [>]" {
		t.Fatalf("fwd-only: got %q, want 3 spaces + [>]", got)
	}
	if m.NavButtonAtColumn(start) != ModebarNavNone || m.NavButtonAtColumn(start+2) != ModebarNavNone {
		t.Fatal("the back placeholder must be inert")
	}
	if m.NavButtonAtColumn(start+3) != ModebarNavFwd {
		t.Fatal("forward button expected at the placeholder+3")
	}
}

// The brackets paint in the button-SHADOW color and the glyph in the button
// color; the captured-and-pressed button uses the pressed variants.
func TestModebarNavButtonColors(t *testing.T) {
	keep := func(*buffer.Buffer) bool { return false }
	wm := window.NewManager()
	id := wm.CreateWindow(window.WindowOptions{
		Type: window.DocWindow, Dock: window.DockNone, Visible: true, SetFocus: true,
		Buffer: buffer.NewFromString("A\n"),
	})
	w := wm.GetWindow(id)
	w.SwapBuffer(buffer.NewFromString("B\n"), keep) // back history
	m := NewModebar(wm)
	tag := func(name string) string { return "<" + name + ">" } // sentinel colors

	// Normal state: brackets = buttonShadow, glyph = button.
	if got := m.renderNav(tag, "", w, 0); got != "<buttonShadow>[<button><<buttonShadow>]" {
		t.Fatalf("normal colors: got %q", got)
	}

	// Pressed state (back captured + pointer over it).
	m.SetNavStateFunc(func() (int, bool, int) { return ModebarNavBack, true, 0 })
	if got := m.renderNav(tag, "", w, 0); got != "<buttonShadowPressed>[<buttonPressed><<buttonShadowPressed>]" {
		t.Fatalf("pressed colors: got %q", got)
	}
}
