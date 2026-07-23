package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// findModebarNavButton scans the rendered modebar row for the screen column of
// a nav button (ModebarNavBack / ModebarNavFwd), or ok=false.
func (e *Editor) findModebarNavButton(want int) (x, y int, ok bool) {
	mw := e.WindowManager.GetWindow(e.Modebar.WindowID())
	if mw == nil {
		return 0, 0, false
	}
	y = mw.ContentY + 1
	for x := 1; x <= 80; x++ {
		if b, hit := e.modebarNavHit(x, y); hit && b == want {
			return x, y, true
		}
	}
	return 0, 0, false
}

// A press-then-release on the modebar's [<] button runs nav_history_prior on
// the focused window (returning to the prior buffer), like a proper button:
// the release must land back on the button to activate.
func TestModebarNavButtonActivates(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t, "A\n", "[options]\n")
	// Build a back history on the focused document: A -> B.
	doc.SwapBuffer(buffer.NewFromString("B\n"), func(*buffer.Buffer) bool { return false })
	if prior, _ := doc.NavHistoryDepths(); prior != 1 {
		t.Fatalf("setup: want back depth 1, got %d", prior)
	}

	e.createPluginWindows() // the modebar window (normally made by run())
	e.performRender()       // records the modebar's row + button column ranges

	// Only a back button should exist (no forward history yet).
	if _, _, ok := e.findModebarNavButton(2 /*Fwd*/); ok {
		t.Fatal("no forward button should be present")
	}
	bx, by, ok := e.findModebarNavButton(1 /*Back*/)
	if !ok {
		t.Fatal("the [<] back button should render with back history")
	}

	// Press captures; release OFF the button abandons (no navigation).
	if !e.modebarNavPressAt(bx, by) {
		t.Fatal("press on the back button should be consumed")
	}
	e.modebarNavRelease(bx-1, by) // release one column to the left (off the button)
	if strings.TrimSpace(doc.Buffer.GetContent()) != "B" {
		t.Fatalf("release off the button must not navigate; content=%q", doc.Buffer.GetContent())
	}

	// Press then release ON the button navigates back to A.
	if !e.modebarNavPressAt(bx, by) {
		t.Fatal("second press should be consumed")
	}
	e.modebarNavRelease(bx, by)
	if strings.TrimSpace(doc.Buffer.GetContent()) != "A" {
		t.Fatalf("release on [<] should nav_history_prior to A; content=%q", doc.Buffer.GetContent())
	}

	// Now on A with a forward history: a [>] button appears and returns to B.
	e.performRender()
	fx, fy, ok := e.findModebarNavButton(2 /*Fwd*/)
	if !ok {
		t.Fatal("a [>] forward button should render with forward history")
	}
	e.modebarNavPressAt(fx, fy)
	e.modebarNavRelease(fx, fy)
	if strings.TrimSpace(doc.Buffer.GetContent()) != "B" {
		t.Fatalf("release on [>] should nav_history_next to B; content=%q", doc.Buffer.GetContent())
	}
}

// Clicking a modebar nav button does not steal focus from the document window.
func TestModebarNavKeepsFocus(t *testing.T) {
	e, doc, _ := renderedEditorWithConfig(t, "A\n", "[options]\n")
	doc.SwapBuffer(buffer.NewFromString("B\n"), func(*buffer.Buffer) bool { return false })
	e.createPluginWindows()
	e.performRender()
	bx, by, ok := e.findModebarNavButton(1)
	if !ok {
		t.Fatal("expected a back button")
	}
	e.modebarNavPressAt(bx, by)
	e.modebarNavRelease(bx, by)
	if f := e.WindowManager.GetFocusedWindow(); f != doc {
		t.Fatalf("focus should stay on the document window, got %v", f)
	}
}
