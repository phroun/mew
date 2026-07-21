package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/window"
)

// --- Occurrence counting (nnn option) ---

func TestFindNthOccurrence(t *testing.T) {
	e, w := newTestEditor(t, "- x 1 x 2 x 3\n")
	// Matches start strictly after the cursor: x at cols 2, 6, 10.
	e.startFind("x", "3", "", true, true, false)
	if w.CursorPos().Rune != 10 {
		t.Fatalf("3rd occurrence should be col 10, got %v", w.CursorPos())
	}
	// Counting never wraps: the 9th occurrence does not exist.
	w.SetCursorPos(window.Position{})
	e.startFind("x", "9", "", true, true, false)
	if w.CursorPos().Rune != 0 {
		t.Fatalf("9th occurrence should not be found, cursor moved to %v", w.CursorPos())
	}
}

func TestFindNthAcrossLines(t *testing.T) {
	content := "# mew Editor Configuration File\n" +
		"# This file contains settings and key mappings for the mew text editor\n" +
		"other\n" +
		"mappings=mew\n"
	want := []window.Position{{Line: 0, Rune: 2}, {Line: 1, Rune: 55}, {Line: 3, Rune: 9}}
	for n := 1; n <= 3; n++ {
		e, w := newTestEditor(t, content)
		w.SetCursorPos(window.Position{})
		e.startFind("mew", string(rune('0'+n)), "", true, true, false)
		if w.CursorPos() != want[n-1] {
			t.Errorf("count=%d: cursor %v, want %v", n, w.CursorPos(), want[n-1])
		}
	}
}

// --- Regex syntax (x and y options, searchRegex default) ---

func TestFindJoeSyntaxDefaultIsLiteral(t *testing.T) {
	e, w := newTestEditor(t, "zz axb then a.b\n")
	// Default JOE syntax: an unescaped dot is literal (skips axb at col 3).
	e.startFind("a.b", "", "", true, true, false)
	if w.CursorPos().Rune != 12 {
		t.Fatalf("unescaped dot should match literally at col 12, got %v", w.CursorPos())
	}
	// Escaped \. is the any-character operator: matches axb first.
	w.SetCursorPos(window.Position{})
	e.startFind(`a\.b`, "", "", true, true, false)
	if w.CursorPos().Rune != 3 {
		t.Fatalf(`\. should match axb at col 3, got %v`, w.CursorPos())
	}
}

func TestFindStandardSyntaxOption(t *testing.T) {
	e, w := newTestEditor(t, " bat bit but\n")
	e.startFind("b.t", "x", "", true, true, false)
	if w.CursorPos().Rune != 1 {
		t.Fatalf("standard regex should match bat, got %v", w.CursorPos())
	}
	e.PawScript.ExecuteAsync("find_next")
	if w.CursorPos().Rune != 5 {
		t.Fatalf("find_next should reach bit, got %v", w.CursorPos())
	}

	// searchRegex config default, and y overriding it back to JOE literal.
	e.Config.SearchRegex = true
	w.SetCursorPos(window.Position{})
	e.startFind("b.t", "", "", true, true, false)
	if w.CursorPos().Rune != 1 {
		t.Fatalf("searchRegex default should regex-match bat, got %v", w.CursorPos())
	}
	w.SetCursorPos(window.Position{})
	e.startFind("b.t", "y", "", true, true, false)
	if w.CursorPos().Rune != 0 {
		t.Fatalf("y should force literal (no match, cursor unmoved): %v", w.CursorPos())
	}
}

// --- Replacement escapes ---

func TestFindReplacementEscapes(t *testing.T) {
	e, w := newTestEditor(t, "john@example tom@site\n")
	e.startFind(`(\w+)@(\w+)`, "x", `\2:\u\1`, true, true, true)
	answerPrompt(t, e, "a")
	if got := docContent(w); got != "example:John site:Tom" {
		t.Fatalf("group/case escapes: %q", got)
	}
}

func TestFindWholeMatchEscape(t *testing.T) {
	e, w := newTestEditor(t, "abc\n")
	e.startFind("abc", "", `[\&]`, true, true, true)
	answerPrompt(t, e, "y")
	if got := docContent(w); got != "[abc]" {
		t.Fatalf(`\& escape: %q`, got)
	}
}

