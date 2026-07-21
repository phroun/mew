package editor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
)

// ansiEscapes matches CSI sequences and the DEC line-attribute / charset
// escapes the renderer emits, so a capture can be checked for its plain text.
var ansiEscapes = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b[#()][0-9A-Za-z]")

// Saving into a directory that does not exist prompts to create it (like the
// overwrite prompt): declining leaves nothing behind, confirming creates the
// tree and writes the file.
func TestSaveIntoMissingDirectoryPrompts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	target := filepath.Join(dir, "newsub", "deep", "file.txt")

	e, w := newTestEditor(t, "")
	typeText(t, e, "hello")

	// Decline: nothing is created or written.
	e.requestSave(w.Buffer, target, nil)
	if fw := focusedPrompt(e); fw == nil || !strings.Contains(promptText(fw), "Directory does not exist") {
		t.Fatalf("expected a create-directory prompt, got %+v", fw)
	}
	confirmPrompt(t, e, false)
	if _, err := os.Stat(filepath.Dir(target)); !os.IsNotExist(err) {
		t.Fatal("declining must not create the directory")
	}

	// Confirm: the tree is created and the file written.
	done, ok := false, false
	e.requestSave(w.Buffer, target, func(o, cancelled bool) { done, ok = true, o })
	confirmPrompt(t, e, true)
	if !done || !ok {
		t.Fatalf("confirmed create+save should succeed (done=%v ok=%v)", done, ok)
	}
	if got := readFile(t, target); !strings.Contains(got, "hello") {
		t.Fatalf("file content after create: %q", got)
	}
}

// buffer_save_all "true" is non-interactive: it never prompts, skips an unnamed
// buffer with a notice, still saves the named ones, and returns false because
// something was skipped (so `buffer_save_all true & exit` won't quit on it).
func TestSaveAllNonInteractiveSkipsUnnamed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e, _ := newTestEditor(t, "")

	named := buffer.NewFromString("ok\n")
	named.SetFilename(filepath.Join(dir, "n.txt"))
	unnamed := buffer.NewFromString("x\n")

	if e.saveAllNonInteractive([]*buffer.Buffer{named, unnamed}) {
		t.Fatal("non-interactive save-all must return false when a buffer is skipped")
	}
	if focusedPrompt(e) != nil {
		t.Fatal("non-interactive mode must not prompt")
	}
	if readFile(t, filepath.Join(dir, "n.txt")) != "ok\n" {
		t.Fatal("the named buffer should still be saved")
	}
}

// Interactive save-all with only clean saves reports success synchronously
// (no prompt, no token needed).
func TestSaveAllInteractiveSavesAllNoPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e, _ := newTestEditor(t, "")

	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "b.txt")
	buf1 := buffer.NewFromString("aaa\n")
	buf1.SetFilename(p1)
	buf2 := buffer.NewFromString("bbb\n")
	buf2.SetFilename(p2)

	success, called := false, false
	e.saveAllSequential([]*buffer.Buffer{buf1, buf2}, saveAllTally{}, func(s bool) {
		success, called = s, true
	})
	if focusedPrompt(e) != nil {
		t.Fatal("clean saves should not prompt")
	}
	if !called || !success {
		t.Fatalf("all clean saves should report success (called=%v success=%v)", called, success)
	}
	if readFile(t, p1) != "aaa\n" || readFile(t, p2) != "bbb\n" {
		t.Fatal("both buffers should be written")
	}
}

// A ^C at any prompt bails the entire remaining interactive batch with a false
// result — later buffers are NOT saved.
func TestSaveAllInteractiveCancelBailsEntireBatch(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e, _ := newTestEditor(t, "")

	buf1 := buffer.NewFromString("aaa\n") // unnamed -> triggers a name prompt
	buf2 := buffer.NewFromString("bbb\n")
	b2 := filepath.Join(dir, "b.txt")
	buf2.SetFilename(b2)

	success, called := true, false
	e.saveAllSequential([]*buffer.Buffer{buf1, buf2}, saveAllTally{}, func(s bool) {
		success, called = s, true
	})
	if focusedPrompt(e) == nil {
		t.Fatal("expected a name prompt for the unnamed buffer")
	}
	cancelPrompt(t, e) // ^C
	if !called || success {
		t.Fatalf("a ^C must abort the batch with false (called=%v success=%v)", called, success)
	}
	if _, err := os.Stat(b2); !os.IsNotExist(err) {
		t.Fatal("the second buffer must NOT be saved after an abort")
	}
}

// debug_screen writes a timestamped .ans capture of the current screen into the
// mew:/// support tree (~/.mew), reproducing the visible text.
func TestDebugScreenWritesAnsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	e, _, _ := renderedEditorWithConfig(t, "hello world\n", "[options]\nsyntax=go\n")
	e.performRender()

	e.PawScript.ExecuteAsync("debug_screen")

	matches, _ := filepath.Glob(filepath.Join(home, ".mew", "*.ans"))
	if len(matches) != 1 {
		t.Fatalf("debug_screen should write exactly one .ans file, found %v", matches)
	}
	// The capture is real ANSI (each cell carries its SGR); strip the escapes
	// to check the plain screen text is present.
	data := readFile(t, matches[0])
	if plain := ansiEscapes.ReplaceAllString(data, ""); !strings.Contains(plain, "hello world") {
		t.Fatalf(".ans capture should contain the screen text (%d bytes)", len(data))
	}
}
