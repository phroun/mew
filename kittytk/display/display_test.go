package display_test

// The payoff test: a client (the app side, linking client+protocol
// only) dials a RUNNING headless desktop over a real unix socket,
// builds UI from protocol text, exchanges events both ways, and
// disconnects - watching its windows disappear.

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// nullBackend: headless RenderBackend (display-test copy).
type nullBackend struct{ mu sync.Mutex }

func (n *nullBackend) Init() error { return nil }
func (n *nullBackend) Shutdown()   {}
func (n *nullBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (n *nullBackend) Size() core.UnitSize                                  { return core.UnitSize{Width: 8 * 80, Height: 16 * 24} }
func (n *nullBackend) BeginFrame()                                          {}
func (n *nullBackend) EndFrame()                                            {}
func (n *nullBackend) Clear(style.CellStyle)                                {}
func (n *nullBackend) SetClip(core.UnitRect)                                {}
func (n *nullBackend) DrawCell(core.Unit, core.Unit, rune, style.CellStyle) {}
func (n *nullBackend) DrawText(x, y core.Unit, t string, s style.CellStyle, f *core.Font) core.Unit {
	return 0
}
func (n *nullBackend) DrawTextAligned(core.UnitRect, string, core.Alignment, core.Alignment, style.CellStyle, *core.Font) {
}
func (n *nullBackend) FillRect(core.UnitRect, rune, style.CellStyle)                     {}
func (n *nullBackend) DrawRect(core.UnitRect, style.BorderStyle, style.CellStyle)        {}
func (n *nullBackend) DrawHLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (n *nullBackend) DrawVLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (n *nullBackend) DrawBox(core.UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (n *nullBackend) PollEvent() core.Event                                             { return nil }
func (n *nullBackend) WaitEvent() core.Event                                             { return nil }
func (n *nullBackend) SetCursorVisible(bool)                                             {}
func (n *nullBackend) SetCursorPosition(core.Unit, core.Unit)                            {}
func (n *nullBackend) SupportsColor() bool                                               { return true }
func (n *nullBackend) SupportsMouse() bool                                               { return true }
func (n *nullBackend) SupportsUnicode() bool                                             { return true }
func (n *nullBackend) ColorDepth() int                                                   { return 256 }
func (n *nullBackend) GetClipboard() string                                              { return "" }
func (n *nullBackend) SetClipboard(string)                                               {}
func (n *nullBackend) Beep()                                                             {}

// onUI runs fn on the desktop's UI thread and waits.
func onUI(d *trinkets.Desktop, fn func()) {
	done := make(chan struct{})
	d.Post(func() { fn(); close(done) })
	<-done
}

func TestRemoteAppOverUnixSocket(t *testing.T) {
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

	// THE moment: a separate "app" dials the running desktop.
	dispatched := make(chan string, 8)
	conn, err := client.Dial(sock, "Remote Test App", func(id string) { dispatched <- id })
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ui, err := conn.Build(`
w=new window title="Remote Window" width=320 height=160 children={
	p=new panel layout=vbox children={
		cb=new checkbox caption="remote checkbox"
		inp=new textinput
		btn=new button caption="Go" action=remote.act
	}
}
wcb=w.p.cb
winp=w.p.inp
wbtn=w.p.btn
`)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, k := range []string{"w", "wcb", "winp", "wbtn"} {
		if ui.ID(k) == 0 {
			t.Fatalf("missing surfaced id %q", k)
		}
	}

	// The connection appears as a full Application with the window.
	var appNames []string
	var winCount int
	var serverCb *trinkets.Checkbox
	var serverInp *trinkets.TextInput
	var serverBtn *trinkets.Button
	onUI(desktop, func() {
		for _, a := range desktop.Applications() {
			appNames = append(appNames, a.Name())
			for _, w := range a.Windows() {
				winCount++
				if p, ok := w.Content().(*trinkets.Panel); ok {
					kids := p.Children()
					if len(kids) == 3 {
						serverCb, _ = kids[0].(*trinkets.Checkbox)
						serverInp, _ = kids[1].(*trinkets.TextInput)
						serverBtn, _ = kids[2].(*trinkets.Button)
					}
				}
			}
		}
	})
	if len(appNames) != 1 || appNames[0] != "Remote Test App" {
		t.Fatalf("applications = %v", appNames)
	}
	if winCount != 1 || serverCb == nil || serverInp == nil || serverBtn == nil {
		t.Fatalf("remote window content not found (windows=%d)", winCount)
	}

	// App -> display: write-through set lands in the real trinket.
	inp := ui.TextInput("winp")
	if err := inp.SetText("over the wire"); err != nil {
		t.Fatalf("SetText: %v", err)
	}
	var got string
	onUI(desktop, func() { got = serverInp.Text() })
	if got != "over the wire" {
		t.Errorf("server text = %q", got)
	}

	// Display -> app: user toggles; the event crosses the socket into
	// the replica and the handler.
	cb := ui.Checkbox("wcb")
	toggled := make(chan bool, 1)
	cb.OnToggle(func(s protocol.FlagState) { toggled <- s == protocol.FlagTrue })
	onUI(desktop, func() { serverCb.Toggle() })
	select {
	case v := <-toggled:
		if !v {
			t.Error("toggle state = false, want true")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("toggle event never arrived")
	}
	if !cb.Checked() {
		t.Error("replica not updated")
	}

	// Command dispatch across the seam: click -> command event ->
	// app-side dispatch sink.
	onUI(desktop, func() { serverBtn.Click() })
	select {
	case id := <-dispatched:
		if id != "remote.act" {
			t.Errorf("dispatched %q", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command never dispatched")
	}

	// Disconnect: the app and its windows leave the desktop.
	conn.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var apps int
		onUI(desktop, func() { apps = len(desktop.Applications()) })
		if apps == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("app still present after disconnect (%d)", apps)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The handshake hands the client its Application ObjectID, and the client
// can then set application-wide properties over the wire (Conn.SetApp), which
// land on the real server-side Application.
func TestHandshakeAppIDAndSetApp(t *testing.T) {
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
		<-exited
	}()

	conn, err := client.Dial(sock, "Settable App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The handshake carried a real app id.
	if conn.AppID() == 0 {
		t.Fatal("handshake did not report an application id")
	}

	// It matches the server-side application's ObjectID, which starts
	// single-window.
	var serverID uint64
	var before bool
	onUI(desktop, func() {
		for _, a := range desktop.Applications() {
			if a.Name() == "Settable App" {
				serverID = uint64(a.ObjectID())
				before = a.MultiWindow()
			}
		}
	})
	if serverID != conn.AppID() {
		t.Fatalf("handshake app id %d != server app id %d", conn.AppID(), serverID)
	}
	if before {
		t.Fatal("app should start single-window")
	}

	// Set an app-wide property over the wire; it lands on the real app.
	if _, err := conn.SetApp("multiwindow"); err != nil {
		t.Fatalf("SetApp: %v", err)
	}
	var after bool
	onUI(desktop, func() {
		for _, a := range desktop.Applications() {
			if a.Name() == "Settable App" {
				after = a.MultiWindow()
			}
		}
	})
	if !after {
		t.Error("SetApp(multiwindow) did not flip the server app to multi-window")
	}
}

// A solo connection puts the desktop into solo mode, and the host quits
// when the last window closes. (The visual tear-off - the main window on
// its own borderless surface - needs a real platform and is covered by a
// trinkets msPlatform test; here there is no surface to tear onto.)
func TestSoloModeReplacesDesktop(t *testing.T) {
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
	defer srv.Close()

	conn, err := client.DialSolo(sock, "Solo App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	ui, err := conn.Build(`
w=new window title="Solo" width=320 height=240 main tearable children={new panel layout=vbox children={new label caption="hi"}}
mb=new menubar children={new menu caption="File" children={new menuitem caption="Quit"}}
`)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// The build is posted async; wait for solo mode to apply.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var solo bool
		onUI(desktop, func() { solo = desktop.IsSolo() })
		if solo {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("desktop did not enter solo mode")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Closing the last window quits the host (peers; quit on last close).
	if _, err := conn.Exec(fmt.Sprintf("destroy %d", ui.ID("w"))); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Error("host did not quit after its last window closed")
	}
}

// The spawndesktop / gosolo app-verbs are consumed by the display (they
// reach the desktop's solo toggle), not passed through to the protocol
// session - which would reject them as unknown verbs. Their visual effect
// needs a real platform (covered by trinkets msPlatform tests); here we only
// assert the wiring accepts them.
func TestSpawnDesktopVerbsAccepted(t *testing.T) {
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

	go func() { desktop.Run() }()
	defer desktop.Quit()
	var srv *display.Server
	select {
	case srv = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("desktop did not start")
	}
	defer srv.Close()

	conn, err := client.Dial(sock, "Toggler", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for _, verb := range []string{"spawndesktop", "gosolo"} {
		if _, err := conn.Exec(verb); err != nil {
			t.Errorf("%s verb rejected: %v", verb, err)
		}
	}
}

// A window built as a child of an MDI pane must NOT also be adopted as a
// top-level application window - otherwise the same window object lives
// both on the desktop and in the pane (a linked "clone").
func TestMDIChildNotAdoptedAsAppWindow(t *testing.T) {
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

	conn, err := client.Dial(sock, "MDI App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// A host window whose content is an MDI pane.
	if _, err := conn.Build(`
w=new window title="Host" width=320 height=240 children={
	pane=new mdipane children={cp=new panel layout=vbox children={new label caption="pane"}}
}
pane=w.pane
`); err != nil {
		t.Fatalf("build host: %v", err)
	}

	// Spawn a document window INTO the pane.
	if _, err := conn.Exec(`set pane children={d=new window title="Doc" width=120 height=80 children={new label caption="doc"}}`); err != nil {
		t.Fatalf("spawn mdi child: %v", err)
	}

	// The application must own exactly one top-level window (the host);
	// the MDI document lives inside the pane, not on the desktop.
	var titles []string
	onUI(desktop, func() {
		for _, a := range desktop.Applications() {
			if a.Name() != "MDI App" {
				continue
			}
			for _, w := range a.Windows() {
				titles = append(titles, w.Title())
			}
		}
	})
	if len(titles) != 1 || titles[0] != "Host" {
		t.Errorf("app top-level windows = %v, want [Host] (MDI child must not be adopted)", titles)
	}
}

// The wire `tearable` and `main` properties must reach the adopted
// window: a "tearable main" window shows the tear handle and becomes the
// application's main window (its chrome detaches with it); a plain window
// does neither.
func TestWindowTearableAndMainFlags(t *testing.T) {
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

	conn, err := client.DialWith(sock, "Tear App", client.DialOptions{MultiWindow: true})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Build(`
main=new window title="Main" width=320 height=240 tearable main children={new label caption="m"}
`); err != nil {
		t.Fatalf("build main: %v", err)
	}
	if _, err := conn.Build(`
plain=new window title="Plain" width=200 height=120 children={new label caption="p"}
`); err != nil {
		t.Fatalf("build plain: %v", err)
	}

	onUI(desktop, func() {
		for _, a := range desktop.Applications() {
			if a.Name() != "Tear App" {
				continue
			}
			mw := a.MainWindow()
			if mw == nil || mw.Title() != "Main" {
				t.Errorf("app MainWindow = %v, want the \"Main\" window", mw)
			}
			for _, w := range a.Windows() {
				switch w.Title() {
				case "Main":
					if !w.IsTearable() {
						t.Error(`"Main" window is not tearable`)
					}
				case "Plain":
					if w.IsTearable() {
						t.Error(`"Plain" window is unexpectedly tearable`)
					}
				}
			}
		}
	})
}

// The display layer enforces the per-type window creation rules: normal
// windows need multiwindow, a dialog needs an owner, and modal/toolpalette
// are always allowed.
func TestWindowTypeCreationGating(t *testing.T) {
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
		<-exited
	}()

	// A single-window app (no multiwindow). A rejected window is closed and
	// not adopted (soft rejection: no Build error), so assert on which
	// windows the app actually holds by title.
	conn, err := client.Dial(sock, "Gate App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	hasWindow := func(title string) bool {
		found := false
		onUI(desktop, func() {
			for _, a := range desktop.Applications() {
				if a.Name() != "Gate App" {
					continue
				}
				for _, w := range a.Windows() {
					if w.Title() == title {
						found = true
					}
				}
			}
		})
		return found
	}

	// The first window defaults to the app's main window: accepted.
	mui, err := conn.Build(`main=new window title="Main" width=320 height=240 children={new label caption="m"}`)
	if err != nil {
		t.Fatalf("build first window: %v", err)
	}
	mainID := mui.ID("main")
	if mainID == 0 {
		t.Fatal("main window id missing")
	}
	if !hasWindow("Main") {
		t.Error("the first (main) window should be adopted")
	}

	// A second normal window without multiwindow is rejected.
	if _, err := conn.Build(`plain=new window title="Plain" width=200 height=120 children={new label caption="p"}`); err != nil {
		t.Fatalf("build plain: %v", err)
	}
	if hasWindow("Plain") {
		t.Error("a second normal window without multiwindow should be rejected")
	}

	// A dialog with no owner is rejected; with an owner it is accepted.
	if _, err := conn.Build(`d=new window type="dialog" title="DlgNoOwner" width=200 height=120 children={new label caption="d"}`); err != nil {
		t.Fatalf("build dialog no owner: %v", err)
	}
	if hasWindow("DlgNoOwner") {
		t.Error("a dialog without an owner should be rejected")
	}
	if _, err := conn.Build(fmt.Sprintf(`d=new window type="dialog" owner=%d title="DlgOwned" width=200 height=120 children={new label caption="d"}`, mainID)); err != nil {
		t.Fatalf("build dialog with owner: %v", err)
	}
	if !hasWindow("DlgOwned") {
		t.Error("a dialog with an owner should be accepted")
	}

	// Modal and tool palette are always allowed, even without multiwindow.
	if _, err := conn.Build(`m=new window type="modal" title="ModalW" width=200 height=120 children={new label caption="m"}`); err != nil {
		t.Fatalf("build modal: %v", err)
	}
	if !hasWindow("ModalW") {
		t.Error("a modal should always be allowed")
	}
	if _, err := conn.Build(`t=new window type="toolpalette" title="ToolW" width=200 height=120 children={new label caption="t"}`); err != nil {
		t.Fatalf("build toolpalette: %v", err)
	}
	if !hasWindow("ToolW") {
		t.Error("a tool palette should always be allowed")
	}
}
