package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// With the caret just past a combining mark, the modebar context names the
// mark backspace would delete — at higher priority than the outline breadcrumb.
func TestMarkContextCombiningAccent(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "éx\n") // e + combining acute + x
	w.SetCursorPos(window.Position{Line: 0, Rune: 2})
	e.performRender()
	if !strings.Contains(w.Context, "combining acute accent") {
		t.Errorf("context should name the mark, got %q", w.Context)
	}
	if !strings.Contains(w.Context, "U+0301") {
		t.Errorf("context should carry the codepoint, got %q", w.Context)
	}
	if !strings.HasPrefix(w.Context, "◌́") {
		t.Errorf("context should show the mark on a dotted-circle placeholder, got %q", w.Context)
	}
}

// Hebrew niqqud: cursoring through a pointed letter names each point.
func TestMarkContextHebrewPoint(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "שָׂx\n")         // shin + sin dot + qamats
	w.SetCursorPos(window.Position{Line: 0, Rune: 2}) // just past the sin dot
	e.performRender()
	if !strings.Contains(w.Context, "hebrew point sin dot") {
		t.Errorf("context should name the sin dot, got %q", w.Context)
	}
	w.SetCursorPos(window.Position{Line: 0, Rune: 3}) // just past the qamats
	e.performRender()
	if !strings.Contains(w.Context, "hebrew point qamats") {
		t.Errorf("context should name the qamats, got %q", w.Context)
	}
}

// An invisible direction control is named too (no placeholder — nothing to
// attach it to).
func TestMarkContextDirectionMark(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "ab‏cd\n") // RLM between ab and cd
	w.SetCursorPos(window.Position{Line: 0, Rune: 3})
	e.performRender()
	if !strings.Contains(w.Context, "right-to-left mark") {
		t.Errorf("context should name the RLM, got %q", w.Context)
	}
	if strings.Contains(w.Context, "◌") {
		t.Errorf("a non-combining control takes no placeholder, got %q", w.Context)
	}
}

// Ordinary positions keep the normal context (spawn placeholder / breadcrumb).
func TestMarkContextOrdinaryRune(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "éx\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 1}) // just past the plain 'e'
	e.performRender()
	if strings.Contains(w.Context, "U+") {
		t.Errorf("backspacing a visible rune needs no mark context, got %q", w.Context)
	}
	if w.Context != w.SpawnContext {
		t.Errorf("context should fall back to the spawn placeholder, got %q", w.Context)
	}
}

// Line start (nothing before the caret) shows no mark context.
func TestMarkContextLineStart(t *testing.T) {
	e, w, _ := newRenderedEditor(t, "́x\n") // degenerate: mark at line start
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.performRender()
	if strings.Contains(w.Context, "U+") {
		t.Errorf("no backspace target at line start, got %q", w.Context)
	}
}
