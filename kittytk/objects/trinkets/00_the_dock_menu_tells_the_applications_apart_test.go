package trinkets

// On macOS every application this display hosts shares the one Dock icon, so
// the icon's menu is where they are told apart: each by name, its windows
// beneath it (the current one ticked, a minimized one marked), the name
// opening Show, Hide and Quit for the application, and last the desktop
// itself, shown or hidden.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// spellDock writes a menu out one line per item: indentation, a mark, the
// title, and "(off)" for a disabled item.
func spellDock(items []platform.DockMenuItem) []string {
	var out []string
	for _, it := range items {
		if it.Separator {
			out = append(out, "-----")
			continue
		}
		line := strings.Repeat("  ", it.Indent)
		switch it.Mark {
		case platform.DockMarkCheck:
			line += "✓ "
		case platform.DockMarkMinimized:
			line += "◆ "
		}
		line += it.Title
		if it.Disabled {
			line += " (off)"
		}
		if len(it.Submenu) > 0 {
			line += " >"
		}
		out = append(out, line)
	}
	return out
}

func findDock(t *testing.T, items []platform.DockMenuItem, title string) platform.DockMenuItem {
	t.Helper()
	for _, it := range items {
		if it.Title == title {
			return it
		}
	}
	t.Fatalf("no %q in the menu: %q", title, spellDock(items))
	return platform.DockMenuItem{}
}

type dockRig struct {
	*focusRig
	demo, mew  *mockApp
	demoMain   *window.Window
	ask, notes *window.Window
	notesSurf  *raisingSurface
}

// newDockRig is a desktop holding two applications -- Demo with two docked
// windows, mew with one torn window minimized on its own surface -- and a
// third with nothing on screen.
func newDockRig(t *testing.T) *dockRig {
	t.Helper()
	r := &dockRig{focusRig: newFocusRig(t)}
	r.demoMain = r.docked("KittyTK Demo")
	r.ask = r.docked("Ask for a Name")
	r.d.windowManager.ActivateWindow(r.demoMain)
	r.demo = &mockApp{name: "Demo", main: r.demoMain, windows: []*window.Window{r.demoMain, r.ask}}

	r.notes = r.torn("notes.txt")
	r.notesSurf = r.d.tornHostForWindow(r.notes).Surface().(*raisingSurface)
	r.notesSurf.minimized = true
	r.mew = &mockApp{name: "mew", main: r.notes, windows: []*window.Window{r.notes}}

	r.d.AddApplication(r.demo)
	r.d.AddApplication(r.mew)
	r.d.AddApplication(&mockApp{name: "Ghost"})
	r.d.windowManager.ActivateWindow(r.demoMain)
	return r
}

func TestTheDockMenuListsEachApplicationAndItsWindows(t *testing.T) {
	r := newDockRig(t)
	got := strings.Join(spellDock(r.d.dockMenu()), "\n")
	want := strings.Join([]string{
		"Demo >",
		"  ✓ KittyTK Demo",
		"  Ask for a Name",
		"mew >",
		"  ◆ notes.txt",
		"-----",
		"Hide Desktop",
	}, "\n")
	if got != want {
		t.Errorf("the Dock menu reads\n%s\nwant\n%s", got, want)
	}
}

// An application's own menu offers Hide only while something of it is out.
func TestAnApplicationsDockMenuShowsHidesAndQuits(t *testing.T) {
	r := newDockRig(t)
	items := r.d.dockMenu()
	demo := strings.Join(spellDock(findDock(t, items, "Demo").Submenu), "\n")
	if want := "Show Demo\nHide Demo\n-----\nQuit Demo"; demo != want {
		t.Errorf("Demo's menu reads\n%s\nwant\n%s", demo, want)
	}
	mew := strings.Join(spellDock(findDock(t, items, "mew").Submenu), "\n")
	if want := "Show mew\n-----\nQuit mew"; mew != want {
		t.Errorf("mew's menu, with everything minimized, reads\n%s\nwant\n%s", mew, want)
	}
}

func TestAWindowsDockItemBringsItForward(t *testing.T) {
	r := newDockRig(t)
	findDock(t, r.d.dockMenu(), "Ask for a Name").Action()
	if got := r.d.windowManager.ActiveWindow(); got != r.ask {
		t.Errorf("choosing Ask for a Name activated %v", got)
	}
	findDock(t, r.d.dockMenu(), "notes.txt").Action()
	if r.notesSurf.minimized {
		t.Error("choosing a minimized torn window left it minimized")
	}
	if r.lastRaised() != "notes.txt" {
		t.Errorf("choosing notes.txt raised %q", r.lastRaised())
	}
}

