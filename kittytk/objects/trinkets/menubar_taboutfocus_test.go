package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// The desktop's bar hands BOTH Tab and Shift+Tab to the dock - it is the only
// other chrome out there, so direction doesn't change the destination.
func TestMenuBarTabAndShiftTabReachDock(t *testing.T) {
	for _, key := range []string{"Tab", "S-Tab"} {
		mb := NewMenuBar()
		mb.AddMenu(NewMenu("&File"))
		docked := 0
		mb.SetOnFocusDock(func() { docked++ })

		if !mb.HandleKeyPress(core.KeyPressEvent{Key: key}) {
			t.Errorf("%s: not handled", key)
		}
		if docked != 1 {
			t.Errorf("%s: dock handoff ran %d times, want 1", key, docked)
		}
	}
}

// A window's own bar (a detached, solo, or torn-off window's chrome) has no
// dock, so it steps into that window's chain instead - back to the title bar,
// forward into the content.
func TestMenuBarTabOutDirection(t *testing.T) {
	cases := []struct {
		key         string
		wantForward bool
	}{
		{"Tab", true},
		{"S-Tab", false},
	}
	for _, c := range cases {
		mb := NewMenuBar()
		mb.AddMenu(NewMenu("&File"))
		var got []bool
		mb.SetOnFocusOut(func(forward bool) bool {
			got = append(got, forward)
			return true
		})

		if !mb.HandleKeyPress(core.KeyPressEvent{Key: c.key}) {
			t.Errorf("%s: not handled", c.key)
		}
		if len(got) != 1 || got[0] != c.wantForward {
			t.Errorf("%s: focus-out %v, want [%v]", c.key, got, c.wantForward)
		}
	}
}

// The composed key string is what decides the direction. A backend that also
// sets the Shift bit alongside "S-Tab" changes nothing - the bitfield is
// derived from the string, never read instead of it.
func TestMenuBarShiftTabIsBackward(t *testing.T) {
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&File"))
	var got []bool
	mb.SetOnFocusOut(func(forward bool) bool { got = append(got, forward); return true })

	mb.HandleKeyPress(core.KeyPressEvent{Key: "S-Tab", Modifiers: core.ShiftModifier})
	if len(got) != 1 || got[0] {
		t.Errorf("S-Tab focus-out %v, want [false]", got)
	}
}

// A focus-out that reports no move leaves the key unhandled, so it falls
// through rather than being swallowed by the bar.
func TestMenuBarTabOutUnhandledFallsThrough(t *testing.T) {
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&File"))
	mb.SetOnFocusOut(func(bool) bool { return false })

	if mb.HandleKeyPress(core.KeyPressEvent{Key: "Tab"}) {
		t.Error("Tab was swallowed even though focus did not move")
	}
}

// With a dropdown open, Tab belongs to the menu, not to leaving the bar.
func TestMenuBarTabStaysInsideOpenMenu(t *testing.T) {
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&File"))
	out := 0
	mb.SetOnFocusOut(func(bool) bool { out++; return true })
	mb.OpenMenu(0)

	mb.HandleKeyPress(core.KeyPressEvent{Key: "Tab"})
	if out != 0 {
		t.Errorf("Tab left the bar while a dropdown was open (%d handoffs)", out)
	}
}

// SetWindowMenuBar wires the window's chain to its own bar, so Shift+Tab off
// the bar lands on the title bar instead of falling through to the content
// trinket (which, for a full-screen one like the mew editor, swallowed it).
func TestWindowMenuBarShiftTabReachesTitleBar(t *testing.T) {
	win := window.NewWindow("solo")
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&File"))
	win.SetWindowMenuBar(mb)

	if !mb.HandleKeyPress(core.KeyPressEvent{Key: "S-Tab"}) {
		t.Fatal("Shift+Tab off the window's own menu bar was not handled")
	}
	if win.TitleFocus() == window.TitleFocusNone {
		t.Error("Shift+Tab did not move focus into the title bar")
	}
}
