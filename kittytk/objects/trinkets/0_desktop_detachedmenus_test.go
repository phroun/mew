package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/objects/window"
)

// A DETACHED main window carries its app's own menu bar on its own surface.
// That bar was built once, when the chrome was attached, so an app that
// declared its menus later - or changed them - showed the change on the
// desktop's bar and nowhere the user was looking.
func TestDetachedMenuBarFollowsTheAppsMenus(t *testing.T) {
	d := newRunnableDesktop(t)
	main := window.NewWindow("Solo")
	app := &mockApp{name: "Demo", menuName: "Demo", main: main, windows: []*window.Window{main}}
	d.AddApplication(app)

	plat := &msPlatform{}
	d.SetOnStartup(func() {
		d.WindowManager().AddWindow(main)
		d.EnterSoloMode(main)

		// Whatever it started with, "Extras" is not in it.
		for _, title := range barTitles(main) {
			if title == "Extras" {
				t.Fatal("the app has not declared an Extras menu yet")
			}
		}

		app.menus = []*Menu{NewMenu("Extras")}
		d.ActiveMenuBarContentChanged(app)

		found := false
		for _, title := range barTitles(main) {
			if title == "Extras" {
				found = true
			}
		}
		if !found {
			t.Errorf("the detached window's bar is %v, want it to carry Extras", barTitles(main))
		}
		d.QuitWithCode(0)
	})
	d.RunOn(plat)
}

// barTitles is the titles on a window's OWN menu bar (the one a detached
// window carries), or nil when it has none.
func barTitles(win *window.Window) []string {
	mb, ok := win.WindowMenuBar().(*MenuBar)
	if !ok || mb == nil {
		return nil
	}
	return menuTitles(mb)
}
