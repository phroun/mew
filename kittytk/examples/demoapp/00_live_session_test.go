package main

// A headless integration test: it stands up a real display service over
// a unix socket (the same one kittytk-sdl serves) and drives the demo's
// shell-free scripts through it, proving the deep surfacing paths, the
// trinket properties across the gallery, and the desktop-action app verbs
// are all accepted by the live session - things parsing alone can't
// catch. Terminal-bearing scripts (which would spawn a shell) are left
// to the parse test.
//
// This test imports the display service and trinkets; the production
// demoapp binary does not (see the backendless go-list check).

import (
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/style"
)

// surfacedKeys reads the script's top-level surfacing statements -- the
// `name=path.to.thing` lines that lift a nested key to where a client can
// address it. Derived rather than listed, so adding a tab and forgetting to
// surface its keys fails here instead of shipping a tab that does nothing.
var surfacingLine = regexp.MustCompile(`(?m)^([A-Za-z_]\w*)=([A-Za-z_]\w*(?:\.\w+)+)\s*$`)

func surfacedKeys(script string) []string {
	var out []string
	for _, m := range surfacingLine.FindAllStringSubmatch(script, -1) {
		out = append(out, m[1])
	}
	return out
}

// wiredNames reads the names one wiring function addresses -- ui.Object("x"),
// ui.TextInput("x") and friends. Every one has to be a key that function's
// build script surfaced, or the handle is inert.
//
// Scoped to a single function on purpose: wire.go wires several different
// builds (the protocol window, an MDI child, a secondary window), each with a
// UI of its own, and a name from one build is nothing to another.
var wiredName = regexp.MustCompile(`\bui\.[A-Z]\w*\("([^"]+)"\)`)

func wiredNames(t *testing.T, funcName string) []string {
	t.Helper()
	src, err := os.ReadFile("wire.go")
	if err != nil {
		t.Fatalf("reading wire.go: %v", err)
	}
	body := string(src)
	start := strings.Index(body, "func (a *app) "+funcName+"(")
	if start < 0 {
		t.Fatalf("wire.go has no %s", funcName)
	}
	body = body[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range wiredName.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s addresses no names; the scan is broken", funcName)
	}
	return out
}

// nullBackend is a headless RenderBackend.
type nullBackend struct{ mu sync.Mutex }

func (n *nullBackend) Init() error { return nil }
func (n *nullBackend) Shutdown()   {}
func (n *nullBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (n *nullBackend) Size() core.UnitSize                                  { return core.UnitSize{Width: 8 * 120, Height: 16 * 40} }
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
func (n *nullBackend) SetCursorStyle(int)                                                {}
func (n *nullBackend) SupportsColor() bool                                               { return true }
func (n *nullBackend) SupportsMouse() bool                                               { return true }
func (n *nullBackend) SupportsUnicode() bool                                             { return true }
func (n *nullBackend) ColorDepth() int                                                   { return 256 }
func (n *nullBackend) GetClipboard() string                                              { return "" }
func (n *nullBackend) SetClipboard(string)                                               {}
func (n *nullBackend) Beep()                                                             {}

func startService(t *testing.T) (sock string, stop func()) {
	t.Helper()
	sock = filepath.Join(t.TempDir(), "display.sock")

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})
	// The desktop's own windowless application (as in kittytk-sdl).
	desktop.SetOnStartup(func() {
		if _, err := display.Serve(desktop, sock); err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
		}
	})

	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()

	// Wait for the socket to appear.
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := (&net.Dialer{}).Dial("unix", sock); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("display service did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	return sock, func() {
		desktop.Quit()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("desktop did not exit")
		}
	}
}

