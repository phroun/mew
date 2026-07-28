package editor

import (
	"os"
	"strings"
	"testing"
)

// The emergency path must be able to ask the HOST to restore the terminal. An
// embedded mew renders into the host's surface, so Renderer.Cleanup restores
// nothing the user can see; only the host can leave the alternate screen, drop
// raw mode and pop its keyboard protocol. os.Exit runs no deferred teardown on
// either side, so this hook is the only route.
func TestRestoreHostTerminalHookPlumbing(t *testing.T) {
	e, _ := newTestEditor(t, "unsaved work\n")
	if e.Config.RestoreHostTerminal != nil {
		t.Error("no host hook should be set unless an embedder wires one")
	}
	called := 0
	e.Config.RestoreHostTerminal = func() { called++ }
	e.Config.RestoreHostTerminal()
	if called != 1 {
		t.Errorf("hook invoked %d times, want 1", called)
	}
}

// The dump is the irreplaceable half of an emergency exit: a restore that hung
// or panicked ahead of it would take the user's unsaved buffers with it. This
// reads the source rather than running it, because emergencyExit ends in
// os.Exit and cannot be called from a test — so it is a guard on the ORDER
// surviving future edits, not a behavioural test.
func TestEmergencyExitDumpsBeforeRestoringTheTerminal(t *testing.T) {
	src, err := os.ReadFile("deadcat.go")
	if err != nil {
		t.Fatalf("read deadcat.go: %v", err)
	}
	body := string(src)
	start := strings.Index(body, "func (e *Editor) emergencyExit(")
	if start < 0 {
		t.Fatal("emergencyExit not found")
	}
	body = body[start:]
	if end := strings.Index(body, "\n}\n"); end > 0 {
		body = body[:end]
	}

	dump := strings.Index(body, "e.DumpDeadcat(")
	restore := strings.Index(body, "e.Config.RestoreHostTerminal()")
	exit := strings.Index(body, "os.Exit(")
	switch {
	case dump < 0:
		t.Fatal("emergencyExit no longer dumps DEADCAT")
	case restore < 0:
		t.Fatal("emergencyExit no longer restores the host terminal")
	case exit < 0:
		t.Fatal("emergencyExit no longer exits")
	case dump > restore:
		t.Error("the DEADCAT dump must come BEFORE the host terminal restore: " +
			"unsaved work is irreplaceable, a tidy prompt is not")
	case restore > exit:
		t.Error("the host restore must come before os.Exit, which runs no deferred teardown")
	}
}
