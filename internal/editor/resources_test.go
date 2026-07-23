package editor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The mew: filesystem is a layered overlay: reads fall through the user's
// ~/.mew, the read-only system resource dirs, then mew's embedded tree; writes
// only ever touch the user layer. This exercises the layering directly on a
// mewVFS with a temp system dir.
func TestMewVFSLayeredReads(t *testing.T) {
	userRoot := t.TempDir()
	sysDir := t.TempDir()

	// System layer ships two files; the embedded layer ships help/start.txt.
	mustWrite(t, filepath.Join(sysDir, "help", "sys.txt"), "from system\n")
	mustWrite(t, filepath.Join(sysDir, "syntax", "go.jsf"), "SYSTEM GO GRAMMAR\n")

	v := &mewVFS{fs: osFileSystem{}, localRoot: userRoot, sysDirs: []string{sysDir}}

	// Absent from user + system: served from the embedded tree.
	if data, err := v.ReadFile("mew:///help/start.txt"); err != nil ||
		!strings.Contains(string(data), "mew Help") {
		t.Fatalf("embedded help/start.txt not served: %v", err)
	}
	// Present in system only: served from the system layer.
	if data, err := v.ReadFile("mew:///help/sys.txt"); err != nil ||
		strings.TrimSpace(string(data)) != "from system" {
		t.Fatalf("system help/sys.txt not served: %v %q", err, data)
	}
	// The real go.jsf (embedded) is shadowed by the system copy.
	if data, err := v.ReadFile("mew:///syntax/go.jsf"); err != nil ||
		!strings.Contains(string(data), "SYSTEM GO GRAMMAR") {
		t.Fatalf("system syntax/go.jsf should shadow embedded: %v", err)
	}

	// The user layer shadows both system and embedded.
	mustWrite(t, filepath.Join(userRoot, "help", "start.txt"), "MY START\n")
	if data, err := v.ReadFile("mew:///help/start.txt"); err != nil ||
		strings.TrimSpace(string(data)) != "MY START" {
		t.Fatalf("user layer should shadow: %v %q", err, data)
	}

	// A write only ever touches the user layer.
	if err := v.WriteFile("mew:///help/sys.txt", []byte("shadowed now\n")); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(userRoot, "help", "sys.txt")); err != nil {
		t.Fatalf("write should land in the user layer: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sysDir, "help", "sys.txt.new")); err == nil {
		t.Fatal("write must not touch the read-only system layer")
	}
}

// Glob unions the layers and de-dups by rel, so a shipped help page lists
// alongside the user's own — exactly as the wiki resolver needs.
func TestMewVFSGlobUnions(t *testing.T) {
	userRoot := t.TempDir()
	sysDir := t.TempDir()
	mustWrite(t, filepath.Join(userRoot, "help", "mine.txt"), "mine\n")
	mustWrite(t, filepath.Join(sysDir, "help", "sys.txt"), "sys\n")

	v := &mewVFS{fs: osFileSystem{}, localRoot: userRoot, sysDirs: []string{sysDir}}
	got := map[string]bool{}
	matches, err := v.Glob("mew:///help/*")
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	for _, m := range matches {
		got[m] = true
	}
	for _, want := range []string{"mew:///help/mine.txt", "mew:///help/sys.txt", "mew:///help/start.txt"} {
		if !got[want] {
			t.Fatalf("Glob missing %s; got %v", want, matches)
		}
	}
}

// systemResourceDirs honors an explicit override that exists and drops one that
// does not.
func TestSystemResourceDirsOverride(t *testing.T) {
	dir := t.TempDir()
	if got := systemResourceDirs(dir); len(got) != 1 || got[0] != dir {
		t.Fatalf("override should resolve to [%s]; got %v", dir, got)
	}
	if got := systemResourceDirs(filepath.Join(dir, "nope")); got != nil {
		t.Fatalf("non-existent override should be dropped; got %v", got)
	}
}

// A syntax grammar resolves from the embedded layer on a fresh install, and no
// copy is written into ~/.mew/syntax (the old install-on-first-run behavior is
// gone).
func TestSyntaxResolvesFromEmbeddedWithoutCopy(t *testing.T) {
	e := mewHomeEditor(t, "[options]\n", nil)
	src, err := e.resolveSyntaxFile("go", false)
	if err != nil || len(src) == 0 {
		t.Fatalf("go grammar should resolve from the embedded layer: %v", err)
	}
	// No physical ~/.mew/syntax directory was created.
	if e.mew.localRoot == "" {
		t.Fatal("expected a local mew root")
	}
	if _, err := os.Stat(filepath.Join(e.mew.localRoot, "syntax")); !os.IsNotExist(err) {
		t.Fatalf("~/.mew/syntax must not be created on first run (err=%v)", err)
	}
}

// A help page the user has no local copy of resolves from the embedded tree and
// loads with the shipped content — not a blank buffer — while a user's own copy
// shadows it.
func TestHelpPageEmbeddedFallbackAndShadow(t *testing.T) {
	// No ~/.mew/help/start.txt: the embedded page backs help:/start.
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", nil)
	res := e.resolveFollow(nil, "help:/start")
	if res.url == "" {
		t.Fatalf("help:/start should resolve from the embedded fallback; got %+v", res)
	}
	buf, err := e.loadBufferURL(res.url)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if buf.GetLineCount() < 2 || !strings.Contains(buf.GetLine(0), "mew Help") {
		t.Fatalf("embedded help came up blank/wrong: %q", buf.GetLine(0))
	}

	// A user copy shadows the shipped page.
	e2 := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", map[string]string{
		"help/start.txt": "MY LOCAL START PAGE\n",
	})
	res2 := e2.resolveFollow(nil, "help:/start")
	buf2, err := e2.loadBufferURL(res2.url)
	if err != nil {
		t.Fatalf("load shadow: %v", err)
	}
	if !strings.Contains(buf2.GetLine(0), "MY LOCAL START PAGE") {
		t.Fatalf("user copy should shadow the shipped page; got %q", buf2.GetLine(0))
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
