package trinkets

import (
	"strings"
	"testing"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// mockApp is a minimal ApplicationProvider for exercising the desktop's
// menu-bar composition.
type mockApp struct {
	name        string
	objectID    core.ObjectID
	menuName    string
	main        *window.Window
	menus       []*Menu
	windows     []*window.Window
	multiWindow bool
	contextOnly bool
}

func (a *mockApp) Name() string            { return a.name }
func (a *mockApp) ObjectID() core.ObjectID { return a.objectID }
func (a *mockApp) MultiWindow() bool       { return a.multiWindow }
func (a *mockApp) ContextOnly() bool       { return a.contextOnly }
func (a *mockApp) MenuName() string {
	if a.menuName == "" {
		return "≡"
	}
	return a.menuName
}
func (a *mockApp) Windows() []*window.Window         { return a.windows }
func (a *mockApp) MainWindow() *window.Window        { return a.main }
func (a *mockApp) AddWindow(*window.Window)          {}
func (a *mockApp) RemoveWindow(*window.Window)       {}
func (a *mockApp) MenuBarContent() []*Menu           { return a.menus }
func (a *mockApp) StatusBarContent() []StatusSection { return nil }
func (a *mockApp) OnActivate()                       {}
func (a *mockApp) OnDeactivate()                     {}
func (a *mockApp) SetDesktop(core.Trinket)           {}
func (a *mockApp) PassNextKeyToTrinket() bool        { return false }
func (a *mockApp) ActivatePassNextKeyToTrinket()     {}
func (a *mockApp) ClearPassNextKeyToTrinket()        {}

// The Ψ menu's About Desktop item opens the About KittyTK dialog, whose
// text carries the recursive name, version, and copyright.
func TestAboutDesktopDialog(t *testing.T) {
	txt := aboutDesktopText()
	for _, want := range []string{
		"image/tty Trinket Kit",
		"Version " + core.FullVersion(),
		"Jeffrey R. Day",
		"All rights reserved",
	} {
		if !strings.Contains(txt, want) {
			t.Errorf("about text missing %q:\n%s", want, txt)
		}
	}

	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	var about *MenuItem
	for _, it := range d.systemMenu.Items() {
		if strings.Contains(it.Text, "About Desktop") {
			about = it
		}
	}
	if about == nil || about.OnTriggered == nil {
		t.Fatal("About Desktop menu item not found or not wired")
	}

	about.OnTriggered()
	wins := d.windowManager.Windows()
	if len(wins) != 1 {
		t.Fatalf("About Desktop opened %d windows, want 1", len(wins))
	}
	if got := wins[0].Title(); got != "About KittyTK" {
		t.Errorf("about dialog title = %q, want About KittyTK", got)
	}
}

func menuTitles(mb *MenuBar) []string {
	var out []string
	for _, m := range mb.Menus() {
		out = append(out, m.Title())
	}
	return out
}

func menuHasItem(m *Menu, substr string) bool {
	for _, it := range m.Items() {
		if strings.Contains(it.Text, substr) {
			return true
		}
	}
	return false
}

func TestDesktopMenuBarFullVsReduced(t *testing.T) {
	d := NewDesktop()
	appDecl := NewMenu("&Demo").SetWellKnownID(MenuIDApp)
	appDecl.AddItem(NewMenuItem("New"))
	app := &mockApp{name: "Demo", menus: []*Menu{appDecl}}
	d.activeApp = app

	// Full bar (no main window): system Psi menu, the app menu carrying the
	// merged Hide + Quit sections, and the mandatory system Edit menu (Edit
	// is always present in the text/TUI version).
	d.updateMenuBarContent()
	if got := menuTitles(d.menuBar); len(got) != 3 || got[0] != "Ψ" || got[1] != "Demo" || got[2] != "Edit" {
		t.Fatalf("full bar titles = %v, want [Ψ Demo Edit]", got)
	}
	appMenu := d.menuBar.Menus()[1]
	if !menuHasItem(appMenu, "Hide Demo") || !menuHasItem(appMenu, "Quit Demo") {
		t.Errorf("full app menu missing Hide/Quit: %v", itemTexts(appMenu))
	}
	editMenu := d.menuBar.Menus()[2]
	for _, want := range []string{"Cut", "Copy", "Paste", "Select All"} {
		if !menuHasItem(editMenu, want) {
			t.Errorf("system Edit menu missing %q: %v", want, itemTexts(editMenu))
		}
	}

	// Reduced bar (main window detached): the real system Psi menu, an
	// app-named menu carrying only the Hide section, and a Window menu
	// (Tile/Cascade). The real Psi keeps its own items (Exit Desktop);
	// the app's menus move to the detached window's own bar.
	main := window.NewWindow("main")
	main.SetDetached(true)
	app.main = main
	d.updateMenuBarContent()
	if got := menuTitles(d.menuBar); len(got) != 3 || got[0] != "Ψ" || got[1] != "Demo" || got[2] != "Window" {
		t.Fatalf("reduced bar titles = %v, want [Ψ Demo Window]", got)
	}
	psi := d.menuBar.Menus()[0]
	if psi != d.systemMenu {
		t.Errorf("reduced bar first menu should be the real system menu")
	}
	if !menuHasItem(psi, "Exit Desktop") {
		t.Errorf("reduced system menu lost its own items: %v", itemTexts(psi))
	}
	appHide := d.menuBar.Menus()[1]
	if !menuHasItem(appHide, "Hide Demo") || !menuHasItem(appHide, "Hide Others") || !menuHasItem(appHide, "Show All") {
		t.Errorf("reduced app menu missing hide section: %v", itemTexts(appHide))
	}
	if menuHasItem(appHide, "Quit") {
		t.Errorf("reduced app menu should not carry Quit: %v", itemTexts(appHide))
	}
	win := d.menuBar.Menus()[2]
	if !menuHasItem(win, "Tile") || !menuHasItem(win, "Cascade") {
		t.Errorf("reduced Window menu missing Tile/Cascade: %v", itemTexts(win))
	}
}

// The detached window's own first menu keeps its items and gains only
// the Quit section - no Hide section, no offset separator.
func TestDetachedWindowFirstMenuQuitOnly(t *testing.T) {
	d := NewDesktop()
	edit := NewMenu("&Edit")
	edit.AddItem(NewMenuItem("Copy"))
	m := d.createAppMenuWithQuitOnly(edit, "≡", "Demo")
	if m.Title() != "≡" {
		t.Errorf("quit-only menu title = %q, want ≡", m.Title())
	}
	if !menuHasItem(m, "Copy") {
		t.Error("quit-only menu dropped the original items")
	}
	if !menuHasItem(m, "Quit Demo") {
		t.Error("quit-only menu missing Quit")
	}
	if menuHasItem(m, "Hide Demo") {
		t.Error("quit-only menu should not carry the Hide section")
	}
}

// The desktop-hosted Window menu keeps its own custom entries, then lists
// the app's own windows, a separator, and the other in-surface desktop
// windows (belonging to other apps). Torn windows of other apps live on
// their own surfaces (not in the manager) and so never appear.
func TestDesktopWindowMenuListsAppThenOthers(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	winMenu := NewMenu("&Window")
	winMenu.AddItem(NewMenuItem("Zoom"))

	appWin1 := window.NewWindow("Doc 1")
	appWin2 := window.NewWindow("Doc 2")
	app := &mockApp{name: "Demo", windows: []*window.Window{appWin1, appWin2}}

	// An in-surface window belonging to some other app.
	other := window.NewWindow("Other Win")
	d.windowManager.AddWindow(other)

	menu := d.buildDesktopWindowMenu(winMenu, app)
	texts := itemTexts(menu)

	// Leads with the system Tile/Cascade, then the app's custom entry, then
	// the window list.
	if len(texts) < 2 || texts[0] != "Tile" || texts[1] != "Cascade" {
		t.Fatalf("Window menu should lead with Tile/Cascade, got %v", texts)
	}
	for _, want := range []string{"Zoom", "Doc 1", "Doc 2", "Other Win"} {
		if !menuHasItem(menu, want) {
			t.Errorf("Window menu missing %q: %v", want, texts)
		}
	}

	idx := func(s string) int {
		for i, it := range menu.Items() {
			if it.Text == s {
				return i
			}
		}
		return -1
	}
	// Custom entry between Tile/Cascade and the window list.
	if idx("Zoom") < idx("Cascade") || idx("Zoom") > idx("Doc 1") {
		t.Errorf("custom entry should sit between Cascade and the window list: %v", texts)
	}
	if idx("Doc 1") > idx("Other Win") || idx("Doc 2") > idx("Other Win") {
		t.Errorf("app windows should precede other-app windows: %v", texts)
	}

	// The app's own windows are not duplicated when they are also in-surface.
	d.windowManager.AddWindow(appWin1)
	menu = d.buildDesktopWindowMenu(winMenu, app)
	count := 0
	for _, it := range menu.Items() {
		if it.Text == "Doc 1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Doc 1 listed %d times, want 1: %v", count, itemTexts(menu))
	}
}

