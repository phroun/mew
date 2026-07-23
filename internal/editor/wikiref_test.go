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
		Visible: true, ID: "wiki", Type: window.DocWindow, Dock: window.DockNone,
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
		Visible: true, ID: "wiki", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buf, SetFocus: true, LinkBrowsing: true,
	})
	w := e.WindowManager.GetWindow("wiki")
	w.WikiRoot = e.canonicalDocURL(wikiDir)

	res := e.resolveFollow(w, link)
	if res.url == "" || !res.newWindow || res.root != "" {
		t.Fatalf("a scheme ref must be a new-window, rootless destination; got %+v", res)
	}

	srcBuf := w.Buffer
	before := len(e.contentWindows())                 // notifications spawn work windows; count main ones
	w.SetCursorPos(window.Position{Line: 0, Rune: 5}) // inside the [[...]] span
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("navFollow should activate")
	}
	if len(e.contentWindows()) != before+1 {
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

	// LOCAL mode: a mew:/// name canonicalizes to the REAL file it names, so
	// the mew spelling and the ~/.mew path are ONE identity (one buffer),
	// and the buffer loads with a real filename (full source tracking).
	rootURL := e.canonicalDocURL("mew:///docs")
	startURL := e.canonicalDocURL("mew:///docs/start.txt")
	widgetURL := e.canonicalDocURL("mew:///docs/sample/widget.txt")
	if !strings.HasPrefix(startURL, "file://") {
		t.Fatalf("local mew identity should be the real file; got %q", startURL)
	}
	if startURL != e.canonicalDocURL(filepath.Join(mewDir, "docs", "start.txt")) {
		t.Fatal("the mew spelling and the real path must be one identity")
	}

	buf, err := e.loadBufferURL("mew:///docs/start.txt")
	if err != nil {
		t.Fatalf("loadBufferURL: %v", err)
	}
	if got := e.bufferCanonicalURL(buf); got != startURL {
		t.Fatalf("mew buffer identity = %q, want %q", got, startURL)
	}
	if buf.GetFilename() != filepath.Join(mewDir, "docs", "start.txt") {
		t.Fatalf("local mew buffer should carry the real path; got %q", buf.GetFilename())
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "helpdoc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buf, SetFocus: true, LinkBrowsing: true,
	})
	w := e.WindowManager.GetWindow("helpdoc")
	w.WikiRoot = rootURL

	// In-wiki absolute id: resolves under the root.
	res := e.resolveFollow(w, "sample:widget")
	if res.url != widgetURL || res.root != rootURL {
		t.Fatalf("sample:widget = %+v", res)
	}

	// A climb cannot back out of the root, even though editor.conf exists
	// one level up.
	if res := e.resolveFollow(w, "..:editor.conf"); res.url != "" {
		t.Fatalf("climb must clamp at the wiki root; got %q", res.url)
	}

	// The full-scheme reference IS the way out: a new-window, rootless
	// destination (canonicalized to its real-file identity).
	res = e.resolveFollow(w, "mew:///editor.conf")
	if res.url != e.canonicalDocURL(filepath.Join(mewDir, "editor.conf")) || !res.newWindow || res.root != "" {
		t.Fatalf("mew:///editor.conf = %+v", res)
	}

	// In-place follow keeps the window's root (root is window identity, not
	// visit state), and history restores don't touch it either.
	w.SetCursorPos(window.Position{Line: 0, Rune: 3}) // inside [[sample:widget]]
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("in-wiki follow should navigate")
	}
	if e.bufferCanonicalURL(w.Buffer) != widgetURL {
		t.Fatalf("follow landed on %q", e.bufferCanonicalURL(w.Buffer))
	}
	if w.WikiRoot != rootURL {
		t.Fatal("the window's root must survive an in-wiki swap")
	}
	if !e.navHistory(-1) {
		t.Fatal("history should return")
	}
	if w.WikiRoot != rootURL {
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
		Visible: true, ID: "doc", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString("see [[help:/start]] ok\n"), SetFocus: true,
		LinkBrowsing: true,
	})
	w := e.WindowManager.GetWindow("doc")

	// Canonical identities (in local mode the mew:/// spellings translate to
	// the real ~/.mew files — one identity either way).
	helpRoot := e.canonicalDocURL("mew:///help")
	startURL := e.canonicalDocURL("mew:///help/start.txt")
	widgetURL := e.canonicalDocURL("mew:///help/sample/widget.txt")

	// Resolution: the scheme opens pages from the registered root; "/" and
	// ":" both separate in the URL-flavored scheme form; the bare scheme is
	// the start page.
	for _, ref := range []string{"help:/start", "help:/", "help://start"} {
		res := e.resolveFollow(w, ref)
		if res.url != startURL || res.root != helpRoot ||
			res.wikiName != "help" || !res.newWindow {
			t.Fatalf("resolveFollow(%q) = %+v", ref, res)
		}
	}
	for _, ref := range []string{"help:/sample/widget", "help:/sample:widget"} {
		if res := e.resolveFollow(w, ref); res.url != widgetURL {
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
	if hw == w || hw.WikiRoot != helpRoot || !hw.BrowseActive {
		t.Fatalf("help follow should focus a rooted, browsing window; got root %q", hw.WikiRoot)
	}
	if hw.WikiName != "help" {
		t.Fatalf("the help window must know its registry name; got %q", hw.WikiName)
	}
	if e.bufferCanonicalURL(hw.Buffer) != startURL {
		t.Fatalf("help window shows %q", e.bufferCanonicalURL(hw.Buffer))
	}
	if w.WikiRoot != "" || w.WikiName != "" {
		t.Fatal("the source window's root and wiki name must stay blank")
	}

	// In-wiki resolutions from the rooted window carry the full identity.
	if res := e.resolveFollow(hw, "sample:widget"); res.wikiName != "help" || res.root != helpRoot {
		t.Fatalf("in-wiki resolution should carry the wiki identity; got %+v", res)
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

// Following a link to a missing page in a WRITABLE wiki space offers a
// two-row create prompt (lock-prompt style; the prompt buffer holds "y" and
// "n" above the blank default line). Accepting mints an empty, unsaved
// buffer named for the would-be file and swaps to it; declining (the
// default) stays put. A wiki registered non-writable never prompts.
func TestCreatePagePrompt(t *testing.T) {
	files := map[string]string{
		"w/page.txt": "see [[newpage|My New Page]] and [[another]] end\n",
	}
	e, w, root := wikiTreeEditor(t, files, "w/page.txt")
	src := w.Buffer

	// Follow the missing page: the prompt appears with the title on the top
	// row, the question on the input row, and y/n/blank in the buffer.
	w.SetCursorPos(window.Position{Line: 0, Rune: 6})
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("navFollow should activate")
	}
	p := focusedPrompt(e)
	if p == nil {
		t.Fatal("a writable-space miss should prompt")
	}
	if p.MessageTopInner != "Page not found: My New Page" {
		t.Fatalf("top message = %q", p.MessageTopInner)
	}
	if len(p.RowMessages) == 0 || !strings.Contains(p.RowMessages[0], "Create it? [y/N]: ") {
		t.Fatalf("question row = %v", p.RowMessages)
	}
	if got := p.Buffer.GetContent(); got != "y\nn\n" {
		t.Fatalf("prompt buffer = %q, want y/n/blank", got)
	}

	// Accept: an unsaved buffer named for the would-be file, seeded with a
	// heading from the link's title, caret at EOF, swapped in.
	answerPrompt(t, e, "y")
	wantPath := filepath.Join(root, "w", "newpage.txt")
	if w.Buffer == src || w.Buffer.GetFilename() != wantPath {
		t.Fatalf("create should swap to %s; got %q", wantPath, w.Buffer.GetFilename())
	}
	if got := w.Buffer.GetContent(); got != "=== My New Page ===\n\n" {
		t.Fatalf("created page seed = %q", got)
	}
	if pos := w.CursorPos(); pos.Line != w.Buffer.GetLineCount()-1 || pos.Rune != 0 {
		t.Fatalf("caret should sit at EOF; got %+v of %d lines", pos, w.Buffer.GetLineCount())
	}
	if _, err := os.Stat(wantPath); !os.IsNotExist(err) {
		t.Fatal("the file must not exist until the buffer is saved")
	}
	if !e.navHistory(-1) || w.Buffer != src {
		t.Fatal("history should return to the source page")
	}

	// Decline (bare Enter = default No): nothing changes.
	w.SetCursorPos(window.Position{Line: 0, Rune: 34}) // inside [[another]]
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("navFollow should activate")
	}
	if focusedPrompt(e) == nil {
		t.Fatal("second miss should prompt too")
	}
	answerPrompt(t, e, "")
	if w.Buffer != src {
		t.Fatal("declining must not swap")
	}

	// A non-writable registered wiki never prompts: notification only.
	wikiRegistry["rotest"] = wikiDef{
		Name: "rotest", Format: "dokuwiki",
		Root: e.canonicalDocURL(filepath.Join(root, "w")), Ext: ".txt", Start: "start",
	}
	defer delete(wikiRegistry, "rotest")
	w.WikiRoot = e.canonicalDocURL(filepath.Join(root, "w"))
	w.WikiName = "rotest"
	w.SetCursorPos(window.Position{Line: 0, Rune: 6})
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("navFollow should still activate")
	}
	if focusedPrompt(e) != nil {
		t.Fatal("a non-writable wiki must not offer creation")
	}
	if w.Buffer != src {
		t.Fatal("non-writable miss must not swap")
	}
}

