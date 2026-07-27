package editor

import (
	"fmt"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/viewport"
)

// helpSceneEditor builds the two-viewport scene the unfocused-mouse rules are
// about: a focused main document viewport plus the docked help viewport open
// on a page (browse mode, link buttons), UNfocused. Returns the editor, the
// focused main, and the docked help viewport, with frame geometry stamped.
func helpSceneEditor(t *testing.T, pages map[string]string, open string) (*Editor, *viewport.Viewport, *viewport.Viewport) {
	t.Helper()
	e := helpTestEditor(t, pages)
	main := e.ViewportManager.GetFocusedViewport() // the main doc viewport
	if main == nil {
		t.Fatal("main doc viewport missing")
	}
	e.executeCommand(fmt.Sprintf("buffer_open_file %q", open))
	var hw *viewport.Viewport
	for _, w := range e.ViewportManager.GetViewportsByDock(viewport.DockTop) {
		if w.WikiName == "help" {
			hw = w
		}
	}
	if hw == nil {
		t.Fatalf("opening %s should create the docked help viewport", open)
	}
	// The 24-row test session has no modebar, so a full-height (Max 20) help
	// page loses the space negotiation outright. Cap it small so both
	// viewports are truly on screen — the situation these rules are about.
	hw.Height = 6
	hw.MaxHeight = 6
	e.ViewportManager.SetFocus(main.ID)
	e.performRender() // stamp the frame's geometry/epoch for both viewports
	if e.ViewportManager.GetFocusedViewport() != main {
		t.Fatal("precondition: the main viewport should be focused")
	}
	if !e.viewportOnScreen(hw) || !e.viewportOnScreen(main) {
		t.Fatal("precondition: both viewports should be on screen")
	}
	return e, main, hw
}

// helpButtonCell returns screen coordinates over the FIRST link button on the
// help page's first line (cell 5 sits inside "go [[x]] ..." style buttons).
func helpButtonCell(hw *viewport.Viewport) (x, y int) {
	return hw.ContentX + 1 + 5, hw.ContentY + 1
}

// Clicking a link button in a truly visible but UNFOCUSED viewport follows it
// — without stealing focus from the main viewport.
func TestUnfocusedClickFollowsLink(t *testing.T) {
	e, main, hw := helpSceneEditor(t, map[string]string{
		"help/start.txt": "go [[other]] now\n",
		"help/other.txt": "other content here\n",
	}, "help:/")
	if !hw.BrowseActive {
		t.Fatal("the help page should be in browse mode (buttons)")
	}
	before := hw.Buffer

	x, y := helpButtonCell(hw)
	e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", x, y))
	e.handleMouseKey("MouseLeftPress")
	if e.ViewportManager.GetFocusedViewport() != main {
		t.Fatal("pressing a link must not steal focus")
	}
	e.handleMouseKey("MouseLeftRelease")

	if e.ViewportManager.GetFocusedViewport() != main {
		t.Fatal("following a link must not steal focus")
	}
	if hw.Buffer == before {
		t.Fatal("the unfocused viewport should have followed the link")
	}
	if got := strings.TrimRight(hw.Buffer.GetLine(0), "\n"); !strings.Contains(got, "other content") {
		t.Fatalf("follow should land on the destination page; line0 %q", got)
	}
}

// The scroll wheel scrolls the truly visible viewport under the pointer, even
// when it is not focused — and focus stays put.
func TestUnfocusedWheelScrolls(t *testing.T) {
	long := strings.Repeat("filler line\n", 40)
	e, main, hw := helpSceneEditor(t, map[string]string{
		"help/start.txt": "top\n" + long,
	}, "help:/")

	x, y := hw.ContentX+2, hw.ContentY+1
	e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", x, y))
	e.handleMouseKey("MouseScrollDown")
	if hw.ViewState.ViewOffsetY != 3 {
		t.Fatalf("wheel over the unfocused viewport should scroll it; off=%d", hw.ViewState.ViewOffsetY)
	}
	if main.ViewState.ViewOffsetY != 0 {
		t.Fatal("the focused viewport must not scroll")
	}
	if e.ViewportManager.GetFocusedViewport() != main {
		t.Fatal("wheel scrolling must not move focus")
	}
	e.handleMouseKey("MouseScrollUp")
	if hw.ViewState.ViewOffsetY != 0 {
		t.Fatalf("wheel up should scroll back; off=%d", hw.ViewState.ViewOffsetY)
	}
}

