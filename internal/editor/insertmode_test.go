package editor

import (
	"testing"

	"github.com/phroun/mew/internal/window"
)

// insertMode defaults to yes (insert), and is a per-window boolean stored
// inverted as OverwriteMode so a zero-value window still defaults to insert.
func TestInsertModeOption(t *testing.T) {
	e, w := newTestEditor(t, "x\n")
	if v, _ := e.getOption(w, "insertMode"); v != "yes" {
		t.Fatalf("default insertMode = %q, want yes", v)
	}
	if w.ViewState.OverwriteMode {
		t.Fatal("a fresh window should default to insert mode (OverwriteMode false)")
	}
	e.setOption(w, "insertMode", "no")
	if v, _ := e.getOption(w, "insertMode"); v != "no" {
		t.Fatalf("insertMode after set no = %q, want no", v)
	}
	if !w.ViewState.OverwriteMode {
		t.Fatal("insertMode=no should turn OverwriteMode on")
	}
	// Input aliases still work.
	e.setOption(w, "insertMode", "on")
	if v, _ := e.getOption(w, "insertMode"); v != "yes" {
		t.Fatalf("insertMode=on -> %q, want yes", v)
	}
}

// Overwrite mode replaces the character under the caret; at end of line it
// appends, and a newline still splits the line (insert).
func TestOverwriteTyping(t *testing.T) {
	type step struct {
		rune_   int
		typed   string
		want    string // buffer content (no trailing newline)
		wantCol int
	}
	cases := []struct {
		name  string
		start string
		steps []step
	}{
		{"mid-line replaces", "abcd", []step{{1, "X", "aXcd", 2}}},
		{"multi-rune replaces run", "abcd", []step{{0, "XY", "XYcd", 2}}},
		{"at end of line appends", "abcd", []step{{4, "Z", "abcdZ", 5}}},
		{"past-end run appends", "ab", []step{{2, "XYZ", "abXYZ", 5}}},
		{"replace up to end then append", "ab", []step{{0, "XYZ", "XYZ", 3}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, w := newTestEditor(t, tc.start+"\n")
			w.ViewState.OverwriteMode = true
			for _, s := range tc.steps {
				w.SetCursorPos(window.Position{Line: 0, Rune: s.rune_})
				e.insertText(s.typed)
				if got := docContent(w); got != s.want {
					t.Fatalf("type %q at %d: content %q, want %q", s.typed, s.rune_, got, s.want)
				}
				if got := w.CursorPos().Rune; got != s.wantCol {
					t.Fatalf("type %q at %d: caret rune %d, want %d", s.typed, s.rune_, got, s.wantCol)
				}
			}
		})
	}
}

// A newline typed in overwrite mode splits the line rather than overwriting.
func TestOverwriteNewlineInserts(t *testing.T) {
	e, w := newTestEditor(t, "abcd\n")
	w.ViewState.OverwriteMode = true
	w.SetCursorPos(window.Position{Line: 0, Rune: 1})
	e.insertText("\n")
	if l0, l1 := w.Buffer.GetLine(0), w.Buffer.GetLine(1); l0 != "a\n" || l1 != "bcd\n" {
		t.Fatalf("newline in overwrite should split: line0=%q line1=%q", l0, l1)
	}
	if p := w.CursorPos(); p.Line != 1 || p.Rune != 0 {
		t.Fatalf("caret after split = %+v, want line 1 rune 0", p)
	}
}

// Insert mode (the default) is unaffected: typing pushes characters right.
func TestInsertModeStillInserts(t *testing.T) {
	e, w := newTestEditor(t, "abcd\n")
	// OverwriteMode false by default.
	w.SetCursorPos(window.Position{Line: 0, Rune: 1})
	e.insertText("X")
	if got := docContent(w); got != "aXbcd" {
		t.Fatalf("insert mode content %q, want aXbcd", got)
	}
}

// The mode is per-window: overwrite on one window does not leak to another on
// the same editor.
func TestInsertModePerWindow(t *testing.T) {
	e, w := newTestEditor(t, "abcd\n")
	w2 := e.WindowManager.GetWindow("doc")
	_ = w2
	e.setOption(w, "insertMode", "no")
	if !w.ViewState.OverwriteMode {
		t.Fatal("target window should be in overwrite mode")
	}
	// The editor-wide default is untouched.
	if e.Config.OverwriteMode {
		t.Fatal("a per-window override must not change the editor default")
	}
	if v, _ := e.getOption(nil, "insertMode"); v != "yes" {
		t.Fatalf("global insertMode should stay yes, got %q", v)
	}
}
