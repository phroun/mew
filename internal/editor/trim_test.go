package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

func TestTrimLineBeg(t *testing.T) {
	e, w := newTestEditor(t, " \t  hello \t \nnext\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 6}) // on 'e' of hello
	e.PawScript.ExecuteAsync(`trim_line_beg & verbose_log "did"`)
	if got := w.Buffer.GetLine(0); got != "hello \t \n" {
		t.Fatalf("line: %q", got)
	}
	if w.CursorPos().Rune != 2 {
		t.Fatalf("cursor should follow its character, rune %d", w.CursorPos().Rune)
	}
	if !strings.Contains(verboseLogContent(e), "did") {
		t.Fatal("trim_line_beg should resolve true when it trimmed")
	}
	// Nothing left to trim: resolves false.
	e.PawScript.ExecuteAsync(`trim_line_beg | verbose_log "none"`)
	if !strings.Contains(verboseLogContent(e), "none") {
		t.Fatal("no-op trim should resolve false")
	}
}

func TestTrimLineEndKeepsTerminator(t *testing.T) {
	e, w := newTestEditor(t, "hello \t \nnext\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 8}) // past the whitespace
	e.PawScript.ExecuteAsync("trim_line_end")
	if got := w.Buffer.GetLine(0); got != "hello\n" {
		t.Fatalf("line must keep its terminator: %q", got)
	}
	if got := w.Buffer.GetLine(1); got != "next\n" {
		t.Fatalf("next line disturbed: %q", got)
	}
	if w.CursorPos().Rune != 5 {
		t.Fatalf("cursor should clamp to the trimmed end, rune %d", w.CursorPos().Rune)
	}
}

func TestTrimLineBoth(t *testing.T) {
	e, w := newTestEditor(t, "\t mid \t\nnext\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	e.PawScript.ExecuteAsync(`trim_line & verbose_log "did"`)
	if got := w.Buffer.GetLine(0); got != "mid\n" {
		t.Fatalf("line: %q", got)
	}
	if !strings.Contains(verboseLogContent(e), "did") {
		t.Fatal("trim_line should resolve true when either side trimmed")
	}
}

func TestTrimNeverDeletesLines(t *testing.T) {
	e, w := newTestEditor(t, "   \nnext\n")
	w.SetCursorPos(window.Position{Line: 0, Rune: 0})
	before := w.Buffer.GetLineCount()
	e.PawScript.ExecuteAsync("trim_line")
	if w.Buffer.GetLineCount() != before {
		t.Fatal("trim_line must never delete lines")
	}
	if got := w.Buffer.GetLine(0); got != "\n" {
		t.Fatalf("whitespace-only line should trim to empty: %q", got)
	}
}