func TestDemoBuildsOverService(t *testing.T) {
	sock, stop := startService(t)
	defer stop()

	conn, err := client.DialWith(sock, "KittyTK Demo", client.DialOptions{MultiWindow: true})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The whole main window, its menu bar and status bar, in one build.
	ui, err := conn.Build(mainBuildScript())
	if err != nil {
		t.Fatalf("build main: %v", err)
	}
	// Two checks, because there are two ways to get this wrong and only one
	// of them is a bad path.
	//
	// First: every surfacing statement in the script must resolve. This
	// catches a path that names something that moved or was renamed.
	surfaced := surfacedKeys(mainBuildScript())
	if len(surfaced) < 20 {
		t.Fatalf("found %d surfacing statements, expected the script to have many more", len(surfaced))
	}
	for _, key := range append(surfaced, "w", "tabs", "mb", "sb") {
		if ui.ID(key) == 0 {
			t.Errorf("surfaced id %q missing — the path it names does not resolve", key)
		}
	}

	// Second, and the one that actually bites: every name the WIRING asks
	// for must be surfaced. A name that is not resolves to id 0, and id 0 is
	// quiet -- handle() skips the subscription without a word, so the events
	// never arrive and Set goes nowhere. A whole tab can be wired to nothing
	// and merely look inert. Reading the names out of wire.go is blunt, but
	// it is the only thing that fails when someone adds a control and
	// forgets to surface it, which is exactly how this test got written.
	for _, name := range wiredNames(t, "wireMainWindow") {
		if ui.ID(name) == 0 {
			t.Errorf("wire.go addresses %q but the script never surfaces it: "+
				"its handle is id 0, so nothing it sets or subscribes will happen", name)
		}
	}

	// The protocol companion window and the About dialog.
	if _, err := conn.Build(protocolWindowScript); err != nil {
		t.Fatalf("build protocol window: %v", err)
	}
	if _, err := conn.Exec(aboutDialogScript); err != nil {
		t.Fatalf("about dialog: %v", err)
	}

	// MDI: spawning a document appends a window into the pane, and a
	// dock set references the surfaced dock by name across builds.
	child, err := conn.Build(mdiChildScript(1))
	if err != nil {
		t.Fatalf("mdi child: %v", err)
	}
	winID := child.ID("wwin")
	if winID == 0 {
		t.Fatal("mdi child window id missing")
	}
	if _, err := conn.Exec("set mdidock children={e1=new dockentry caption=\"Document 1\" window=" + strconv.FormatUint(winID, 10) + "}"); err != nil {
		t.Fatalf("dock entry: %v", err)
	}

	// The tab background property (the bg radios' effect).
	if err := ui.Object("tabs").Set("background=green"); err != nil {
		t.Errorf("tabs background=green: %v", err)
	}
	if err := ui.Object("tabs").Set(`background="#333333"`); err != nil {
		t.Errorf("tabs background rgb: %v", err)
	}
	if err := ui.Object("tabs").Set("background=default"); err != nil {
		t.Errorf("tabs background=default: %v", err)
	}

	// The window font / denomination properties.
	if err := ui.Object("w").Set(`font="tuesday12"`); err != nil {
		t.Errorf("window font: %v", err)
	}
	if err := ui.Object("w").Set("denomination=32"); err != nil {
		t.Errorf("window denomination: %v", err)
	}

	// Every desktop-action app verb must be accepted by the display.
	for _, verb := range []string{
		"status text=\"hi there\"", "cut", "copy", "paste", "selectall",
		"tile", "cascade", "theme", "desktopfont tuesday",
		"desktopfont default", "announce_visual", "announce_speak",
		"rawkey",
	} {
		if _, err := conn.Exec(verb); err != nil {
			t.Errorf("app verb %q: %v", verb, err)
		}
	}
}

func TestSecondaryBuildsOverService(t *testing.T) {
	sock, stop := startService(t)
	defer stop()

	conn, err := client.Dial(sock, "App 1", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	ui, err := conn.Build(secondaryBuildScript(1))
	if err != nil {
		t.Fatalf("build secondary: %v", err)
	}
	for _, key := range []string{"w", "closer", "term", "mb", "sb"} {
		if ui.ID(key) == 0 {
			t.Errorf("secondary surfaced id %q missing", key)
		}
	}
}
