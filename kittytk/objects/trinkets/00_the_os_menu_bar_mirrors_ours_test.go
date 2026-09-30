package trinkets

// On a platform with a menu bar of its own, the menus the desktop draws are
// put there as well, for the window that has the focus. A native item is our
// item: choosing it, or pressing the key it shows, triggers the very MenuItem
// our menu would, and the key is the one our menu advertises. When that key
// no longer means the item where the focus is, the item says it may not run,
// so the key goes on to the focus. Items are rebuilt from ours each time they
// are asked for, about-to-show first; the set of menus is pushed only when it
// changes.

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/platform"
)

// spellNative writes native items out one per line.
func spellNative(items []platform.NativeMenuItem) []string {
	var out []string
	for _, it := range items {
		if it.Separator {
			out = append(out, "-----")
			continue
		}
		line := it.Title
		if it.Checked {
			line = "✓ " + line
		}
		if it.Key != "" {
			line += " [" + it.Key + "]"
		}
		if it.Submenu != nil {
			line += " > " + strings.Join(spellNative(it.Submenu.Items()), ", ")
		}
		if it.Enabled != nil && !it.Enabled() {
			line += " (off)"
		}
		out = append(out, line)
	}
	return out
}

func TestANativeMenuIsOurMenuItemForItem(t *testing.T) {
	var ran []string
	m := NewMenu("&File")
	open := NewMenuItem("&Open").SetShortcut("s-o")
	open.SetOnTriggered(func() { ran = append(ran, "open") })
	m.AddItem(open)
	wrap := NewMenuItem("&Wrap").SetCheckable(true).SetChecked(true)
	m.AddItem(wrap)
	m.AddSeparator()
	gone := NewMenuItem("&Revert")
	gone.SetEnabled(false)
	m.AddItem(gone)
	sub := NewMenu("&Recent")
	sub.AddItem(NewMenuItem("notes.txt"))
	m.AddMenu(sub)
	m.AddItem(NewMenuItem("&Print").SetShortcutText("C-x C-p"))
	m.AddItem(NewMenuItem("&Plain").SetChecked(true)) // checked, but not a checkable item

	got := strings.Join(spellNative(nativeItems(m)), "\n")
	want := strings.Join([]string{
		"Open [s-o]",
		"✓ Wrap",
		"-----",
		"Revert (off)",
		"Recent > notes.txt",
		"Print",
		"Plain",
	}, "\n")
	if got != want {
		t.Fatalf("the native items read\n%s\nwant\n%s", got, want)
	}

	items := nativeItems(m)
	items[0].Action()
	if len(ran) != 1 || ran[0] != "open" {
		t.Errorf("choosing Open ran %q", ran)
	}
	items[1].Action()
	if wrap.Checked {
		t.Error("choosing Wrap did not toggle it, as our menu's click does")
	}
}

// The key is asked for where it is asked for in our own menu, and an item
// whose key has moved on says it may not run.
func TestANativeItemsKeyIsTheOneOurMenuShows(t *testing.T) {
	key := "s-z"
	m := NewMenu("&Edit")
	undo := NewMenuItem("&Undo").SetCommand(core.CmdTrinketUndo)
	m.AddItem(undo)
	m.SetKeyResolver(func(string) string { return key })

	items := nativeItems(m)
	if items[0].Key != "s-z" || !items[0].Enabled() {
		t.Fatalf("Undo is bound to %q, enabled %v; want s-z and enabled", items[0].Key, items[0].Enabled())
	}
	key = "" // something has taken the keyboard on its own terms
	if items[0].Enabled() {
		t.Error("with its key meaning nothing here, Undo still says it may run")
	}
	if again := nativeItems(m); again[0].Key != "" {
		t.Errorf("asked again, Undo is bound to %q, want nothing", again[0].Key)
	}
	key = "s-z"
	undo.SetEnabled(false)
	if nativeItems(m)[0].Enabled() {
		t.Error("a disabled Undo says it may run")
	}
}

// Each time the items are asked for, the menu's about-to-show runs first, so
// a menu that fills itself is read as it stands.
func TestANativeMenuIsReadAfterItsAboutToShow(t *testing.T) {
	m := NewMenu("&Window")
	n := 0
	m.SetOnAboutToShow(func() {
		n++
		m.Clear()
		for i := 0; i < n; i++ {
			m.AddItem(NewMenuItem("win"))
		}
	})
	if got := len(nativeItems(m)); got != 1 {
		t.Errorf("first asked, %d items, want 1", got)
	}
	if got := len(nativeItems(m)); got != 2 {
		t.Errorf("asked again, %d items, want 2", got)
	}
}

