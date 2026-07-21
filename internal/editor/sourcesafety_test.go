package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/phroun/mew/internal/window"
)

// --- helpers ---------------------------------------------------------------

// openInEditor opens a real file through the editor's full open path (locks,
// backups, notices) and returns its window.
func openInEditor(t *testing.T, e *Editor, path string) *window.Window {
	t.Helper()
	if !e.openFile(path) {
		t.Fatalf("openFile(%s) failed", path)
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil || w.Buffer.GetFilename() != path {
		t.Fatalf("focused window should hold %s", path)
	}
	return w
}

// promptText returns the visible message of a prompt window.
func promptText(fw *window.Window) string {
	if fw == nil || len(fw.RowMessages) == 0 {
		return ""
	}
	return fw.RowMessages[0]
}

// typeText inserts text into the focused buffer via the real insert command.
func typeText(t *testing.T, e *Editor, text string) {
	t.Helper()
	e.PawScript.ExecuteAsync(fmt.Sprintf("insert %q", text))
}

// confirmPrompt answers a pending confirmation prompt.
func confirmPrompt(t *testing.T, e *Editor, yes bool) {
	t.Helper()
	fw := focusedPrompt(e)
	if fw == nil {
		t.Fatal("expected a confirmation prompt")
	}
	answer := "n"
	if yes {
		answer = "y"
	}
	answerPrompt(t, e, answer)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// --- save safety: different file -------------------------------------------

// Saving over a DIFFERENT existing file prompts; declining cancels, and
// confirming overwrites and adopts the target as the buffer's new source.
func TestSaveOverDifferentExistingFilePrompts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim.txt")
	os.WriteFile(victim, []byte("precious\n"), 0o644)

	e, w := newTestEditor(t, "")
	typeText(t, e, "mine")

	// Decline: the victim is untouched and the buffer keeps no filename.
	e.requestSave(w.Buffer, victim, nil)
	if fw := focusedPrompt(e); fw == nil || !strings.Contains(promptText(fw), "OVERWRITE EXISTING FILE") {
		t.Fatalf("expected overwrite prompt, got %+v", fw)
	}
	confirmPrompt(t, e, false)
	if got := readFile(t, victim); got != "precious\n" {
		t.Fatalf("declined overwrite must not touch the file, got %q", got)
	}

	// Confirm: the file is replaced and the buffer adopts it.
	done := false
	e.requestSave(w.Buffer, victim, func(ok, cancelled bool) { done = ok })
	confirmPrompt(t, e, true)
	if !done {
		t.Fatal("confirmed save should succeed")
	}
	if got := readFile(t, victim); !strings.Contains(got, "mine") {
		t.Fatalf("confirmed overwrite should write buffer content, got %q", got)
	}
	if w.Buffer.GetFilename() != victim || !w.Buffer.HasSource() {
		t.Fatalf("save-as should adopt the target as source (filename=%q hasSource=%v)",
			w.Buffer.GetFilename(), w.Buffer.HasSource())
	}

	// A fresh (non-existing) target saves without any prompt.
	fresh := filepath.Join(dir, "fresh.txt")
	e.requestSave(w.Buffer, fresh, nil)
	if focusedPrompt(e) != nil {
		t.Fatal("saving to a new path must not prompt")
	}
	if got := readFile(t, fresh); !strings.Contains(got, "mine") {
		t.Fatalf("fresh save content: %q", got)
	}
}

// --- save safety: same file changed on disk --------------------------------

// Saving to the buffer's own filename warns when the file changed underneath
// us (metadata mismatch); clean files save silently.
func TestSaveSameFileChangedOnDisk(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("original\n"), 0o644)

	e, _ := newTestEditor(t, "")
	openInEditor(t, e, path)
	typeText(t, e, "edit-")

	// Someone else rewrites the file behind our back.
	os.WriteFile(path, []byte("theirs\n"), 0o644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(path, past, past)

	// Decline the overwrite: their version survives.
	e.executeCommand("buffer_save")
	if fw := focusedPrompt(e); fw == nil || !strings.Contains(promptText(fw), "CHANGED ON DISK") {
		t.Fatalf("expected changed-on-disk prompt, got %+v", fw)
	}
	confirmPrompt(t, e, false)
	if got := readFile(t, path); got != "theirs\n" {
		t.Fatalf("declined save must keep the disk version, got %q", got)
	}

	// Confirm: our buffer wins.
	e.executeCommand("buffer_save")
	confirmPrompt(t, e, true)
	if got := readFile(t, path); !strings.Contains(got, "edit-") {
		t.Fatalf("confirmed save should overwrite, got %q", got)
	}

	// Now the buffer and disk agree again: a further save must not prompt.
	typeText(t, e, "more-")
	e.executeCommand("buffer_save")
	if focusedPrompt(e) != nil {
		t.Fatal("clean same-file save must not prompt")
	}
	if got := readFile(t, path); !strings.Contains(got, "more-") {
		t.Fatalf("clean save content: %q", got)
	}
}

// --- buffer_revert ----------------------------------------------------------

func TestBufferRevert(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("base\n"), 0o644)

	e, _ := newTestEditor(t, "")
	w := openInEditor(t, e, path)

	typeText(t, e, "saved-")
	e.executeCommand("buffer_save")
	if focusedPrompt(e) != nil {
		t.Fatal("unexpected prompt on clean save")
	}
	savedContent := w.Buffer.GetContent()

	typeText(t, e, "abandon-")
	if w.Buffer.GetContent() == savedContent {
		t.Fatal("edit after save should change content")
	}

	e.executeCommand("buffer_revert")
	if got := w.Buffer.GetContent(); got != savedContent {
		t.Fatalf("revert should restore the saved content: got %q want %q", got, savedContent)
	}
	if w.Buffer.IsModified() {
		t.Fatal("reverted buffer should not be modified")
	}
}

