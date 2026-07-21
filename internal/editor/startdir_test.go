package editor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// ensureUsefulStartDir: a root working directory (a GUI launch) moves to the
// last main buffer's file location, then [general] startPath, then home; a
// deliberate working directory is never overridden.
func TestStartDirResolution(t *testing.T) {
	// 1) Root cwd, no better source: home wins.
	e := mewHomeEditor(t, "[options]\n", nil)
	t.Chdir("/")
	e.ensureUsefulStartDir()
	if wd, _ := os.Getwd(); wd != e.home {
		t.Fatalf("bare root launch should land at home; got %q want %q", wd, e.home)
	}

	// 2) startPath beats home.
	sp := t.TempDir()
	e2 := mewHomeEditor(t, "[general]\nstartPath="+sp+"\n", nil)
	if got := e2.LoadedConfig.General.StartPath; got != sp {
		t.Fatalf("startPath parse = %q, want %q", got, sp)
	}
	t.Chdir("/")
	e2.ensureUsefulStartDir()
	if wd, _ := os.Getwd(); wd != sp {
		t.Fatalf("startPath should win over home; got %q want %q", wd, sp)
	}

	// 3) The last main buffer's on-disk file beats startPath.
	docDir := t.TempDir()
	docPath := filepath.Join(docDir, "doc.txt")
	if err := os.WriteFile(docPath, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e3 := mewHomeEditor(t, "[general]\nstartPath="+sp+"\n", nil)
	buf, err := e3.loadBuffer(docPath)
	if err != nil {
		t.Fatal(err)
	}
	e3.WindowManager.CreateWindow(windowMainOpts("doc3", buf))
	t.Chdir("/")
	e3.ensureUsefulStartDir()
	if wd, _ := os.Getwd(); wd != docDir {
		t.Fatalf("the document's directory should win; got %q want %q", wd, docDir)
	}

	// 4) A deliberate (non-root) working directory is untouched.
	keep := t.TempDir()
	t.Chdir(keep)
	e3.ensureUsefulStartDir()
	if wd, _ := os.Getwd(); wd != keep {
		t.Fatalf("a useful cwd must never be overridden; got %q", wd)
	}
}

// isRootDir recognizes filesystem roots only.
func TestIsRootDir(t *testing.T) {
	if !isRootDir("/") {
		t.Fatal("/ is a root")
	}
	if isRootDir("/home") || isRootDir(".") {
		t.Fatal("non-roots misclassified")
	}
}

// A tilde filename normalizes to a real absolute path at open, so in-place
// saves (garland's rename dance) never see the literal "~".
func TestTildeFilenameNormalizesAndSaves(t *testing.T) {
	e := mewHomeEditor(t, "[options]\n", nil)
	docPath := filepath.Join(e.home, "doc.txt")
	if err := os.WriteFile(docPath, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, err := e.loadBuffer("~/doc.txt")
	if err != nil {
		t.Fatalf("loadBuffer(~/doc.txt): %v", err)
	}
	if got := buf.GetFilename(); got != docPath {
		t.Fatalf("filename should normalize to %q; got %q", docPath, got)
	}
	buf.InsertText(0, 0, "X")
	if !e.performSave(buf, buf.GetFilename()) {
		t.Fatal("in-place save should succeed")
	}
	data, err := os.ReadFile(docPath)
	if err != nil || !strings.HasPrefix(string(data), "Xhello") {
		t.Fatalf("saved content = %q, %v", data, err)
	}

	// Save-as with a tilde target also normalizes.
	if !e.performSave(buf, "~/new.txt") {
		t.Fatal("tilde save-as should succeed")
	}
	if _, err := os.Stat(filepath.Join(e.home, "new.txt")); err != nil {
		t.Fatal("tilde save-as should land under home")
	}
}

// Saving a mew:-scheme filename under a VIRTUALIZED mew tree writes through
// the mew VFS and marks the buffer clean — garland has no source for it and
// the document FS does not speak the scheme.
func TestSaveMewTargetVirtual(t *testing.T) {
	fs := &recFS{}
	e := mewFSEditor(t, fs)
	buf, err := e.createBufferURL("mew:///help/start.txt", "start page\n")
	if err != nil {
		t.Fatal(err)
	}
	buf.InsertText(0, 0, "==")
	if !buf.IsModified() {
		t.Fatal("edit should modify")
	}
	if !e.performSave(buf, buf.GetFilename()) {
		t.Fatal("mew:-target save should succeed")
	}
	if got := string(fs.files["mew:///help/start.txt"]); got != "==start page\n" {
		t.Fatalf("VFS received %q", got)
	}
	if buf.IsModified() {
		t.Fatal("save should mark the buffer clean")
	}
}

// windowMainOpts is a tiny helper: a visible, focused main-buffer window.
func windowMainOpts(id string, buf *buffer.Buffer) window.WindowOptions {
	return window.WindowOptions{
		Visible: true, ID: id, Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buf, SetFocus: true, LinkBrowsing: true,
	}
}

// mewFSEditor builds an editor whose mew tree is VIRTUALIZED onto fs.
func mewFSEditor(t *testing.T, fs *recFS) *Editor {
	t.Helper()
	var out bytes.Buffer
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.MewFS = fs
	configText := "[options]\n"
	cfg.ConfigText = &configText
	cfg.Terminal = &TerminalIO{
		Input:  bytes.NewReader(nil),
		Output: &out,
		Size:   func() (int, int, error) { return 80, 24, nil },
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return e
}