// nav_clear forgets every visited link editor-wide; nav_history_clear drops
// the window's back/forward history, releasing stacked bindings EXCEPT one
// holding the last reference to a buffer, which is kept so the buffer stays
// reachable.
func TestNavClearAndHistoryClear(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
		"w/third.txt": "third content\n",
	}
	e, w, root := wikiTreeEditor(t, files, "w/page.txt")
	src := w.Buffer

	// Visit a link, then clear the visited set.
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("follow should navigate")
	}
	otherBuf := w.Buffer
	if !e.navHistory(-1) {
		t.Fatal("history should return")
	}
	if !e.linkTargetVisited(w, "other") {
		t.Fatal("the followed link should read visited")
	}
	if !e.navClearVisited() {
		t.Fatal("nav_clear should clear something")
	}
	if e.linkTargetVisited(w, "other") {
		t.Fatal("visited status should be gone after nav_clear")
	}
	if e.navClearVisited() {
		t.Fatal("a second nav_clear has nothing to do (chains fall through)")
	}

	// State now: fwd = [other] (its only reference). A new departure would
	// invalidate the forward trail — the graveyard catches the orphan. Open
	// third.txt in ANOTHER window so its bindings are never last references.
	thirdPath := filepath.Join(root, "w", "third.txt")
	thirdBuf, err := buffer.NewFromBytes([]byte(files["w/third.txt"]), thirdPath)
	if err != nil {
		t.Fatal(err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "elsewhere", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: thirdBuf, SetFocus: false, LinkBrowsing: true,
	})
	e.swapBuffer(w, thirdBuf) // invalidates fwd=[other]: other is BURIED, not released
	gb := w.GraveyardBuffers()
	if len(gb) != 1 || gb[0] != otherBuf {
		t.Fatalf("graveyard = %v, want [other]", gb)
	}
	if !w.NavHistoryPrior() {
		t.Fatal("prior should restore src")
	}

	// Re-following the link RESURRECTS the graveyard buffer: the same buffer
	// object comes back, not a re-load.
	otherURL := e.canonicalDocURL(filepath.Join(root, "w", "other.txt"))
	if e.findOpenBuffer(otherURL) != otherBuf {
		t.Fatal("the buried buffer must be findable by canonical URL")
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 5})
	w.BrowseActive = true
	if !e.navFollow() {
		t.Fatal("re-follow should navigate")
	}
	if w.Buffer != otherBuf {
		t.Fatal("re-follow must resurrect the graveyard buffer, not re-load")
	}
	if !e.navHistory(-1) {
		t.Fatal("prior should restore src again")
	}

	// Clear: fwd = [other] again, but other already has a graveyard entry, so
	// the stack binding is released (duplicate burial) — the stacks empty,
	// and the buffer stays reachable through the graveyard alone.
	if !e.navHistoryClear() {
		t.Fatal("nav_history_clear should act on a non-empty history")
	}
	if prior, next := w.NavHistoryDepths(); prior != 0 || next != 0 {
		t.Fatalf("depths after clear = (%d,%d): the stacks must empty", prior, next)
	}
	if e.findOpenBuffer(otherURL) != otherBuf {
		t.Fatal("the graveyard must keep other.txt reachable after the clear")
	}
	if w.Buffer != src {
		t.Fatal("the active buffer is never touched by a history clear")
	}

	// A history whose entries are all referenced elsewhere clears fully with
	// nothing new buried: stack third (shown in the other window) then clear.
	e.swapBuffer(w, thirdBuf)
	if !w.NavHistoryPrior() {
		t.Fatal("prior should restore src once more")
	}
	if !e.navHistoryClear() {
		t.Fatal("clear should act")
	}
	if prior, next := w.NavHistoryDepths(); prior != 0 || next != 0 {
		t.Fatalf("depths = (%d,%d): a shared buffer's binding is released", prior, next)
	}
	if len(w.GraveyardBuffers()) != 1 {
		t.Fatal("no new burials expected for a shared buffer")
	}
	if e.navHistoryClear() {
		t.Fatal("an empty history has nothing to clear (chains fall through)")
	}
}