// --- Replace counting ---

func TestFindCountReplaceNoPrompt(t *testing.T) {
	e, w := newTestEditor(t, "z z z z z\n")
	// A count with r performs exactly N replacements without prompting.
	e.startFind("z", "2r", "Q", true, true, true)
	if focusedPrompt(e) != nil {
		t.Fatal("count+r must not prompt per match")
	}
	if got := docContent(w); got != "Q Q z z z" {
		t.Fatalf("count-limited replace: %q", got)
	}
}

func TestFindCountReplaceFewerMatches(t *testing.T) {
	e, w := newTestEditor(t, "z z\n")
	e.startFind("z", "9r", "Q", true, true, true)
	if got := docContent(w); got != "Q Q" {
		t.Fatalf("count larger than matches: %q", got)
	}
}

// --- Config toggles ---

func TestFindSearchWrapAndIgnoreCaseConfig(t *testing.T) {
	e, w := newTestEditor(t, "alpha\nBETA\n")
	// searchWrap=false: no match behind the cursor.
	e.Config.SearchWrap = false
	w.SetCursorPos(window.Position{Line: 1, Rune: 0})
	e.startFind("alpha", "", "", true, true, false)
	if w.CursorPos().Line != 1 {
		t.Fatalf("wrap disabled: cursor should not move, got %v", w.CursorPos())
	}
	e.Config.SearchWrap = true
	e.startFind("alpha", "", "", true, true, false)
	if w.CursorPos().Line != 0 {
		t.Fatalf("wrap enabled: should find alpha, got %v", w.CursorPos())
	}

	// searchIgnoreCase default applies without the i letter.
	e.Config.SearchIgnoreCase = true
	w.SetCursorPos(window.Position{})
	e.startFind("beta", "", "", true, true, false)
	if w.CursorPos().Line != 1 {
		t.Fatalf("icase default should match BETA, got %v", w.CursorPos())
	}
}

func TestFindSetOptionSearchToggles(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	e.PawScript.ExecuteAsync(`set_option searchRegex, true`)
	if !e.Config.SearchRegex {
		t.Fatal("set_option searchRegex failed")
	}
	e.PawScript.ExecuteAsync(`set_option searchWrap, false`)
	if e.Config.SearchWrap {
		t.Fatal("set_option searchWrap failed")
	}
	e.PawScript.ExecuteAsync(`set_option searchIgnoreCase, true`)
	if !e.Config.SearchIgnoreCase {
		t.Fatal("set_option searchIgnoreCase failed")
	}
}

// --- Verbose log (v option and verbose_log command) ---

func TestFindVerboseLogWindow(t *testing.T) {
	e, w := newTestEditor(t, "needle in haystack\n")
	e.startFind("needle", "v", "", true, true, false)

	vw := windowByClass(e, "verboseLog")
	if vw == nil {
		t.Fatal("verbose log window should exist")
	}
	// Unfocused, and it must not steal the painted main area or modebar.
	if e.WindowManager.GetFocusedWindow().ID == vw.ID {
		t.Fatal("verbose log must not take focus")
	}
	if e.WindowManager.GetLastNormalWindow().ID != w.ID {
		t.Fatal("verbose log must not steal the painted main area")
	}
	if e.WindowManager.GetLastMainWindow().ID != w.ID {
		t.Fatal("verbose log must not become the last main buffer")
	}
	if !strings.Contains(vw.Buffer.GetContent(), `term="needle"`) {
		t.Fatalf("log content: %q", vw.Buffer.GetContent())
	}

	// A second verbose search appends to the SAME window.
	before := vw.Buffer.GetLineCount()
	e.startFind("haystack", "v", "", true, true, false)
	count := 0
	for _, win := range e.WindowManager.AllWindows() {
		if win.Class == "verboseLog" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("verbose log window should be reused, found %d", count)
	}
	if vw.Buffer.GetLineCount() <= before {
		t.Fatal("second search should append to the log")
	}
}

