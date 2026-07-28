// Command demoapp is the full KittyTK demo as a display-protocol
// APPLICATION: it links only client + protocol (no rendering backend),
// dials a running KittyTK display service, and drives its entire UI - the
// nine-tab trinket gallery, menu bars, status bar, the protocol-built
// window, terminal child windows, dialogs and MDI - over the socket.
//
//	terminal 1:  go run ./cmd/kittytk-tui             (or -tags sdl ./cmd/kittytk-sdl)
//	terminal 2:  go run ./examples/demoapp
//
// It is the backendless twin of examples/demo: same windows, built from
// the same protocol scripts, but sent across the wire instead of
// constructed in process. "Window > New Window" opens a second, fully
// independent application by dialing another connection.
package main

import (
	"flag"
	"fmt"
	"os"
	"sync"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/ptydriver"
)

// soloMode, set by -solo, makes the primary app the whole display: its
// main window replaces the desktop (no Psi menu, dock or wallpaper).
var soloMode bool

func main() {
	flag.BoolVar(&soloMode, "solo", false, "run as the whole display (solo mode)")
	flag.Parse()

	path := client.DefaultSocketPath()

	a, err := newPrimary(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot reach display service at %s: %v\n", path, err)
		fmt.Fprintln(os.Stderr, "start a desktop first: go run ./cmd/kittytk-tui (or -tags sdl ./cmd/kittytk-sdl)")
		os.Exit(1)
	}
	a.wait() // blocks until the main window closes or the desktop exits
}

// secondaryCount names the applications opened via Window > New Window.
var (
	secondaryMu    sync.Mutex
	secondaryCount int
)

// app is one connection = one Application on the desktop.
type app struct {
	path    string
	conn    *client.Conn
	ui      *client.UI // the app's main build (window + chrome)
	primary bool

	// MDI bookkeeping (primary only): document and dock-entry counters.
	mdiCount int
	dockSeq  int

	// Client-side PTYs backing this app's terminal surfaces, closed when
	// the app quits.
	drivers []*ptydriver.Driver

	quit     chan struct{}
	quitOnce sync.Once
}

// newApp dials a fresh connection (a new Application on the desktop).
// The primary app dials solo when -solo is set, so its main window
// becomes the whole display.
func newApp(path, name string, primary bool) (*app, error) {
	a := &app{path: path, primary: primary, quit: make(chan struct{})}
	// Command dispatch is observed via conn.OnCommand handlers, so the
	// Dial sink is unused here.
	// The demo opens secondary windows, so it declares multi-window: the
	// display supplies its Window menu. Command dispatch is observed via
	// conn.OnCommand handlers, so the Dial sink is unused here.
	opts := client.DialOptions{MultiWindow: true, Solo: primary && soloMode}
	conn, err := client.DialWith(path, name, opts)
	if err != nil {
		return nil, err
	}
	a.conn = conn
	return a, nil
}

// newPrimary builds the primary application: the full demo window, its
// menus and status bar, plus the protocol-built companion window.
func newPrimary(path string) (*app, error) {
	a, err := newApp(path, "KittyTK Demo", true)
	if err != nil {
		return nil, err
	}
	ui, err := a.conn.Build(mainBuildScript())
	if err != nil {
		a.conn.Close()
		return nil, fmt.Errorf("build main window: %w", err)
	}
	a.ui = ui

	a.wireMainWindow()
	a.wireMenus()
	a.wireMDI()
	a.wireDetails()
	a.openProtocolWindow()

	// The demo ends when its main window closes (or the desktop exits).
	ui.Window("w").OnClosed(func() { a.signalQuit() })
	return a, nil
}

// signalQuit ends the app's wait exactly once.
func (a *app) signalQuit() { a.quitOnce.Do(func() { close(a.quit) }) }

// wait blocks until the app quits or the display service disconnects.
func (a *app) wait() {
	select {
	case <-a.quit:
	case <-a.conn.Closed():
	}
	for _, d := range a.drivers {
		d.Close()
	}
	a.conn.Close()
}

// setStatus narrates to the desktop status bar via the status app verb
// (the display shows the active app's status text).
func (a *app) setStatus(text string) {
	_, _ = a.conn.Exec("status text=" + protocol.Quote(text))
}
