package interop_c_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
)

// buildCPty compiles the ptydriver smoke: kittytk.c + scripts.c +
// ptydriver.c, linking libutil for forkpty.
func buildCPty(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "ptysmoke")
	cmd := exec.Command("cc", "-std=c11", "-O2", "-o", bin,
		filepath.Join("..", "ptydriver_smoke.c"),
		filepath.Join("..", "kittytk.c"),
		filepath.Join("..", "scripts.c"),
		filepath.Join("..", "ptydriver.c"),
		"-lpthread", "-lutil")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cc ptydriver_smoke: %v\n%s", err, out)
	}
	return bin
}

// findTerminal walks the desktop for the first PurfecTerm surface.
func findTerminal(d *trinkets.Desktop) *trinkets.PurfecTerm {
	var found *trinkets.PurfecTerm
	onUI(d, func() {
		for _, a := range d.Applications() {
			for _, w := range a.Windows() {
				if term, ok := w.Content().(*trinkets.PurfecTerm); ok {
					found = term
					return
				}
			}
		}
	})
	return found
}

// terminalText reads the terminal's visible cells as a flat string.
func terminalText(d *trinkets.Desktop, term *trinkets.PurfecTerm) string {
	var sb strings.Builder
	onUI(d, func() {
		cli := term.Terminal()
		if cli == nil {
			return
		}
		for _, row := range cli.GetCells() {
			for _, cell := range row {
				if cell.Char != 0 {
					sb.WriteRune(cell.Char)
				}
			}
		}
	})
	return sb.String()
}

// TestCPtyDriver proves the client-side PTY round trip: the C client spawns
// a PTY, streams its output to the terminal via feed= (output path), and
// writes the terminal's input events back to the PTY (input path). The
// child prints a marker then execs cat, so:
//   - the printed marker must appear in the terminal buffer (feed works)
//   - characters typed into the terminal server-side must echo back through
//     the PTY into the buffer (input events reach the child)
func TestCPtyDriver(t *testing.T) {
	bin := buildCPty(t)
	desktop, sock, stop := startService(t)
	defer stop()

	// The child: print a deterministic marker, then cat to echo our input.
	script := filepath.Join(t.TempDir(), "child.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf OUTMARK\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, sock)
	cmd.Env = append(os.Environ(), "PTY_SMOKE_SHELL="+script)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()

	// Wait for the terminal to appear on the desktop.
	var term *trinkets.PurfecTerm
	deadline := time.Now().Add(10 * time.Second)
	for term == nil && time.Now().Before(deadline) {
		term = findTerminal(desktop)
		if term == nil {
			time.Sleep(50 * time.Millisecond)
		}
	}
	if term == nil {
		t.Fatalf("terminal never appeared on the desktop\n%s", out.String())
	}

	// Output path: the child's printed marker must feed into the buffer.
	if !waitForText(desktop, term, "OUTMARK", 10*time.Second) {
		t.Fatalf("terminal never received the child's output %q\ngot: %q\nclient: %s",
			"OUTMARK", terminalText(desktop, term), out.String())
	}

	// Input path: type a marker; the PTY (line-discipline echo + cat) sends
	// it back through feed=, so it lands in the buffer.
	onUI(desktop, func() {
		term.HandleFocusIn()
		for _, ch := range "inmark" {
			term.HandleKeyPress(core.KeyPressEvent{Key: string(ch)})
		}
	})
	if !waitForText(desktop, term, "inmark", 10*time.Second) {
		t.Fatalf("typed input never round-tripped through the client PTY\ngot: %q\nclient: %s",
			terminalText(desktop, term), out.String())
	}
}

// waitForText polls the terminal buffer until it contains want or times out.
func waitForText(d *trinkets.Desktop, term *trinkets.PurfecTerm, want string, dur time.Duration) bool {
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		if strings.Contains(terminalText(d, term), want) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