// The reported bug: opening a file, editing, and reverting WITHOUT any save
// must return to the opened content — not act as if there were no prior
// state. The revert baseline is captured at open, so this works even though
// garland's own save history is empty.
func TestBufferRevertOpenedButNeverSaved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("line one\nline two\n"), 0o644)

	e, _ := newTestEditor(t, "")
	w := openInEditor(t, e, path)
	opened := w.Buffer.GetContent()

	// Edit at the start of the buffer, no save.
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	typeText(t, e, "PREFIX ")
	if w.Buffer.GetContent() == opened {
		t.Fatal("edit should change content")
	}
	if !w.Buffer.IsModified() {
		t.Fatal("edited buffer should be modified")
	}

	e.executeCommand("buffer_revert")
	if got := w.Buffer.GetContent(); got != opened {
		t.Fatalf("revert should restore the opened content: got %q want %q", got, opened)
	}
	if w.Buffer.IsModified() {
		t.Fatal("reverted buffer should not be modified")
	}

	// Redo still reaches the abandoned edit (pure history seek).
	if !w.Buffer.Redo() {
		t.Fatal("redo should reach the reverted edit")
	}
	if got := w.Buffer.GetContent(); got == opened {
		t.Fatal("redo should restore the abandoned edit")
	}
}

// A scratch buffer that was never opened from (or saved to) a file has no
// baseline: buffer_revert fails cleanly rather than pretending.
func TestBufferRevertScratchHasNoBaseline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	e, w := newTestEditor(t, "")
	typeText(t, e, "scratch text")
	before := w.Buffer.GetContent()
	e.executeCommand("buffer_revert")
	if got := w.Buffer.GetContent(); got != before {
		t.Fatalf("revert on a baseline-less scratch buffer must not change content: %q -> %q", before, got)
	}
}

// --- locks ------------------------------------------------------------------

// The emacs-lock decision: on outside git; off (with a .gitignore hint)
// inside a repo that does not ignore ".#*"; on again once it does; off
// entirely under useLocks=false.
func TestEmacsLockDecision(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	e, _ := newTestEditor(t, "")

	plain := t.TempDir()
	plainFile := filepath.Join(plain, "a.txt")
	if on, warn := e.emacsLockDecision(plainFile); !on || warn != "" {
		t.Fatalf("outside git: want enabled, got on=%v warn=%q", on, warn)
	}

	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	repoFile := filepath.Join(repo, "sub", "b.txt")
	os.MkdirAll(filepath.Dir(repoFile), 0o755)
	on, warn := e.emacsLockDecision(repoFile)
	if on {
		t.Fatal("git repo without ignore: emacs locks must be skipped")
	}
	if !strings.Contains(warn, ".gitignore") || !strings.Contains(warn, ".#*") {
		t.Fatalf("skip warning should name the .gitignore fix, got %q", warn)
	}

	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("*.tmp\n.#*\n"), 0o644)
	if on, warn := e.emacsLockDecision(repoFile); !on || warn != "" {
		t.Fatalf("repo ignoring .#*: want enabled, got on=%v warn=%q", on, warn)
	}

	e.Config.UseLocks = false
	if on, warn := e.emacsLockDecision(plainFile); on || warn != "" {
		t.Fatalf("useLocks=false: want disabled silently, got on=%v warn=%q", on, warn)
	}
}

