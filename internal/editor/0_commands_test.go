package editor

import "testing"

// show_desktop / hide_desktop invoke the host hooks when wired, and are safe
// no-ops when they are not (the standalone editor never sets them).
func TestShowHideDesktopCommandsInvokeHooks(t *testing.T) {
	var shown, hidden int
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.ShowDesktop = func() { shown++ }
	cfg.HideDesktop = func() { hidden++ }

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { settleBackups(e) })

	e.executeCommand("show_desktop")
	e.executeCommand("hide_desktop")
	e.executeCommand("show_desktop")
	if shown != 2 || hidden != 1 {
		t.Fatalf("show=%d hide=%d, want show=2 hide=1", shown, hidden)
	}
}

func TestShowHideDesktopNoopWithoutHooks(t *testing.T) {
	e, _ := newTestEditor(t, "hi\n")
	// No ShowDesktop/HideDesktop wired (standalone): must not panic.
	e.executeCommand("show_desktop")
	e.executeCommand("hide_desktop")
}

// host_suspend calls the host's hook and passes its answer on as the command's
// status; with no hook, or a host that cannot suspend, it fails, so a chain
// like host_suspend|show_desktop falls through to its next command.
func TestHostSuspendCallsTheHookAndFallsThroughWithoutOne(t *testing.T) {
	var asked, shown int
	answer := true
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.SuspendHost = func() bool { asked++; return answer }
	cfg.ShowDesktop = func() { shown++ }

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { settleBackups(e) })

	e.executeCommand("host_suspend|show_desktop")
	if asked != 1 || shown != 0 {
		t.Fatalf("a host that suspended: asked=%d shown=%d, want asked=1 shown=0", asked, shown)
	}

	answer = false
	e.executeCommand("host_suspend|show_desktop")
	if asked != 2 || shown != 1 {
		t.Fatalf("a host that could not suspend: asked=%d shown=%d, want asked=2 shown=1", asked, shown)
	}

	e.Config.SuspendHost = nil
	e.executeCommand("host_suspend|show_desktop")
	if shown != 2 {
		t.Fatalf("no host: shown=%d, want 2 (host_suspend must fail and fall through)", shown)
	}
}
