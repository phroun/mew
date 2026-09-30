//go:build sdl

package sdl

import (
	"sync"

	"github.com/phroun/kittytk/platform"
)

// The Dock menu's portable half. The desktop hands over a builder
// (platform.DockMenuHost); each time the Dock is about to show the menu, the
// builder is asked for the items, they are written into a native menu, and
// every item that does something is given a tag naming its action. Choosing
// the item comes back here with that tag, and the action is posted to run
// once the menu has closed.
//
// Only macOS has a Dock, so only dockmenu_darwin.go writes a real menu. The
// writing is here, against a sink, so what goes into the menu can be tested
// on any platform.

var dockMenu struct {
	mu      sync.Mutex
	build   func() []platform.DockMenuItem
	post    func(func())
	actions []func() // tag n runs actions[n-1]; tag 0 runs nothing
}

// SetDockMenu implements platform.DockMenuHost.
func (p *Platform) SetDockMenu(build func() []platform.DockMenuItem) {
	dockMenu.mu.Lock()
	dockMenu.build = build
	dockMenu.post = p.Post
	dockMenu.mu.Unlock()
	// Once the main window exists the application is up and has a delegate
	// to answer the Dock; before that, Run installs it when it gets there.
	if p.main != nil {
		installDockMenu()
	}
}

// dockMenuSink is what a menu is written into: AppKit on macOS, a recorder in
// the tests. Menus and items are handles the sink hands out.
type dockMenuSink interface {
	newMenu() uintptr
	addItem(menu uintptr, title string, tag int, enabled bool, mark platform.DockMark, indent int) uintptr
	addSeparator(menu uintptr)
	setSubmenu(item, submenu uintptr)
}

// writeDockMenu writes items into menu, appending each action to actions and
// giving its item the tag that names it, and returns the grown list.
//
// An item is enabled when it does something -- runs an action or opens a
// submenu -- and is not marked disabled. One that does neither is a label,
// and is shown dimmed as a label is.
func writeDockMenu(s dockMenuSink, menu uintptr, items []platform.DockMenuItem, actions []func()) []func() {
	for _, it := range items {
		if it.Separator {
			s.addSeparator(menu)
			continue
		}
		tag := 0
		if it.Action != nil && !it.Disabled {
			actions = append(actions, it.Action)
			tag = len(actions)
		}
		enabled := !it.Disabled && (it.Action != nil || len(it.Submenu) > 0)
		item := s.addItem(menu, it.Title, tag, enabled, it.Mark, it.Indent)
		if len(it.Submenu) > 0 {
			sub := s.newMenu()
			actions = writeDockMenu(s, sub, it.Submenu, actions)
			s.setSubmenu(item, sub)
		}
	}
	return actions
}

// buildDockMenu asks the desktop for the menu and writes it into s, replacing
// the actions the last menu's tags named. It returns 0 -- no menu of ours --
// when nothing has been set.
func buildDockMenu(s dockMenuSink) uintptr {
	dockMenu.mu.Lock()
	build := dockMenu.build
	dockMenu.mu.Unlock()
	if build == nil {
		return 0
	}
	items := build()
	menu := s.newMenu()
	actions := writeDockMenu(s, menu, items, nil)
	dockMenu.mu.Lock()
	dockMenu.actions = actions
	dockMenu.mu.Unlock()
	return menu
}

// dockItemChosen posts the action the chosen item's tag names. A tag from a
// menu since replaced, or one naming nothing, does nothing.
func dockItemChosen(tag int) {
	dockMenu.mu.Lock()
	var fn func()
	if tag > 0 && tag <= len(dockMenu.actions) {
		fn = dockMenu.actions[tag-1]
	}
	post := dockMenu.post
	dockMenu.mu.Unlock()
	if fn == nil || post == nil {
		return
	}
	post(fn)
}