// Mew-native locks: acquired under ~/.mew/locks when emacs locks are off,
// released when the buffer closes, and a live foreign lock is respected
// with a warning instead of being clobbered.
func TestMewLockLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("x\n"), 0o644)

	e, _ := newTestEditor(t, "", "useEmacsLocks=false")
	w := openInEditor(t, e, path)

	lockPath := e.mewLocks[w.Buffer]
	if lockPath == "" {
		t.Fatal("mew lock should be held after open")
	}
	if !strings.HasPrefix(lockPath, filepath.Join(home, ".mew", "locks")) {
		t.Fatalf("lock should live under ~/.mew/locks, got %s", lockPath)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("lock file should exist: %v", err)
	}
	owner := strings.SplitN(readFile(t, lockPath), "\n", 2)[0]
	if !strings.Contains(owner, fmt.Sprintf(".%d", os.Getpid())) {
		t.Fatalf("lock owner should carry our pid, got %q", owner)
	}

	// Closing the buffer releases the lock (a second buffer keeps the
	// editor alive through the close).
	e.executeCommand("buffer_close")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock file should be removed on close, err=%v", err)
	}

	// A live foreign lock (different host: never probed) is respected.
	other := filepath.Join(dir, "other.txt")
	os.WriteFile(other, []byte("y\n"), 0o644)
	otherLock := filepath.Join(home, ".mew", "locks", pathHash(other)+".lock")
	os.MkdirAll(filepath.Dir(otherLock), 0o755)
	os.WriteFile(otherLock, []byte("someone@elsewhere.4242\n"+other+"\n"), 0o644)

	w2 := openInEditor(t, e, other)
	if e.mewLocks[w2.Buffer] != "" {
		t.Fatal("foreign live lock must not be taken over")
	}
	if got := readFile(t, otherLock); !strings.HasPrefix(got, "someone@elsewhere.4242") {
		t.Fatalf("foreign lock file must be untouched, got %q", got)
	}
	if len(e.bufNotices[w2.Buffer]) == 0 {
		t.Fatal("foreign lock should be captured as a notice")
	}

	// A stale lock (this host, dead pid) is silently replaced.
	stale := filepath.Join(dir, "stale.txt")
	os.WriteFile(stale, []byte("z\n"), 0o644)
	staleLock := filepath.Join(home, ".mew", "locks", pathHash(stale)+".lock")
	host, _ := os.Hostname()
	os.WriteFile(staleLock, []byte(fmt.Sprintf("ghost@%s.4194301\n%s\n", host, stale)), 0o644)

	w3 := openInEditor(t, e, stale)
	if e.mewLocks[w3.Buffer] != staleLock {
		t.Fatal("stale lock should be taken over")
	}
	if got := readFile(t, staleLock); !strings.Contains(got, fmt.Sprintf(".%d", os.Getpid())) {
		t.Fatalf("stale lock should be rewritten to us, got %q", got)
	}
}

// --- backups ----------------------------------------------------------------

