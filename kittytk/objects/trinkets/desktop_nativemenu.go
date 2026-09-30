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
// Items are rebuilt from our menus whenever the OS asks (about to show a menu,
// or looking through the bar for a key), running each menu's about-to-show
// first as our own bar does, and reading the bar as it is at that moment: a
// native menu stands for a POSITION in the focused window's bar, not for a
// Menu object, since the desktop builds its bar afresh on every change. So
// nothing needs telling when an item changes, and only the titles in the bar
// are pushed, when they change.

// syncNativeMenuBar puts the bar for the focused window into the OS menu bar,
// when there is one and its titles have changed since the last time.
func (d *Desktop) syncNativeMenuBar() {
	d.mu.RLock()
	host := d.nativeMenuHost
	d.mu.RUnlock()
	if host == nil {
		return
	}
	menus := d.nativeBarMenus()
	titles := make([]string, len(menus))
	for i, m := range menus {
		titles[i] = m.Title()
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

	out := make([]platform.NativeMenu, 0, len(menus))
	for i, title := range titles {
		out = append(out, d.nativeBarMenu(i, title))
	}
	host.SetNativeMenus(out)
}

// nativeBarMenu is the menu at position i of the focused window's bar, read
// each time the OS asks. If the bar no longer has that title there -- it has
// changed and the new set is on its way -- the menu reads as empty.
func (d *Desktop) nativeBarMenu(i int, title string) platform.NativeMenu {
	return platform.NativeMenu{
		Title: title,
		Items: func() []platform.NativeMenuItem {
			menus := d.nativeBarMenus()
			if i >= len(menus) || menus[i].Title() != title {
				return nil
			}
			return nativeItems(menus[i])
		},
	}
}

// nativeBarMenus is the bar the focused window reads its menus from: when a
// torn window has the focus, its application's main window's own bar -- only
// a torn main window carries one -- else the desktop's.
func (d *Desktop) nativeBarMenus() []*Menu {
	d.mu.RLock()
	torn := d.tornFocusOwner
	bar := d.menuBar
	d.mu.RUnlock()
	if torn != nil {
		if app := d.findApplicationForWindow(torn); app != nil {
			if main := app.MainWindow(); main != nil {
				if mb := windowMenuBarOf(main); mb != nil {
					return mb.Menus()
				}
			}
		}
	}
	if bar == nil {
		return nil
	}
	return bar.Menus()
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
	var out []platform.NativeMenuItem
	for _, it := range m.Items() {
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
