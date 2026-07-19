package interop_c_test

// Cross-language interop for the C client: the same real Go display host,
// driven by a C program over an actual unix socket. The harness compiles
// the C smoke with cc, launches it, then drives server-side input and
// confirms the C client received the events - proving a C client is
// indistinguishable on the wire from a Go one.

import (
	"bufio"
	"context"
	"fmt"
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

// buildC compiles ../<src> + ../kittytk.c into a temp binary (the C
// sources live one dir up so this Go test package holds no .c files).
func buildC(t *testing.T, src string) string {
	t.Helper()
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "smoke")
	cmd := exec.Command("cc", "-std=c11", "-O2", "-o", bin,
		filepath.Join("..", src), filepath.Join("..", "kittytk.c"),
		filepath.Join("..", "scripts.c"), "-lpthread")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cc %s: %v\n%s", src, err, out)
	}
	return bin
}

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

func TestCClientInterop(t *testing.T) {
	bin := buildC(t, "interop_smoke.c")
	desktop, sock, stop := startService(t)
	defer stop()
	driveCInterop(t, desktop, bin, sock, nil)
}

// driveCInterop runs the C interop smoke with endpoint as its last
// argument (plus any extra env) and drives the full bidirectional
// exchange against the running desktop.
func driveCInterop(t *testing.T, desktop *trinkets.Desktop, bin, endpoint string, env []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, endpoint)
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
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
					t.Fatalf("c: client exited before %q (stderr: %s)", marker, stderr.String())
				}
				t.Logf("c > %s", ln)
				seen[strings.SplitN(ln, " ", 2)[0]] = true
			case <-deadline:
				t.Fatalf("c: timed out waiting for %q (stderr: %s)", marker, stderr.String())
			}
		}
	}

	await("READY", 15*time.Second)

	cb, inp, btn := findRemoteTrinkets(desktop)
	if cb == nil || inp == nil || btn == nil {
		t.Fatal("c: interop trinkets not found on the desktop")
	}
	var got string
	onUI(desktop, func() { got = inp.Text() })
	if got != "over the wire" {
		t.Errorf("c: host-side textinput = %q, want %q", got, "over the wire")
	}

	onUI(desktop, func() { cb.Toggle() })
	onUI(desktop, func() { btn.Click() })

	await("TOGGLE", 10*time.Second)
	await("COMMAND", 10*time.Second)
	await("DONE", 10*time.Second)

	if err := cmd.Wait(); err != nil {
		t.Fatalf("c: client exited non-zero: %v (stderr: %s)", err, stderr.String())
	}
}

func TestCDemoBuildsOverService(t *testing.T) {
	bin := buildC(t, "demoapp_smoke.c")
	_, sock, stop := startService(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, sock).CombinedOutput()
	if err != nil {
		t.Fatalf("c demo build failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("c demo build did not report OK:\n%s", out)
	}
	t.Logf("c demo build: OK")
}

// findCounter walks the desktop for the counter app's window: a panel with
// a label over a button.
func findCounter(d *trinkets.Desktop) (*trinkets.Label, *trinkets.Button) {
	var lbl *trinkets.Label
	var btn *trinkets.Button
	onUI(d, func() {
		for _, a := range d.Applications() {
			for _, w := range a.Windows() {
				p, ok := w.Content().(*trinkets.Panel)
				if !ok {
					continue
				}
				kids := p.Children()
				if len(kids) != 2 {
					continue
				}
				l, okl := kids[0].(*trinkets.Label)
				b, okb := kids[1].(*trinkets.Button)
				if okl && okb {
					lbl, btn = l, b
				}
			}
		}
	})
	return lbl, btn
}

// TestCCounterExample proves the minimal counter example works end to end:
// clicking the button (server-side) makes the C app rewrite the label.
func TestCCounterExample(t *testing.T) {
	bin := buildC(t, "counter.c")
	desktop, sock, stop := startService(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin) // counter takes no arg; find the host via env
	cmd.Env = append(os.Environ(), "KITTYTK_DISPLAY="+sock)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start counter: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// Wait for the counter window to appear on the desktop.
	var lbl *trinkets.Label
	var btn *trinkets.Button
	deadline := time.Now().Add(10 * time.Second)
	for {
		lbl, btn = findCounter(desktop)
		if lbl != nil && btn != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("counter window never appeared (stderr: %s)", stderr.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	// The initial caption.
	var got string
	onUI(desktop, func() { got = lbl.Text() })
	if got != "Count: 0" {
		t.Errorf("initial label = %q, want %q", got, "Count: 0")
	}

	// Give the client a moment to send its click subscription, then click,
	// confirming each increment before the next (robust to the sub race).
	time.Sleep(300 * time.Millisecond)
	for want := 1; want <= 3; want++ {
		onUI(desktop, func() { btn.Click() })
		ok := false
		for i := 0; i < 100; i++ {
			onUI(desktop, func() { got = lbl.Text() })
			if got == fmt.Sprintf("Count: %d", want) {
				ok = true
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if !ok {
			t.Fatalf("after click %d, label = %q, want Count: %d", want, got, want)
		}
	}
	t.Logf("counter reached %q after 3 clicks", got)
}
