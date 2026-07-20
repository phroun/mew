package editor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/buffer"
	"github.com/phroun/mew/internal/window"
)

// canonicalDocURL normalizes every accepted spelling to one identity:
// scheme + empty authority + "/"-separated absolute path (file:///... or
// mew:///...).
func TestCanonicalDocURL(t *testing.T) {
	e, _, _ := renderedEditorWithConfig(t, "x\n", "[options]\n")
	cases := map[string]string{
		"/a/../b/c.txt":     "file:///b/c.txt",
		"/a/./b//c.txt":     "file:///a/b/c.txt",
		"file:///x//y/../z": "file:///x/z",
		"file://x/y":        "file:///x/y",
		"":                  "",
	}
	for in, want := range cases {
		if got := e.canonicalDocURL(in); got != want {
			t.Errorf("canonicalDocURL(%q) = %q, want %q", in, got, want)
		}
	}

	// In LOCAL mode every mew: spelling translates to the REAL file under
	// ~/.mew — the same identity the plain path canonicalizes to, so the two
	// routes to one file can never alias into two buffers.
	mewCases := map[string]string{
		"mew:syntax/x.jsf":  "syntax/x.jsf",
		"mew:/syntax/x.jsf": "syntax/x.jsf",
		"mew:///a/../../b":  "b", // confined: ".." cannot escape the mew root
	}
	for in, rel := range mewCases {
		want := e.canonicalDocURL(filepath.Join(e.home, ".mew", filepath.FromSlash(rel)))
		got := e.canonicalDocURL(in)
		if got != want || !strings.HasPrefix(got, "file://") {
			t.Errorf("canonicalDocURL(%q) = %q, want real-file identity %q", in, got, want)
		}
	}

	// A relative path resolves against the working directory.
	abs, err := filepath.Abs("rel.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := "file://" + filepath.ToSlash(abs)
	if got := e.canonicalDocURL("rel.txt"); got != want {
		t.Errorf("relative path = %q, want %q", got, want)
	}
}

// findOpenBuffer matches canonical identities across every window's active
// binding AND its nav-history stacks: a buffer parked behind a link follow is
// still open and must be reused, not re-loaded.
func TestFindOpenBufferAcrossStacks(t *testing.T) {
	e, w, _ := renderedEditorWithConfig(t, "seed\n", "[options]\n")

	bufA, err := buffer.NewFromBytes([]byte("alpha\n"), "/tmp/canon-a.txt")
	if err != nil {
		t.Fatal(err)
	}
	bufB, err := buffer.NewFromBytes([]byte("beta\n"), "/tmp/canon-b.txt")
	if err != nil {
		t.Fatal(err)
	}

	e.swapBuffer(w, bufA) // seed buffer -> back stack
	e.swapBuffer(w, bufB) // bufA -> back stack; bufB active

	// The active buffer matches, through a non-normalized spelling.
	if got := e.findOpenBuffer(e.canonicalDocURL("/tmp/../tmp/canon-b.txt")); got != bufB {
		t.Fatal("active buffer should be found via its canonical identity")
	}
	// A stacked buffer matches too.
	if got := e.findOpenBuffer(e.canonicalDocURL("/tmp/canon-a.txt")); got != bufA {
		t.Fatal("a nav-history-stacked buffer is still open and must be found")
	}
	if e.findOpenBuffer(e.canonicalDocURL("/tmp/canon-missing.txt")) != nil {
		t.Fatal("an unopened file must not match")
	}

	// openMainBuffers spans active + stacked (seed, bufA, bufB).
	if got := len(e.openMainBuffers()); got != 3 {
		t.Fatalf("openMainBuffers = %d buffers, want 3", got)
	}
}

// The nav_history commands walk the swap history on the focused window and
// fail cleanly (for chain fallthrough) when there is nothing to walk.
func TestNavHistoryCommands(t *testing.T) {
	e, w, _ := renderedEditorWithConfig(t, "one\ntwo\n", "[options]\n")
	orig := w.Buffer

	if e.navHistory(-1) || e.navHistory(+1) {
		t.Fatal("no history yet: both directions must fail")
	}

	w.SetCursorPos(window.Position{Line: 1, Rune: 2})
	bufB, err := buffer.NewFromBytes([]byte("dest\n"), "/tmp/canon-nav.txt")
	if err != nil {
		t.Fatal(err)
	}
	e.swapBuffer(w, bufB)

	if !e.navHistory(-1) {
		t.Fatal("prior should succeed after a swap")
	}
	if w.Buffer != orig {
		t.Fatal("prior must restore the original buffer")
	}
	if got := w.CursorPos(); got.Line != 1 || got.Rune != 2 {
		t.Fatalf("prior must restore the caret; got %+v", got)
	}
	if !e.navHistory(+1) {
		t.Fatal("next should re-advance")
	}
	if w.Buffer != bufB {
		t.Fatal("next must re-bind the destination")
	}
	if e.navHistory(+1) {
		t.Fatal("no further forward history: next must fail")
	}
}
