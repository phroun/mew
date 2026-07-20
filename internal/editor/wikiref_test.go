package editor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// Layer 2: the spec's worked examples, context namespace "a:b".
func TestResolveWikiRefSpecExamples(t *testing.T) {
	cfg := defaultWikiCfg()
	cases := map[string]string{
		"foo":          "a:b:foo",
		"foo:bar":      "foo:bar",
		":foo:bar":     "foo:bar",
		".foo":         "a:b:foo",
		".foo:bar":     "a:b:foo:bar",
		"..foo":        "a:foo",
		"..:foo":       "a:foo",
		".:..:example": "a:example",
		"..:..:x":      "x",
	}
	for ref, want := range cases {
		got, _, _ := resolveWikiRef(ref, "a:b", cfg)
		if got != want {
			t.Errorf("resolve(%q) = %q, want %q", ref, got, want)
		}
	}

	// Fragment splits off; namespace targets are flagged.
	id, anchor, _ := resolveWikiRef("foo#section one", "a:b", cfg)
	if id != "a:b:foo" || anchor != "section one" {
		t.Errorf("fragment: id=%q anchor=%q", id, anchor)
	}
	if _, _, ns := resolveWikiRef("foo:", "a:b", cfg); !ns {
		t.Error("trailing separator should flag a namespace target")
	}

	// useslash: "/" becomes a separator.
	slash := cfg
	slash.useSlash = true
	if got, _, _ := resolveWikiRef("x/y", "", slash); got != "x:y" {
		t.Errorf("useslash resolve = %q, want x:y", got)
	}
}

