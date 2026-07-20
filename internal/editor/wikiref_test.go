package editor

import (
	"bytes"
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

// A window's WikiRoot confines resolution: absolute ids resolve from the
// root (never by ancestor discovery above it) and relative climbs clamp at
// it — a rooted window's links cannot back out of the root.
func TestWikiRootConfinement(t *testing.T) {
	files := map[string]string{
		"wiki/notes/a/b/c.txt":  "[[..:..:..:escape]] [[top:page]]\n",
		"wiki/notes/escape.txt": "clamped target\n",
		"escape.txt":            "outside the root\n",
		"top/page.txt":          "outside the root too\n",
	}
	e, w, root := wikiTreeEditor(t, files, "wiki/notes/a/b/c.txt")
	w.WikiRoot = e.canonicalDocURL(filepath.Join(root, "wiki", "notes"))

	// A triple climb from notes/a/b would reach the tree root; the clamp
	// stops at the wiki root, so it finds notes/escape.txt — NOT the
	// escape.txt above the root.
	res := e.resolveFollow(w, "..:..:..:escape")
	want := e.canonicalDocURL(filepath.Join(root, "wiki", "notes", "escape.txt"))
	if res.url != want {
		t.Fatalf("clamped climb = %q (%q), want %q", res.url, res.message, want)
	}
	if res.root != w.WikiRoot {
		t.Fatal("an in-wiki resolution must carry the window's root")
	}

	// An absolute id resolves from the root ONLY: top/page.txt exists above
	// the root, so within the rooted window it is not found.
	if res := e.resolveFollow(w, "top:page"); res.url != "" {
		t.Fatalf("absolute id above the root must not resolve; got %q", res.url)
	}

	// The same reference resolves fine in an unrooted window (ancestor
	// discovery finds it).
	w.WikiRoot = ""
	if res := e.resolveFollow(w, "top:page"); res.url == "" {
		t.Fatalf("unrooted ancestor discovery should find top:page; got %q", res.message)
	}
}

// A full-scheme reference is the one way out of a rooted wiki: it resolves
// as a NEW-WINDOW destination with no root, and navFollow surfaces a fresh
// window rather than swapping in place — the source window keeps its buffer
// and its root untouched.
func TestSchemeRefOpensNewWindow(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside.txt")
	if err := os.WriteFile(outside, []byte("outside doc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wikiDir := filepath.Join(root, "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := "file://" + filepath.ToSlash(outside)
	content := "x [[" + link + "]] y\n"
	page := filepath.Join(wikiDir, "page.txt")
	if err := os.WriteFile(page, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	e, _, _ := renderedEditorWithConfig(t, "seed\n", "[options]\nsyntax=dokuwiki\n")
	buf, err := buffer.NewFromBytes([]byte(content), page)
	if err != nil {
		t.Fatal(err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "wiki", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buf, SetFocus: true, LinkBrowsing: true,
	})
	w := e.WindowManager.GetWindow("wiki")
	w.WikiRoot = e.canonicalDocURL(wikiDir)

	res := e.resolveFollow(w, link)
	if res.url == "" || !res.newWindow || res.root != "" {
		t.Fatalf("a scheme ref must be a new-window, rootless destination; got %+v", res)
	}

	srcBuf := w.Buffer
	before := len(e.getMainBuffers())                 // notifications spawn work windows; count main ones
	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // inside the [[...]] span
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("navFollow should activate")
	}
	if len(e.getMainBuffers()) != before+1 {
		t.Fatal("a scheme follow must create a new main window")
	}
	if w.Buffer != srcBuf || w.WikiRoot == "" {
		t.Fatal("the source window must keep its buffer and its root")
	}
	fw := e.WindowManager.GetFocusedWindow()
	if fw == w || fw.WikiRoot != "" {
		t.Fatalf("focus should move to a fresh rootless window; got root %q", fw.WikiRoot)
	}
	if fw.Buffer.GetFilename() != outside {
		t.Fatalf("new window should show the destination; got %q", fw.Buffer.GetFilename())
	}
}

// A wiki hosted inside mew's own support tree (mew:///docs) resolves through
// the mew VFS: in-wiki ids match under the root, climbs clamp at it (the
// tree above stays unreachable), and a full mew:/// reference is the
// explicit way out.
func TestMewSpaceWikiRoot(t *testing.T) {
	home := t.TempDir()
	mewDir := filepath.Join(home, ".mew")
	for rel, content := range map[string]string{
		"docs/start.txt":         "[[sample:widget]] [[..:editor.conf]] [[mew:///editor.conf]]\n",
		"docs/sample/widget.txt": "widget page\n",
		"editor.conf":            "# config\n",
	} {
		p := filepath.Join(mewDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.HomeDir = home
	configText := "[options]\nsyntax=dokuwiki\n"
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

	buf, err := e.loadBufferURL("mew:///docs/start.txt")
	if err != nil {
		t.Fatalf("loadBufferURL: %v", err)
	}
	if got := e.bufferCanonicalURL(buf); got != "mew:///docs/start.txt" {
		t.Fatalf("mew buffer identity = %q", got)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "helpdoc", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buf, SetFocus: true, LinkBrowsing: true,
	})
	w := e.WindowManager.GetWindow("helpdoc")
	w.WikiRoot = "mew:///docs"

	// In-wiki absolute id: resolves under the root through the mew VFS.
	res := e.resolveFollow(w, "sample:widget")
	if res.url != "mew:///docs/sample/widget.txt" || res.root != "mew:///docs" {
		t.Fatalf("sample:widget = %+v", res)
	}

	// A climb cannot back out of the root, even though editor.conf exists
	// one level up.
	if res := e.resolveFollow(w, "..:editor.conf"); res.url != "" {
		t.Fatalf("climb must clamp at the wiki root; got %q", res.url)
	}

	// The full-scheme reference IS the way out: a new-window, rootless
	// destination.
	res = e.resolveFollow(w, "mew:///editor.conf")
	if res.url != "mew:///editor.conf" || !res.newWindow || res.root != "" {
		t.Fatalf("mew:///editor.conf = %+v", res)
	}

	// In-place follow keeps the window's root (root is window identity, not
	// visit state), and history restores don't touch it either.
	w.SetCursorPos(window.Position{Line: 0, Rune: 3}) // inside [[sample:widget]]
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("in-wiki follow should navigate")
	}
	if e.bufferCanonicalURL(w.Buffer) != "mew:///docs/sample/widget.txt" {
		t.Fatalf("follow landed on %q", e.bufferCanonicalURL(w.Buffer))
	}
	if w.WikiRoot != "mew:///docs" {
		t.Fatal("the window's root must survive an in-wiki swap")
	}
	if !e.navHistory(-1) {
		t.Fatal("history should return")
	}
	if w.WikiRoot != "mew:///docs" {
		t.Fatal("the window's root must survive a history restore")
	}
}

// mewHomeEditor builds an editor whose mew: tree lives under a temp home,
// pre-populated with files (paths relative to ~/.mew).
func mewHomeEditor(t *testing.T, configText string, files map[string]string) *Editor {
	t.Helper()
	home := t.TempDir()
	mewDir := filepath.Join(home, ".mew")
	for rel, content := range files {
		p := filepath.Join(mewDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.HomeDir = home
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

// The hardcoded help wiki: "help:/..." resolves within mew:///help with the
// dokuwiki format, ".txt" pages, and a "start" start page; following it
// surfaces a NEW window rooted at the wiki (in browse mode), unless the
// current window already carries that root.
func TestHelpWikiScheme(t *testing.T) {
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", map[string]string{
		"help/start.txt":         "[[sample:widget]]\n",
		"help/sample/widget.txt": "widget page\n",
	})
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "doc", Type: window.MainBuffer, Dock: window.DockNone,
		Buffer: buffer.NewFromString("see [[help:/start]] ok\n"), SetFocus: true,
		LinkBrowsing: true,
	})
	w := e.WindowManager.GetWindow("doc")

	// Resolution: the scheme opens pages from the registered root; "/" and
	// ":" both separate in the URL-flavored scheme form; the bare scheme is
	// the start page.
	for _, ref := range []string{"help:/start", "help:/", "help://start"} {
		res := e.resolveFollow(w, ref)
		if res.url != "mew:///help/start.txt" || res.root != "mew:///help" || !res.newWindow {
			t.Fatalf("resolveFollow(%q) = %+v", ref, res)
		}
	}
	for _, ref := range []string{"help:/sample/widget", "help:/sample:widget"} {
		if res := e.resolveFollow(w, ref); res.url != "mew:///help/sample/widget.txt" {
			t.Fatalf("resolveFollow(%q) = %+v", ref, res)
		}
	}
	if res := e.resolveFollow(w, "help:/missing"); res.url != "" || res.message == "" {
		t.Fatalf("missing help page should not resolve; got %+v", res)
	}

	// Following surfaces a fresh window rooted at the wiki, in browse mode;
	// the source window keeps its blank root.
	w.SetCursorPos(window.Position{Line: 0, Rune: 6}) // inside [[help:/start]]
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("navFollow should activate")
	}
	hw := e.WindowManager.GetFocusedWindow()
	if hw == w || hw.WikiRoot != "mew:///help" || !hw.BrowseActive {
		t.Fatalf("help follow should focus a rooted, browsing window; got root %q", hw.WikiRoot)
	}
	if e.bufferCanonicalURL(hw.Buffer) != "mew:///help/start.txt" {
		t.Fatalf("help window shows %q", e.bufferCanonicalURL(hw.Buffer))
	}
	if w.WikiRoot != "" {
		t.Fatal("the source window's root must stay blank")
	}

	// From INSIDE the help window, a help:/ reference is an in-wiki jump:
	// same root, no new window.
	if res := e.resolveFollow(hw, "help:/sample:widget"); res.newWindow {
		t.Fatal("a help:/ ref from the help window must swap in place")
	}
}

// A page inside a registered wiki's root highlights as the wiki's format via
// the registry (no [formats] config needed): the help tree's .txt pages get
// the dokuwiki grammar, so their links extract and browse works.
func TestHelpWikiGrammarFromRegistry(t *testing.T) {
	e := mewHomeEditor(t, "[options]\nsyntaxDetect=yes\n", map[string]string{
		"help/start.txt": "[[sample:widget]]\n",
	})
	buf, err := e.loadBufferURL("mew:///help/start.txt")
	if err != nil {
		t.Fatalf("loadBufferURL: %v", err)
	}
	g, _ := e.bufferGrammar(buf)
	if g == nil || !grammarLinkable(g) {
		t.Fatal("a help page must take the wiki's (linkable) dokuwiki grammar from the registry")
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