// Clicking a NON-link spot in an unfocused, cycle-reachable viewport focuses
// it exactly as ^B N would — focus only, no caret placement from that click.
func TestUnfocusedNonLinkClickFocuses(t *testing.T) {
	e, main, hw := helpSceneEditor(t, map[string]string{
		"help/start.txt": "go [[other]] now\nplain second line\n",
		"help/other.txt": "other\n",
	}, "help:/")
	if !hw.CanFocus {
		t.Fatal("precondition: a regular help page is a cycle stop")
	}
	caretBefore := hw.CursorPos()

	// Click the plain second line (no link there).
	x, y := hw.ContentX+3, hw.ContentY+2
	e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", x, y))
	e.handleMouseKey("MouseLeftPress")
	e.handleMouseKey("MouseLeftRelease")

	if e.ViewportManager.GetFocusedViewport() != hw {
		t.Fatal("a non-link click should focus the viewport like ^B N")
	}
	if hw.CursorPos() != caretBefore {
		t.Fatal("the focusing click must not also place the caret")
	}
	_ = main
}

// A viewport the ^B N cycle skips (CanFocus=false — Quick Help) is not
// focusable by click either; the click is inert.
func TestUnfocusedClickSkipsNonCycleStops(t *testing.T) {
	e := helpTestEditor(t, map[string]string{"help/keys.txt": "=== K ===\nbody\n"})
	main := e.ViewportManager.GetFocusedViewport()
	e.KeyProcessor.MapKey("help", "keys")
	e.executeCommand("help_toggle") // Quick Help: CanFocus=false
	hw := e.helpViewport()
	if hw == nil || hw.CanFocus {
		t.Fatal("precondition: Quick Help should be open with CanFocus=false")
	}
	e.ViewportManager.SetFocus(main.ID)
	e.performRender()

	x, y := hw.ContentX+2, hw.ContentY+1
	e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", x, y))
	e.handleMouseKey("MouseLeftPress")
	e.handleMouseKey("MouseLeftRelease")
	if e.ViewportManager.GetFocusedViewport() != main {
		t.Fatal("clicking a non-cycle-stop viewport must not move focus")
	}
}

// With a modal prompt focused, none of the unfocused-viewport exceptions
// apply: clicks and wheel over other viewports stay inert.
func TestUnfocusedExceptionsBlockedByPrompt(t *testing.T) {
	long := strings.Repeat("filler line\n", 40)
	e, _, hw := helpSceneEditor(t, map[string]string{
		"help/start.txt": "go [[other]] now\n" + long,
		"help/other.txt": "other\n",
	}, "help:/")
	before := hw.Buffer

	e.PromptMgr.PromptForConfirmationTop("modal test", "Sure? [y/N]: ", false,
		func(accepted, yes bool) {})
	e.performRender()
	if !e.promptHasPriority() {
		t.Fatal("precondition: prompt should hold focus")
	}

	x, y := helpButtonCell(hw)
	e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", x, y))
	e.handleMouseKey("MouseLeftPress")
	e.handleMouseKey("MouseLeftRelease")
	if hw.Buffer != before {
		t.Fatal("a modal prompt must block the unfocused link follow")
	}
	e.handleMouseKey(fmt.Sprintf("Mouse@%d,%d", hw.ContentX+2, hw.ContentY+1))
	e.handleMouseKey("MouseScrollDown")
	if hw.ViewState.ViewOffsetY != 0 {
		t.Fatal("a modal prompt must block the unfocused wheel scroll")
	}
	if !e.promptHasPriority() {
		t.Fatal("the prompt must keep focus throughout")
	}
}
