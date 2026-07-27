package editor

import (
	"testing"
)

// With autoIndent off, insert_newline is exactly insert '\n'.
func TestInsertNewlineWithoutAutoIndent(t *testing.T) {
	e, w := newTestEditor(t, "    foo")
	e.executeCommand("go_line_end")

	e.executeCommand("insert_newline")

	if got := w.Buffer.GetContent(); got != "    foo\n" {
		t.Fatalf("content = %q, want a bare line break", got)
	}
	if pos := w.CursorPos(); pos.Line != 1 || pos.Rune != 0 {
		t.Fatalf("caret %v, want column 0 of the new line", pos)
	}
}

// With autoIndent on, the new line opens with the split line's indent and the
// caret lands after it.
func TestInsertNewlineReplicatesIndent(t *testing.T) {
	e, w := newTestEditor(t, "    foo")
	w.ViewState.AutoIndent = true
	e.executeCommand("go_line_end")

	e.executeCommand("insert_newline")

	if got := w.Buffer.GetContent(); got != "    foo\n    " {
		t.Fatalf("content = %q, want the indent replicated", got)
	}
	if pos := w.CursorPos(); pos.Line != 1 || pos.Rune != 4 {
		t.Fatalf("caret %v, want to sit after the replicated indent", pos)
	}
}

// The indent is replicated VERBATIM: tabs stay tabs, and a mixed run survives
// exactly as it was written (no re-computation against tabSize).
func TestInsertNewlineReplicatesTabsAndMixedIndent(t *testing.T) {
	for _, tc := range []struct{ name, indent string }{
		{"tabs", "\t\t"},
		{"mixed", "\t  \t"},
		{"spaces", "      "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, w := newTestEditor(t, tc.indent+"x")
			w.ViewState.AutoIndent = true
			e.executeCommand("go_line_end")

			e.executeCommand("insert_newline")

			want := tc.indent + "x\n" + tc.indent
			if got := w.Buffer.GetContent(); got != want {
				t.Fatalf("content = %q, want %q", got, want)
			}
		})
	}
}

// Splitting mid-text carries the indent to the moved remainder, lining the two
// halves up under each other.
func TestInsertNewlineSplitsMidLine(t *testing.T) {
	e, w := newTestEditor(t, "    foobar")
	w.ViewState.AutoIndent = true
	e.executeCommand("go_line_start")
	for i := 0; i < 7; i++ { // past "    foo"
		e.executeCommand("go_char_next")
	}

	e.executeCommand("insert_newline")

	if got := w.Buffer.GetContent(); got != "    foo\n    bar" {
		t.Fatalf("content = %q, want the remainder indented to match", got)
	}
}

// Enter at column 0 of an indented line must NOT push it right: the line moves
// down whole, keeping its own indent, and nothing is replicated above it.
func TestInsertNewlineAtColumnZeroDoesNotDoubleIndent(t *testing.T) {
	e, w := newTestEditor(t, "    foo")
	w.ViewState.AutoIndent = true
	e.executeCommand("go_line_start")

	e.executeCommand("insert_newline")

	if got := w.Buffer.GetContent(); got != "\n    foo" {
		t.Fatalf("content = %q, want the line to move down unchanged", got)
	}
}

// From INSIDE the indent, only the part already above the caret repeats, so the
// two halves still line up at the original indent.
func TestInsertNewlineInsideIndent(t *testing.T) {
	e, w := newTestEditor(t, "    foo")
	w.ViewState.AutoIndent = true
	e.executeCommand("go_line_start")
	e.executeCommand("go_char_next")
	e.executeCommand("go_char_next") // column 2, inside the 4-space indent

	e.executeCommand("insert_newline")

	// "  " above, and the moved half keeps its remaining 2 spaces plus the
	// 2 replicated: still 4 columns of indent on the text.
	if got := w.Buffer.GetContent(); got != "  \n    foo" {
		t.Fatalf("content = %q, want the text to stay at its original indent", got)
	}
}

// A blank line replicates nothing, and a whitespace-only line replicates all of
// itself.
func TestInsertNewlineBlankAndWhitespaceOnlyLines(t *testing.T) {
	e, w := newTestEditor(t, "")
	w.ViewState.AutoIndent = true
	e.executeCommand("insert_newline")
	if got := w.Buffer.GetContent(); got != "\n" {
		t.Fatalf("blank line: content = %q, want a bare break", got)
	}

	e2, w2 := newTestEditor(t, "\t\t")
	w2.ViewState.AutoIndent = true
	e2.executeCommand("go_line_end")
	e2.executeCommand("insert_newline")
	if got := w2.Buffer.GetContent(); got != "\t\t\n\t\t" {
		t.Fatalf("whitespace-only line: content = %q", got)
	}
}

// In overwrite mode the break appends as usual and the indent is INSERTED, not
// overwritten — it must never consume the text the break just moved down.
func TestInsertNewlineOverwriteModeInsertsIndent(t *testing.T) {
	e, w := newTestEditor(t, "    foobar")
	w.ViewState.AutoIndent = true
	w.ViewState.OverwriteMode = true
	e.executeCommand("go_line_start")
	for i := 0; i < 7; i++ {
		e.executeCommand("go_char_next")
	}

	e.executeCommand("insert_newline")

	if got := w.Buffer.GetContent(); got != "    foo\n    bar" {
		t.Fatalf("content = %q, want the moved text intact behind the indent", got)
	}
}

// A read-only viewport rejects insert_newline at its source, indent and all.
func TestInsertNewlineReadOnlyRejected(t *testing.T) {
	e, w := newTestEditor(t, "    foo")
	w.ViewState.AutoIndent = true
	e.executeCommand("go_line_end")
	w.ViewState.ReadOnly = true

	e.executeCommand("insert_newline")

	if got := w.Buffer.GetContent(); got != "    foo" {
		t.Fatalf("content = %q, want the buffer untouched", got)
	}
}

// The autoIndent option round-trips through set_option/get_option per viewport,
// and drives insert_newline live.
func TestAutoIndentOption(t *testing.T) {
	e, w := newTestEditor(t, "    foo")
	if v, _ := e.getOption(w, "autoindent"); v != "no" {
		t.Fatalf("autoIndent default = %q, want no", v)
	}
	e.executeCommand(`set_option "autoIndent", "yes"`)
	if v, _ := e.getOption(w, "autoindent"); v != "yes" {
		t.Fatalf("autoIndent after set = %q, want yes", v)
	}
	if !w.ViewState.AutoIndent {
		t.Fatal("set_option should have armed the viewport's AutoIndent")
	}

	e.executeCommand("go_line_end")
	e.executeCommand("insert_newline")
	if got := w.Buffer.GetContent(); got != "    foo\n    " {
		t.Fatalf("content = %q, want the option to drive the indent", got)
	}
}

// The default Enter binding ends in insert_newline, so pressing return in an
// ordinary buffer auto-indents.
func TestReturnKeyAutoIndents(t *testing.T) {
	e, w := newTestEditor(t, "\tfoo")
	w.ViewState.AutoIndent = true
	e.executeCommand("go_line_end")

	e.dispatchKey("return")

	if got := w.Buffer.GetContent(); got != "\tfoo\n\t" {
		t.Fatalf("content = %q, want return to auto-indent", got)
	}
}
