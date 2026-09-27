package editor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/viewport"
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

// buffer_save_all "true": named buffers save without prompting, but an
// UNNAMED buffer now raises the Save-as prompt (after the quiet saves land)
// instead of being skipped with a toast. Cancelling it aborts with false —
// with the named save already safely on disk.
func TestSaveAllNonInteractivePromptsUnnamed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e, _ := newTestEditor(t, "")

	named := buffer.NewFromString("ok\n")
	named.SetFilename(filepath.Join(dir, "n.txt"))
	unnamed := buffer.NewFromString("x\n")

	success, called := true, false
	e.saveAllNonInteractive([]*buffer.Buffer{named, unnamed}, func(s bool) {
		success, called = s, true
	})
	// The named buffer saved FIRST, before any prompting.
	if readFile(t, filepath.Join(dir, "n.txt")) != "ok\n" {
		t.Fatal("the named buffer should be saved before prompting starts")
	}
	fw := focusedPrompt(e)
	if fw == nil || !strings.Contains(promptText(fw), "Save as") {
		t.Fatalf("the unnamed buffer should raise a Save-as prompt; got %+v", fw)
	}
	cancelPrompt(t, e)
	if !called || success {
		t.Fatalf("cancelling the Save-as must abort with false (called=%v success=%v)", called, success)
	}
}

