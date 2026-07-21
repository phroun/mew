package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/pawscript"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// promptLine returns the trimmed text on the focused prompt's caret line.
func promptLine(t *testing.T, e *Editor) string {
	t.Helper()
	p := focusedPrompt(e)
	if p == nil {
		t.Fatal("no focused prompt")
	}
	return strings.TrimRight(p.Buffer.GetLine(p.CursorPos().Line), "\n\r")
}

// The completion command routes to the focused window's callback and returns
// its result; with no callback it fails (so a tab fallback can run).
func TestCompletionCommandRouting(t *testing.T) {
	e, w := newTestEditor(t, "")
	if res := e.PawScript.ExecuteAsync("completion"); res != pawscript.BoolStatus(false) {
		t.Fatalf("no callback should fail, got %v", res)
	}
	w.CompletionCallback = func() bool { return true }
	if res := e.PawScript.ExecuteAsync("completion"); res != pawscript.BoolStatus(true) {
		t.Fatal("callback returning true should succeed")
	}
	w.CompletionCallback = func() bool { return false }
	if res := e.PawScript.ExecuteAsync("completion"); res != pawscript.BoolStatus(false) {
		t.Fatal("callback returning false should fail")
	}
}

// anchorPrompt opens a filename prompt whose parent buffer is anchored at dir,
// so completion globs dir.
func anchorPrompt(t *testing.T, e *Editor, doc *window.Window, dir string) {
	t.Helper()
	doc.Buffer.SetFilename(filepath.Join(dir, "anchor.txt"))
	e.PromptMgr.PromptForFilename("Open", "", func(bool, string, string) {})
	if focusedPrompt(e) == nil {
		t.Fatal("filename prompt should be focused")
	}
}

// Filename completion auto-fills the shared prefix and lists the rest.
func TestFilenameCompletionSharedPrefix(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"alpha.go", "alphabet.go", "beta.go"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	e, doc := newTestEditor(t, "")
	anchorPrompt(t, e, doc, dir)

	typeText(t, e, "al")
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("completion should succeed with matches")
	}
	if got := promptLine(t, e); got != "alpha" {
		t.Fatalf("should auto-fill the shared prefix 'alpha', got %q", got)
	}
	if !hasNotification(e, "alpha.go") || !hasNotification(e, "alphabet.go") {
		t.Fatal("ambiguous completion should list the candidates as a transient")
	}

	// Typing more to disambiguate, then completing again, finishes the name.
	typeText(t, e, "b") // now "alphab"
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("second completion should succeed")
	}
	if got := promptLine(t, e); got != "alphabet.go" {
		t.Fatalf("should complete to the single remaining match, got %q", got)
	}
}

// An empty prompt line globs the whole base directory.
func TestFilenameCompletionEmptyLine(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"one.txt", "two.txt"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	e, doc := newTestEditor(t, "")
	anchorPrompt(t, e, doc, dir)

	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("empty-line completion should glob the base dir and succeed")
	}
	if !hasNotification(e, "one.txt") || !hasNotification(e, "two.txt") {
		t.Fatal("empty-line completion should list the directory")
	}
}

// No matching file: a filename prompt still owns the key (returns true) so a
// literal tab is never inserted into a filename.
func TestFilenameCompletionNoMatch(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "real.txt"), nil, 0o644)
	e, doc := newTestEditor(t, "")
	anchorPrompt(t, e, doc, dir)

	typeText(t, e, "zzz")
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("no match in a filename prompt should still return true (no tab)")
	}
}

// completionBaseDir precedence: the launch dir / documents fallback when the
// anchoring buffer has no file of its own.
func TestCompletionBaseDirFallback(t *testing.T) {
	e, doc := newTestEditor(t, "") // doc buffer is unnamed
	e.launchDir = "/launch/dir"
	if got := e.completionBaseDir(doc); got != "/launch/dir" {
		t.Fatalf("unnamed buffer should fall back to launch dir, got %q", got)
	}
	e.launchDir = ""
	e.LoadedConfig.Storage.Documents = "/docs"
	if got := e.completionBaseDir(doc); got != "/docs" {
		t.Fatalf("with no launch dir, documents= should apply, got %q", got)
	}
}

// Directories complete with a trailing slash (via the Statter/DirGlobber
// capability), so the shared-prefix fill descends into them.
func TestFilenameCompletionDirSlash(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)
	os.WriteFile(filepath.Join(dir, "readme.txt"), nil, 0o644)
	e, doc := newTestEditor(t, "")
	anchorPrompt(t, e, doc, dir)

	typeText(t, e, "subd") // only the directory matches
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("completion should succeed")
	}
	if got := promptLine(t, e); got != "subdir/" {
		t.Fatalf("a sole directory match should complete to 'subdir/', got %q", got)
	}
}

// A re-completion replaces the previous option list rather than stacking.
func TestCompletionTransientReplaces(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"alpha.go", "alphabet.go"} {
		os.WriteFile(filepath.Join(dir, n), nil, 0o644)
	}
	e, doc := newTestEditor(t, "")
	anchorPrompt(t, e, doc, dir)

	countTagged := func() int {
		n := 0
		for _, w := range e.WindowManager.GetWindowsByDock(window.DockBottom) {
			if w.Tag == "completion" {
				n++
			}
		}
		return n
	}
	typeText(t, e, "al")
	e.completeFilename(focusedPrompt(e))
	if countTagged() != 1 {
		t.Fatalf("first ambiguous completion should show one transient, got %d", countTagged())
	}
	e.completeFilename(focusedPrompt(e)) // re-complete same text
	if countTagged() != 1 {
		t.Fatalf("re-completion should replace, not stack: got %d", countTagged())
	}
}

