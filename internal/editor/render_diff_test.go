package editor

import (
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// allEscRe strips every escape sequence, leaving only painted glyphs.
var allEscRe = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

func paintedGlyphs(s string) string {
	return strings.TrimSpace(allEscRe.ReplaceAllString(s, ""))
}

// The off-screen buffer means a re-render with no state change sends no painted
// content — only the (unavoidable) cursor placement — while the first frame and
// a screen_refresh both repaint everything.
func TestRenderDiffSuppressesUnchangedFrames(t *testing.T) {
	e, w, out := newRenderedEditor(t, "hello world\nsecond line\n")

	// First frame: full paint.
	e.performRender()
	if !strings.Contains(paintedGlyphs(out.String()), "hello world") {
		t.Fatalf("first frame should paint content: %q", paintedGlyphs(out.String()))
	}

	// Re-render with nothing changed: no glyphs on the wire.
	out.Reset()
	e.performRender()
	if g := paintedGlyphs(out.String()); g != "" {
		t.Errorf("unchanged frame should send no glyphs, got %q", g)
	}

	// Change one line; only that line's cells should be repainted.
	w.SetCursorPos(window.Position{Line: 0, Rune: 11})
	e.executeCommand("insert '!'")
	out.Reset()
	e.performRender()
	g := paintedGlyphs(out.String())
	if !strings.Contains(g, "!") {
		t.Errorf("the edited cell should be repainted: %q", g)
	}
	if strings.Contains(g, "second line") {
		t.Errorf("the untouched line should not be repainted: %q", g)
	}
}

func TestScreenRefreshForcesFullRepaint(t *testing.T) {
	e, _, out := newRenderedEditor(t, "alpha beta\n")
	e.performRender()

	// A quiet frame sends nothing…
	out.Reset()
	e.performRender()
	if g := paintedGlyphs(out.String()); g != "" {
		t.Fatalf("expected a quiet frame, got %q", g)
	}

	// …until screen_refresh forces a clear + full repaint.
	out.Reset()
	e.executeCommand("screen_refresh")
	e.performRender()
	raw := out.String()
	if !strings.Contains(raw, "\x1b[2J") {
		t.Errorf("screen_refresh should clear the screen: %q", raw)
	}
	if !strings.Contains(paintedGlyphs(raw), "alpha beta") {
		t.Errorf("screen_refresh should repaint content: %q", paintedGlyphs(raw))
	}
}
