package trinkets

import (
	"testing"

	"github.com/phroun/kittytk/backend/raster"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/window"
)

// When the desktop boots with only its windowless, menuless host
// application (no client app connected yet), the menu bar shows just the
// Psi system menu - not an app-named menu for the host.
func TestEmptyDesktopShowsOnlyPsiMenu(t *testing.T) {
	t.Cleanup(func() { core.SetTextMeasurer(nil) })
	px, _ := raster.New(400, 200)
	d := NewDesktop()
	d.SetBackend(px)

	// The host app, as kittytk-sdl creates it: a name, but no windows and
	// no menu-bar content. It is context-only - the desktop host has no
	// editing surface of its own - so the graphical surface adds no
	// automatic Edit menu for it.
	host := &mockApp{name: "SDL Desktop", contextOnly: true}
	d.AddApplication(host)

	menus := d.MenuBar().Menus()
	if len(menus) != 1 {
		var titles []string
		for _, m := range menus {
			titles = append(titles, m.Title())
		}
		t.Fatalf("empty-desktop menu bar = %v, want only the Psi system menu", titles)
	}
	if menus[0] != d.systemMenu {
		t.Errorf("sole menu is %q, want the Psi system menu", menus[0].Title())
	}

	// Once the app gains a window, it earns a standard app menu again.
	host.windows = []*window.Window{window.NewWindow("w")}
	d.updateMenuBarContent()
	if got := len(d.MenuBar().Menus()); got != 2 {
		t.Errorf("with a window, menu count = %d, want 2 (Psi + app)", got)
	}
}
