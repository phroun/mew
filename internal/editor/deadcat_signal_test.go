//go:build unix

package editor

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// TestDeadcatSignalHelper is the child process for TestDeadcatSignalE2E. It
// builds a live editor with a modified buffer, arms the REAL crash-dump signal
// handlers (usingOSFS + no virtualized terminal), announces readiness, and
// blocks until a signal dumps DEADCAT and exits the process. In a normal run
// it is skipped (the env gate is unset).
func TestDeadcatSignalHelper(t *testing.T) {
	if os.Getenv("MEW_DEADCAT_HELPER") != "1" {
		t.Skip("child process only")
	}
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = os.Getenv("MEW_DEADCAT_COLD")
	e, err := New(cfg) // real terminal + OS FS: the signal handlers arm
	if err != nil {
		os.Stderr.WriteString("helper New: " + err.Error() + "\n")
		os.Exit(2)
	}
	// Point the dump at the parent's temp dirs (not the real config/cwd).
	e.deadcat = deadcatPlan{
		configTarget: filepath.Join(os.Getenv("MEW_DEADCAT_CFGDIR"), "DEADCAT"),
		cwd:          os.Getenv("MEW_DEADCAT_CWD"),
	}
	buf := buffer.NewFromString("chapter one\n")
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buf, SetFocus: true,
	})
	e.insertText("UNSAVED ") // now the buffer is modified

	stop := e.installDeadcatSignals()
	defer stop()

	os.Stdout.WriteString("READY\n")
	os.Stdout.Sync()
	select {} // wait for the signal; emergencyExit ends the process
}

// TestDeadcatSignalE2E launches mew's own test binary as a child, waits for it
// to arm, sends it a real SIGHUP, and verifies the live signal handler wrote
// DEADCAT containing the unsaved buffer content.
func TestDeadcatSignalE2E(t *testing.T) {
	cfgDir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestDeadcatSignalHelper$", "-test.v=true")
	cmd.Env = append(os.Environ(),
		"MEW_DEADCAT_HELPER=1",
		"MEW_DEADCAT_CFGDIR="+cfgDir,
		"MEW_DEADCAT_CWD="+t.TempDir(),
		"MEW_DEADCAT_COLD="+t.TempDir(),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}

	ready := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			if strings.Contains(sc.Text(), "READY") {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			_ = cmd.Process.Kill()
			t.Fatalf("child never armed; stderr:\n%s", stderr.String())
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("timeout waiting for child to arm; stderr:\n%s", stderr.String())
	}

	if err := cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatalf("send SIGHUP: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done: // a non-zero exit (os.Exit(1)) is expected
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("child did not exit after SIGHUP")
	}

	data, err := os.ReadFile(filepath.Join(cfgDir, "DEADCAT"))
	if err != nil {
		t.Fatalf("DEADCAT not written by the live signal handler: %v\nstderr:\n%s", err, stderr.String())
	}
	got := string(data)
	if !strings.Contains(got, "UNSAVED chapter one") {
		t.Fatalf("DEADCAT missing the unsaved content:\n%s", got)
	}
	if !strings.Contains(got, "aborted on") || !strings.Contains(got, "hangup") {
		t.Fatalf("DEADCAT header/reason wrong (want abort + SIGHUP 'hangup'):\n%s", got)
	}
}
