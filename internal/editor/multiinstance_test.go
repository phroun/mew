package editor

import (
	"os"
	"testing"
)

// Two editors run in one process with independent garland libraries and
// cold-storage subfolders; editing one never touches the other. This is the
// guarantee that lets a host embed many mews (e.g. as KittyTK editor trinkets).
func TestMultipleInstancesAreIndependent(t *testing.T) {
	e1, w1 := newTestEditor(t, "one\n")
	e2, w2 := newTestEditor(t, "two\n")

	// Each editor owns its own library and a distinct, real cold-storage dir.
	if e1.lib == nil || e2.lib == nil {
		t.Fatal("each editor must have its own buffer library")
	}
	if e1.lib == e2.lib {
		t.Fatal("editors must not share a buffer library")
	}
	if e1.coldDir == "" || e2.coldDir == "" || e1.coldDir == e2.coldDir {
		t.Fatalf("editors must own distinct cold-storage dirs: %q vs %q", e1.coldDir, e2.coldDir)
	}
	for _, d := range []string{e1.coldDir, e2.coldDir} {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			t.Fatalf("cold-storage dir %q should exist: %v", d, err)
		}
	}

	// Editing one instance leaves the other untouched, both directions.
	e1.executeCommand(`insert "X"`)
	if got := docContent(w1); got != "Xone" {
		t.Fatalf("e1 content = %q, want Xone", got)
	}
	if got := docContent(w2); got != "two" {
		t.Fatalf("e2 must be untouched by e1's edit: %q", got)
	}

	e2.executeCommand(`insert "Y"`)
	if got := docContent(w2); got != "Ytwo" {
		t.Fatalf("e2 content = %q, want Ytwo", got)
	}
	if got := docContent(w1); got != "Xone" {
		t.Fatalf("e1 must be untouched by e2's edit: %q", got)
	}
}

// Cleanup releases the instance's library and removes its private cold-storage
// subfolder, so a long-lived host opening and closing many mews doesn't leak.
func TestInstanceCleanupRemovesColdDir(t *testing.T) {
	base := t.TempDir()
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = base

	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cold := e.coldDir
	if cold == "" {
		t.Fatal("editor should own a cold-storage subfolder")
	}
	if _, err := os.Stat(cold); err != nil {
		t.Fatalf("cold-storage dir should exist before Cleanup: %v", err)
	}

	e.Cleanup()
	if _, err := os.Stat(cold); !os.IsNotExist(err) {
		t.Errorf("Cleanup should remove the cold-storage subfolder (stat err: %v)", err)
	}
}