// buffer_close with a non-empty graveyard resurrects the most recent burial
// into the SAME window instead of closing it; and a buffer becoming actively
// bound anywhere (window creation, swap) leaves every graveyard — it is no
// longer at risk of orphaning.
func TestBufferCloseResurrectsAndUnbury(t *testing.T) {
	files := map[string]string{
		"w/page.txt":  "go [[other]] now\n",
		"w/other.txt": "other content\n",
	}
	e, w, root := wikiTreeEditor(t, files, "w/page.txt")

	buryOther := func() *buffer.Buffer {
		w.SetCursorPos(window.Position{Line: 0, Rune: 5})
		w.BrowseActive = true
		if !e.navFollow() {
			t.Fatal("follow should navigate")
		}
		other := w.Buffer
		if !e.navHistory(-1) {
			t.Fatal("prior should return")
		}
		third, err := buffer.NewFromBytes([]byte("third\n"), filepath.Join(root, "w", "third.txt"))
		if err != nil {
			t.Fatal(err)
		}
		e.swapBuffer(w, third) // invalidates fwd=[other]: buried
		gb := w.GraveyardBuffers()
		if len(gb) != 1 || gb[0] != other {
			t.Fatalf("graveyard = %v, want [other]", gb)
		}
		return other
	}

	// Resurrection: closing the active buffer surfaces the burial; the
	// window survives with its identity and history.
	other := buryOther()
	mains := len(e.contentWindows())
	if !e.closeCurrentBuffer() {
		t.Fatal("buffer_close should act")
	}
	if e.WindowManager.GetWindow("wiki") != w {
		t.Fatal("the window must survive a resurrecting close")
	}
	if len(e.contentWindows()) != mains {
		t.Fatal("no window should close during resurrection")
	}
	if w.Buffer != other {
		t.Fatalf("the most recent burial should surface; got %q", w.Buffer.GetFilename())
	}
	if len(w.GraveyardBuffers()) != 0 {
		t.Fatal("the resurrected binding must leave the graveyard")
	}

	// With the graveyard empty, a second close takes the normal window-close
	// path (the harness "doc" window remains, so no exit).
	if !e.closeCurrentBuffer() {
		t.Fatal("second close should act")
	}
	if e.WindowManager.GetWindow("wiki") != nil {
		t.Fatal("an empty-graveyard close removes the window")
	}

	// Unbury on active bind: rebuild a burial in a fresh window, then bind
	// the buried buffer in a NEW window — the graveyard entry dissolves.
	buf2, err := buffer.NewFromBytes([]byte(files["w/page.txt"]), filepath.Join(root, "w", "page.txt"))
	if err != nil {
		t.Fatal(err)
	}
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "wiki", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buf2, SetFocus: true, LinkBrowsing: true,
	})
	w = e.WindowManager.GetWindow("wiki")
	other2 := buryOther()
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "viewer", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: other2, SetFocus: false, LinkBrowsing: true,
	})
	if len(w.GraveyardBuffers()) != 0 {
		t.Fatal("binding the buried buffer in another window must unbury it")
	}
}

