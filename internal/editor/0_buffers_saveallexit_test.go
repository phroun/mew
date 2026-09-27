package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
)

// Regression guard for the data-loss bug: ^B X = "buffer_save_all & exit".
// With a modified file plus a new unnamed buffer, save-all quietly writes the
// file, then raises a Save-as prompt for the unnamed buffer. Pressing ^C to
// cancel must abort the whole thing WITHOUT exiting — otherwise the never-saved
// buffer is lost. This relies on PawScript honoring `&`/`|` flow control when a
// suspended command sequence resumes (fixed in pawscript v0.2.12-alpha); before
// that, `exit` ran despite the cancel and the editor quit.
func TestSaveAllExitCancelMustNotExit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()

	e, w1 := newTestEditor(t, "") // the "new" buffer: starts empty
	e.Running = true
	typeText(t, e, "precious unsaved work") // unnamed + modified

	named := filepath.Join(dir, "kept.txt")
	if err := os.WriteFile(named, []byte("orig\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nb, err := buffer.OpenFile(named, buffer.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	nb.InsertText(0, 0, "X") // modified but clean-on-disk source -> quiet save
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, ID: "named", Type: viewport.DocViewport, Dock: viewport.DockNone,
		Buffer: nb, SetFocus: false,
	})
	e.ViewportManager.SetFocus(w1.ID)

	e.executeCommand("buffer_save_all & exit")

	fw := focusedPrompt(e)
	if fw == nil || !strings.Contains(promptText(fw), "Save as") {
		t.Fatalf("expected a Save-as prompt for the unnamed buffer; got %+v", fw)
	}

	e.dispatchKey("^C") // real ^C == nav_cancel|cancel|viewport_close

	if !e.Running {
		t.Fatal("cancelling the Save-as exited the editor — work lost")
	}
}
