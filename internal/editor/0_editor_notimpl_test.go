package editor

import "testing"

// not_implemented warns, names what was asked for, and reports false so a chain
// falls through it exactly as an unhandled command would.
func TestNotImplementedWarnsAndFails(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	e.executeCommand(`not_implemented "Justify Block"`)
	if !hasWarning(e, "Justify Block: not yet implemented") {
		t.Error("expected a warning naming what was asked for")
	}

	e2, _ := newTestEditor(t, "x\n")
	e2.executeCommand("not_implemented")
	if !hasWarning(e2, "Not yet implemented") {
		t.Error("expected the bare warning with no name")
	}
}

// All of them share one tag, so trying several planned items in a row leaves a
// single toast rather than a stack of identical ones.
func TestNotImplementedToastsReplaceEachOther(t *testing.T) {
	e, _ := newTestEditor(t, "x\n")
	count := func() int {
		n := 0
		for _, w := range e.ViewportManager.AllViewports() {
			if w.Class == "warning" {
				n++
			}
		}
		return n
	}
	e.executeCommand(`not_implemented "Justify Block"`)
	first := count()
	e.executeCommand(`not_implemented "Convert to Title Case"`)
	e.executeCommand(`not_implemented "Swap camelCase snake_case"`)
	if got := count(); got != first {
		t.Errorf("%d warning transients after three placeholders, want %d — they share a tag and must replace", got, first)
	}
	if !hasWarning(e, "Swap camelCase snake_case: not yet implemented") {
		t.Error("the surviving toast should be the most recent one")
	}
}
