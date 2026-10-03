package trinkets

// Hide, Hide Others and Show All are items the desktop adds to the active
// application's first menu, so they are in the bar it draws and nowhere in
// the application's own menus. Their keys find them there.

import "testing"

func TestTheHideKeyHidesTheActiveApplication(t *testing.T) {
	r := newDockRig(t)
	r.d.windowManager.ActivateWindow(r.demoMain)
	r.d.updateMenuBarContent()

	if !r.d.HandleResolvedCommand("app_hide", "^H") {
		t.Fatal("app_hide went unanswered")
	}
	if !r.demoMain.IsMinimized() || !r.ask.IsMinimized() {
		t.Errorf("after app_hide: main minimized=%v, ask minimized=%v; want both hidden",
			r.demoMain.IsMinimized(), r.ask.IsMinimized())
	}
}