func TestVerboseLogCommand(t *testing.T) {
	e, _ := newTestEditor(t, "doc\n")
	e.PawScript.ExecuteAsync(`verbose_log "hello log", "second line"`)
	got := verboseLogContent(e)
	if !strings.Contains(got, "hello log") || !strings.Contains(got, "second line") {
		t.Fatalf("log content: %q", got)
	}
	if vw := windowByClass(e, "verboseLog"); e.WindowManager.GetFocusedWindow().ID == vw.ID {
		t.Fatal("verbose_log must not take focus")
	}
}

// --- Interactive flow and prompt ergonomics ---

func TestFindInteractiveFlow(t *testing.T) {
	e, w := newTestEditor(t, "- x 1 x 2 x 3\n")
	w.SetCursorPos(window.Position{})
	e.PawScript.ExecuteAsync("find") // fully bare: term prompt, then options
	answerPrompt(t, e, "x")
	answerPrompt(t, e, "2")
	if w.CursorPos().Rune != 6 {
		t.Fatalf("interactive find x,2: cursor %v, want col 6", w.CursorPos())
	}
}

func TestFindBlankAcceptRepeatsPrevious(t *testing.T) {
	e, w := newTestEditor(t, "aa bb aa\n")
	e.startFind("aa", "", "", true, true, false)
	if w.CursorPos().Rune != 6 {
		// From col 0, strictly-after finds the second aa? No: first match
		// after col 0 is... "aa" at col 0 is not strictly after; col 6 is
		// wrong too — recompute: matches at 0 and 6; after cursor col 0 the
		// first allowed start is col 1, so col 6 matches.
		t.Fatalf("first search: cursor %v", w.CursorPos())
	}
	// Blank-accept at the term prompt repeats the previous term. With wrap
	// off, a backwards search from the start finds nothing.
	e.Config.SearchWrap = false
	w.SetCursorPos(window.Position{})
	e.PawScript.ExecuteAsync("find")
	answerPrompt(t, e, "")  // blank term = repeat "aa"
	answerPrompt(t, e, "b") // backwards: nothing behind the start
	if w.CursorPos().Rune != 0 {
		t.Fatalf("backwards from start should not move, got %v", w.CursorPos())
	}
	// Forward blank repeat finds the col-6 occurrence again.
	e.Config.SearchWrap = true
	e.PawScript.ExecuteAsync("find")
	answerPrompt(t, e, "")
	answerPrompt(t, e, "")
	if w.CursorPos().Rune != 6 {
		t.Fatalf("blank repeat should find col 6, got %v", w.CursorPos())
	}
}

func TestFindOptionsHistoryNotDefaulted(t *testing.T) {
	e, _ := newTestEditor(t, "l1\nl2\nl3\n")
	e.PawScript.ExecuteAsync("find")
	answerPrompt(t, e, "l2")
	answerPrompt(t, e, "i")
	// Second find: options prompt starts blank with "i" reachable in history.
	e.PawScript.ExecuteAsync("find")
	answerPrompt(t, e, "l3")
	fw := focusedPrompt(e)
	if fw == nil {
		t.Fatal("expected options prompt")
	}
	cursorLine := strings.TrimRight(fw.Buffer.GetLine(fw.CursorPos().Line), "\n\r")
	if cursorLine != "" {
		t.Fatalf("options prompt must start blank, got %q", cursorLine)
	}
	if !strings.Contains(fw.Buffer.GetContent(), "i") {
		t.Fatalf("options history missing: %q", fw.Buffer.GetContent())
	}
	cancelPrompt(t, e)
}

// --- Replace-loop interaction ---

func TestFindReplaceInteractive(t *testing.T) {
	e, w := newTestEditor(t, "cat dog cat\n")
	w.SetCursorPos(window.Position{})
	e.startFind("cat", "r", "pet", true, true, true)
	// First match offered at col 0 (replace scans from cursor inclusive).
	answerPrompt(t, e, "y")
	// Second match offered; skip it.
	answerPrompt(t, e, "n")
	if got := docContent(w); got != "pet dog cat" {
		t.Fatalf("y then n: %q", got)
	}
	// The match highlight must be cleared after the loop ends.
	if w.MatchHighlight {
		t.Fatal("match highlight should clear when the loop ends")
	}
}