func itemTexts(m *Menu) []string {
	var out []string
	for _, it := range m.Items() {
		out = append(out, it.Text)
	}
	return out
}

func titlesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// A multi-window app gets a system Window menu on the desktop bar without
// declaring one; it is the final menu, but a Help menu comes after it. The
// mandatory app and Edit menus lead.
func TestMultiWindowGetsWindowMenuBeforeHelp(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	appDecl := NewMenu("&Demo").SetWellKnownID(MenuIDApp)
	help := NewMenu("&Help").SetWellKnownID(MenuIDHelp)
	help.AddItem(NewMenuItem("About"))
	app := &mockApp{name: "Demo", multiWindow: true, menus: []*Menu{appDecl, help}}
	d.activeApp = app

	d.updateMenuBarContent()
	got := menuTitles(d.menuBar)
	if want := []string{"Ψ", "Demo", "Edit", "Window", "Help"}; !titlesEqual(got, want) {
		t.Fatalf("multi-window bar = %v, want %v", got, want)
	}
}

// A single-window app gets no Window menu (Help still floats to the end).
func TestSingleWindowHasNoWindowMenu(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	appDecl := NewMenu("&Demo").SetWellKnownID(MenuIDApp)
	help := NewMenu("&Help").SetWellKnownID(MenuIDHelp)
	app := &mockApp{name: "Demo", multiWindow: false, menus: []*Menu{appDecl, help}}
	d.activeApp = app

	d.updateMenuBarContent()
	got := menuTitles(d.menuBar)
	if want := []string{"Ψ", "Demo", "Edit", "Help"}; !titlesEqual(got, want) {
		t.Fatalf("single-window bar = %v, want %v", got, want)
	}
}

