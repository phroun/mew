package display_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

// A window and its menubar arrive in one build batch, window first. Adopting
// the window activates the app and rebuilds the desktop menu bar - while the
// app still has no menus. The menubar is adopted a moment later via
// SetMenuBarContent, which must refresh the visible bar so the menus appear
// immediately, not only after the next focus switch.
func TestMenuBarShowsAfterAdoptedWithWindow(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "display.sock")

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})

	ready := make(chan *display.Server, 1)
	desktop.SetOnStartup(func() {
		srv, err := display.Serve(desktop, sock)
		if err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
			return
		}
		ready <- srv
	})
	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()
	var srv *display.Server
	select {
	case srv = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("desktop did not start")
	}
	defer func() {
		srv.Close()
		desktop.Quit()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("desktop did not exit")
		}
	}()

	conn, err := client.Dial(sock, "Menu App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Window first, then the menubar - the order a build batch adopts them.
	if _, err := conn.Build(`
w=new window title="Menu App" width=320 height=200 main children={new panel layout=vbox children={new label caption="hi"}}
mb=new menubar children={new menu caption="Zebra" children={new menuitem caption="Raw Key"}}
`); err != nil {
		t.Fatalf("build: %v", err)
	}

	// The desktop menu bar must adopt the app's "Zebra" menu without any
	// focus switch.
	deadline := time.Now().Add(5 * time.Second)
	for {
		found := false
		onUI(desktop, func() {
			for _, m := range desktop.MenuBar().Menus() {
				if m.Title() == "Zebra" {
					found = true
				}
			}
		})
		if found {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("desktop menu bar never showed the app's menu (needed a focus switch)")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
