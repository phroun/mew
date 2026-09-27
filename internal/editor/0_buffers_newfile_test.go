package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// Opening a filename that does not exist yet must start a NEW empty document
// under that name, not error — otherwise `mew newfile` flickers and exits
// straight back to the shell (the launch path returns the open error).
func TestLoadBufferNewFile(t *testing.T) {
	e := mewHomeEditor(t, "[options]\n", nil)
	p := filepath.Join(e.home, "brand-new.txt")
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s must not exist", p)
	}

	buf, err := e.loadBuffer(p)
	if err != nil {
		t.Fatalf("loadBuffer on a nonexistent file must open a NEW buffer, not error: %v", err)
	}
	if buf == nil {
		t.Fatal("nil buffer for a new file")
	}
	if got := buf.GetFilename(); got != p {
		t.Fatalf("new buffer filename = %q, want %q", got, p)
	}
	if got := buf.GetContent(); got != "" {
		t.Fatalf("a new file should start empty, got %q", got)
	}
	// The file still must not exist until an actual save.
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("opening a new file must not create it on disk yet")
	}

	// Typing then saving CREATES the file — the new document is real.
	buf.InsertText(0, 0, "hello\n")
	if !e.performSave(buf, buf.GetFilename()) {
		t.Fatal("saving a new file should succeed (creates it)")
	}
	data, err := os.ReadFile(p)
	if err != nil || string(data) != "hello\n" {
		t.Fatalf("saved new file = %q, %v", data, err)
	}
}