// An app's own Window menu customizes the system menu's title and its items
// are merged between Tile/Cascade and the window list.
func TestAppWindowMenuCustomizesTitleAndMerges(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	appDecl := NewMenu("&Demo").SetWellKnownID(MenuIDApp)
	win := NewMenu("&Windows").SetWellKnownID(MenuIDWindow) // custom title + tag
	win.AddItem(NewMenuItem("Zoom"))
	app := &mockApp{name: "Demo", multiWindow: true, menus: []*Menu{appDecl, win}}
	d.activeApp = app

	d.updateMenuBarContent()
	got := menuTitles(d.menuBar)
	if want := []string{"Ψ", "Demo", "Edit", "Windows"}; !titlesEqual(got, want) {
		t.Fatalf("bar = %v, want the custom-titled Windows menu last", got)
	}
	wm := d.menuBar.Menus()[3]
	texts := itemTexts(wm)
	if len(texts) < 2 || texts[0] != "Tile" || texts[1] != "Cascade" {
		t.Fatalf("Window menu should lead with Tile/Cascade: %v", texts)
	}
	if !menuHasItem(wm, "Zoom") {
		t.Errorf("app's custom Window item not merged: %v", texts)
	}
}

// Menus are laid out in the canonical order regardless of the order the app
// declares them: app, file, edit, select, format, view, custom..., window,
// help. Detection is by well-known tag only.
func TestCanonicalMenuOrdering(t *testing.T) {
	d := NewDesktop()
	d.windowManager = window.NewWindowManager()

	mk := func(title, id string) *Menu { return NewMenu(title).SetWellKnownID(id) }
	// Declared deliberately out of order, with a custom (untagged) menu.
	menus := []*Menu{
		mk("&Help", MenuIDHelp),
		mk("&View", MenuIDView),
		NewMenu("&Tools"), // custom, untagged
		mk("&Format", MenuIDFormat),
		mk("&Select", MenuIDSelect),
		mk("&File", MenuIDFile),
		mk("&Demo", MenuIDApp),
	}
	app := &mockApp{name: "Demo", multiWindow: true, menus: menus}
	d.activeApp = app

	d.updateMenuBarContent()
	got := menuTitles(d.menuBar)
	want := []string{"Ψ", "Demo", "File", "Edit", "Select", "Format", "View", "Tools", "Window", "Help"}
	if !titlesEqual(got, want) {
		t.Fatalf("canonical order = %v, want %v", got, want)
	}
}

// On a graphical surface a ContextOnly app has no automatic Edit menu when it
// declared none; when it does declare one, the auto Cut/Copy/Paste/Select All
// items are omitted and only its own items remain.
func TestContextOnlySuppressesAutoEditMenu(t *testing.T) {
	// No declared Edit menu -> no Edit menu at all on a graphical surface.
	d := NewDesktop()
	d.graphicalFrames = true
	appDecl := NewMenu("&Demo").SetWellKnownID(MenuIDApp)
	app := &mockApp{name: "Demo", contextOnly: true, menus: []*Menu{appDecl}}
	d.activeApp = app
	d.updateMenuBarContent()
	if got := menuTitles(d.menuBar); !titlesEqual(got, []string{"Ψ", "Demo"}) {
		t.Fatalf("context-only bar = %v, want [Ψ Demo] (no Edit menu)", got)
	}

	// A declared Edit menu shows, but without the automatic items.
	d2 := NewDesktop()
	d2.graphicalFrames = true
	appDecl2 := NewMenu("&Demo").SetWellKnownID(MenuIDApp)
	edit := NewMenu("&Edit").SetWellKnownID(MenuIDEdit)
	edit.AddItem(NewMenuItem("&Raw Key Input"))
	app2 := &mockApp{name: "Demo", contextOnly: true, menus: []*Menu{appDecl2, edit}}
	d2.activeApp = app2
	d2.updateMenuBarContent()
	if got := menuTitles(d2.menuBar); !titlesEqual(got, []string{"Ψ", "Demo", "Edit"}) {
		t.Fatalf("context-only bar = %v, want [Ψ Demo Edit]", got)
	}
	em := d2.menuBar.Menus()[2]
	if menuHasItem(em, "Cut") || menuHasItem(em, "Paste") {
		t.Errorf("context-only Edit menu should omit auto items: %v", itemTexts(em))
	}
	if !menuHasItem(em, "Raw Key Input") {
		t.Errorf("context-only Edit menu dropped the app's own item: %v", itemTexts(em))
	}
}
