package trinkets

// On a platform with a menu bar of its own, the menus the desktop draws are
// put there as well, for the window that has the focus. A native item is our
// item: choosing it, or pressing the key it shows, triggers the very MenuItem
// our menu would, and the key is the one our menu advertises. When that key
// no longer means the item where the focus is, the item says it may not run,
// so the key goes on to the focus. Items are rebuilt from ours each time they
// are asked for, about-to-show first; the set of menus is pushed only when it
// changes. The desktop's own menu, Ψ, is the OS's application menu, and is
// not repeated after it.

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
	apps []platform.NativeMenu
}

func (p *nativeBarPlatform) SetNativeMenus(app platform.NativeMenu, menus []platform.NativeMenu) {
	p.apps = append(p.apps, app)
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

// desktopBarTitles is the desktop's bar as the OS bar shows it after the
// application menu: without Ψ.
func desktopBarTitles(mb *MenuBar) string {
	var t []string
	for _, m := range mb.Menus() {
		if m.Title() != "Ψ" {
			t = append(t, m.Title())
		}
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
		if strings.Contains(titlesOf(last), "Ψ") {
			t.Error("Ψ is repeated after the application menu")
		}
		app := plat.apps[len(plat.apps)-1]
		if got, want := len(app.Items()), len(nativeItems(d.systemMenu)); got != want || got == 0 {
			t.Errorf("the application menu reads %d items, Ψ has %d", got, want)
		}
		pushes := len(plat.sets)
		d.updateMenuBarContent()
		d.updateMenuBarContent()
		if len(plat.sets) != pushes {
			t.Errorf("rebuilding the same bar pushed it %d more times", len(plat.sets)-pushes)
		}
		// A bar whose titles change is pushed again.
		active := d.ActiveApplication().(*mockApp)
		active.menus = []*Menu{NewMenu("&Reports")}
		d.updateMenuBarContent()
		if len(plat.sets) != pushes+1 || !strings.Contains(titlesOf(plat.sets[len(plat.sets)-1]), "Reports") {
			t.Errorf("after the app's menus changed, %d more pushes, last %q", len(plat.sets)-pushes, titlesOf(plat.sets[len(plat.sets)-1]))
		}
		last = plat.sets[len(plat.sets)-1]
		// The menus it pushed read the bar the desktop has now.
		for i, m := range last {
			bar, _ := d.nativeBar()
			if got, want := len(m.Items()), len(nativeItems(bar[i].menu)); got != want {
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
		bar, _ := r.d.nativeBar()
		for _, e := range bar {
			t = append(t, e.title)
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
	// A torn bar's first menu, titled with the app's menu name, goes under
	// the app's own name.
	hamburger := NewMenuBar()
	hamburger.AddMenu(NewMenu("≡"))
	hamburger.AddMenu(NewMenu("View"))
	main.SetWindowMenuBar(hamburger)
	if got := titles(); got != "App View" {
		t.Errorf("with the torn bar's first menu named ≡, the bar is %q, want App View", got)
	}

	docked := r.docked("docked")
	r.d.windowFocusChanged(docked)
	if got, want := titles(), desktopBarTitles(r.d.menuBar); got != want || strings.Contains(got, "File") {
		t.Errorf("with a docked window focused, the bar is %q, want the desktop's %q", got, want)
	}
}

// The host application -- the one the process is named for -- has its leading
// menu folded into the application menu while it has the focus: the desktop's
// groups, then its own items less the desktop's items it repeats, then Exit
// Desktop beside its Quit. It stands in the bar after that like any other app
// when it is not the host.
func TestTheHostApplicationsMenuJoinsTheApplicationMenu(t *testing.T) {
	d, _ := desktopWithApps(t, "mew")
	plat := &nativeBarPlatform{}
	plat.script = func() {
		mew := d.ActiveApplication()
		bar, host := d.nativeBar()
		if host != nil {
			t.Fatal("an app not declared the host was folded in")
		}
		if len(bar) == 0 || bar[0].title != "mew" {
			t.Fatalf("undeclared, mew's menu is not first in the bar: %+v", bar)
		}

		d.SetHostApplication(mew)
		if got := d.HostApplication(); got != mew {
			t.Fatalf("the host application is %v", got)
		}
		bar, host = d.nativeBar()
		if host == nil {
			t.Fatal("declared the host, mew's menu was not folded in")
		}
		for _, e := range bar {
			if e.title == "mew" {
				t.Error("declared the host, mew's menu still stands in the bar")
			}
		}
		last := plat.sets[len(plat.sets)-1]
		if strings.Contains(titlesOf(last), "mew") {
			t.Errorf("the OS bar after the application menu holds %q", titlesOf(last))
		}

		got := spellNative(plat.apps[len(plat.apps)-1].Items())
		if n := strings.Count(strings.Join(got, "\n"), "Narration"); n != 1 {
			t.Errorf("Narration appears %d times in\n%s", n, strings.Join(got, "\n"))
		}
		joined := strings.Join(got, "\n")
		for _, want := range []string{"About Desktop", "Hide mew", "Show All", "Quit mew"} {
			if !strings.Contains(joined, want) {
				t.Errorf("the merged application menu has no %q:\n%s", want, joined)
			}
		}
		if n := len(got); n < 2 || !strings.HasPrefix(got[n-2], "Quit mew") || !strings.HasPrefix(got[n-1], "Exit Desktop") {
			t.Errorf("the merged menu does not end with Quit mew then Exit Desktop:\n%s", joined)
		}
		if strings.HasPrefix(got[0], "-----") || strings.Contains(joined, "-----\n-----") {
			t.Errorf("the merged menu has a stray separator:\n%s", joined)
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// The merge, item by item, with the desktop's own menu and an application
// menu as the desktop builds them.
func TestMergingTheHostsMenuIntoTheDesktops(t *testing.T) {
	r := newFocusRig(t)
	sys := NewMenu("Ψ")
	sys.AddItem(NewMenuItem("About Desktop"))
	sys.AddItem(NewMenuItem("Narration").SetWellKnownID(ItemIDNarration))
	sys.AddSeparator()
	sys.AddItem(NewMenuItem("Event Viewer"))
	sys.AddSeparator()
	sys.AddItem(NewMenuItem("Exit Desktop"))

	host := r.d.createStandardAppMenu("mew")
	got := strings.Join(spellNative(mergedAppMenuItems(sys, host)), "\n")
	want := strings.Join([]string{
		"About Desktop",
		"Narration",
		"-----",
		"Event Viewer",
		"-----",
		"Hide mew [^H]",
		"Hide Others [M-^H]",
		"Show All",
		"-----",
		"Quit mew [^Q]",
		"Exit Desktop",
	}, "\n")
	if got != want {
		t.Errorf("merged, the application menu reads\n%s\nwant\n%s", got, want)
	}

	// A host menu of nothing but the desktop's own items adds nothing.
	dup := NewMenu("mew")
	dup.AddItem(NewMenuItem("Narration").SetWellKnownID(ItemIDNarration))
	if got, want := len(mergedAppMenuItems(sys, dup)), len(nativeItems(sys)); got != want {
		t.Errorf("a host menu of duplicates gives %d items, the desktop's alone %d", got, want)
	}
	// A desktop menu of one group puts the host's items before it all.
	one := NewMenu("Ψ")
	one.AddItem(NewMenuItem("Exit Desktop"))
	mine := NewMenu("mew")
	mine.AddItem(NewMenuItem("Quit mew"))
	if got := strings.Join(spellNative(mergedAppMenuItems(one, mine)), "\n"); got != "Quit mew\nExit Desktop" {
		t.Errorf("merged with a one-group desktop menu:\n%s", got)
	}
}

// A torn host application's own bar -- as the desktop builds it, with the
// app's declared leading menu or the one it synthesizes -- folds its first
// menu in too; a first menu that is not the application menu stays put.
func TestATornHostApplicationsMenuJoinsTheApplicationMenu(t *testing.T) {
	for _, tc := range []struct {
		name  string
		menus []*Menu
	}{
		{"declared", []*Menu{NewMenu("&mew").SetWellKnownID(MenuIDApp), NewMenu("&View")}},
		{"synthesized", []*Menu{NewMenu("&View")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFocusRig(t)
			main := r.torn("main")
			app := &mockApp{name: "mew", main: main, windows: []*window.Window{main}, menus: tc.menus}
			r.d.AddApplication(app)
			r.d.attachMainWindowChrome(main)
			r.d.SetHostApplication(app)
			r.d.windowFocusChanged(main)
			bar, host := r.d.nativeBar()
			if host == nil {
				t.Fatal("the torn host's first menu was not folded in")
			}
			for _, e := range bar {
				if e.menu == host {
					t.Error("the folded menu is still in the bar")
				}
			}
		})
	}

	r := newFocusRig(t)
	main := r.torn("main")
	own := NewMenuBar()
	own.AddMenu(NewMenu("View"))
	main.SetWindowMenuBar(own)
	app := &mockApp{name: "mew", main: main, windows: []*window.Window{main}}
	r.d.AddApplication(app)
	r.d.SetHostApplication(app)
	r.d.windowFocusChanged(main)
	if bar, host := r.d.nativeBar(); host != nil || len(bar) != 1 {
		t.Errorf("a first menu that is not the application menu was folded in (bar %+v)", bar)
	}
}

// The host application's declared leading menu, on the desktop's bar, is
// folded in as the synthesized one is.
func TestADeclaredHostMenuJoinsTheApplicationMenu(t *testing.T) {
	d, wins := desktopWithApps(t, "mew")
	plat := &nativeBarPlatform{}
	plat.script = func() {
		mew := d.ActiveApplication().(*mockApp)
		own := NewMenu("&mew").SetWellKnownID(MenuIDApp)
		own.AddItem(NewMenuItem("About mew"))
		mew.menus = []*Menu{own}
		mew.windows = wins
		d.SetHostApplication(mew)
		_, host := d.nativeBar()
		if host == nil {
			t.Fatal("the declared leading menu was not folded in")
		}
		if got := strings.Join(spellNative(mergedAppMenuItems(d.systemMenu, host)), "\n"); !strings.Contains(got, "About mew") {
			t.Errorf("the merged menu has no About mew:\n%s", got)
		}
		d.ForceQuitWithCode(0)
	}
	d.RunOn(plat)
}

// Both menus are brought up to date before they are merged, as each would be
// before being shown.
func TestBothMenusAreRefreshedBeforeTheMerge(t *testing.T) {
	sys := NewMenu("Ψ")
	sys.SetOnAboutToShow(func() { sys.Clear(); sys.AddItem(NewMenuItem("fresh desktop")) })
	host := NewMenu("mew")
	host.SetOnAboutToShow(func() { host.Clear(); host.AddItem(NewMenuItem("fresh host")) })
	got := strings.Join(spellNative(mergedAppMenuItems(sys, host)), "\n")
	if got != "fresh host\nfresh desktop" {
		t.Errorf("merged before refreshing:\n%s", got)
	}
}

// Leaving items out leaves no separator at either end or doubled.
func TestTidyingSeparators(t *testing.T) {
	spell := func(items []*MenuItem) string {
		var out []string
		for _, it := range items {
			if it.Separator {
				out = append(out, "-")
			} else {
				out = append(out, it.Text)
			}
		}
		return strings.Join(out, " ")
	}
	in := []*MenuItem{NewSeparator(), NewMenuItem("a"), NewSeparator(), NewSeparator(), NewMenuItem("b"), NewSeparator()}
	if got := spell(tidySeparators(in)); got != "a - b" {
		t.Errorf("tidied: %q, want %q", got, "a - b")
	}
}