// nativeBarPlatform is a platform with a menu bar of its own.
type nativeBarPlatform struct {
	msPlatform
	sets [][]platform.NativeMenu
}

func (p *nativeBarPlatform) SetNativeMenus(menus []platform.NativeMenu) {
	p.sets = append(p.sets, menus)
}

func (p *nativeBarPlatform) Run(init func(platform.Platform)) int {
	return p.msPlatform.Run(func(platform.Platform) { init(p) })
}

func titlesOf(menus []platform.NativeMenu) string {
	var t []string
	for _, m := range menus {
		t = append(t, m.Title)
	}
	return strings.Join(t, " ")
}

func desktopBarTitles(mb *MenuBar) string {
	var t []string
	for _, m := range mb.Menus() {
		t = append(t, m.Title())
	}
	return strings.Join(t, " ")
}

// The desktop puts its bar in the OS menu bar when it starts, and again only
// when the titles in it change; a native menu reads the bar as it is when
// asked, so a bar rebuilt with the same titles needs nothing pushed.
func TestTheOSMenuBarFollowsTheDesktopsBar(t *testing.T) {
	d, _ := desktopWithApps(t, "Ledger")
	plat := &nativeBarPlatform{}
	plat.script = func() {
		if len(plat.sets) == 0 {
			t.Fatal("the desktop put nothing in the OS menu bar")
		}
		last := plat.sets[len(plat.sets)-1]
		if got, want := titlesOf(last), desktopBarTitles(d.MenuBar()); got != want || got == "" {
			t.Errorf("the OS menu bar holds %q, the desktop's %q", got, want)
		}
		pushes := len(plat.sets)
		d.updateMenuBarContent()
		d.updateMenuBarContent()
		if len(plat.sets) != pushes {
			t.Errorf("rebuilding the same bar pushed it %d more times", len(plat.sets)-pushes)
		}
		// A bar whose titles change is pushed again.
		app := d.ActiveApplication().(*mockApp)
		app.menus = []*Menu{NewMenu("&Reports")}
		d.updateMenuBarContent()
		if len(plat.sets) != pushes+1 || !strings.Contains(titlesOf(plat.sets[len(plat.sets)-1]), "Reports") {
			t.Errorf("after the app's menus changed, %d more pushes, last %q", len(plat.sets)-pushes, titlesOf(plat.sets[len(plat.sets)-1]))
		}
		last = plat.sets[len(plat.sets)-1]
		// The menus it pushed read the bar the desktop has now.
		for i, m := range last {
			if got, want := len(m.Items()), len(nativeItems(d.MenuBar().Menus()[i])); got != want {
				t.Errorf("the native %q reads %d items, the desktop's %d", m.Title, got, want)
			}
		}
		// A bar whose titles have moved on reads as empty until the new set
		// arrives.
		stale := d.nativeBarMenu(0, "no such menu")
		if got := stale.Items(); got != nil {
			t.Errorf("a menu no longer in the bar reads %d items", len(got))
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// A torn window reads its menus from its own bar, or its application's main
// window's; a window on the desktop from the desktop's.
func TestTheOSMenuBarIsTheFocusedWindowsBar(t *testing.T) {
	r := newFocusRig(t)
	main := r.torn("main")
	own := NewMenuBar()
	own.AddMenu(NewMenu("File"))
	own.AddMenu(NewMenu("Edit"))
	main.SetWindowMenuBar(own)
	dialog := r.torn("dialog")
	r.d.AddApplication(&mockApp{name: "App", main: main, windows: []*window.Window{main, dialog}})

	titles := func() string {
		var t []string
		for _, m := range r.d.nativeBarMenus() {
			t = append(t, m.Title())
		}
		return strings.Join(t, " ")
	}
	r.d.windowFocusChanged(main)
	if got := titles(); got != "File Edit" {
		t.Errorf("with the torn main window focused, the bar is %q", got)
	}
	r.d.windowFocusChanged(dialog)
	if got := titles(); got != "File Edit" {
		t.Errorf("with the app's torn dialog focused, the bar is %q, want its main window's", got)
	}
	docked := r.docked("docked")
	r.d.windowFocusChanged(docked)
	if got, want := titles(), desktopBarTitles(r.d.menuBar); got != want || strings.Contains(got, "File") {
		t.Errorf("with a docked window focused, the bar is %q, want the desktop's %q", got, want)
	}
}
