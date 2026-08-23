package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// guestTrinket is a trinket that takes the keyboard on its own terms, the way
// an embedded terminal or a hosted editor does.
type guestTrinket struct {
	core.TrinketBase
	core.TrinketKeys
}

func newGuestTrinket() *guestTrinket {
	g := &guestTrinket{}
	g.TrinketBase = *core.NewTrinketBase()
	g.SetFocusPolicy(core.StrongFocus)
	g.Init(g)
	return g
}

func (g *guestTrinket) Paint(*core.Painter) {}

// Which keymap is in force is a property of where the FOCUS is. A window whose
// content took the keyboard resolves ITS OWN frame commands through that
// keymap too, so what the guest did not share is not spent above its head.
func TestWindowFollowsTheFocusedRegistry(t *testing.T) {
	win := window.NewWindow("Host")
	guest := newGuestTrinket()
	win.SetContent(guest)

	// Before anything is declared, the window resolves against the default.
	if got := win.FocusedKeyRegistry(); got != core.DefaultKeyRegistry() {
		t.Fatalf("registry = %q, want the default", got.Name())
	}
	if cmd := win.KeyCommand("^W"); cmd != core.CmdWindowClose {
		t.Fatalf("^W resolved to %q, want %s", cmd, core.CmdWindowClose)
	}

	// The guest takes the keyboard, sharing nothing.
	captured := core.NewKeyRegistry("captured", nil)
	guest.SetKeyRegistry(captured)
	win.FocusManager().SetFocusedTrinket(guest)

	if got := win.FocusedKeyRegistry(); got != captured {
		t.Fatalf("registry = %q, want captured", got.Name())
	}
	if cmd := win.KeyCommand("^W"); cmd != "" {
		t.Errorf("^W resolved to %q; the guest has the keyboard, so it is the guest's key", cmd)
	}
	if ctx := win.KeyContext(); ctx != nil && ctx.Resolve("^W") != "" {
		t.Error("the window's context still offers a command the captured keymap does not bind")
	}
}

// ...and it comes back. Nothing has to announce the change: the context is
// compared against the keymap in force whenever it is used.
func TestWindowContextReturnsWhenFocusLeavesTheGuest(t *testing.T) {
	win := window.NewWindow("Host")
	guest := newGuestTrinket()
	win.SetContent(guest)
	guest.SetKeyRegistry(core.NewKeyRegistry("captured", nil))
	win.FocusManager().SetFocusedTrinket(guest)

	if cmd := win.KeyCommand("^W"); cmd != "" {
		t.Fatalf("^W resolved to %q while the guest held focus", cmd)
	}

	win.FocusManager().SetFocusedTrinket(nil)

	if cmd := win.KeyCommand("^W"); cmd != core.CmdWindowClose {
		t.Errorf("^W resolved to %q after focus left the guest, want %s", cmd, core.CmdWindowClose)
	}
}

// The desktop asks the same question one level further out: its focus is
// inside whichever window is active, and its own commands stand down with
// everything else while a guest holds the keyboard.
func TestDesktopFollowsTheFocusedRegistry(t *testing.T) {
	d := newRunnableDesktop(t)
	win := window.NewWindow("Host")
	guest := newGuestTrinket()
	win.SetContent(guest)

	plat := &msPlatform{}
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(win)
		d.WindowManager().ActivateWindow(win)

		if got := d.FocusedKeyRegistry(); got != core.DefaultKeyRegistry() {
			t.Fatalf("registry = %q, want the default", got.Name())
		}

		guest.SetKeyRegistry(core.NewKeyRegistry("captured", nil))
		win.FocusManager().SetFocusedTrinket(guest)

		if got := d.FocusedKeyRegistry(); got.Name() != "captured" {
			t.Errorf("registry = %q, want captured - a guest holds the keyboard", got.Name())
		}
		if ctx := d.KeyContext(); ctx != nil && ctx.Resolve("^Q") != "" {
			t.Error("the desktop still offers app_quit while the guest has the keyboard")
		}
		d.QuitWithCode(0)
	})
	d.RunOn(plat)
}