// Layer 3: canonicalization by Unicode categories, separator normalization,
// collapsing, and edge trimming.
func TestCleanWikiID(t *testing.T) {
	cfg := defaultWikiCfg()
	cases := map[string]string{
		"Hello World":  "hello_world",
		"a;b":          "a:b",
		" :foo:bar: ":  "foo:bar",
		"foo__bar":     "foo_bar",
		"a:_b":         "a:b",
		"Épée être":    "epee_etre",
		"Mixed.Case-1": "mixed.case-1",
		"a//b":         "a_b", // useslash off: "/" is not a separator
	}
	for in, want := range cases {
		if got := cleanWikiID(in, cfg); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
	slash := cfg
	slash.useSlash = true
	if got := cleanWikiID("a/b", slash); got != "a:b" {
		t.Errorf("useslash clean = %q, want a:b", got)
	}
}

// Glued leading dot-runs split into markers; separated forms pass through.
func TestSplitGluedDots(t *testing.T) {
	cases := map[string]string{
		"..foo":  "..:foo",
		".foo":   ".:foo",
		"..:foo": "..:foo",
		"foo":    "foo",
		"..":     "..",
	}
	for in, want := range cases {
		if got := splitGluedDots(in); got != want {
			t.Errorf("splitGluedDots(%q) = %q, want %q", in, got, want)
		}
	}
}

// Layer 1: only registered schemes with a slash form gate out; a bare
// namespace path never does. Interwiki is recognized by its ">" form.
func TestSchemeGate(t *testing.T) {
	if _, ok := schemeRef("wiki:syntax"); ok {
		t.Error("a namespace path must not gate as a scheme")
	}
	if _, ok := schemeRef("http://example.com/x"); !ok {
		t.Error("http:// must gate as a scheme")
	}
	if _, ok := schemeRef("mew:/syntax/x.jsf"); !ok {
		t.Error("mew:/ must gate as a scheme")
	}
	if _, _, ok := interwikiRef("wp>Main Page"); !ok {
		t.Error("shortcut>rest must gate as interwiki")
	}
}

// wikiTreeEditor builds a real on-disk wiki tree, opens the page at relPath
// (content given) in a focused main window, and returns the pieces.
func wikiTreeEditor(t *testing.T, files map[string]string, openRel string) (*Editor, *window.Window, string) {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e, _, _ := renderedEditorWithConfig(t, "seed\n", "[options]\nsyntax=dokuwiki\n")
	openPath := filepath.Join(root, filepath.FromSlash(openRel))
	buf, err := buffer.NewFromBytes([]byte(files[openRel]), openPath)
	if err != nil {
		t.Fatal(err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "wiki", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buf, SetFocus: true, LinkBrowsing: true,
	})
	return e, e.WindowManager.GetWindow("wiki"), root
}

// resolveFollow matches ids against the real tree: relative and dot-climb
// refs against the page's directory, absolute refs from the nearest ancestor
// that holds them, case-insensitively, with start pages for namespaces.
func TestResolveFollowMatching(t *testing.T) {
	files := map[string]string{
		"notes/a/b/c.txt":         "[[foo]] [[..bar]] [[wiki:syntax]] [[sub:]] [[MyPage]]\n",
		"notes/a/b/foo.txt":       "foo page\n",
		"notes/a/bar.txt":         "bar page\n",
		"wiki/syntax.txt":         "syntax page\n",
		"notes/a/b/sub/start.txt": "sub start\n",
		"notes/a/b/MyPage.txt":    "cased page\n",
	}
	e, w, root := wikiTreeEditor(t, files, "notes/a/b/c.txt")

	expect := map[string]string{
		"foo":         "notes/a/b/foo.txt",
		"..bar":       "notes/a/bar.txt",
		"wiki:syntax": "wiki/syntax.txt",
		"sub:":        "notes/a/b/sub/start.txt",
		"mypage":      "notes/a/b/MyPage.txt", // case-folded match, on-disk name wins
	}
	for ref, rel := range expect {
		want := e.canonicalDocURL(filepath.Join(root, filepath.FromSlash(rel)))
		res := e.resolveFollow(w, ref)
		if res.url != want {
			t.Errorf("resolveFollow(%q) = %q (%q), want %q", ref, res.url, res.message, want)
		}
	}

	// Gated and missing targets come back as messages, not URLs.
	for _, ref := range []string{"http://example.com/", "wp>Main", "no_such_page"} {
		if res := e.resolveFollow(w, ref); res.url != "" || res.message == "" {
			t.Errorf("resolveFollow(%q) should not resolve; got %q", ref, res.url)
		}
	}
}

// navFollow navigates: the focused button's target loads (or reuses) the
// destination buffer and swaps it into the window; nav_history_prior returns
// to the source with the caret intact; re-following reuses the SAME buffer.
func TestNavFollowSwapsAndReuses(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, root := wikiTreeEditor(t, files, "w/page.txt")
	src := w.Buffer

	// Focus the button: caret inside the [[other]] span (runes 3..12).
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("navFollow should activate the focused button")
	}
	wantPath := filepath.Join(root, "w", "other.txt")
	if w.Buffer == src || w.Buffer.GetFilename() != wantPath {
		t.Fatalf("navFollow should swap to %s; got %q", wantPath, w.Buffer.GetFilename())
	}
	dest := w.Buffer
	if !w.BrowseActive {
		t.Fatal("browse mode should stay armed after a follow")
	}
	// The visit is recorded editor-wide under the RESOLVED identity, so any
	// spelling of this destination now reads visited.
	if !e.linkVisitSeen[e.canonicalDocURL(wantPath)] {
		t.Fatal("the visit should be recorded under the resolved canonical URL")
	}

	// Back: the source binding restores with its caret.
	if !e.navHistory(-1) {
		t.Fatal("nav_history_prior should return")
	}
	if w.Buffer != src {
		t.Fatal("prior must restore the source buffer")
	}
	if got := w.CursorPos(); got.Line != 0 || got.Rune != 5 {
		t.Fatalf("prior must restore the caret; got %+v", got)
	}

	// Re-follow: the destination buffer is REUSED (found in the forward
	// stack), not re-loaded.
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("re-follow should activate")
	}
	if w.Buffer != dest {
		t.Fatal("re-follow must reuse the already-open destination buffer")
	}
}
