package interop_test

// Cross-language interop: a REAL Go display host, driven by a client
// written in another language over an actual unix socket. The host is the
// same display.Serve that powers kittytk-tui / kittytk-sdl; only the
// client language changes.
//
// The harness stands up a headless desktop, launches the client
// subprocess (Python here), waits for it to build its window and
// subscribe (the client prints READY), then drives server-side input -
// toggling a checkbox and clicking a button - and confirms the client
// received the events. It also reads a trinket's text back to prove the
// app->host write-through direction. If the wire protocol matches, a
// non-Go client is indistinguishable from a Go one.

import (
	"bufio"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/style"
)

// nullBackend is a headless RenderBackend (copy; test-only, as elsewhere).
type nullBackend struct{ mu sync.Mutex }

func (n *nullBackend) Init() error { return nil }
func (n *nullBackend) Shutdown()   {}
func (n *nullBackend) Metrics() core.CellMetrics {
	return core.CellMetrics{CellWidth: 8, CellHeight: 16}
}
func (n *nullBackend) Size() core.UnitSize {
	return core.UnitSize{Width: 8 * 120, Height: 16 * 40}
}
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

func onUI(d *trinkets.Desktop, fn func()) {
	done := make(chan struct{})
	d.Post(func() { fn(); close(done) })
	<-done
}

func startService(t *testing.T) (*trinkets.Desktop, string, func()) {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "display.sock")

	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})
	desktop.SetOnStartup(func() {
		if _, err := display.Serve(desktop, sock); err != nil {
			t.Errorf("serve: %v", err)
			desktop.Quit()
		}
	})

	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if c, err := (&net.Dialer{}).Dial("unix", sock); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("display service did not start")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return desktop, sock, func() {
		desktop.Quit()
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("desktop did not exit")
		}
	}
}

// findRemoteTrinkets walks the desktop for the interop app's window and
// returns its checkbox / textinput / button (the smoke builds exactly one
// panel with these three).
func findRemoteTrinkets(d *trinkets.Desktop) (*trinkets.Checkbox, *trinkets.TextInput, *trinkets.Button) {
	var cb *trinkets.Checkbox
	var inp *trinkets.TextInput
	var btn *trinkets.Button
	onUI(d, func() {
		for _, a := range d.Applications() {
			for _, w := range a.Windows() {
				p, ok := w.Content().(*trinkets.Panel)
				if !ok {
					continue
				}
				kids := p.Children()
				if len(kids) != 3 {
					continue
				}
				cb, _ = kids[0].(*trinkets.Checkbox)
				inp, _ = kids[1].(*trinkets.TextInput)
				btn, _ = kids[2].(*trinkets.Button)
			}
		}
	})
	return cb, inp, btn
}

// runInteropClient drives one client subprocess through the full exchange
// over the default unix socket.
func runInteropClient(t *testing.T, name string, argv ...string) {
	desktop, sock, stop := startService(t)
	defer stop()
	exchange(t, name, desktop, sock, nil, argv...)
}

// exchange launches the client with endpoint as its last argument (plus
// any extra env) and drives the full bidirectional handshake against the
// running desktop.
func exchange(t *testing.T, name string, desktop *trinkets.Desktop, endpoint string, env []string, argv ...string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	full := append(argv, endpoint)
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("%s: stdout pipe: %v", name, err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("%s: start (is it built/available?): %v", name, err)
	}

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()

	seen := map[string]bool{}
	await := func(marker string, dur time.Duration) {
		deadline := time.After(dur)
		for {
			if seen[marker] {
				return
			}
			select {
			case ln, ok := <-lines:
				if !ok {
					t.Fatalf("%s: client exited before %q (stderr: %s)", name, marker, stderr.String())
				}
				t.Logf("%s > %s", name, ln)
				seen[strings.SplitN(ln, " ", 2)[0]] = true
			case <-deadline:
				t.Fatalf("%s: timed out waiting for %q (stderr: %s)", name, marker, stderr.String())
			}
		}
	}

	// 1. The client builds + subscribes, then prints READY.
	await("READY", 15*time.Second)

	// 2. app -> host write-through landed in the real trinket.
	cb, inp, btn := findRemoteTrinkets(desktop)
	if cb == nil || inp == nil || btn == nil {
		t.Fatalf("%s: interop trinkets not found on the desktop", name)
	}
	var got string
	onUI(desktop, func() { got = inp.Text() })
	if got != "over the wire" {
		t.Errorf("%s: host-side textinput = %q, want %q", name, got, "over the wire")
	}

	// 3. host -> app: drive input; the client must receive both events.
	onUI(desktop, func() { cb.Toggle() })
	onUI(desktop, func() { btn.Click() })

	await("TOGGLE", 10*time.Second)
	await("COMMAND", 10*time.Second)
	await("DONE", 10*time.Second)

	if err := cmd.Wait(); err != nil {
		t.Fatalf("%s: client exited non-zero: %v (stderr: %s)", name, err, stderr.String())
	}
}

func TestPythonClientInterop(t *testing.T) {
	runInteropClient(t, "python", "python3", "interop_smoke.py")
}

// runBuildSmoke runs a client that builds the full demo against the host
// and prints OK on success (no input driving needed).
func runBuildSmoke(t *testing.T, name string, argv ...string) {
	_, sock, stop := startService(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	full := append(argv, sock)
	cmd := exec.CommandContext(ctx, full[0], full[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: client failed: %v\n%s", name, err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("%s: client did not report OK:\n%s", name, out)
	}
	t.Logf("%s demo build: OK", name)
}

func TestPythonDemoBuildsOverService(t *testing.T) {
	runBuildSmoke(t, "python", "python3", "demoapp_smoke.py")
}