// Interactive save-all with only clean, source-backed saves reports success
// synchronously (no prompt, no token needed).
func TestSaveAllInteractiveSavesAllNoPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e, _ := newTestEditor(t, "")

	p1 := filepath.Join(dir, "a.txt")
	p2 := filepath.Join(dir, "b.txt")
	os.WriteFile(p1, []byte("aaa\n"), 0o644)
	os.WriteFile(p2, []byte("bbb\n"), 0o644)
	buf1, err := buffer.OpenFile(p1, buffer.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	buf2, err := buffer.OpenFile(p2, buffer.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	buf1.InsertText(0, 0, "X")
	buf2.InsertText(0, 0, "Y")

	success, called := false, false
	e.saveAllInteractive([]*buffer.Buffer{buf1, buf2}, func(s bool) {
		success, called = s, true
	})
	if focusedPrompt(e) != nil {
		t.Fatal("clean source-backed saves should not prompt")
	}
	if !called || !success {
		t.Fatalf("all clean saves should report success (called=%v success=%v)", called, success)
	}
	if readFile(t, p1) != "Xaaa\n" || readFile(t, p2) != "Ybbb\n" {
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
	e.saveAllInteractive([]*buffer.Buffer{buf1, buf2}, func(s bool) {
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
// box:/// support tree (~/.mew), reproducing the visible text — and must do so
// WITHOUT re-locking renderMu, which the key loop holds across command
// execution (calling performRender there is a hard lock).
func TestDebugScreenWritesAnsFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	e, _, _ := renderedEditorWithConfig(t, "hello world\n", "[options]\nsyntax=go\n")
	e.performRender() // establish geometry

	// Arm the command exactly as the key loop does: holding renderMu. The command
	// must only arm (never re-render), so this can't deadlock; a guard timeout
	// turns a regression that re-introduces performRender into a failure, not a
	// hang.
	done := make(chan struct{})
	go func() {
		e.renderMu.Lock()
		e.executeCommand("debug_screen")
		e.renderMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("debug_screen deadlocked while renderMu was held")
	}

	// The capture rides the next render.
	e.performRender()

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

// The interactive save-all saves everything that needs NO question first;
// only then does the prompting pass start. Cancelling a prompt aborts the
// remainder with false — but the quiet saves are already on disk.
func TestSaveAllQuietSavesLandBeforePrompts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e, _ := newTestEditor(t, "")

	clean := filepath.Join(dir, "clean.txt")
	os.WriteFile(clean, []byte("c\n"), 0o644)
	cbuf, err := buffer.OpenFile(clean, buffer.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cbuf.InsertText(0, 0, "Z")
	unnamed := buffer.NewFromString("u\n")

	// The unnamed buffer comes FIRST in the list; the clean one must still
	// save before its prompt appears.
	success, called := true, false
	e.saveAllInteractive([]*buffer.Buffer{unnamed, cbuf}, func(s bool) {
		success, called = s, true
	})
	if readFile(t, clean) != "Zc\n" {
		t.Fatal("the no-question save must land BEFORE any prompting")
	}
	fw := focusedPrompt(e)
	if fw == nil || !strings.Contains(promptText(fw), "Save as") {
		t.Fatalf("expected the Save-as prompt for the unnamed buffer; got %+v", fw)
	}
	cancelPrompt(t, e)
	if !called || success {
		t.Fatal("cancel must abort with false; the quiet save stays on disk")
	}
}

// A NAMED but never-saved buffer gets a Save-as prompt seeded with its name;
// accepting the seed saves it. If the (possibly edited) name collides with an
// existing file, the overwrite prompt appears next — declining saves nothing.
func TestSaveAllNamedNewFilePromptsSeededAndOverwrite(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	e, _ := newTestEditor(t, "")

	// Case 1: seed accepted, no collision -> file created.
	fresh := filepath.Join(dir, "fresh.txt")
	nb := buffer.NewFromString("new\n")
	nb.SetFilename(fresh)
	success, called := false, false
	e.saveAllInteractive([]*buffer.Buffer{nb}, func(s bool) { success, called = s, true })
	fw := focusedPrompt(e)
	if fw == nil || !strings.Contains(promptText(fw), "Save as") {
		t.Fatalf("a named never-saved buffer should raise a seeded Save-as; got %+v", fw)
	}
	if got := fw.Buffer.GetContent(); !strings.Contains(got, "fresh.txt") {
		t.Fatalf("the prompt should be seeded with the buffer's name; content %q", got)
	}
	answerPrompt(t, e, "") // accept the seed
	if !called || !success {
		t.Fatalf("accepting the seeded name should save (called=%v success=%v)", called, success)
	}
	if readFile(t, fresh) != "new\n" {
		t.Fatal("the new file should be written under the seeded name")
	}

	// Case 2: the name collides with an existing file -> overwrite prompt;
	// declining saves nothing and the batch reports failure.
	exist := filepath.Join(dir, "exist.txt")
	os.WriteFile(exist, []byte("original\n"), 0o644)
	cb := buffer.NewFromString("clobber\n")
	cb.SetFilename(exist)
	success, called = true, false
	e.saveAllInteractive([]*buffer.Buffer{cb}, func(s bool) { success, called = s, true })
	if fw := focusedPrompt(e); fw == nil || !strings.Contains(promptText(fw), "Save as") {
		t.Fatalf("expected the Save-as prompt; got %+v", fw)
	}
	answerPrompt(t, e, "") // accept the seed -> collision
	fw = focusedPrompt(e)
	if fw == nil || !strings.Contains(strings.ToUpper(promptText(fw)), "OVERWRITE") {
		t.Fatalf("a colliding name should raise the overwrite prompt; got %+v", fw)
	}
	confirmPrompt(t, e, false) // decline
	if !called || success {
		t.Fatalf("declined overwrite counts as a failure (called=%v success=%v)", called, success)
	}
	if readFile(t, exist) != "original\n" {
		t.Fatal("declining the overwrite must leave the existing file untouched")
	}
}

// The prompting pass SURFACES each buffer: its viewport becomes the focused
// main behind the prompt, so the Save-as question appears over the file it is
// about. And the fresh Save-as prompt carries no filename history — only the
// seeded name is in the prompt buffer.
func TestSaveAllPromptSurfacesBufferAndHasNoHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	e, w1 := newTestEditor(t, "front buffer\n")

	// Pollute the shared filename-prompt history.
	e.PromptMgr.PromptForFilename("Open", "", func(bool, string, string) {})
	answerPrompt(t, e, "/some/old/path.txt")

	// A second, background viewport holds the unnamed buffer to be saved.
	nb := buffer.NewFromString("needs a name\n")
	e.ViewportManager.CreateViewport(viewport.ViewportOptions{
		Visible: true, ID: "back", Type: viewport.DocViewport, Dock: viewport.DockNone,
		Buffer: nb, SetFocus: false,
	})
	e.ViewportManager.SetFocus(w1.ID)

	e.saveAllInteractive([]*buffer.Buffer{nb}, func(bool) {})
	fw := focusedPrompt(e)
	if fw == nil || !strings.Contains(promptText(fw), "Save as") {
		t.Fatalf("expected the Save-as prompt; got %+v", fw)
	}
	// Surfaced: the last MAIN behind the prompt is the buffer's viewport.
	if lm := e.ViewportManager.GetLastMainViewport(); lm == nil || lm.Buffer != nb {
		t.Fatal("the buffer being saved should be surfaced as the visible main")
	}
	// Fresh: no history above the (empty) seed — the polluted path is absent.
	if got := fw.Buffer.GetContent(); strings.Contains(got, "/some/old/path.txt") {
		t.Fatalf("the Save-as prompt must not preload filename history; content %q", got)
	}
	cancelPrompt(t, e)
}
