package trinkets

import (
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// The menu on the host's icon in the Dock.
//
// **One icon stands for every application**, because on macOS one process
// has one Dock icon and every application this display hosts lives in it. So
// the menu is where they are told apart: each application by name, its
// windows listed beneath it, one click from coming forward, and the name
// opening what can be done to the application as a whole. Then the desktop
// itself, shown or hidden.
//
//	KittyTK Demo          >  Show KittyTK Demo / Hide KittyTK Demo / Quit KittyTK Demo
//	    ✓ KittyTK Demo
//	      Ask for a Name
//	mew                   >
//	    ◆ notes.txt
//	-----
//	Hide Desktop
//
// The Dock adds its own items below these (Options, Show All Windows, Hide,
// Quit), which stand for the whole host.
//
// The menu is built each time the Dock asks for it, so it lists what is there
// now. Every action runs on the platform thread after the menu has closed.

// dockMenu is the platform.DockMenuHost builder.
func (d *Desktop) dockMenu() []platform.DockMenuItem {
	var items []platform.DockMenuItem
	for _, app := range d.Applications() {
		wins := d.dockWindows(app)
		if len(wins) == 0 {
			// Nothing of it on screen, so nothing to bring forward or hide;
			// the Window menus leave such an application out as well.
			continue
		}
		items = append(items, platform.DockMenuItem{
			Title:   app.Name(),
			Submenu: d.dockAppMenu(app, wins),
		})
		for _, w := range wins {
			w := w
			mark := platform.DockMarkNone
			switch {
			case d.dockWindowMinimized(w):
				mark = platform.DockMarkMinimized
			case d.isCurrentTopLevel(w):
				mark = platform.DockMarkCheck
			}
			items = append(items, platform.DockMenuItem{
				Title:  dockWindowTitle(w),
				Indent: 1,
				Mark:   mark,
				Action: func() { d.activateWindowFromMenu(w) },
			})
		}
	}
	if len(items) > 0 {
		items = append(items, platform.DockMenuItem{Separator: true})
	}
	return append(items, d.dockDesktopItem())
}

// dockWindows is app's windows as the Dock menu lists them: every one shown,
// minimized ones included -- minimizing, on the desktop or on a surface of
// its own, leaves a window visible. A window hidden is not somewhere a person
// can be sent, and closing one hides it.
func (d *Desktop) dockWindows(app ApplicationProvider) []*window.Window {
	var out []*window.Window
	for _, w := range app.Windows() {
		if w != nil && w.IsVisible() {
			out = append(out, w)
		}
	}
	return out
}

// dockWindowMinimized reports whether w is minimized: on its own surface, by
// the OS, if it is torn off; in the desktop's dock otherwise.
func (d *Desktop) dockWindowMinimized(w *window.Window) bool {
	if h := d.hostForWindow(w); h != nil {
		if n, ok := h.Surface().(platform.NativeSurface); ok && n.Minimized() {
			return true
		}
	}
	return w.IsMinimized()
}

// dockWindowTitle is how a window is named in the menu. One with no title of
// its own is still somewhere to go, so it is named as untitled rather than
// shown as a blank line.
func dockWindowTitle(w *window.Window) string {
	if t := w.Title(); t != "" {
		return t
	}
	return "Untitled"
}

// dockAppMenu is what can be done to one application as a whole: bring all of
// it forward, put all of it away, or end it.
func (d *Desktop) dockAppMenu(app ApplicationProvider, wins []*window.Window) []platform.DockMenuItem {
	name := app.Name()
	items := []platform.DockMenuItem{{
		Title:  "Show " + name,
		Action: func() { d.showApplication(app) },
	}}
	for _, w := range wins {
		if !d.dockWindowMinimized(w) {
			// Only while something of it is out to put away.
			items = append(items, platform.DockMenuItem{
				Title:  "Hide " + name,
				Action: func() { d.hideApplication(app) },
			})
			break
		}
	}
	return append(items,
		platform.DockMenuItem{Separator: true},
		platform.DockMenuItem{
			Title: "Quit " + name,
			// Through the application's own quit, so a window with unsaved
			// work is asked about exactly as the Quit item would ask.
			Action: func() { d.quitApplication(app) },
		})
}

// showApplication brings every window of app back from minimized and app
// forward: its main window if it has one showing, else the first of the
// others.
func (d *Desktop) showApplication(app ApplicationProvider) {
	wins := app.Windows()
	for _, w := range wins {
		d.showAppWindow(w)
	}
	target := app.MainWindow()
	if target == nil || target.IsClosed() {
		target = nil
		for _, w := range wins {
			if w != nil && !w.IsClosed() {
				target = w
				break
			}
		}
	}
	if target != nil {
		d.activateWindowFromMenu(target)
	}
}

// hideApplication minimizes every window of app, as Hide on its own menu does.
func (d *Desktop) hideApplication(app ApplicationProvider) {
	for _, w := range app.Windows() {
		d.hideAppWindow(w)
	}
}

// dockDesktopItem shows the desktop when it is hidden and hides it when it is
// showing -- the same as `set host desktop` and `set host !desktop`. Hiding
// hands the display to an application, so with none to hand it to the item is
// there but dimmed.
func (d *Desktop) dockDesktopItem() platform.DockMenuItem {
	if d.IsSolo() {
		return platform.DockMenuItem{Title: "Show Desktop", Action: d.ExitSoloMode}
	}
	return platform.DockMenuItem{
		Title:    "Hide Desktop",
		Disabled: !d.canHideDesktop(),
		Action:   d.EnterSoloFromDesktop,
	}
}

// canHideDesktop reports whether EnterSoloFromDesktop has a window to promote:
// a torn window, or failing that a docked one.
func (d *Desktop) canHideDesktop() bool {
	d.mu.RLock()
	hosts := append([]*window.TearOffHost(nil), d.tornHosts...)
	wm := d.windowManager
	d.mu.RUnlock()
	if pickPromotable(hosts) != nil {
		return true
	}
	return wm != nil && pickDockedMain(wm.Windows()) != nil
}
