package viewport

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// Doc and tool viewports are peers now: both focus-eligible; only the modebar
// (chrome) and prompts are excluded. Per-set last-focused tracking records
// the last focused viewport within each ViewportSet.
func TestFocusEligibilityAndViewportSets(t *testing.T) {
	m := NewManager()
	doc := m.CreateViewport(ViewportOptions{
		Type: DocViewport, Dock: DockNone, Buffer: buffer.NewFromString("d\n"),
		Visible: true, SetFocus: true,
	})
	help := m.CreateViewport(ViewportOptions{
		Type: ToolViewport, ViewportSet: "help", Dock: DockTop,
		Buffer: buffer.NewFromString("h\n"), Visible: true,
	})
	bar := m.CreateViewport(ViewportOptions{
		Type: ToolViewport, ViewportSet: ViewportSetModebar, Dock: DockBottom,
		Buffer: buffer.NewFromString("m\n"), Visible: true,
	})

	dw, hw, bw := m.GetViewport(doc), m.GetViewport(help), m.GetViewport(bar)
	if !dw.FocusEligible() || !hw.FocusEligible() {
		t.Fatal("doc and tool(help) viewports must be focus-eligible")
	}
	if bw.FocusEligible() {
		t.Fatal("the modebar (chrome) must not be focus-eligible")
	}

	// A tool viewport can now take focus (the old work-buffer block is gone).
	if !m.SetFocus(help) {
		t.Fatal("a tool viewport should be focusable")
	}
	if m.GetFocusedViewport() != hw {
		t.Fatal("focus should move to the help viewport")
	}
	// The modebar cannot.
	if m.SetFocus(bar) {
		t.Fatal("the modebar must not accept focus")
	}
	if m.GetFocusedViewport() != hw {
		t.Fatal("a rejected focus must not move focus")
	}

	// Per-set memory: focusing the help viewport recorded it for "help";
	// focusing the doc records it for the "" set. Each set is independent.
	if m.LastFocusedInSet("help") != hw {
		t.Fatal("help set should remember the help viewport")
	}
	m.SetFocus(doc)
	if m.LastFocusedInSet("") != dw {
		t.Fatal("document set should remember the doc viewport")
	}
	if m.LastFocusedInSet("help") != hw {
		t.Fatal("the help set's memory is independent of the doc set")
	}

	// Removing a set's remembered viewport clears that entry.
	m.RemoveViewport(help)
	if m.LastFocusedInSet("help") != nil {
		t.Fatal("removing the help viewport should clear the help set's memory")
	}
}