// The first edit arms a pre-session backup into ~/.mew/backups (hash-
// disambiguated name), and a save commits it with the pre-edit content.
func TestAutomaticBackups(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("pre-session\n"), 0o644)

	e, _ := newTestEditor(t, "")
	w := openInEditor(t, e, path)
	typeText(t, e, "edited-")

	// The backup streams in the background; wait for it to land.
	deadline := time.Now().Add(3 * time.Second)
	for {
		bs := w.Buffer.BackupStatus()
		if bs.State == "ready" || bs.State == "committed" {
			break
		}
		if bs.State == "failed" {
			t.Fatalf("backup failed: %s", bs.Err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("backup never became ready (state %s)", bs.State)
		}
		time.Sleep(10 * time.Millisecond)
	}

	bs := w.Buffer.BackupStatus()
	if !strings.HasPrefix(bs.Path, filepath.Join(home, ".mew", "backups")) {
		t.Fatalf("backup should land under ~/.mew/backups, got %s", bs.Path)
	}
	if got := readFile(t, bs.Path); got != "pre-session\n" {
		t.Fatalf("backup must hold the pre-session content, got %q", got)
	}

	e.executeCommand("buffer_save")
	if focusedPrompt(e) != nil {
		t.Fatal("unexpected prompt")
	}
	if got := w.Buffer.BackupStatus(); got.State != "committed" {
		t.Fatalf("backup should be committed after save, got %s", got.State)
	}
	if got := readFile(t, bs.Path); got != "pre-session\n" {
		t.Fatalf("committed backup still holds pre-session content, got %q", got)
	}
}

// --- host FS bridge ---------------------------------------------------------

// fakeHostFS is an in-memory host file system.
type fakeHostFS struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (f *fakeHostFS) ReadFile(name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeHostFS) WriteFile(name string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[name] = append([]byte(nil), data...)
	return nil
}

func (f *fakeHostFS) Glob(pattern string) ([]string, error) { return nil, nil }

// A host-virtualized editor opens and saves through garland via the bridge:
// in-place saves write back through the host, and save-as over an existing
// host file prompts exactly like the native path.
func TestHostBridgeOpenAndSave(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	host := &fakeHostFS{files: map[string][]byte{
		"virt.txt":  []byte("virtual content\n"),
		"other.txt": []byte("keep me\n"),
	}}

	var out strings.Builder
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.FS = host
	_ = out
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if !e.openFile("virt.txt") {
		t.Fatal("openFile through host FS failed")
	}
	w := e.WindowManager.GetFocusedWindow()
	if w == nil || w.Buffer == nil {
		t.Fatal("no focused window")
	}
	if !w.Buffer.HasSource() {
		t.Fatal("host-opened buffer should have a garland-tracked source (bridged)")
	}
	if got := docContent(w); !strings.Contains(got, "virtual content") {
		t.Fatalf("buffer content: %q", got)
	}

	// In-place save flows back through the host callbacks.
	typeText(t, e, "host-edit-")
	e.executeCommand("buffer_save")
	if focusedPrompt(e) != nil {
		t.Fatal("same-file host save must not prompt (no metadata: untracked)")
	}
	if got := string(host.files["virt.txt"]); !strings.Contains(got, "host-edit-") {
		t.Fatalf("host save content: %q", got)
	}

	// Save-as onto another existing host file prompts; declining cancels.
	e.requestSave(w.Buffer, "other.txt", nil)
	if fw := focusedPrompt(e); fw == nil || !strings.Contains(promptText(fw), "OVERWRITE EXISTING FILE") {
		t.Fatal("expected overwrite prompt for existing host file")
	}
	confirmPrompt(t, e, false)
	if got := string(host.files["other.txt"]); got != "keep me\n" {
		t.Fatalf("declined host overwrite must not write, got %q", got)
	}

	// Confirming adopts the new host path.
	e.requestSave(w.Buffer, "other.txt", nil)
	confirmPrompt(t, e, true)
	if got := string(host.files["other.txt"]); !strings.Contains(got, "host-edit-") {
		t.Fatalf("confirmed host overwrite content: %q", got)
	}
	if w.Buffer.GetFilename() != "other.txt" {
		t.Fatalf("host save-as should adopt, filename=%q", w.Buffer.GetFilename())
	}
}

// --- buffer_status ----------------------------------------------------------

func TestBufferStatusWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.txt")
	os.WriteFile(path, []byte("x\n"), 0o644)

	e, _ := newTestEditor(t, "")
	w := openInEditor(t, e, path)
	e.noteBuffer(w.Buffer, "lock", "example captured notice", false)

	e.executeCommand("buffer_status")
	sw := windowByClass(e, "bufstatus")
	if sw == nil {
		t.Fatal("buffer_status should open a status window")
	}
	content := sw.Buffer.GetContent()
	for _, want := range []string{"Buffer: " + path, "Source: clean", "Backup:", "example captured notice"} {
		if !strings.Contains(content, want) {
			t.Fatalf("status window missing %q:\n%s", want, content)
		}
	}
}
