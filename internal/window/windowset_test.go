package window

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// Doc and tool windows are peers now: both focus-eligible; only the modebar
// (chrome) and prompts are excluded. Per-set last-focused tracking records
// the last focused window within each WindowSet.
func TestFocusEligibilityAndWindowSets(t *testing.T) {
	m := NewManager()
	doc := m.CreateWindow(WindowOptions{
		Type: DocWindow, Dock: DockNone, Buffer: buffer.NewFromString("d\n"),
		Visible: true, SetFocus: true,
	})
	help := m.CreateWindow(WindowOptions{
		Type: ToolWindow, WindowSet: "help", Dock: DockTop,
		Buffer: buffer.NewFromString("h\n"), Visible: true,
	})
	bar := m.CreateWindow(WindowOptions{
		Type: ToolWindow, WindowSet: WindowSetModebar, Dock: DockBottom,
		Buffer: buffer.NewFromString("m\n"), Visible: true,
	})

	dw, hw, bw := m.GetWindow(doc), m.GetWindow(help), m.GetWindow(bar)
	if !dw.FocusEligible() || !hw.FocusEligible() {
		t.Fatal("doc and tool(help) windows must be focus-eligible")
	}
	if bw.FocusEligible() {
		t.Fatal("the modebar (chrome) must not be focus-eligible")
	}

	// A tool window can now take focus (the old work-buffer block is gone).
	if !m.SetFocus(help) {
		t.Fatal("a tool window should be focusable")
	}
	if m.GetFocusedWindow() != hw {
		t.Fatal("focus should move to the help window")
	}
	// The modebar cannot.
	if m.SetFocus(bar) {
		t.Fatal("the modebar must not accept focus")
	}
	if m.GetFocusedWindow() != hw {
		t.Fatal("a rejected focus must not move focus")
	}

	// Per-set memory: focusing the help window recorded it for "help";
	// focusing the doc records it for the "" set. Each set is independent.
	if m.LastFocusedInSet("help") != hw {
		t.Fatal("help set should remember the help window")
	}
	m.SetFocus(doc)
	if m.LastFocusedInSet("") != dw {
		t.Fatal("document set should remember the doc window")
	}
	if m.LastFocusedInSet("help") != hw {
		t.Fatal("the help set's memory is independent of the doc set")
	}

	// Removing a set's remembered window clears that entry.
	m.RemoveWindow(help)
	if m.LastFocusedInSet("help") != nil {
		t.Fatal("removing the help window should clear the help set's memory")
	}
}
