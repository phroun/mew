package editor

import (
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// addNegotiableTopWindows creates two top-dock windows: A wants 8 rows
// (min 3), B wants 6 (min 2).
func addNegotiableTopWindows(e *Editor) {
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "A", Type: window.ToolWindow, Dock: window.DockTop,
		Priority: 90, MinHeight: 3, MaxHeight: 8, Height: 8,
		Buffer: buffer.NewFromString("A\n"),
	})
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "B", Type: window.ToolWindow, Dock: window.DockTop,
		Priority: 50, MinHeight: 2, MaxHeight: 6, Height: 6,
		Buffer: buffer.NewFromString("B\n"),
	})
}

func layoutHeights(l window.Layout) map[string]int {
	h := map[string]int{}
	for _, group := range [][]window.WindowLayout{l.TopLayout, l.BottomLayout} {
		for _, wl := range group {
			key := wl.Window.ID
			if wl.Window.Class == "modebar" {
				key = "modebar"
			}
			h[key] = wl.Height
		}
	}
	return h
}

// --- Modebar location ---

func TestModebarTopDefault(t *testing.T) {
	e, _ := newTestEditor(t, "hello\n")
	e.createPluginWindows()
	l := e.LayoutManager.CalculateLayout(80, 24)
	mb := layoutByClass(l, "modebar")
	if mb == nil || mb.Y != 0 {
		t.Fatalf("default modebar should be the top line: %+v", mb)
	}
}

func TestModebarBottomOwnsLastLine(t *testing.T) {
	e, _ := newTestEditor(t, "hello\n", "modebarLocation=bottom")
	e.createPluginWindows()
	l := e.LayoutManager.CalculateLayout(80, 24)
	mb := layoutByClass(l, "modebar")
	if mb == nil || mb.Y != 23 || mb.Height != 1 {
		t.Fatalf("bottom modebar should own the last line: %+v", mb)
	}

	// A prompt stacks ABOVE the bottom modebar, which keeps the last line.
	e.PromptMgr.PromptForInput("Q: ", "", func(bool, string, string) {}, "")
	l = e.LayoutManager.CalculateLayout(80, 24)
	if mb = layoutByClass(l, "modebar"); mb.Y != 23 {
		t.Fatalf("modebar must keep the last line under a prompt, Y=%d", mb.Y)
	}
	promptY := -1
	for _, wl := range l.BottomLayout {
		if wl.Window.Type == window.PromptWindow {
			promptY = wl.Y
		}
	}
	if promptY != 22 {
		t.Fatalf("prompt should sit just above the modebar, Y=%d", promptY)
	}
}

func TestModebarBottomNotificationStacks(t *testing.T) {
	e, _ := newTestEditor(t, "hello\n", "modebarLocation=bottom")
	e.createPluginWindows()
	e.ShowNotification("hi there")
	l := e.LayoutManager.CalculateLayout(80, 24)
	mb := layoutByClass(l, "modebar")
	if mb == nil || mb.Y != 23 {
		t.Fatalf("modebar must keep the last line under a notification: %+v", mb)
	}
	nt := layoutByClass(l, "notification")
	if nt == nil || nt.Y >= 23 {
		t.Fatalf("notification should sit above the modebar: %+v", nt)
	}
}

func TestModebarSetOptionRelocates(t *testing.T) {
	e, _ := newTestEditor(t, "hello\n")
	e.createPluginWindows()
	e.PawScript.ExecuteAsync(`set_option modebarLocation, bottom`)
	if e.Config.ModebarLocation != "bottom" {
		t.Fatalf("config not updated: %q", e.Config.ModebarLocation)
	}
	l := e.LayoutManager.CalculateLayout(80, 24)
	if mb := layoutByClass(l, "modebar"); mb == nil || mb.Y != 23 {
		t.Fatalf("live relocation to bottom failed: %+v", mb)
	}
	e.PawScript.ExecuteAsync(`set_option modebarLocation, top`)
	l = e.LayoutManager.CalculateLayout(80, 24)
	if mb := layoutByClass(l, "modebar"); mb == nil || mb.Y != 0 {
		t.Fatalf("live relocation back to top failed: %+v", mb)
	}
	// Invalid values are rejected.
	e.PawScript.ExecuteAsync(`set_option modebarLocation, sideways`)
	if e.Config.ModebarLocation != "top" {
		t.Fatalf("invalid value should be rejected: %q", e.Config.ModebarLocation)
	}
}