// dirGlobFS is a host FileSystem implementing the single-call DirGlobber
// capability (with directory info), so completion never round-trips a Stat.
type dirGlobFS struct {
	fakeHostFS
}

func (*dirGlobFS) GlobStat(pattern string) ([]FileInfo, error) {
	return []FileInfo{
		{Path: "proj/src", IsDir: true},
		{Path: "proj/setup.txt"},
	}, nil
}

// A host implementing DirGlobber drives completion (with the trailing-slash
// directory marker) in a single glob call.
func TestFilenameCompletionDirGlobber(t *testing.T) {
	host := &dirGlobFS{fakeHostFS{files: map[string][]byte{}}}
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.FS = host
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString(""), SetFocus: true,
	})
	e.WindowManager.GetWindow("doc").Buffer.SetFilename("proj/anchor.txt")

	e.PromptMgr.PromptForFilename("Open", "", func(bool, string, string) {})
	typeText(t, e, "s")
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("DirGlobber completion should succeed (proves GlobStat, not Glob)")
	}
	if !hasNotification(e, "src/") {
		t.Fatal("directory match should be listed with a trailing slash")
	}
}

// resolvePromptPath anchors relative names to the base and leaves the marked
// non-relative forms (absolute, ../, scheme:/) untouched.
func TestResolvePromptPath(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	e, _ := newTestEditor(t, "")
	base := "/home/user/project"
	cases := []struct{ in, want string }{
		{"notes.txt", "/home/user/project/notes.txt"},
		{"sub/notes.txt", "/home/user/project/sub/notes.txt"},
		{"./notes.txt", "/home/user/project/notes.txt"},
		{"/etc/hosts", "/etc/hosts"},
		{"../other/x", "/home/user/other/x"}, // ../ walks up from the anchor
		{"..", "/home/user"},
		{"http://example.com/f", "http://example.com/f"},
		{"file:/abs", "file:/abs"},
		{"s3://bucket/key", "s3://bucket/key"},
		{"~/x", "/home/tester/x"},
		{"", ""},
		{"weird:name.txt", "/home/user/project/weird:name.txt"}, // colon, no slash: not a scheme
	}
	for _, c := range cases {
		if got := e.resolvePromptPath(c.in, base); got != c.want {
			t.Errorf("resolvePromptPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := e.resolvePromptPath("notes.txt", ""); got != "notes.txt" {
		t.Errorf("empty base should not prefix: %q", got)
	}
}

// A relative name accepted at a filename prompt is anchored to the same
// directory completion searches; an absolute path is left as typed.
func TestPromptResolvesRelativeToAnchor(t *testing.T) {
	dir := t.TempDir()
	e, doc := newTestEditor(t, "")
	doc.Buffer.SetFilename(filepath.Join(dir, "anchor.txt"))

	var got string
	cb := func(accepted bool, _, cursorLineText string) {
		if accepted {
			got = cursorLineText
		}
	}

	e.PromptMgr.PromptForFilename("Open", "", cb)
	answerPrompt(t, e, "notes.txt")
	if want := filepath.Join(dir, "notes.txt"); got != want {
		t.Fatalf("relative name should anchor: got %q, want %q", got, want)
	}

	e.PromptMgr.PromptForFilename("Open", "", cb)
	answerPrompt(t, e, "/etc/hosts")
	if got != "/etc/hosts" {
		t.Fatalf("absolute path should pass through: got %q", got)
	}
}

// globFS is a host FileSystem that records the pattern completion globs and
// returns canned results — proving completion goes through the abstraction,
// not filepath.Glob.
type globFS struct {
	fakeHostFS
	result      []string
	lastPattern string
}

func (g *globFS) Glob(pattern string) ([]string, error) {
	g.lastPattern = pattern
	return g.result, nil
}

// Under a host file system, completion globs through the host's Glob.
func TestFilenameCompletionHostFS(t *testing.T) {
	host := &globFS{
		fakeHostFS: fakeHostFS{files: map[string][]byte{}},
		result:     []string{"proj/one.txt", "proj/only.txt"},
	}
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.FS = host
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString(""), SetFocus: true,
	})
	doc := e.WindowManager.GetWindow("doc")
	doc.Buffer.SetFilename("proj/anchor.txt")

	e.PromptMgr.PromptForFilename("Open", "", func(bool, string, string) {})
	typeText(t, e, "o")
	if !e.completeFilename(focusedPrompt(e)) {
		t.Fatal("host completion should succeed")
	}
	if !strings.Contains(host.lastPattern, "proj") || !strings.Contains(host.lastPattern, "o*") {
		t.Fatalf("completion should glob the host FS with a proj/o* pattern, got %q", host.lastPattern)
	}
	if got := promptLine(t, e); got != "on" {
		t.Fatalf("should auto-fill the shared 'on' prefix from host results, got %q", got)
	}
}
