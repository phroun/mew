package trinkets

import (
	"strings"

	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// The OS menu bar, mirroring ours.
//
// On a platform with a menu bar of its own (macOS), the menus the desktop
// draws are also put there, for the window that has the focus: the desktop's
// bar for a window on the desktop, and a torn-off application's own bar --
// its main window's -- for a window of its on a surface of its own. The bar
// the desktop draws stays where it is.
//
// **A native item does what ours does.** Choosing it, or pressing the key it
// shows, triggers the very MenuItem our menu would have, so nothing new is
// invoked anywhere. The key is the one our menu advertises, worked out the
// same way at the same moment; when the OS asks whether the item may run and
// that key no longer means the item here -- the focus has moved to something
// that takes the keyboard on its own terms -- the answer is no, and the key
// goes on to the focus as it would have without a native menu.
//
// The desktop's own menu, Ψ, becomes the OS's application menu -- the one the
// OS titles with the process's name -- since what it holds is about the whole
// desktop, as that menu is about the whole process. It is there whichever
// window has the focus, and is not repeated in the bar after it. While the
// HOST application has the focus (see SetHostApplication) -- the one the
// process is named for, so the one the OS's title already names -- its own
// leading menu is folded into the application menu as well, rather than
// standing after it under the same name. The OS's
// defaults there go (the desktop gives the OS the whole bar), so every key in
// the bar acts through the desktop's own items and its own keymap.
//
// Items are rebuilt from our menus whenever the OS asks (about to show a menu,
// or looking through the bar for a key), running each menu's about-to-show
// first as our own bar does, and reading the bar as it is at that moment: a
// native menu stands for a POSITION in the focused window's bar, not for a
// Menu object, since the desktop builds its bar afresh on every change. So
// nothing needs telling when an item changes, and only the titles in the bar
// are pushed, when they change.

// nativeBarEntry is one menu of the focused window's bar as the OS menu bar
// shows it: the menu, and the title it goes under there.
type nativeBarEntry struct {
	menu  *Menu
	title string
}

// syncNativeMenuBar puts the bar for the focused window into the OS menu bar,
// when there is one and its titles have changed since the last time.
func (d *Desktop) syncNativeMenuBar() {
	d.mu.RLock()
	host := d.nativeMenuHost
	d.mu.RUnlock()
	if host == nil {
		return
	}
	bar, _ := d.nativeBar()
	titles := make([]string, len(bar))
	for i, e := range bar {
		titles[i] = e.title
	}
	sig := strings.Join(titles, "\x00")
	d.mu.Lock()
	if sig == d.nativeMenuSig {
		// The same titles, or still none: nothing to push, since the native
		// menus read the bar as it is whenever they are asked.
		d.mu.Unlock()
		return
	}
	d.nativeMenuSig = sig
	d.mu.Unlock()

	out := make([]platform.NativeMenu, 0, len(bar))
	for i, title := range titles {
		out = append(out, d.nativeBarMenu(i, title))
	}
	host.SetNativeMenus(d.nativeAppMenu(), out)
}

// nativeAppMenu is the OS's application menu, read each time the OS asks: the
// desktop's own menu, with the host application's leading menu folded in
// while that application has the focus.
func (d *Desktop) nativeAppMenu() platform.NativeMenu {
	return platform.NativeMenu{
		Title: "Ψ",
		Items: func() []platform.NativeMenuItem {
			d.mu.RLock()
			sys := d.systemMenu
			d.mu.RUnlock()
			_, host := d.nativeBar()
			return mergedAppMenuItems(sys, host)
		},
	}
}

// mergedAppMenuItems is the desktop's own menu with host -- the host
// application's leading menu, or nil -- folded into it: the desktop's groups
// but its last, then the host's items, then the desktop's last group (Exit
// Desktop) joining the host's last (its Quit), so the two ways of leaving
// stand together at the end. An item of the host's that is the desktop's own,
// already above (Narration, Connections), is left out, and a group it leaves
// empty goes with it -- so a host menu with nothing of its own changes nothing.
func mergedAppMenuItems(sys, host *Menu) []platform.NativeMenuItem {
	if host == nil {
		return nativeItems(sys)
	}
	if refresh := sys.OnAboutToShow(); refresh != nil {
		refresh()
	}
	if refresh := host.OnAboutToShow(); refresh != nil {
		refresh()
	}
	own := map[string]bool{}
	for _, it := range sys.Items() {
		if id := it.WellKnownID(); id != "" {
			own[id] = true
		}
	}
	var hostItems []*MenuItem
	for _, it := range host.Items() {
		if id := it.WellKnownID(); id != "" && own[id] {
			continue
		}
		hostItems = append(hostItems, it)
	}

	// The desktop's last group starts after its last separator -- or is the
	// whole of it, when it has none.
	sysItems := sys.Items()
	last := -1
	for i := len(sysItems) - 1; i >= 0; i-- {
		if sysItems[i].Separator {
			last = i
			break
		}
	}
	var merged []*MenuItem
	merged = append(merged, sysItems[:max(last, 0)]...)
	merged = append(merged, NewSeparator())
	merged = append(merged, hostItems...)
	merged = append(merged, sysItems[last+1:]...)
	return nativeItemsOf(tidySeparators(merged))
}

// tidySeparators drops a separator at either end or next to another, which
// is what leaving items out of a menu can leave behind.
func tidySeparators(items []*MenuItem) []*MenuItem {
	var out []*MenuItem
	for _, it := range items {
		if it.Separator && (len(out) == 0 || out[len(out)-1].Separator) {
			continue
		}
		out = append(out, it)
	}
	for len(out) > 0 && out[len(out)-1].Separator {
		out = out[:len(out)-1]
	}
	return out
}

// nativeBarMenu is the menu at position i of the focused window's bar, read
// each time the OS asks. If the bar no longer has that title there -- it has
// changed and the new set is on its way -- the menu reads as empty.
func (d *Desktop) nativeBarMenu(i int, title string) platform.NativeMenu {
	return platform.NativeMenu{
		Title: title,
		Items: func() []platform.NativeMenuItem {
			bar, _ := d.nativeBar()
			if i >= len(bar) || bar[i].title != title {
				return nil
			}
			return nativeItems(bar[i].menu)
		},
	}
}

// nativeBar is the bar the focused window reads its menus from, as the OS
// menu bar shows it after the application menu: when a torn window has the
// focus, its application's main window's own bar -- only a torn main window
// carries one -- else the desktop's, less the desktop's own menu. A torn bar's
// first menu goes under the application's name rather than its menu name
// (the "≡" that reads well in a bar of ours and not beside the OS's own).
//
// When the bar is the host application's, its leading menu is not in the bar
// at all: it is handed back as host, to be folded into the application menu.
func (d *Desktop) nativeBar() (bar []nativeBarEntry, host *Menu) {
	d.mu.RLock()
	torn := d.tornFocusOwner
	desk := d.menuBar
	sys := d.systemMenu
	active := d.activeApp
	hostApp := d.hostApp
	d.mu.RUnlock()

	var menus []*Menu
	var app ApplicationProvider
	if torn != nil {
		if a := d.findApplicationForWindow(torn); a != nil {
			if main := a.MainWindow(); main != nil {
				if mb := windowMenuBarOf(main); mb != nil {
					menus, app = mb.Menus(), a
				}
			}
		}
	}
	if app == nil {
		if desk == nil {
			return nil, nil
		}
		for _, m := range desk.Menus() {
			if m != sys {
				menus = append(menus, m)
			}
		}
		app = active
	}

	for i, m := range menus {
		if i == 0 && app != nil && app == hostApp && m.WellKnownID() == MenuIDApp {
			host = m
			continue
		}
		title := m.Title()
		if i == 0 && app != nil && title == app.MenuName() {
			title = app.Name()
		}
		bar = append(bar, nativeBarEntry{menu: m, title: title})
	}
	return bar, host
}

func windowMenuBarOf(w *window.Window) *MenuBar {
	mb, _ := w.WindowMenuBar().(*MenuBar)
	return mb
}

// nativeItems is m's items as they stand now, after its about-to-show has had
// the chance to bring them up to date.
func nativeItems(m *Menu) []platform.NativeMenuItem {
	if refresh := m.OnAboutToShow(); refresh != nil {
		refresh()
	}
	return nativeItemsOf(m.Items())
}

// nativeItemsOf is items as native items.
func nativeItemsOf(items []*MenuItem) []platform.NativeMenuItem {
	var out []platform.NativeMenuItem
	for _, it := range items {
		switch {
		case it.Separator:
			out = append(out, platform.NativeMenuItem{Separator: true})
		case it.SubMenu != nil:
			sub := platform.NativeMenu{
				Title: it.SubMenu.Title(),
				Items: func(s *Menu) func() []platform.NativeMenuItem {
					return func() []platform.NativeMenuItem { return nativeItems(s) }
				}(it.SubMenu),
			}
			out = append(out, platform.NativeMenuItem{Title: it.Text, Submenu: &sub})
		default:
			it := it
			key := nativeKey(it)
			out = append(out, platform.NativeMenuItem{
				Title:   it.Text,
				Checked: it.Checkable && it.Checked,
				Key:     key,
				Action:  it.Trigger,
				Enabled: func() bool { return it.Enabled && nativeKey(it) == key },
			})
		}
	}
	return out
}

// nativeKey is the key an item advertises, in KittyTK's own spelling: the one
// its command resolves to where the focus is, or the shortcut it holds. Its
// literal ShortcutText is for keys the toolkit does not handle, and is not a
// key the OS could be told.
func nativeKey(it *MenuItem) string {
	if it.Command != "" {
		return it.resolveCommandKey()
	}
	return string(it.Shortcut)
}
