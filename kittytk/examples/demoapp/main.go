// Command demoapp is the full KittyTK demo as a display-protocol
// APPLICATION: it links only client + protocol (no rendering backend),
// dials a running KittyTK display service, and drives its entire UI - the
// tabbed trinket gallery, menu bars, status bar, the protocol-built
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
	// It ends when it has no windows left, and says so: a demo that exits quietly
	// and one that has hung look identical from a terminal.
	fmt.Println("demoapp: exiting —", a.wait())
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

	// MDI bookkeeping (primary only): document and dock-entry counters, and
	// the close questions, which are keyed apart so two documents can both be
	// asking at once.
	mdiCount  int
	dockSeq   int
	closeAsks int

	// Client-side PTYs backing this app's terminal surfaces, closed when
	// the app quits.
	drivers []*ptydriver.Driver

	// Set once the Terminal tab has been opened and its shell started.
	terminalStarted bool

	// What the desktop has kept for this app, as its answers arrive.
	shelf storeReport

	// The top-level windows this app has open, by object id, and why it stopped.
	// An application with none left has nothing to be -- see watchWindow.
	//
	// Under a lock because they are counted on the connection's event goroutine
	// and read from whoever asks: an example that races is an example of racing.
	windowsMu   sync.Mutex
	openWindows map[uint64]string
	why         string

	quit     chan struct{}
	quitOnce sync.Once
}

// newApp dials a fresh connection (a new Application on the desktop).
// The primary app dials solo when -solo is set, so its main window
// becomes the whole display.
func newApp(path, name string, primary bool) (*app, error) {
	a := &app{path: path, primary: primary, quit: make(chan struct{}),
		openWindows: map[uint64]string{}}
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
	// Served BEFORE the build, the build naming it. See refusing.go.
	if err := a.provideRefusal(); err != nil {
		a.conn.Close()
		return nil, fmt.Errorf("serve the refusing source: %w", err)
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
	a.stockTheStore()
	a.openProtocolWindow()

	// The demo ends when it has no windows left -- see watchWindow.
	a.watchWindow(ui.ID("w"), "the main window")
	return a, nil
}

// watchWindow follows one window this application opened, so the demo can tell when
// it has none left.
//
// **An application with no windows has nothing to be.** Nothing tells it to go: the
// display closes its windows, for a desktop quitting or a person pressing [x], and
// then says nothing more -- an application may perfectly well live on with no windows
// and open one later. So noticing is the application's own job, and a demo that did
// not notice sat in a terminal, connected to a display it had nothing on, until
// somebody pressed ctrl-C.
//
// Only TOP-LEVEL windows count. An MDI document lives inside the main window and
// closing the last of them leaves an application with an empty pane, not with
// nothing; a dialog is a question, not somewhere to be.
func (a *app) watchWindow(id uint64, what string) {
	if id == 0 {
		return
	}
	a.windowsMu.Lock()
	a.openWindows[id] = what
	a.windowsMu.Unlock()

	a.conn.Object(id).On("window_closed", func(*protocol.Event) {
		a.windowsMu.Lock()
		delete(a.openWindows, id)
		left := len(a.openWindows)
		a.windowsMu.Unlock()
		if left == 0 {
			a.signalQuit(what + " was the last window it had open")
		}
	})
}

// windowsOpen is what it still has, by id and by what each one is.
func (a *app) windowsOpen() map[uint64]string {
	a.windowsMu.Lock()
	defer a.windowsMu.Unlock()
	out := make(map[uint64]string, len(a.openWindows))
	for id, what := range a.openWindows {
		out[id] = what
	}
	return out
}

// signalQuit ends the app's wait exactly once, saying why.
func (a *app) signalQuit(why string) {
	a.quitOnce.Do(func() {
		a.why = why
		close(a.quit)
	})
}

// wait blocks until the app has nothing left to do, and says what that was: the
// message the terminal gets is the proof it ended of its own accord rather than
// being killed.
func (a *app) wait() string {
	why := ""
	select {
	case <-a.quit:
		why = a.why
	case <-a.conn.Closed():
		// The display went. Its windows went with it, and there is nothing to say
		// goodbye to -- though the display may have said one on its way out, and
		// which kind it was is worth repeating.
		why = "the display service went away"
		if reason, said := a.conn.Goodbye(); said {
			why = "the display said goodbye: " + reason
		}
	}
	for _, d := range a.drivers {
		d.Close()
	}
	a.conn.Close()
	return why
}

// setStatus narrates to the desktop status bar via the status app verb
// (the display shows the active app's status text).
func (a *app) setStatus(text string) {
	_, _ = a.conn.Exec("status text=" + protocol.Quote(text))
}
