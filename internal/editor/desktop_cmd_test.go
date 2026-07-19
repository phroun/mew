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