func TestHideShowAndQuitActOnTheWholeApplication(t *testing.T) {
	r := newDockRig(t)
	demo := findDock(t, r.d.dockMenu(), "Demo").Submenu

	findDock(t, demo, "Hide Demo").Action()
	if !r.demoMain.IsMinimized() || !r.ask.IsMinimized() {
		t.Fatalf("Hide Demo left main minimized %v, ask minimized %v", r.demoMain.IsMinimized(), r.ask.IsMinimized())
	}
	findDock(t, demo, "Show Demo").Action()
	if r.demoMain.IsMinimized() || r.ask.IsMinimized() {
		t.Errorf("Show Demo left main minimized %v, ask minimized %v", r.demoMain.IsMinimized(), r.ask.IsMinimized())
	}
	if got := r.d.windowManager.ActiveWindow(); got != r.demoMain {
		t.Errorf("Show Demo activated %v, want its main window", got)
	}

	findDock(t, demo, "Quit Demo").Action()
	for _, a := range r.d.Applications() {
		if a == ApplicationProvider(r.demo) {
			t.Fatal("Quit Demo left Demo on the desktop")
		}
	}
	if !r.demoMain.IsClosed() || !r.ask.IsClosed() {
		t.Error("Quit Demo left a window of it open")
	}
}

// Show falls back to the first window when the main one is gone.
func TestShowingAnApplicationWithoutItsMainWindow(t *testing.T) {
	r := newDockRig(t)
	r.demoMain.Close()
	r.demo.windows = []*window.Window{r.demoMain, r.ask}
	r.d.hideApplication(r.demo)
	r.d.showApplication(r.demo)
	if got := r.d.windowManager.ActiveWindow(); got != r.ask {
		t.Errorf("Show with the main window closed activated %v, want Ask for a Name", got)
	}
}

// The desktop's own line: Show while hidden, Hide while showing, and Hide
// dimmed when there is no application to hand the display to.
func TestTheDockMenuShowsOrHidesTheDesktop(t *testing.T) {
	r := newDockRig(t)
	if it := r.d.dockDesktopItem(); it.Title != "Hide Desktop" || it.Disabled {
		t.Errorf("with windows to promote, the desktop's line is %q disabled=%v", it.Title, it.Disabled)
	}

	bare := newFocusRig(t)
	if it := bare.d.dockDesktopItem(); it.Title != "Hide Desktop" || !it.Disabled {
		t.Errorf("with nothing to promote, the desktop's line is %q disabled=%v", it.Title, it.Disabled)
	}
	if got := spellDock(bare.d.dockMenu()); len(got) != 1 || got[0] != "Hide Desktop (off)" {
		t.Errorf("with no applications, the menu reads %q", got)
	}

	docked := newFocusRig(t)
	docked.docked("only")
	if it := docked.d.dockDesktopItem(); it.Disabled {
		t.Error("a docked window alone was not enough to hide the desktop for")
	}
	torn := newFocusRig(t)
	torn.torn("only")
	if it := torn.d.dockDesktopItem(); it.Disabled {
		t.Error("a torn window alone was not enough to hide the desktop for")
	}

	r.d.mu.Lock()
	r.d.solo = true
	r.d.mu.Unlock()
	if it := r.d.dockDesktopItem(); it.Title != "Show Desktop" || it.Disabled {
		t.Errorf("with the desktop hidden, its line is %q disabled=%v", it.Title, it.Disabled)
	}
}

// A window with no title is still somewhere to go; a hidden one is not, nor
// one closed while minimized.
func TestUntitledAndHiddenWindows(t *testing.T) {
	r := newFocusRig(t)
	blank := r.docked("")
	gone := r.docked("gone")
	gone.Hide()
	shelved := r.docked("shelved")
	r.d.windowManager.MinimizeWindow(shelved)
	closed := r.docked("closed")
	r.d.windowManager.MinimizeWindow(closed)
	closed.Close()
	r.d.AddApplication(&mockApp{name: "App", main: blank, windows: []*window.Window{blank, gone, shelved, closed}})
	got := strings.Join(spellDock(r.d.dockMenu()), "\n")
	want := "App >\n  Untitled\n  ◆ shelved\n-----\nHide Desktop"
	if got != want {
		t.Errorf("the menu reads\n%s\nwant\n%s", got, want)
	}
}

// dockPlatform is a platform with a Dock.
type dockPlatform struct {
	msPlatform
	build func() []platform.DockMenuItem
}

func (p *dockPlatform) SetDockMenu(build func() []platform.DockMenuItem) { p.build = build }

// Run hands the desktop this platform, Dock and all, rather than the one it
// wraps.
func (p *dockPlatform) Run(init func(platform.Platform)) int {
	return p.msPlatform.Run(func(platform.Platform) { init(p) })
}

// A desktop running on a platform with a Dock hands it the menu.
func TestTheDesktopHandsTheDockItsMenu(t *testing.T) {
	d, _ := desktopWithApps(t, "Ledger")
	plat := &dockPlatform{}
	plat.script = func() {
		if plat.build == nil {
			t.Error("the desktop gave the Dock no menu")
		} else if got := spellDock(plat.build()); len(got) == 0 || got[0] != "Ledger >" {
			t.Errorf("the Dock's menu reads %q, want Ledger first", got)
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}