// createBufferURL under a VIRTUALIZED mew tree (no real path exists) mints an
// empty memory buffer whose filename is the canonical URL — regression for
// garland treating nil DataBytes as "no data source provided".
func TestCreateBufferURLVirtualMew(t *testing.T) {
	var out bytes.Buffer
	cfg := DefaultConfig()
	cfg.SkipUserConfig = true
	cfg.SkipProfileScript = true
	cfg.ColdStoragePath = t.TempDir()
	cfg.MewFS = &recFS{}
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
	buf, err := e.createBufferURL("mew:///help/newpage.txt", "")
	if err != nil {
		t.Fatalf("createBufferURL: %v", err)
	}
	if buf.GetFilename() != "mew:///help/newpage.txt" {
		t.Fatalf("virtual mew buffer filename = %q", buf.GetFilename())
	}
	if got := buf.GetContent(); got != "" {
		t.Fatalf("created page should be empty; got %q", got)
	}
	seeded, err := e.createBufferURL("mew:///help/seeded.txt", "=== T ===\n\n")
	if err != nil {
		t.Fatalf("seeded create: %v", err)
	}
	if got := seeded.GetContent(); got != "=== T ===\n\n" {
		t.Fatalf("seed content = %q", got)
	}
}

// openFile on a registered wiki scheme opens the PAGE (loading the real
// file, rooted in the wiki), instead of a blank buffer under the literal
// name — and a trailing ".txt" the user typed is tolerated and stripped
// (the page id is extensionless internally).
func TestOpenFileWikiScheme(t *testing.T) {
	startURL := ""
	newFocused := func(e *Editor) *window.Window {
		return e.WindowManager.GetFocusedWindow()
	}

	for _, ref := range []string{"help:/start", "help:/start.txt", "help:/"} {
		e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", map[string]string{
			"help/start.txt": "the start page body\n",
		})
		if startURL == "" {
			startURL = e.canonicalDocURL("mew:///help/start.txt")
		}
		if !e.openFile(ref) {
			t.Fatalf("openFile(%q) should succeed", ref)
		}
		w := newFocused(e)
		if got := e.bufferCanonicalURL(w.Buffer); got != e.canonicalDocURL("mew:///help/start.txt") {
			t.Fatalf("openFile(%q): buffer %q, want the help start page", ref, got)
		}
		if !strings.Contains(w.Buffer.GetContent(), "the start page body") {
			t.Fatalf("openFile(%q): page content not loaded: %q", ref, w.Buffer.GetContent())
		}
		if w.WikiName != "help" || w.WikiRoot != e.canonicalDocURL("mew:///help") {
			t.Fatalf("openFile(%q): window not rooted in the wiki (name %q root %q)", ref, w.WikiName, w.WikiRoot)
		}
	}
}