// --- Space negotiation ---

// Negotiation must treat the other windows identically regardless of where
// the modebar is located: essential status belongs to the modebar itself,
// not to a dock position.
func TestNegotiationLocationParity(t *testing.T) {
	results := map[string]map[string]int{}
	for _, loc := range []string{"top", "bottom"} {
		e, _ := newTestEditor(t, "hello\n", "modebarLocation="+loc)
		e.createPluginWindows()
		addNegotiableTopWindows(e)
		// 18 rows: top dock wants 8+6(+1 modebar)=15, main needs 6, so
		// shrinking must kick in.
		l := e.LayoutManager.CalculateLayout(80, 18)
		results[loc] = layoutHeights(l)
		if l.MainHeight < 6 {
			t.Fatalf("%s: main area got %d rows, needs >= 6", loc, l.MainHeight)
		}
		if _, ok := results[loc]["modebar"]; !ok {
			t.Fatalf("%s: modebar missing from layout", loc)
		}
	}
	for _, id := range []string{"A", "B", "modebar"} {
		if results["top"][id] != results["bottom"][id] {
			t.Errorf("window %s: height %d with modebar top, %d with modebar bottom",
				id, results["top"][id], results["bottom"][id])
		}
	}
	// The highest-priority negotiable window shrinks first: A gives up 3
	// rows (8->5), B keeps its desired 6.
	if results["bottom"]["A"] != 5 || results["bottom"]["B"] != 6 {
		t.Errorf("expected A=5 B=6, got A=%d B=%d", results["bottom"]["A"], results["bottom"]["B"])
	}
}

// The modebar and the active prompt survive severe squeezes at either
// location, with a bottom modebar still holding the last line.
func TestNegotiationModebarSurvivesSqueeze(t *testing.T) {
	for _, loc := range []string{"top", "bottom"} {
		e, _ := newTestEditor(t, "hello\n", "modebarLocation="+loc)
		e.createPluginWindows()
		addNegotiableTopWindows(e)
		e.PromptMgr.PromptForInput("Q: ", "", func(bool, string, string) {}, "")
		l := e.LayoutManager.CalculateLayout(80, 10)
		h := layoutHeights(l)
		if h["modebar"] != 1 {
			t.Fatalf("%s: modebar should survive at 1 row, heights: %v", loc, h)
		}
		promptSeen := false
		for _, wl := range l.BottomLayout {
			if wl.Window.Type == window.PromptWindow {
				promptSeen = true
			}
		}
		if !promptSeen {
			t.Fatalf("%s: active prompt must survive, heights: %v", loc, h)
		}
		if loc == "bottom" {
			for _, wl := range l.BottomLayout {
				if wl.Window.Class == "modebar" && wl.Y != 9 {
					t.Fatalf("bottom modebar should hold the last line, Y=%d", wl.Y)
				}
			}
		}
	}
}

// A background window creation (the verbose log) must not disturb which
// window owns the painted main area.
func TestBackgroundWindowDoesNotStealMainArea(t *testing.T) {
	e, w := newTestEditor(t, "hello\n")
	e.appendVerboseLog("line")
	l := e.LayoutManager.CalculateLayout(80, 24)
	main := layoutByID(l, w.ID)
	if main == nil {
		t.Fatal("doc window should still be laid out in the main area")
	}
	if lg := layoutByClass(l, "verboseLog"); lg != nil {
		t.Fatal("unfocused verbose log should not be painted in the main area")
	}
}
