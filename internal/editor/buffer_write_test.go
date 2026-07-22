package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// buffer_write exports the whole buffer to the named file without adopting it:
// the buffer's filename and modified flag are left untouched (unlike a save).
func TestBufferWriteExportsWithoutAdopting(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.txt")
	dest := filepath.Join(dir, "export.txt")

	e, w := newTestEditor(t, "hello\nworld\n")
	w.Buffer.SetFilename(orig)
	// Dirty the buffer so we can prove the export does NOT clear modified.
	w.SetCursorPos(window.Position{})
	e.PawScript.ExecuteAsync(`insert "!"`)
	if !w.Buffer.IsModified() {
		t.Fatal("precondition: buffer should be modified after an edit")
	}
	want := w.Buffer.GetContent()

	e.PawScript.ExecuteAsync("buffer_write")
	if focusedPrompt(e) == nil {
		t.Fatal("buffer_write did not raise a filename prompt")
	}
	answerPrompt(t, e, dest) // absolute path passes through unanchored

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("export not written: %v", err)
	}
	if string(got) != want {
		t.Fatalf("export content: %q, want %q", got, want)
	}
	// Non-adoption: filename unchanged, and the buffer stays dirty (an export is
	// not a save, so it does not clean the modified flag or re-home the source).
	if fn := w.Buffer.GetFilename(); fn != orig {
		t.Fatalf("filename changed by export: %q, want %q", fn, orig)
	}
	if !w.Buffer.IsModified() {
		t.Fatal("export cleared the modified flag; it should not (not a save)")
	}
}

// Writing over an existing file asks first; "no" leaves the target untouched,
// "yes" overwrites it.
func TestBufferWriteOverwriteConfirm(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(dest, []byte("OLD"), 0o644); err != nil {
		t.Fatal(err)
	}

	e, w := newTestEditor(t, "NEW\n")

	// Decline: the existing file survives unchanged.
	e.PawScript.ExecuteAsync("buffer_write")
	answerPrompt(t, e, dest)
	answerPrompt(t, e, "n")
	if got, _ := os.ReadFile(dest); string(got) != "OLD" {
		t.Fatalf("declined overwrite still wrote: %q", got)
	}

	// Confirm: the export replaces it.
	e.PawScript.ExecuteAsync("buffer_write")
	answerPrompt(t, e, dest)
	answerPrompt(t, e, "y")
	if got, _ := os.ReadFile(dest); string(got) != w.Buffer.GetContent() {
		t.Fatalf("confirmed overwrite content: %q, want %q", got, w.Buffer.GetContent())
	}
}