// openFile on a missing wiki page creates it (writable wiki) rather than
// leaving a blank buffer named for the literal scheme text.
func TestOpenFileWikiSchemeMissingCreates(t *testing.T) {
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", map[string]string{
		"help/start.txt": "start\n",
	})
	if !e.openFile("help:/brandnew") {
		t.Fatal("openFile on a missing writable page should succeed (create)")
	}
	w := e.WindowManager.GetFocusedWindow()
	if w.WikiName != "help" {
		t.Fatalf("created page window should be rooted in the wiki, got %q", w.WikiName)
	}
	// The buffer must not be named for the literal "help:/brandnew".
	if fn := w.Buffer.GetFilename(); strings.Contains(fn, "help:/") {
		t.Fatalf("created buffer should carry the resolved page path, not %q", fn)
	}
}

// A wiki-rooted window displays its page as the scheme form (help:/start),
// hiding the .txt and the underlying file path; non-wiki windows and buffers
// outside the root defer to the ordinary filename.
func TestWikiDisplayName(t *testing.T) {
	e := mewHomeEditor(t, "[options]\nsyntax=dokuwiki\n", map[string]string{
		"help/start.txt":         "start\n",
		"help/sample/widget.txt": "widget\n",
	})
	e.openFile("help:/start")
	w := e.WindowManager.GetFocusedWindow()
	if got := e.wikiDisplayName(w); got != "help:/start" {
		t.Fatalf("wikiDisplayName(start) = %q, want help:/start", got)
	}

	e.openFile("help:/sample/widget")
	w2 := e.WindowManager.GetFocusedWindow()
	if got := e.wikiDisplayName(w2); got != "help:/sample/widget" {
		t.Fatalf("wikiDisplayName(widget) = %q, want help:/sample/widget", got)
	}

	// A non-wiki window has no scheme display name.
	e.WindowManager.CreateWindow(window.WindowOptions{
		Visible: true, ID: "plain", Type: window.DocWindow, Dock: window.DockNone,
		Buffer: buffer.NewFromString("hi\n"), SetFocus: true,
	})
	if got := e.wikiDisplayName(e.WindowManager.GetWindow("plain")); got != "" {
		t.Fatalf("a non-wiki window should have no wiki display name, got %q", got)
	}
}
