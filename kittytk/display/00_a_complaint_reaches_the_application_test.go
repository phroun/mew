package display

// A complaint travelling to the application that caused it.
//
// The whole path, over a real socket: an application names a bundle, the bundle
// loads with something to say about it, and what it said comes back on the reply
// to the very batch that named it. Before this it went nowhere at all -- the
// display built it, held it, and dropped it -- which is indistinguishable from
// nothing having gone wrong.
//
// It is a report and not a refusal, so the batch is answered with its reply and
// the objects it made are there. See wire.TroubleVerb.

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/kittytk/wire"
)

// quietBackend is a desktop with nowhere to draw, which is every display test
// here: what is being watched is the wire.
type quietBackend struct{}

func (quietBackend) Init() error { return nil }
func (quietBackend) Shutdown()   {}
func (quietBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{UnitsPerCellWidth: 8, UnitsPerCellHeight: 16}
}
func (quietBackend) Size() core.UnitSize {
	return core.UnitSize{Width: 8 * 80, Height: 16 * 24}
}
func (quietBackend) BeginFrame()                                          {}
func (quietBackend) EndFrame()                                            {}
func (quietBackend) Clear(style.CellStyle)                                {}
func (quietBackend) SetClip(core.UnitRect)                                {}
func (quietBackend) DrawCell(core.Unit, core.Unit, rune, style.CellStyle) {}
func (quietBackend) DrawText(core.Unit, core.Unit, string, style.CellStyle, *core.Font) core.Unit {
	return 0
}
func (quietBackend) DrawTextAligned(core.UnitRect, string, core.HSide, core.VAlign, style.CellStyle, *core.Font) {
}
func (quietBackend) FillRect(core.UnitRect, rune, style.CellStyle)                     {}
func (quietBackend) DrawRect(core.UnitRect, style.BorderStyle, style.CellStyle)        {}
func (quietBackend) DrawHLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (quietBackend) DrawVLine(core.Unit, core.Unit, core.Unit, rune, style.CellStyle)  {}
func (quietBackend) DrawBox(core.UnitRect, style.BorderStyle, string, style.CellStyle) {}
func (quietBackend) PollEvent() core.Event                                             { return nil }
func (quietBackend) WaitEvent() core.Event                                             { return nil }
func (quietBackend) SetCursorVisible(bool)                                             {}
func (quietBackend) SetCursorPosition(core.Unit, core.Unit)                            {}
func (quietBackend) SetCursorStyle(int)                                                {}
func (quietBackend) SupportsColor() bool                                               { return true }
func (quietBackend) SupportsMouse() bool                                               { return true }
func (quietBackend) SupportsUnicode() bool                                             { return true }
func (quietBackend) ColorDepth() int                                                   { return 256 }
func (quietBackend) GetClipboard() string                                              { return "" }
func (quietBackend) SetClipboard(string)                                               {}
func (quietBackend) Beep()                                                             {}

// serving is a desktop serving one socket, from inside the package -- what the
// connection knows about itself is what this test is about.
func serving(t *testing.T, sock string) (*trinkets.Desktop, *Server, func()) {
	t.Helper()
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(quietBackend{})

	ready := make(chan *Server, 1)
	desktop.SetOnStartup(func() {
		srv, err := Serve(desktop, sock)
		if err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
			return
		}
		ready <- srv
	})
	go desktop.Run()

	var srv *Server
	select {
	case srv = <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("the desktop did not start")
	}
	return desktop, srv, func() {
		srv.Close()
		desktop.Quit()
	}
}

// muddled is a bundle whose tree hint cannot mean what it says: two ways down at
// once. The records are all still there, so it loads -- and says why it is not a
// tree, which is the complaint this test follows.
const muddled = `(
  _bundle: (
    key: "muddle", version: "1.0.0",
    tree: ( parent: "up", location: "where", delimiter: "/" )
  ),
  ("a record")
)`

func TestAComplaintReachesTheApplicationThatCausedIt(t *testing.T) {
	tempConfig(t) // XDG_CONFIG_HOME, so the shelves below are this test's own
	sock := filepath.Join(t.TempDir(), "display.sock")
	desktop, srv, stop := serving(t, sock)
	defer stop()

	conn, err := client.Dial(sock, "Papers App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Being let in is what gives this application a shelf to keep a bundle on, so
	// the bundle goes there once the display has admitted it.
	var host, item string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range srv.known.all() {
			if r.app == "Papers App" {
				host, item = srv.known.folders(r.identity, "Papers App")
			}
		}
		if host != "" && item != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if host == "" || item == "" {
		t.Fatal("the application connected and the desktop keeps nothing for it")
	}
	stock(t, newAppStore(host, item), "muddle-1.0.0", muddled)

	// The batch that names it. It is answered with a reply, the load having
	// finished: a complaint is not a refusal.
	reply, err := conn.Exec(`lv=new listview source="bundle:muddle"`)
	if err != nil {
		t.Fatalf("the batch was refused: %v", err)
	}
	if reply.IDs["lv"] == 0 {
		t.Error("the list was not made, so the complaint refused something after all")
	}

	// And what the load had to say came back on it.
	if len(reply.Trouble) == 0 {
		t.Fatal("the bundle loaded with something to say and the reply carried none of it")
	}
	got := reply.Trouble[0]
	if got.About != "bundle:muddle" {
		t.Errorf("it is about %q, want the name the statement used", got.About)
	}
	if !strings.Contains(got.Text, "two ways down") {
		t.Errorf("it reads %q, want what the loader said", got.Text)
	}

	// The display keeps its own copy, which is the floor under all of this: a load
	// with no statement behind it has nobody to tell.
	held := desktop.Reported()
	if len(held) == 0 || !strings.Contains(held[0], "two ways down") {
		t.Errorf("the display's own log holds %v", held)
	}
}

// A complaint with nothing in it is not said.
//
// It would cross as `trouble text=""`, and what a reader makes of a row with no
// reason on it is that there is a problem and nothing else -- which is worse than
// not being told. Whoever noticed nothing says nothing.
func TestAComplaintWithNothingInItIsNotSaid(t *testing.T) {
	c := &conn{}
	c.trouble(wire.Trouble{About: "bundle:papers"})
	if len(c.troubles) != 0 {
		t.Errorf("a complaint with no reason was held: %+v", c.troubles)
	}
	c.trouble(wire.Trouble{About: "bundle:papers", Text: "no such include"})
	if len(c.troubles) != 1 {
		t.Fatalf("a complaint with a reason was not held: %+v", c.troubles)
	}
}
