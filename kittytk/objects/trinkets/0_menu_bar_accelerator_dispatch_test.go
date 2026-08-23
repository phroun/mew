package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// An accelerator must survive being recomputed. The bar publishes its formed
// chords into the key context and then asks that same context who has CLAIMED
// each chord, to decide which accelerators are live -- so unless last time's
// are cleared first, the bar reads its own assignment as a clash and mutes
// every accelerator it had, from the second refresh onward.
func TestAcceleratorsSurviveRepeatedRefresh(t *testing.T) {
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&File"))
	mb.AddMenu(NewMenu("&Help"))
	mb.SetAcceleratorChord("M-*")
	mb.SetKeyContext(core.DefaultKeyRegistry().BuildStateContext(core.StateNormal))

	for pass := 1; pass <= 3; pass++ {
		mb.InvalidateAccelerators()
		mb.refreshAccelerators()
		for i, a := range mb.accelAssignments {
			if !a.Active || a.Char == 0 {
				t.Fatalf("pass %d: menu %d accelerator %q went dead (active=%v)",
					pass, i, a.Char, a.Active)
			}
		}
		if !mb.ActivateAcceleratorSequence("M-h") {
			t.Fatalf("pass %d: M-h no longer reaches the Help menu", pass)
		}
		mb.CloseMenu()
	}
}

// A chord something ELSE has claimed is still not the accelerator's to take:
// clearing the bar's own entries must not blind it to a real clash.
func TestRealClashStillMutesAnAccelerator(t *testing.T) {
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&Help"))
	mb.SetAcceleratorChord("M-*")
	// A registry where M-h means something the situation offers.
	r := core.NewKeyRegistryFromMap("clash", map[string][]string{
		"M-h": {core.CmdAppMinimize},
	})
	mb.SetKeyContext(r.BuildContext([]string{core.CmdAppMinimize}))

	mb.refreshAccelerators()
	if mb.accelAssignments[0].Active {
		t.Error("M-h is claimed by a binding; the accelerator must yield to it")
	}
	if mb.ActivateAcceleratorSequence("M-h") {
		t.Error("a muted accelerator must not answer")
	}
}

// The two ways a key actually arrives: through the window manager, which
// resolves the desktop's context above any window, and through the desktop
// itself when nothing above it took the key. Both must open the menu.
func TestAcceleratorOpensMenuThroughBothDispatchPaths(t *testing.T) {
	for _, c := range []struct {
		name string
		key  string
		send func(d *Desktop, wm *window.WindowManager, ev core.KeyPressEvent) bool
	}{
		{"desktop", "M-h", func(d *Desktop, _ *window.WindowManager, ev core.KeyPressEvent) bool {
			return d.HandleKeyPress(ev)
		}},
		{"window manager", "M-f", func(_ *Desktop, wm *window.WindowManager, ev core.KeyPressEvent) bool {
			return wm.HandleKeyPress(ev)
		}},
	} {
		d := NewDesktop()
		d.windowManager = window.NewWindowManager()
		d.windowManager.SetDesktop(d)
		mb := NewMenuBar()
		mb.AddMenu(NewMenu("&File"))
		mb.AddMenu(NewMenu("&Help"))
		d.SetMenuBar(mb)

		if !c.send(d, d.windowManager, core.KeyPressEvent{Key: c.key}) {
			t.Errorf("%s path: %s was not handled", c.name, c.key)
		}
		if mb.ActiveMenu() == nil {
			t.Errorf("%s path: %s did not open a menu", c.name, c.key)
		}
	}
}

// A muted accelerator has lost its CHORD, not its letter. The two are
// different affordances: the chord reaches the menu while the bar is not
// focused, and the bare letter navigates once it is. So a menu that had to
// yield its chord to the keymap is still one keystroke away from a focused
// bar, and never becomes unreachable from the keyboard.
func TestAMutedAcceleratorKeepsItsBareLetter(t *testing.T) {
	mb := NewMenuBar()
	mb.AddMenu(NewMenu("&Help"))
	mb.SetAcceleratorChord("M-*")
	r := core.NewKeyRegistryFromMap("clash", map[string][]string{
		"M-h": {core.CmdAppMinimize},
	})
	mb.SetKeyContext(r.BuildContext([]string{core.CmdAppMinimize}))

	mb.refreshAccelerators()
	if mb.accelAssignments[0].Active {
		t.Fatal("M-h is spoken for; the chord must yield")
	}
	if got := mb.accelAssignments[0].Char; got != 'h' {
		t.Errorf("the muted menu carries %q, want h - the letter is not the chord", got)
	}

	// A focused bar answers to the bare letter, which is ordinary typing and
	// clashes with no keymap.
	mb.SetFocus()
	mb.setAcceleratorsActive(true)
	if !mb.HandleKeyPress(core.KeyPressEvent{Key: "h"}) {
		t.Fatal("the focused bar did not answer to the bare letter")
	}
	if mb.ActiveMenu() == nil {
		t.Error("bare h did not open the Help menu")
	}
}
